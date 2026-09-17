package main

import (
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

func TestRecoveredFlightClosesAfterLongPowerOffGap(t *testing.T) {
	lastTime := time.Date(2026, 9, 16, 18, 0, 0, 0, time.UTC)
	flightCurrent = &flightRecord{
		ID: "20260916T170000Z", Phase: flightPhaseTaxiIn, LastUpdateUTC: lastTime.Format(time.RFC3339),
		Track: []flightTrackPoint{{
			TimeUTC: lastTime.Format(time.RFC3339), Latitude: 45.5, Longitude: -73.5,
			AltitudeFt: 120, GroundSpeedKt: 0.5,
		}},
	}
	flightJournalRecovered = true
	if !recoveredFlightNeedsClosure(flightLiveGPS{TimeUTC: lastTime.Add(30 * time.Minute).Format(time.RFC3339)}) {
		t.Fatal("stale recovered flight should close before a new flight can start")
	}
	if recoveredFlightNeedsClosure(flightLiveGPS{TimeUTC: lastTime.Add(5 * time.Minute).Format(time.RFC3339)}) {
		t.Fatal("short in-flight reboot gap should remain recoverable")
	}
	flightCurrent.LandingUTC = lastTime.Add(-time.Minute).Format(time.RFC3339)
	if !recoveredFlightNeedsClosure(flightLiveGPS{TimeUTC: lastTime.Add(20 * time.Second).Format(time.RFC3339)}) {
		t.Fatal("landed flight should close promptly after a reboot")
	}
}
