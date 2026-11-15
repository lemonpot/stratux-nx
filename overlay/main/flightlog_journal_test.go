package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFlightJournalPreservesTrackParameters(t *testing.T) {
	root := t.TempDir()
	flightStorageDir = root
	flightFilesDir = filepath.Join(root, "flights")
	flightJournalRoot = filepath.Join(root, "journals")
	flightJournalSegment = 0
	flightJournalSegmentEntries = 0
	flightStorageError = ""
	if err := os.MkdirAll(flightFilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(flightJournalRoot, 0755); err != nil {
		t.Fatal(err)
	}

	first := flightTrackPoint{
		TimeUTC: "2026-09-16T14:00:00Z", Latitude: 45.4706, Longitude: -73.7408,
		AltitudeFt: 118, GroundSpeedKt: 4.5, CourseDeg: 72, VerticalSpeedFps: 0.2,
	}
	second := flightTrackPoint{
		TimeUTC: "2026-09-16T14:00:01Z", Latitude: 45.4707, Longitude: -73.7405,
		AltitudeFt: 123, GroundSpeedKt: 8.75, CourseDeg: 74, VerticalSpeedFps: 1.25,
	}
	flightCurrent = &flightRecord{
		ID: "20260916T140000Z", Phase: flightPhaseTaxiOut,
		OffBlockUTC: first.TimeUTC, LastUpdateUTC: first.TimeUTC, Track: []flightTrackPoint{first},
	}
	beginFlightJournalLocked(flightCurrent)
	flightCurrent.Track = append(flightCurrent.Track, second)
	flightCurrent.LastUpdateUTC = second.TimeUTC
	appendFlightJournalPointLocked(second)
	checkpointFlightJournalLocked()

	recovered, ended, _ := readFlightJournalLocked(flightJournalDirectory(flightCurrent.ID))
	if ended {
		t.Fatal("active journal was incorrectly marked complete")
	}
	if recovered == nil || len(recovered.Track) != 2 {
		t.Fatalf("expected two recovered points, got %#v", recovered)
	}
	got := recovered.Track[1]
	if got.TimeUTC != second.TimeUTC || got.Latitude != second.Latitude || got.Longitude != second.Longitude ||
		got.AltitudeFt != second.AltitudeFt || got.GroundSpeedKt != second.GroundSpeedKt ||
		got.CourseDeg != second.CourseDeg || got.VerticalSpeedFps != second.VerticalSpeedFps {
		t.Fatalf("journal changed a GPS parameter: got %#v want %#v", got, second)
	}
}

func TestFlightJournalKeepsEarlierSegmentsWhenLatestIsDamaged(t *testing.T) {
	root := t.TempDir()
	flightStorageDir = root
	flightFilesDir = filepath.Join(root, "flights")
	flightJournalRoot = filepath.Join(root, "journals")
	flightJournalSegment = 0
	flightJournalSegmentEntries = 0
	flightStorageError = ""
	if err := os.MkdirAll(flightFilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(flightJournalRoot, 0755); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	first := flightTrackPoint{TimeUTC: start.Format(time.RFC3339), Latitude: 45, Longitude: -73, AltitudeFt: 100, GroundSpeedKt: 3}
	flightCurrent = &flightRecord{
		ID: "20260916T150000Z", Phase: flightPhaseTaxiOut,
		OffBlockUTC: first.TimeUTC, LastUpdateUTC: first.TimeUTC, Track: []flightTrackPoint{first},
	}
	beginFlightJournalLocked(flightCurrent)
	for i := 1; i < 75; i++ {
		point := flightTrackPoint{
			TimeUTC:  start.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			Latitude: 45 + float64(i)/10000, Longitude: -73 + float64(i)/10000,
			AltitudeFt: 100 + float64(i), GroundSpeedKt: 20 + float64(i)/10,
			CourseDeg: 90, VerticalSpeedFps: 2,
		}
		flightCurrent.Track = append(flightCurrent.Track, point)
		flightCurrent.LastUpdateUTC = point.TimeUTC
		appendFlightJournalPointLocked(point)
	}

	latest := filepath.Join(flightJournalDirectory(flightCurrent.ID), "000003.ndjson")
	if err := os.WriteFile(latest, []byte("{damaged"), 0644); err != nil {
		t.Fatal(err)
	}
	recovered, _, _ := readFlightJournalLocked(flightJournalDirectory(flightCurrent.ID))
	if recovered == nil || len(recovered.Track) < 50 {
		t.Fatalf("earlier immutable journal segment was not recoverable: %#v", recovered)
	}
	if math.Abs(recovered.Track[0].AltitudeFt-first.AltitudeFt) > 0.001 {
		t.Fatal("first recorded altitude was lost")
	}
}

func setupFlightRecoveryStorage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	flightInitialized = true
	flightStorageDir = root
	flightFilesDir = filepath.Join(root, "flights")
	flightJournalRoot = filepath.Join(root, "journals")
	flightIndexPath = filepath.Join(root, "index.json")
	flightCurrentPath = filepath.Join(root, "current-flight.json")
	flightHistory = nil
	flightCurrent = nil
	flightLastPosition = nil
	flightJournalSegment = 0
	flightJournalSegmentEntries = 0
	flightJournalRecovered = false
	flightStorageError = ""
	flightAirportDBReady = false
	for _, dir := range []string{flightFilesDir, flightJournalRoot} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Writes an unfinished journal exactly as the live recorder does, then forgets it.
func writeUnfinishedJournal(t *testing.T, id, phase string, start time.Time, takeoff, landing time.Duration, speeds []float64) {
	t.Helper()
	point := func(i int) flightTrackPoint {
		return flightTrackPoint{
			TimeUTC:  start.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			Latitude: 45 + float64(i)/100000, Longitude: -73, AltitudeFt: 120, GroundSpeedKt: speeds[i],
		}
	}
	flightCurrent = &flightRecord{
		ID: id, Phase: flightPhaseTaxiOut, OffBlockUTC: start.Format(time.RFC3339),
		LastUpdateUTC: start.Format(time.RFC3339), Track: []flightTrackPoint{point(0)},
	}
	beginFlightJournalLocked(flightCurrent)
	for i := 1; i < len(speeds); i++ {
		p := point(i)
		flightCurrent.Track = append(flightCurrent.Track, p)
		flightCurrent.LastUpdateUTC = p.TimeUTC
		appendFlightJournalPointLocked(p)
	}
	if takeoff > 0 {
		flightCurrent.TakeoffUTC = start.Add(takeoff).Format(time.RFC3339)
	}
	if landing > 0 {
		flightCurrent.LandingUTC = start.Add(landing).Format(time.RFC3339)
	}
	flightCurrent.Phase = phase
	checkpointFlightJournalLocked()
	flightCurrent = nil
	flightLastPosition = nil
}

func repeatSpeed(speed float64, count int) []float64 {
	speeds := make([]float64, count)
	for i := range speeds {
		speeds[i] = speed
	}
	return speeds
}

func readCompletedFlight(t *testing.T, id string) flightRecord {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(flightFilesDir, id+".json"))
	if err != nil {
		t.Fatalf("flight %s was not saved: %v", id, err)
	}
	var record flightRecord
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestRecoveryClosesEveryUnfinishedJournalWithRecordedTimes(t *testing.T) {
	root := setupFlightRecoveryStorage(t)
	day := time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)

	// Landed and taxiing in, then 30 s stopped: power cut before the 45 s stop timer.
	landed := append(append(repeatSpeed(60, 60), repeatSpeed(10, 30)...), repeatSpeed(0, 30)...)
	writeUnfinishedJournal(t, "20261025T140000Z", flightPhaseTaxiIn, day, 10*time.Second, 60*time.Second, landed)

	// Power cut at cruise speed: must stay incomplete.
	writeUnfinishedJournal(t, "20261025T150000Z", flightPhaseAirborne, day.Add(time.Hour), 10*time.Second, 0, repeatSpeed(90, 60))

	// Taxi only, never took off: not a flight.
	writeUnfinishedJournal(t, "20261025T160000Z", flightPhaseTaxiOut, day.Add(2*time.Hour), 0, 0, repeatSpeed(8, 30))

	// Newest: landing was in progress (20 s slow roll) but not yet confirmed.
	rollout := append(repeatSpeed(70, 40), repeatSpeed(12, 20)...)
	writeUnfinishedJournal(t, "20261025T170000Z", flightPhaseAirborne, day.Add(3*time.Hour), 10*time.Second, 0, rollout)

	recoverFlightJournalsLocked()

	if flightCurrent != nil || flightJournalRecovered {
		t.Fatalf("all journals should be resolved at boot, current=%#v", flightCurrent)
	}
	if len(flightHistory) != 3 {
		t.Fatalf("expected 3 flights in history, got %d", len(flightHistory))
	}

	first := readCompletedFlight(t, "20261025T140000Z")
	if first.Incomplete || first.OnBlockUTC != day.Add(90*time.Second).Format(time.RFC3339) ||
		first.LandingUTC != day.Add(60*time.Second).Format(time.RFC3339) || first.LastUpdateUTC != day.Add(119*time.Second).Format(time.RFC3339) {
		t.Fatalf("landed flight closed with wrong times: %#v", first)
	}

	inAir := readCompletedFlight(t, "20261025T150000Z")
	if !inAir.Incomplete || inAir.ArrivalAirport != nil || inAir.LandingUTC != "" ||
		inAir.OnBlockUTC != day.Add(time.Hour+59*time.Second).Format(time.RFC3339) {
		t.Fatalf("in-air flight should be incomplete at its last recorded point: %#v", inAir)
	}

	if _, err := os.Stat(filepath.Join(flightFilesDir, "20261025T160000Z.json")); err == nil {
		t.Fatal("taxi-only journal must not become a flight")
	}
	if _, err := os.Stat(filepath.Join(root, flightArchivedJournalsDir, "20261025T160000Z", "000001.ndjson")); err != nil {
		t.Fatalf("taxi-only journal must be archived, not deleted: %v", err)
	}

	latest := readCompletedFlight(t, "20261025T170000Z")
	if latest.Incomplete || latest.LandingUTC != day.Add(3*time.Hour+40*time.Second).Format(time.RFC3339) ||
		latest.OnBlockUTC != day.Add(3*time.Hour+59*time.Second).Format(time.RFC3339) {
		t.Fatalf("landing roll should be recognised from recorded points: %#v", latest)
	}

	for _, id := range []string{"20261025T140000Z", "20261025T150000Z", "20261025T170000Z"} {
		if _, err := os.Stat(filepath.Join(flightJournalRoot, id, "000001.ndjson")); err != nil {
			t.Fatalf("journal %s must be kept on disk: %v", id, err)
		}
	}

	// A second boot must not duplicate anything.
	recoverFlightJournalsLocked()
	if len(flightHistory) != 3 || flightCurrent != nil {
		t.Fatalf("recovery is not idempotent: %d flights", len(flightHistory))
	}
}

func TestLatestInAirJournalWaitsForGPSThenClosesIncomplete(t *testing.T) {
	setupFlightRecoveryStorage(t)
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	writeUnfinishedJournal(t, "20261026T120000Z", flightPhaseAirborne, start, 10*time.Second, 0, repeatSpeed(95, 60))

	recoverFlightJournalsLocked()
	if flightCurrent == nil || !flightJournalRecovered {
		t.Fatal("latest in-air journal should wait for GPS to resume")
	}
	last := start.Add(59 * time.Second)
	if !recoveredFlightCanResume(flightLiveGPS{TimeUTC: last.Add(2 * time.Minute).Format(time.RFC3339), GroundSpeedKt: 95}) {
		t.Fatal("short in-flight reboot should resume")
	}
	if recoveredFlightCanResume(flightLiveGPS{TimeUTC: last.Add(30 * time.Minute).Format(time.RFC3339), GroundSpeedKt: 95}) {
		t.Fatal("long gap must not stitch two flights together")
	}
	if recoveredFlightCanResume(flightLiveGPS{TimeUTC: last.Add(2 * time.Minute).Format(time.RFC3339), GroundSpeedKt: 0}) {
		t.Fatal("aircraft found parked must not resume the airborne flight")
	}

	if !closeRecoveredFlightLocked() {
		t.Fatal("recovered flight could not be closed")
	}
	record := readCompletedFlight(t, "20261026T120000Z")
	if !record.Incomplete || record.OnBlockUTC != last.Format(time.RFC3339) || record.AirTimeSeconds != 49 {
		t.Fatalf("incomplete flight must keep its own recorded times: %#v", record)
	}
	if len(flightHistory) != 1 || !flightHistory[0].Incomplete {
		t.Fatal("incomplete flight must be listed")
	}
}

func TestWeatherAndContextSurvivePowerLoss(t *testing.T) {
	setupFlightRecoveryStorage(t)
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	id := "20261114T120000Z"
	flightCurrent = &flightRecord{
		ID: id, Phase: flightPhaseAirborne, OffBlockUTC: start.Format(time.RFC3339),
		TakeoffUTC: start.Add(10 * time.Second).Format(time.RFC3339), LastUpdateUTC: start.Format(time.RFC3339),
		Track: []flightTrackPoint{{TimeUTC: start.Format(time.RFC3339), Latitude: 45, Longitude: -73, GroundSpeedKt: 95}},
	}
	beginFlightJournalLocked(flightCurrent)
	startFlightWeatherCapture(id, nil)
	for i := 1; i < 60; i++ {
		p := flightTrackPoint{TimeUTC: start.Add(time.Duration(i) * time.Second).Format(time.RFC3339), Latitude: 45 + float64(i)/100000, Longitude: -73, GroundSpeedKt: 95}
		flightCurrent.Track = append(flightCurrent.Track, p)
		flightCurrent.LastUpdateUTC = p.TimeUTC
		appendFlightJournalPointLocked(p)
	}

	captureFlightWeatherText(WeatherMessage{Type: "METAR", Location: "CYUL", Time: "141200Z", Data: "CYUL 141200Z 27010KT"})
	captureFlightNEXRADFrame(63, 4)
	syncFlightWeatherLocked()
	written := flightJournalSegment*flightJournalEntriesPerSegment + flightJournalSegmentEntries
	syncFlightWeatherLocked()
	if flightJournalSegment*flightJournalEntriesPerSegment+flightJournalSegmentEntries != written {
		t.Fatal("unchanged weather must not be written again")
	}
	captureFlightWeatherText(WeatherMessage{Type: "TAF", Location: "CYUL", Time: "141130Z", Data: "TAF CYUL 141130Z"})
	syncFlightWeatherLocked()
	appendFlightContextLocked(flightContextSnapshot{TimeUTC: start.Add(30 * time.Second).Format(time.RFC3339), Phase: flightPhaseAirborne})
	checkpointFlightJournalLocked()

	// Power loss: memory is gone, only the journal remains.
	stopFlightWeatherCapture(id)
	flightCurrent = nil
	recoverFlightJournalsLocked()
	if flightCurrent == nil || flightCurrent.Weather == nil || len(flightCurrent.Weather.TextReports) != 2 ||
		flightCurrent.Weather.Summary["METAR"] != 1 || flightCurrent.Weather.Summary["NEXRAD"] != 1 ||
		len(flightCurrent.Weather.NEXRADSnapshots) != 1 || len(flightCurrent.Context) != 1 {
		t.Fatalf("weather/context not recovered from journal: %#v", flightCurrent)
	}
	if !closeRecoveredFlightLocked() {
		t.Fatal("recovered flight could not be closed")
	}
	record := readCompletedFlight(t, id)
	if record.Weather == nil || len(record.Weather.TextReports) != 2 || len(record.Context) != 1 || record.Context[0].Phase != flightPhaseAirborne {
		t.Fatalf("completed flight lost recovered weather/context: %#v", record)
	}
}

func TestReconcileFlightIndexListsEveryCompletedFile(t *testing.T) {
	setupFlightRecoveryStorage(t)
	record := flightRecord{ID: "20261027T090000Z", Phase: flightPhaseParked, LastUpdateUTC: "2026-10-27T10:00:00Z"}
	if err := writeFlightJSONAtomic(filepath.Join(flightFilesDir, record.ID+".json"), record); err != nil {
		t.Fatal(err)
	}
	reconcileFlightIndexLocked()
	if len(flightHistory) != 1 || flightHistory[0].ID != record.ID {
		t.Fatalf("completed flight missing from index: %#v", flightHistory)
	}
	if _, err := os.Stat(flightIndexPath); err != nil {
		t.Fatalf("index was not saved: %v", err)
	}
}

func TestFlightStorageRejectsRAM(t *testing.T) {
	if flightStorageIsPhysical("/dev/shm") {
		t.Fatal("tmpfs must never be used for flight storage")
	}
}
