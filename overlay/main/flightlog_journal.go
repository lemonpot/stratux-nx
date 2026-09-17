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

const flightJournalEntriesPerSegment = 60

type flightJournalEntry struct {
	Kind       string            `json:"kind"`
	FlightID   string            `json:"flight_id"`
	RecordedAt string            `json:"recorded_at"`
	State      *flightRecord     `json:"state,omitempty"`
	Point      *flightTrackPoint `json:"point,omitempty"`
}

var (
	flightJournalRoot           string
	flightJournalSegment        int
	flightJournalSegmentEntries int
	flightJournalRecovered      bool
	flightStorageError          string
)

func initializeFlightJournalLocked() {
	flightJournalRoot = filepath.Join(flightStorageDir, "journals")
	if err := os.MkdirAll(flightJournalRoot, 0755); err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Flight storage is not writable: %v", err))
		return
	}
	recovered := recoverLatestFlightJournalLocked()
	if recovered != nil {
		if flightCurrent == nil || parseFlightTime(recovered.LastUpdateUTC).After(parseFlightTime(flightCurrent.LastUpdateUTC)) {
			flightCurrent = recovered
			if len(recovered.Track) > 0 {
				last := recovered.Track[len(recovered.Track)-1]
				flightLastPosition = &last
			}
			flightJournalRecovered = true
		} else {
			flightJournalSegment = 0
			flightJournalSegmentEntries = 0
		}
	}
	if flightCurrent != nil && flightJournalSegment == 0 {
		adoptLegacyFlightIntoJournalLocked(flightCurrent)
	}
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
	flightJournalRecovered = false
}

func discardFlightJournalLocked(flightID string) {
	if flightID != "" && flightJournalRoot != "" {
		if err := os.RemoveAll(flightJournalDirectory(flightID)); err != nil {
			setFlightStorageErrorLocked(fmt.Sprintf("Cannot discard flight journal: %v", err))
		}
	}
	flightJournalSegment = 0
	flightJournalSegmentEntries = 0
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

func appendFlightJournalLocked(entry flightJournalEntry) {
	if flightJournalRoot == "" || entry.FlightID == "" {
		return
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
		return
	}
	body, err := json.Marshal(entry)
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot encode flight journal: %v", err))
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("%06d.ndjson", flightJournalSegment))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot open flight journal: %v", err))
		return
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
		return
	}
	flightJournalSegmentEntries++
}

func recoverLatestFlightJournalLocked() *flightRecord {
	directories, err := os.ReadDir(flightJournalRoot)
	if err != nil {
		setFlightStorageErrorLocked(fmt.Sprintf("Cannot read flight journals: %v", err))
		return nil
	}
	sort.Slice(directories, func(i, j int) bool { return directories[i].Name() > directories[j].Name() })
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		record, ended, nextSegment := readFlightJournalLocked(filepath.Join(flightJournalRoot, directory.Name()))
		if ended && record != nil {
			restoreCompletedFlightFromJournalLocked(record)
			continue
		}
		if record == nil {
			continue
		}
		if flightHistoryContains(record.ID) {
			continue
		}
		flightJournalSegment = nextSegment
		flightJournalSegmentEntries = 0
		return record
	}
	return nil
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

func recoveredFlightNeedsClosure(sample flightLiveGPS) bool {
	if !flightJournalRecovered || flightCurrent == nil || len(flightCurrent.Track) == 0 {
		return false
	}
	last := flightCurrent.Track[len(flightCurrent.Track)-1]
	lastTime := parseFlightTime(last.TimeUTC)
	sampleTime := parseFlightTime(sample.TimeUTC)
	if lastTime.IsZero() || sampleTime.IsZero() {
		return false
	}
	gap := sampleTime.Sub(lastTime)
	if gap <= 0 {
		return false
	}
	if flightCurrent.Phase == flightPhaseTaxiIn && flightCurrent.LandingUTC != "" && last.GroundSpeedKt <= 2 {
		return gap >= 15*time.Second
	}
	return gap >= 10*time.Minute
}
