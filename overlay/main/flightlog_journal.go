package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	flightJournalEntriesPerSegment = 60
	flightArchivedJournalsDir      = "archived-journals"
	flightRecoveryResumeWindow     = 10 * time.Minute
)

type flightRecoveryOutcome int

const (
	flightRecoveryInAir flightRecoveryOutcome = iota
	flightRecoveryLanded
	flightRecoveryGround
)

type flightRecoveryAnalysis struct {
	outcome    flightRecoveryOutcome
	landing    flightTrackPoint
	onBlockUTC string
	lastSample flightLiveGPS
}

type flightJournalEntry struct {
	Kind       string                 `json:"kind"`
	FlightID   string                 `json:"flight_id"`
	RecordedAt string                 `json:"recorded_at"`
	State      *flightRecord          `json:"state,omitempty"`
	Point      *flightTrackPoint      `json:"point,omitempty"`
	Context    *flightContextSnapshot `json:"context,omitempty"`
	Weather    *flightWeatherMemory   `json:"weather,omitempty"`
}

// Weather already written to the open journal: text reports are appended as
// deltas, radar rollups and counters are rewritten only when the total changes.
var (
	flightJournalWeatherTexts    int
	flightJournalWeatherMessages int
)

var (
	flightJournalRoot           string
	flightJournalSegment        int
	flightJournalSegmentEntries int
	flightJournalRecovered      bool
	flightRecoveredAt           time.Time
	flightStorageError          string
)

func initializeFlightJournalLocked() {
	flightJournalRoot = filepath.Join(flightStorageDir, "journals")
	if err := os.MkdirAll(flightJournalRoot, 0755); err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Flight storage is not writable: %v", err))
		return
	}
	if legacy := flightCurrent; legacy != nil {
		flightCurrent = nil
		flightLastPosition = nil
		if !flightHistoryContains(legacy.ID) && !flightJournalKnownLocked(legacy.ID) {
			adoptLegacyFlightIntoJournalLocked(legacy)
		}
	}
	recoverFlightJournalsLocked()
}

// Every unfinished journal is resolved from its own recorded points. Only the
// newest one may stay open, so a restart in flight can resume once GPS returns.
func recoverFlightJournalsLocked() {
	directories, err := os.ReadDir(flightJournalRoot)
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot read flight journals: %v", err))
		return
	}
	sort.Slice(directories, func(i, j int) bool { return directories[i].Name() < directories[j].Name() })
	type unfinishedJournal struct {
		record      *flightRecord
		nextSegment int
	}
	var unfinished []unfinishedJournal
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		record, ended, nextSegment := readFlightJournalLocked(filepath.Join(flightJournalRoot, directory.Name()))
		if record == nil {
			continue
		}
		if ended {
			restoreCompletedFlightFromJournalLocked(record)
			continue
		}
		if flightHistoryContains(record.ID) {
			continue
		}
		unfinished = append(unfinished, unfinishedJournal{record: record, nextSegment: nextSegment})
	}
	for i, journal := range unfinished {
		flightCurrent = journal.record
		flightJournalSegment = journal.nextSegment
		flightJournalSegmentEntries = 0
		markFlightJournalWeatherLocked(journal.record.Weather)
		flightJournalRecovered = true
		flightRecoveredAt = time.Now()
		if len(journal.record.Track) > 0 {
			last := journal.record.Track[len(journal.record.Track)-1]
			flightLastPosition = &last
		}
		if i == len(unfinished)-1 && analyzeRecoveredFlightLocked(journal.record).outcome != flightRecoveryLanded {
			log.Printf("flight log: unfinished flight %s recovered; waiting for GPS before closing it", journal.record.ID)
			return
		}
		if !closeRecoveredFlightLocked() {
			return
		}
	}
}

func analyzeRecoveredFlightLocked(record *flightRecord) flightRecoveryAnalysis {
	if record == nil || record.TakeoffUTC == "" || len(record.Track) == 0 {
		return flightRecoveryAnalysis{outcome: flightRecoveryGround}
	}
	track := record.Track
	last := track[len(track)-1]
	analysis := flightRecoveryAnalysis{
		outcome:    flightRecoveryInAir,
		onBlockUTC: last.TimeUTC,
		lastSample: flightLiveGPS{
			Valid: true, TimeUTC: last.TimeUTC, Latitude: last.Latitude, Longitude: last.Longitude,
			AltitudeFt: last.AltitudeFt, GroundSpeedKt: last.GroundSpeedKt, CourseDeg: last.CourseDeg,
			VerticalSpeedFps: last.VerticalSpeedFps,
		},
	}
	for i := len(track) - 1; i >= 0 && track[i].GroundSpeedKt <= 2; i-- {
		analysis.onBlockUTC = track[i].TimeUTC
	}
	if record.LandingUTC != "" {
		analysis.outcome = flightRecoveryLanded
		analysis.landing = last
		landingTime := parseFlightTime(record.LandingUTC)
		for _, point := range track {
			if !parseFlightTime(point.TimeUTC).Before(landingTime) {
				analysis.landing = point
				break
			}
		}
	} else {
		start := len(track)
		for start > 0 && flightLandingEvidenceLocked(track[start-1]) {
			start--
		}
		if start < len(track) && parseFlightTime(last.TimeUTC).Sub(parseFlightTime(track[start].TimeUTC)) >= flightLandingConfirmDuration {
			analysis.outcome = flightRecoveryLanded
			analysis.landing = track[start]
		}
	}
	if analysis.outcome == flightRecoveryLanded && parseFlightTime(analysis.onBlockUTC).Before(parseFlightTime(analysis.landing.TimeUTC)) {
		analysis.onBlockUTC = analysis.landing.TimeUTC
	}
	return analysis
}

// Closes the recovered flightCurrent using only times recorded in its journal.
func closeRecoveredFlightLocked() bool {
	record := flightCurrent
	if record == nil {
		flightJournalRecovered = false
		return true
	}
	analysis := analyzeRecoveredFlightLocked(record)
	switch analysis.outcome {
	case flightRecoveryGround:
		log.Printf("flight log: archived ground-only journal %s (no takeoff recorded)", record.ID)
		stopFlightWeatherCapture(record.ID)
		archiveFlightJournalLocked(record.ID)
		_ = os.Remove(flightCurrentPath)
		flightCurrent = nil
		flightLastPosition = nil
		return true
	case flightRecoveryLanded:
		if record.LandingUTC == "" {
			record.LandingUTC = analysis.landing.TimeUTC
		}
		if record.ArrivalAirport == nil {
			record.ArrivalAirport = nearestAirportLocked(analysis.landing.Latitude, analysis.landing.Longitude, analysis.landing.AltitudeFt, 8.0)
		}
		record.Incomplete = false
		record.EndReason = "recovered after power loss"
	default:
		record.Incomplete = true
		record.EndReason = "power lost before landing was recorded"
	}
	log.Printf("flight log: closing recovered flight %s (%s) at %s", record.ID, record.EndReason, analysis.onBlockUTC)
	return finishFlightLocked(parseFlightTime(analysis.onBlockUTC), analysis.lastSample)
}

func recoveredFlightCanResume(sample flightLiveGPS) bool {
	if flightCurrent == nil || len(flightCurrent.Track) == 0 {
		return false
	}
	last := flightCurrent.Track[len(flightCurrent.Track)-1]
	lastTime := parseFlightTime(last.TimeUTC)
	sampleTime := parseFlightTime(sample.TimeUTC)
	if lastTime.IsZero() || sampleTime.IsZero() {
		return false
	}
	gap := sampleTime.Sub(lastTime)
	if gap < 0 || gap > flightRecoveryResumeWindow {
		return false
	}
	switch flightCurrent.Phase {
	case flightPhaseAirborne:
		return sample.GroundSpeedKt > 35
	case flightPhaseTaxiOut:
		return true
	}
	return false
}

func setFlightStorageErrorLocked(message string) {
	if message == "" || message == flightStorageError {
		return
	}
	flightStorageError = message
	log.Printf("flight log storage: %s", message)
}

func flightJournalDirectory(flightID string) string {
	return filepath.Join(flightJournalRoot, flightID)
}

func flightJournalState(rec *flightRecord, includeTrack bool) *flightRecord {
	if rec == nil {
		return nil
	}
	copyRecord := *rec
	if !includeTrack {
		copyRecord.Track = nil
	}
	copyRecord.Context = nil
	copyRecord.Weather = nil
	return &copyRecord
}

func beginFlightJournalLocked(rec *flightRecord) {
	flightJournalSegment = 1
	flightJournalSegmentEntries = 0
	markFlightJournalWeatherLocked(nil)
	flightJournalRecovered = false
	if rec == nil {
		return
	}
	appendFlightJournalLocked(flightJournalEntry{
		Kind:       "state",
		FlightID:   rec.ID,
		RecordedAt: rec.LastUpdateUTC,
		State:      flightJournalState(rec, false),
	})
	// Seal the start record in its own immutable segment. Even if power is
	// removed while a later track segment is being extended, the flight itself
	// remains discoverable and recoverable.
	flightJournalSegment++
	flightJournalSegmentEntries = 0
	if len(rec.Track) > 0 {
		point := rec.Track[len(rec.Track)-1]
		appendFlightJournalPointLocked(point)
	}
}

func adoptLegacyFlightIntoJournalLocked(rec *flightRecord) {
	if rec == nil {
		return
	}
	flightJournalSegment = 1
	flightJournalSegmentEntries = 0
	appendFlightJournalLocked(flightJournalEntry{
		Kind:       "state",
		FlightID:   rec.ID,
		RecordedAt: rec.LastUpdateUTC,
		State:      flightJournalState(rec, true),
	})
	for i := range rec.Context {
		snapshot := rec.Context[i]
		appendFlightJournalLocked(flightJournalEntry{Kind: "context", FlightID: rec.ID, RecordedAt: snapshot.TimeUTC, Context: &snapshot})
	}
	if rec.Weather != nil {
		appendFlightJournalLocked(flightJournalEntry{Kind: "weather", FlightID: rec.ID, RecordedAt: rec.LastUpdateUTC, Weather: rec.Weather})
	}
	flightJournalSegment++
	flightJournalSegmentEntries = 0
}

func checkpointFlightJournalLocked() {
	if flightCurrent == nil {
		return
	}
	appendFlightJournalLocked(flightJournalEntry{
		Kind:       "state",
		FlightID:   flightCurrent.ID,
		RecordedAt: flightCurrent.LastUpdateUTC,
		State:      flightJournalState(flightCurrent, false),
	})
}

func finishFlightJournalLocked(rec *flightRecord) {
	if rec == nil {
		return
	}
	appendFlightJournalLocked(flightJournalEntry{
		Kind:       "end",
		FlightID:   rec.ID,
		RecordedAt: rec.LastUpdateUTC,
		State:      flightJournalState(rec, false),
	})
	flightJournalSegment = 0
	flightJournalSegmentEntries = 0
	markFlightJournalWeatherLocked(nil)
	flightJournalRecovered = false
}

// Journals are never deleted; discarded or ground-only ones are set aside.
func archiveFlightJournalLocked(flightID string) {
	if flightID != "" && flightJournalRoot != "" {
		archiveRoot := filepath.Join(flightStorageDir, flightArchivedJournalsDir)
		target := filepath.Join(archiveRoot, flightID)
		if _, err := os.Stat(target); err == nil {
			target += "." + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		err := os.MkdirAll(archiveRoot, 0755)
		if err == nil {
			err = os.Rename(flightJournalDirectory(flightID), target)
		}
		if err != nil && !os.IsNotExist(err) {
			setFlightStorageErrorLocked(fmt.Sprintf("Cannot archive flight journal: %v", err))
		}
		syncFlightDirectory(flightJournalRoot)
		syncFlightDirectory(archiveRoot)
	}
	flightJournalSegment = 0
	flightJournalSegmentEntries = 0
	markFlightJournalWeatherLocked(nil)
	flightJournalRecovered = false
}

func appendFlightJournalPointLocked(point flightTrackPoint) {
	if flightCurrent == nil {
		return
	}
	pointCopy := point
	appendFlightJournalLocked(flightJournalEntry{
		Kind:       "point",
		FlightID:   flightCurrent.ID,
		RecordedAt: point.TimeUTC,
		Point:      &pointCopy,
	})
}

func appendFlightJournalContextLocked(snapshot flightContextSnapshot) {
	if flightCurrent == nil {
		return
	}
	appendFlightJournalLocked(flightJournalEntry{
		Kind:       "context",
		FlightID:   flightCurrent.ID,
		RecordedAt: snapshot.TimeUTC,
		Context:    &snapshot,
	})
}

func appendFlightJournalWeatherLocked(weather *flightWeatherMemory) {
	if flightCurrent == nil || weather == nil {
		return
	}
	if len(weather.TextReports) < flightJournalWeatherTexts {
		flightJournalWeatherTexts = 0
	}
	messages := flightWeatherMessageTotal(weather)
	if len(weather.TextReports) == flightJournalWeatherTexts && messages == flightJournalWeatherMessages {
		return
	}
	delta := *weather
	delta.TextReports = weather.TextReports[flightJournalWeatherTexts:]
	if appendFlightJournalLocked(flightJournalEntry{
		Kind:       "weather",
		FlightID:   flightCurrent.ID,
		RecordedAt: flightCurrent.LastUpdateUTC,
		Weather:    &delta,
	}) {
		markFlightJournalWeatherLocked(weather)
	}
}

func markFlightJournalWeatherLocked(weather *flightWeatherMemory) {
	flightJournalWeatherTexts = 0
	flightJournalWeatherMessages = 0
	if weather != nil {
		flightJournalWeatherTexts = len(weather.TextReports)
		flightJournalWeatherMessages = flightWeatherMessageTotal(weather)
	}
}

func flightWeatherMessageTotal(weather *flightWeatherMemory) int {
	total := 0
	for _, count := range weather.Summary {
		total += count
	}
	return total
}

func appendFlightJournalLocked(entry flightJournalEntry) bool {
	if flightJournalRoot == "" || entry.FlightID == "" {
		return false
	}
	if flightJournalSegment < 1 {
		flightJournalSegment = 1
	}
	if flightJournalSegmentEntries >= flightJournalEntriesPerSegment {
		flightJournalSegment++
		flightJournalSegmentEntries = 0
	}
	dir := flightJournalDirectory(entry.FlightID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot create flight journal: %v", err))
		return false
	}
	body, err := json.Marshal(entry)
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot encode flight journal: %v", err))
		return false
	}
	path := filepath.Join(dir, fmt.Sprintf("%06d.ndjson", flightJournalSegment))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot open flight journal: %v", err))
		return false
	}
	if _, err = file.Write(append(body, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot write flight journal: %v", err))
		return false
	}
	flightJournalSegmentEntries++
	return true
}

func flightHistoryContains(flightID string) bool {
	for i := range flightHistory {
		if flightHistory[i].ID == flightID {
			return true
		}
	}
	return false
}

func restoreCompletedFlightFromJournalLocked(record *flightRecord) {
	if record == nil || record.ID == "" {
		return
	}
	completedPath := filepath.Join(flightFilesDir, record.ID+".json")
	body, readErr := os.ReadFile(completedPath)
	var stored flightRecord
	validCompletedFile := readErr == nil && json.Unmarshal(body, &stored) == nil && stored.ID == record.ID && len(stored.Track) > 0
	if !validCompletedFile {
		if err := writeFlightJSONAtomic(completedPath, record); err != nil {
			setFlightStorageErrorLocked(fmt.Sprintf("Cannot restore completed flight %s: %v", record.ID, err))
			return
		}
	}
	if flightHistoryContains(record.ID) {
		return
	}
	flightHistory = append(flightHistory, summarizeFlight(*record))
	sort.SliceStable(flightHistory, func(i, j int) bool { return flightHistory[i].ID > flightHistory[j].ID })
	if err := saveFlightIndexLocked(); err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot rebuild flight history: %v", err))
	}
}

func readFlightJournalLocked(directory string) (*flightRecord, bool, int) {
	files, err := filepath.Glob(filepath.Join(directory, "*.ndjson"))
	if err != nil || len(files) == 0 {
		return nil, false, 1
	}
	sort.Strings(files)
	var record *flightRecord
	var contexts []flightContextSnapshot
	var weather *flightWeatherMemory
	points := make([]flightTrackPoint, 0, len(files)*flightJournalEntriesPerSegment)
	ended := false
	maxSegment := 0
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if segment, parseErr := strconv.Atoi(name); parseErr == nil && segment > maxSegment {
			maxSegment = segment
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			setFlightStorageErrorLocked(fmt.Sprintf("Cannot read flight journal segment %s: %v", filepath.Base(path), openErr))
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			var entry flightJournalEntry
			if json.Unmarshal(scanner.Bytes(), &entry) != nil {
				continue
			}
			if entry.State != nil {
				if len(entry.State.Track) > 0 {
					points = append(points, entry.State.Track...)
				}
				state := *entry.State
				state.Track = nil
				record = &state
			}
			if entry.Point != nil {
				points = append(points, *entry.Point)
			}
			if entry.Context != nil {
				contexts = append(contexts, *entry.Context)
			}
			if entry.Weather != nil {
				if weather == nil {
					weather = &flightWeatherMemory{}
				}
				weather.CaptureVersion = entry.Weather.CaptureVersion
				weather.TextReports = append(weather.TextReports, entry.Weather.TextReports...)
				weather.NEXRADSnapshots = entry.Weather.NEXRADSnapshots
				weather.Summary = entry.Weather.Summary
			}
			if entry.Kind == "end" {
				ended = true
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			setFlightStorageErrorLocked(fmt.Sprintf("Damaged flight journal segment %s: %v", filepath.Base(path), scanErr))
		}
		_ = file.Close()
	}
	if record == nil {
		return nil, ended, maxSegment + 1
	}
	if len(contexts) > flightContextMaximum {
		contexts = contexts[len(contexts)-flightContextMaximum:]
	}
	record.Context = contexts
	record.Weather = weather
	record.Track = deduplicateJournalPoints(points)
	recalculateRecoveredFlight(record)
	return record, ended, maxSegment + 1
}

func deduplicateJournalPoints(points []flightTrackPoint) []flightTrackPoint {
	if len(points) < 2 {
		return points
	}
	result := make([]flightTrackPoint, 0, len(points))
	var previous flightTrackPoint
	for _, point := range points {
		if len(result) > 0 && point.TimeUTC == previous.TimeUTC && point.Latitude == previous.Latitude && point.Longitude == previous.Longitude {
			continue
		}
		result = append(result, point)
		previous = point
	}
	return result
}

func recalculateRecoveredFlight(record *flightRecord) {
	if record == nil || len(record.Track) == 0 {
		return
	}
	record.DistanceNM = 0
	record.MaxGroundSpeedKt = 0
	record.MaxAltitudeFt = record.Track[0].AltitudeFt
	for i, point := range record.Track {
		if point.GroundSpeedKt > record.MaxGroundSpeedKt {
			record.MaxGroundSpeedKt = point.GroundSpeedKt
		}
		if point.AltitudeFt > record.MaxAltitudeFt {
			record.MaxAltitudeFt = point.AltitudeFt
		}
		if i > 0 {
			distance := flightDistanceNM(record.Track[i-1].Latitude, record.Track[i-1].Longitude, point.Latitude, point.Longitude)
			if distance >= 0 && distance <= 2 {
				record.DistanceNM += distance
			}
		}
	}
	last := record.Track[len(record.Track)-1]
	record.LastUpdateUTC = last.TimeUTC
	updateFlightDurationsLocked(record, parseFlightTime(last.TimeUTC))
}
