package main

import (
	"encoding/csv"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	flightPhaseParked   = "PARKED"
	flightPhaseTaxiOut  = "TAXI_OUT"
	flightPhaseAirborne = "AIRBORNE"
	flightPhaseTaxiIn   = "TAXI_IN"
)

type flightLogSettings struct {
	Timezone   string `json:"Timezone"`
	AutoDetect bool   `json:"AutoDetect"`
}

type flightAirport struct {
	Code          string  `json:"Code"`
	Name          string  `json:"Name"`
	Municipality  string  `json:"Municipality,omitempty"`
	Country       string  `json:"Country,omitempty"`
	Type          string  `json:"Type,omitempty"`
	Latitude      float64 `json:"Latitude"`
	Longitude     float64 `json:"Longitude"`
	ElevationFt   float64 `json:"ElevationFt"`
	DistanceNM    float64 `json:"DistanceNM,omitempty"`
}

type flightTrackPoint struct {
	TimeUTC          string  `json:"TimeUTC"`
	Latitude         float64 `json:"Latitude"`
	Longitude        float64 `json:"Longitude"`
	AltitudeFt       float64 `json:"AltitudeFt"`
	GroundSpeedKt    float64 `json:"GroundSpeedKt"`
	CourseDeg        float64 `json:"CourseDeg"`
	VerticalSpeedFps float64 `json:"VerticalSpeedFps"`
}

type flightRecord struct {
	ID               string             `json:"ID"`
	Phase            string             `json:"Phase"`
	OffBlockUTC      string             `json:"OffBlockUTC,omitempty"`
	TakeoffUTC       string             `json:"TakeoffUTC,omitempty"`
	LandingUTC       string             `json:"LandingUTC,omitempty"`
	OnBlockUTC       string             `json:"OnBlockUTC,omitempty"`
	DepartureAirport *flightAirport     `json:"DepartureAirport,omitempty"`
	ArrivalAirport   *flightAirport     `json:"ArrivalAirport,omitempty"`
	AirTimeSeconds   int64              `json:"AirTimeSeconds"`
	BlockTimeSeconds int64              `json:"BlockTimeSeconds"`
	TaxiOutSeconds   int64              `json:"TaxiOutSeconds"`
	TaxiInSeconds    int64              `json:"TaxiInSeconds"`
	DistanceNM       float64            `json:"DistanceNM"`
	MaxGroundSpeedKt float64            `json:"MaxGroundSpeedKt"`
	MaxAltitudeFt    float64            `json:"MaxAltitudeFt"`
	TouchAndGoCount  int                `json:"TouchAndGoCount"`
	Track            []flightTrackPoint `json:"Track,omitempty"`
	Context          []flightContextSnapshot `json:"Context,omitempty"`
	Weather          *flightWeatherMemory `json:"Weather,omitempty"`
	LastUpdateUTC    string             `json:"LastUpdateUTC"`
	SyncStatus       string             `json:"SyncStatus,omitempty"`
	RemoteID         string             `json:"RemoteID,omitempty"`
	UploadedAtUTC    string             `json:"UploadedAtUTC,omitempty"`
	SyncError        string             `json:"SyncError,omitempty"`
	ContentHash      string             `json:"ContentHash,omitempty"`
}

type flightSummary struct {
	ID               string         `json:"ID"`
	Phase            string         `json:"Phase"`
	OffBlockUTC      string         `json:"OffBlockUTC,omitempty"`
	TakeoffUTC       string         `json:"TakeoffUTC,omitempty"`
	LandingUTC       string         `json:"LandingUTC,omitempty"`
	OnBlockUTC       string         `json:"OnBlockUTC,omitempty"`
	DepartureAirport *flightAirport `json:"DepartureAirport,omitempty"`
	ArrivalAirport   *flightAirport `json:"ArrivalAirport,omitempty"`
	AirTimeSeconds   int64          `json:"AirTimeSeconds"`
	BlockTimeSeconds int64          `json:"BlockTimeSeconds"`
	TaxiOutSeconds   int64          `json:"TaxiOutSeconds"`
	TaxiInSeconds    int64          `json:"TaxiInSeconds"`
	DistanceNM       float64        `json:"DistanceNM"`
	MaxGroundSpeedKt float64        `json:"MaxGroundSpeedKt"`
	MaxAltitudeFt    float64        `json:"MaxAltitudeFt"`
	TouchAndGoCount  int            `json:"TouchAndGoCount"`
	LastUpdateUTC    string         `json:"LastUpdateUTC"`
	SyncStatus       string         `json:"SyncStatus"`
	RemoteID         string         `json:"RemoteID,omitempty"`
	UploadedAtUTC    string         `json:"UploadedAtUTC,omitempty"`
	SyncError        string         `json:"SyncError,omitempty"`
	ContentHash      string         `json:"ContentHash,omitempty"`
}

type flightLiveGPS struct {
	Valid               bool    `json:"Valid"`
	TimeUTC             string  `json:"TimeUTC"`
	Latitude            float64 `json:"Latitude"`
	Longitude           float64 `json:"Longitude"`
	AltitudeFt          float64 `json:"AltitudeFt"`
	GroundSpeedKt       float64 `json:"GroundSpeedKt"`
	CourseDeg           float64 `json:"CourseDeg"`
	VerticalSpeedFps    float64 `json:"VerticalSpeedFps"`
	HorizontalAccuracyM float64 `json:"HorizontalAccuracyM"`
}

type flightLogResponse struct {
	Phase                string             `json:"Phase"`
	GPS                  flightLiveGPS      `json:"GPS"`
	Current              *flightSummary     `json:"Current,omitempty"`
	CurrentTrack         []flightTrackPoint `json:"CurrentTrack"`
	CurrentAirport       *flightAirport     `json:"CurrentAirport,omitempty"`
	Flights              []flightSummary    `json:"Flights"`
	Settings             flightLogSettings  `json:"Settings"`
	AirportDatabaseReady bool               `json:"AirportDatabaseReady"`
	AirportCount         int                `json:"AirportCount"`
	StoragePath          string             `json:"StoragePath"`
	Sync                 flightSyncPublicState `json:"Sync"`
}

type flightCandidate struct {
	since time.Time
	point flightTrackPoint
}

var (
	flightLogMu               sync.Mutex
	flightSettings            = flightLogSettings{Timezone: "UTC", AutoDetect: true}
	flightAirports            []flightAirport
	flightAirportDBReady      bool
	flightCurrent             *flightRecord
	flightHistory             []flightSummary
	flightStorageDir          string
	flightSettingsPath        string
	flightIndexPath           string
	flightCurrentPath         string
	flightFilesDir            string
	flightInitialized         bool
	flightMoveCandidate       flightCandidate
	flightTakeoffCandidate    flightCandidate
	flightLandingCandidate    flightCandidate
	flightStopCandidate       flightCandidate
	flightTaxiBaseAltitude    float64
	flightTaxiBaseSamples     int
	flightLastPosition        *flightTrackPoint
	flightLastPersist         time.Time
	flightCurrentAirport      *flightAirport
	flightLastAirportResolve  time.Time
	flightLastContextSample   time.Time
)

func init() {
	http.HandleFunc("/flightLog", handleFlightLog)
	http.HandleFunc("/flightLog/settings", handleFlightLogSettings)
	http.HandleFunc("/flightLog/flight", handleFlightLogFlight)
	http.HandleFunc("/flightLog/action", handleFlightLogAction)
	go flightLogMonitorLoop()
}

func flightLogMonitorLoop() {
	time.Sleep(5 * time.Second)
	initializeFlightLog()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		sample := readFlightGPS()
		contextFlightID := ""
		contextPhase := ""
		flightLogMu.Lock()
		if flightSettings.AutoDetect {
			updateFlightStateLocked(sample)
		}
		if sample.Valid && sample.GroundSpeedKt < 20 && time.Since(flightLastAirportResolve) > 15*time.Second {
			flightCurrentAirport = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)
			flightLastAirportResolve = time.Now()
		}
		if flightCurrent != nil && time.Since(flightLastPersist) > 15*time.Second {
			persistCurrentFlightLocked()
			flightLastPersist = time.Now()
		}
		if flightCurrent != nil && time.Since(flightLastContextSample) >= flightContextInterval {
			contextFlightID = flightCurrent.ID
			contextPhase = flightCurrent.Phase
			flightLastContextSample = time.Now()
		}
		flightLogMu.Unlock()
		if contextFlightID != "" {
			snapshot := captureFlightContext(sample, contextPhase)
			flightLogMu.Lock()
			if flightCurrent != nil && flightCurrent.ID == contextFlightID {
				appendFlightContextLocked(snapshot)
				syncFlightWeatherLocked()
			}
			flightLogMu.Unlock()
		}
	}
}

func initializeFlightLog() {
	flightLogMu.Lock()
	if flightInitialized {
		flightLogMu.Unlock()
		return
	}
	flightStorageDir = chooseFlightStorageDir()
	flightSettingsPath = filepath.Join(flightStorageDir, "settings.json")
	flightIndexPath = filepath.Join(flightStorageDir, "index.json")
	flightCurrentPath = filepath.Join(flightStorageDir, "current-flight.json")
	flightFilesDir = filepath.Join(flightStorageDir, "flights")
	_ = os.MkdirAll(flightFilesDir, 0755)
	loadFlightSettingsLocked()
	loadFlightIndexLocked()
	sort.SliceStable(flightHistory, func(i, j int) bool { return flightHistory[i].ID > flightHistory[j].ID })
	loadCurrentFlightLocked()
	if flightCurrent != nil {
		startFlightWeatherCapture(flightCurrent.ID, flightCurrent.Weather)
	}
	loadAirportDatabaseLocked()
	flightSyncInitializeLocked()
	flightInitialized = true
	flightLogMu.Unlock()
}

func chooseFlightStorageDir() string {
	candidates := []string{
		"/boot/firmware/stratux-flight-log",
		"/boot/stratux-flight-log",
		"/var/log/stratux-flight-log",
		"/tmp/stratux-flight-log",
	}
	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		test := filepath.Join(dir, ".write-test")
		if err := os.WriteFile(test, []byte("ok"), 0644); err == nil {
			_ = os.Remove(test)
			return dir
		}
	}
	return "/tmp/stratux-flight-log"
}

func loadFlightSettingsLocked() {
	b, err := os.ReadFile(flightSettingsPath)
	if err != nil {
		return
	}
	var cfg flightLogSettings
	if json.Unmarshal(b, &cfg) == nil {
		if cfg.Timezone != "" {
			if _, err := time.LoadLocation(cfg.Timezone); err == nil {
				flightSettings.Timezone = cfg.Timezone
			}
		}
		flightSettings.AutoDetect = cfg.AutoDetect
	}
}

func saveFlightSettingsLocked() {
	_ = writeFlightJSONAtomic(flightSettingsPath, flightSettings)
}

func loadFlightIndexLocked() {
	b, err := os.ReadFile(flightIndexPath)
	if err != nil {
		return
	}
	var items []flightSummary
	if json.Unmarshal(b, &items) == nil {
		flightHistory = items
	}
}

func saveFlightIndexLocked() error {
	return writeFlightJSONAtomic(flightIndexPath, flightHistory)
}

func writeFlightJSONAtomic(path string, value interface{}) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err = file.Write(body); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if directory, openErr := os.Open(filepath.Dir(path)); openErr == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func loadCurrentFlightLocked() {
	b, err := os.ReadFile(flightCurrentPath)
	if err != nil {
		return
	}
	var rec flightRecord
	if json.Unmarshal(b, &rec) == nil && rec.ID != "" {
		flightCurrent = &rec
		if len(rec.Track) > 0 {
			last := rec.Track[len(rec.Track)-1]
			flightLastPosition = &last
		}
	}
}

func persistCurrentFlightLocked() {
	if flightCurrent == nil {
		_ = os.Remove(flightCurrentPath)
		return
	}
	updateFlightDurationsLocked(flightCurrent, flightNowUTC())
	syncFlightWeatherLocked()
	_ = writeFlightJSONAtomic(flightCurrentPath, flightCurrent)
}

func loadAirportDatabaseLocked() {
	paths := []string{"/opt/stratux/share/airports.csv", "/opt/stratux/airports.csv"}
	var f *os.File
	for _, path := range paths {
		candidate, err := os.Open(path)
		if err == nil {
			f = candidate
			break
		}
	}
	if f == nil {
		flightAirportDBReady = false
		return
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		flightAirportDBReady = false
		return
	}
	airports := make([]flightAirport, 0, len(rows)-1)
	for i, row := range rows {
		if i == 0 || len(row) < 8 {
			continue
		}
		lat, err1 := strconv.ParseFloat(row[2], 64)
		lon, err2 := strconv.ParseFloat(row[3], 64)
		elev, _ := strconv.ParseFloat(row[4], 64)
		if err1 != nil || err2 != nil || lat == 0 || lon == 0 {
			continue
		}
		airports = append(airports, flightAirport{Code: row[0], Name: row[1], Latitude: lat, Longitude: lon, ElevationFt: elev, Type: row[5], Municipality: row[6], Country: row[7]})
	}
	flightAirports = airports
	flightAirportDBReady = len(airports) > 0
}

func readFlightGPS() flightLiveGPS {
	sample := flightLiveGPS{TimeUTC: time.Now().UTC().Format(time.RFC3339)}
	valid := isGPSValid()
	if mySituation.muGPS != nil {
		mySituation.muGPS.Lock()
		defer mySituation.muGPS.Unlock()
	}
	if !mySituation.GPSTime.IsZero() && mySituation.GPSTime.Year() >= 2000 {
		sample.TimeUTC = mySituation.GPSTime.UTC().Format(time.RFC3339)
	}
	sample.Valid = valid && mySituation.GPSLatitude != 0 && mySituation.GPSLongitude != 0
	sample.Latitude = float64(mySituation.GPSLatitude)
	sample.Longitude = float64(mySituation.GPSLongitude)
	sample.AltitudeFt = float64(mySituation.GPSAltitudeMSL)
	sample.GroundSpeedKt = mySituation.GPSGroundSpeed
	sample.CourseDeg = float64(mySituation.GPSTrueCourse)
	sample.VerticalSpeedFps = float64(mySituation.GPSVerticalSpeed)
	sample.HorizontalAccuracyM = float64(mySituation.GPSHorizontalAccuracy)
	return sample
}

func flightNowUTC() time.Time {
	t := mySituation.GPSTime
	if !t.IsZero() && t.Year() >= 2000 {
		return t.UTC()
	}
	return time.Now().UTC()
}

func pointFromGPS(sample flightLiveGPS) flightTrackPoint {
	return flightTrackPoint{TimeUTC: sample.TimeUTC, Latitude: sample.Latitude, Longitude: sample.Longitude, AltitudeFt: sample.AltitudeFt, GroundSpeedKt: sample.GroundSpeedKt, CourseDeg: sample.CourseDeg, VerticalSpeedFps: sample.VerticalSpeedFps}
}

func parseFlightTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339, value)
	return t
}

func newFlightLocked(sample flightLiveGPS, start time.Time) {
	p := pointFromGPS(sample)
	flightCurrent = &flightRecord{
		ID: start.UTC().Format("20060102T150405Z"), Phase: flightPhaseTaxiOut,
		OffBlockUTC: start.UTC().Format(time.RFC3339), MaxGroundSpeedKt: sample.GroundSpeedKt,
		MaxAltitudeFt: sample.AltitudeFt, LastUpdateUTC: sample.TimeUTC, Track: []flightTrackPoint{p},
	}
	flightLastContextSample = time.Time{}
	startFlightWeatherCapture(flightCurrent.ID, nil)
	flightTaxiBaseAltitude = sample.AltitudeFt
	flightTaxiBaseSamples = 1
	flightLastPosition = &p
	flightCurrent.DepartureAirport = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)
	persistCurrentFlightLocked()
}

func updateFlightStateLocked(sample flightLiveGPS) {
	if !sample.Valid || sample.HorizontalAccuracyM > 250 {
		return
	}
	now := parseFlightTime(sample.TimeUTC)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !flightTrackPointPlausibleLocked(sample, now) {
		return
	}
	speed := sample.GroundSpeedKt
	absVS := math.Abs(sample.VerticalSpeedFps)

	if flightCurrent == nil {
		if speed >= 3 {
			if flightMoveCandidate.since.IsZero() {
				flightMoveCandidate = flightCandidate{since: now, point: pointFromGPS(sample)}
			}
			if now.Sub(flightMoveCandidate.since) >= 8*time.Second {
				newFlightLocked(sample, flightMoveCandidate.since)
				flightMoveCandidate = flightCandidate{}
			}
		} else {
			flightMoveCandidate = flightCandidate{}
		}
		return
	}

	flightCurrent.LastUpdateUTC = sample.TimeUTC
	appendFlightTrackLocked(sample)
	if sample.GroundSpeedKt > flightCurrent.MaxGroundSpeedKt {
		flightCurrent.MaxGroundSpeedKt = sample.GroundSpeedKt
	}
	if sample.AltitudeFt > flightCurrent.MaxAltitudeFt {
		flightCurrent.MaxAltitudeFt = sample.AltitudeFt
	}

	switch flightCurrent.Phase {
	case flightPhaseTaxiOut:
		if speed < 25 && flightTaxiBaseSamples < 60 {
			flightTaxiBaseAltitude = (flightTaxiBaseAltitude*float64(flightTaxiBaseSamples) + sample.AltitudeFt) / float64(flightTaxiBaseSamples+1)
			flightTaxiBaseSamples++
		}
		takeoffEvidence := (speed >= 38 && (sample.VerticalSpeedFps >= 1.0 || sample.AltitudeFt-flightTaxiBaseAltitude >= 60)) || speed >= 52
		if takeoffEvidence {
			if flightTakeoffCandidate.since.IsZero() {
				flightTakeoffCandidate = flightCandidate{since: now, point: pointFromGPS(sample)}
			}
			if now.Sub(flightTakeoffCandidate.since) >= 6*time.Second {
				flightCurrent.TakeoffUTC = flightTakeoffCandidate.since.UTC().Format(time.RFC3339)
				flightCurrent.Phase = flightPhaseAirborne
				if flightCurrent.DepartureAirport == nil {
					p := flightTakeoffCandidate.point
					flightCurrent.DepartureAirport = nearestAirportLocked(p.Latitude, p.Longitude, p.AltitudeFt, 8.0)
				}
				flightTakeoffCandidate = flightCandidate{}
				persistCurrentFlightLocked()
			}
		} else {
			flightTakeoffCandidate = flightCandidate{}
		}

	case flightPhaseAirborne:
		nearby := nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)
		altitudePlausible := nearby != nil && math.Abs(sample.AltitudeFt-nearby.ElevationFt) < 1800
		landingEvidence := (speed <= 35 && absVS <= 4.0 && altitudePlausible) || speed <= 20
		if landingEvidence {
			if flightLandingCandidate.since.IsZero() {
				flightLandingCandidate = flightCandidate{since: now, point: pointFromGPS(sample)}
			}
			if now.Sub(flightLandingCandidate.since) >= 10*time.Second {
				flightCurrent.LandingUTC = flightLandingCandidate.since.UTC().Format(time.RFC3339)
				flightCurrent.Phase = flightPhaseTaxiIn
				p := flightLandingCandidate.point
				flightCurrent.ArrivalAirport = nearestAirportLocked(p.Latitude, p.Longitude, p.AltitudeFt, 8.0)
				flightLandingCandidate = flightCandidate{}
				flightStopCandidate = flightCandidate{}
				persistCurrentFlightLocked()
			}
		} else {
			flightLandingCandidate = flightCandidate{}
		}

	case flightPhaseTaxiIn:
		if speed >= 45 && (sample.VerticalSpeedFps >= 1.0 || (flightCurrent.ArrivalAirport != nil && sample.AltitudeFt-flightCurrent.ArrivalAirport.ElevationFt > 100)) {
			flightCurrent.TouchAndGoCount++
			flightCurrent.LandingUTC = ""
			flightCurrent.ArrivalAirport = nil
			flightCurrent.Phase = flightPhaseAirborne
			flightStopCandidate = flightCandidate{}
			persistCurrentFlightLocked()
			break
		}
		if speed <= 2 {
			if flightStopCandidate.since.IsZero() {
				flightStopCandidate = flightCandidate{since: now, point: pointFromGPS(sample)}
			}
			if now.Sub(flightStopCandidate.since) >= 45*time.Second {
				finishFlightLocked(flightStopCandidate.since, sample)
				flightStopCandidate = flightCandidate{}
			}
		} else {
			flightStopCandidate = flightCandidate{}
		}
	}
	if flightCurrent != nil {
		updateFlightDurationsLocked(flightCurrent, now)
	}
}

func flightTrackPointPlausibleLocked(sample flightLiveGPS, now time.Time) bool {
	if flightCurrent == nil || flightLastPosition == nil {
		return true
	}
	previousTime := parseFlightTime(flightLastPosition.TimeUTC)
	distanceNM := flightDistanceNM(
		flightLastPosition.Latitude,
		flightLastPosition.Longitude,
		sample.Latitude,
		sample.Longitude,
	)
	if distanceNM <= 0 {
		return true
	}
	if previousTime.IsZero() || !now.After(previousTime) {
		return distanceNM <= 0.25
	}

	elapsedHours := now.Sub(previousTime).Hours()
	reportedSpeedKt := math.Max(flightLastPosition.GroundSpeedKt, sample.GroundSpeedKt)
	accuracyNM := math.Max(0, sample.HorizontalAccuracyM) / 1852.0
	maximumDistanceNM := math.Max(0.35, reportedSpeedKt*elapsedHours*3.0+accuracyNM*2.0+0.10)
	if distanceNM <= maximumDistanceNM {
		return true
	}

	log.Printf(
		"Flight Log: ignored implausible GPS jump of %.2f NM in %.1f seconds (allowed %.2f NM)",
		distanceNM,
		now.Sub(previousTime).Seconds(),
		maximumDistanceNM,
	)
	return false
}

func appendFlightTrackLocked(sample flightLiveGPS) {
	if flightCurrent == nil {
		return
	}
	p := pointFromGPS(sample)
	if flightLastPosition != nil {
		dist := flightDistanceNM(flightLastPosition.Latitude, flightLastPosition.Longitude, p.Latitude, p.Longitude)
		if dist >= 0 && dist <= 2.0 {
			flightCurrent.DistanceNM += dist
		}
	}
	add := false
	if len(flightCurrent.Track) == 0 {
		add = true
	} else {
		last := flightCurrent.Track[len(flightCurrent.Track)-1]
		if parseFlightTime(p.TimeUTC).Sub(parseFlightTime(last.TimeUTC)) >= 5*time.Second || flightDistanceNM(last.Latitude, last.Longitude, p.Latitude, p.Longitude) >= 0.05 {
			add = true
		}
	}
	if add {
		flightCurrent.Track = append(flightCurrent.Track, p)
	}
	flightLastPosition = &p
}

func updateFlightDurationsLocked(rec *flightRecord, now time.Time) {
	if rec == nil {
		return
	}
	off, takeoff, landing, on := parseFlightTime(rec.OffBlockUTC), parseFlightTime(rec.TakeoffUTC), parseFlightTime(rec.LandingUTC), parseFlightTime(rec.OnBlockUTC)
	if !takeoff.IsZero() {
		end := now
		if !landing.IsZero() {
			end = landing
		}
		if end.After(takeoff) {
			rec.AirTimeSeconds = int64(end.Sub(takeoff).Seconds())
		}
	}
	if !off.IsZero() {
		end := now
		if !on.IsZero() {
			end = on
		}
		if end.After(off) {
			rec.BlockTimeSeconds = int64(end.Sub(off).Seconds())
		}
	}
	if !off.IsZero() && !takeoff.IsZero() && takeoff.After(off) {
		rec.TaxiOutSeconds = int64(takeoff.Sub(off).Seconds())
	}
	if !landing.IsZero() {
		end := now
		if !on.IsZero() {
			end = on
		}
		if end.After(landing) {
			rec.TaxiInSeconds = int64(end.Sub(landing).Seconds())
		}
	}
}

func finishFlightLocked(onBlock time.Time, sample flightLiveGPS) bool {
	if flightCurrent == nil {
		return false
	}
	previousPhase := flightCurrent.Phase
	flightCurrent.OnBlockUTC = onBlock.UTC().Format(time.RFC3339)
	flightCurrent.Phase = flightPhaseParked
	flightCurrent.LastUpdateUTC = sample.TimeUTC
	flightCurrent.SyncStatus = flightSyncCompletedStatusLocked()
	if flightCurrent.ArrivalAirport == nil {
		flightCurrent.ArrivalAirport = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)
	}
	updateFlightDurationsLocked(flightCurrent, onBlock)
	syncFlightWeatherLocked()
	completedFlightID := flightCurrent.ID
	completedPath := filepath.Join(flightFilesDir, flightCurrent.ID+".json")
	if err := writeFlightJSONAtomic(completedPath, flightCurrent); err != nil {
		flightCurrent.Phase = previousPhase
		flightCurrent.OnBlockUTC = ""
		flightCurrent.SyncError = "Unable to save completed flight"
		persistCurrentFlightLocked()
		log.Printf("flight log: unable to save completed flight %s: %v", completedFlightID, err)
		return false
	}
	previousHistory := flightHistory
	flightHistory = append([]flightSummary{summarizeFlight(*flightCurrent)}, flightHistory...)
	if len(flightHistory) > 500 {
		flightHistory = flightHistory[:500]
	}
	if err := saveFlightIndexLocked(); err != nil {
		flightHistory = previousHistory
		_ = os.Remove(completedPath)
		flightCurrent.Phase = previousPhase
		flightCurrent.OnBlockUTC = ""
		flightCurrent.SyncError = "Unable to update flight index"
		persistCurrentFlightLocked()
		log.Printf("flight log: unable to update flight index for %s: %v", completedFlightID, err)
		return false
	}
	_ = os.Remove(flightCurrentPath)
	flightCurrent = nil
	flightLastPosition = nil
	flightTaxiBaseAltitude = 0
	flightTaxiBaseSamples = 0
	flightCurrentAirport = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)
	if flightSyncEnabled {
		wakeFlightSync()
	}
	stopFlightWeatherCapture(completedFlightID)
	return true
}

func summarizeFlight(rec flightRecord) flightSummary {
	return flightSummary{ID: rec.ID, Phase: rec.Phase, OffBlockUTC: rec.OffBlockUTC, TakeoffUTC: rec.TakeoffUTC, LandingUTC: rec.LandingUTC, OnBlockUTC: rec.OnBlockUTC, DepartureAirport: rec.DepartureAirport, ArrivalAirport: rec.ArrivalAirport, AirTimeSeconds: rec.AirTimeSeconds, BlockTimeSeconds: rec.BlockTimeSeconds, TaxiOutSeconds: rec.TaxiOutSeconds, TaxiInSeconds: rec.TaxiInSeconds, DistanceNM: rec.DistanceNM, MaxGroundSpeedKt: rec.MaxGroundSpeedKt, MaxAltitudeFt: rec.MaxAltitudeFt, TouchAndGoCount: rec.TouchAndGoCount, LastUpdateUTC: rec.LastUpdateUTC, SyncStatus: rec.SyncStatus, RemoteID: rec.RemoteID, UploadedAtUTC: rec.UploadedAtUTC, SyncError: rec.SyncError, ContentHash: rec.ContentHash}
}

func nearestAirportLocked(lat, lon, altitudeFt, maxNM float64) *flightAirport {
	if !flightAirportDBReady || lat == 0 || lon == 0 {
		return nil
	}
	bestScore := math.MaxFloat64
	var best *flightAirport
	for i := range flightAirports {
		a := &flightAirports[i]
		d := flightDistanceNM(lat, lon, a.Latitude, a.Longitude)
		if d > maxNM {
			continue
		}
		penalty := 0.0
		if a.ElevationFt != 0 && altitudeFt != 0 {
			diff := math.Abs(altitudeFt - a.ElevationFt)
			if diff > 2500 {
				continue
			}
			penalty = diff / 5000.0
		}
		if d+penalty < bestScore {
			copyAirport := *a
			copyAirport.DistanceNM = d
			best = &copyAirport
			bestScore = d + penalty
		}
	}
	return best
}

func flightDistanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusNM = 3440.065
	toRad := math.Pi / 180
	p1, p2 := lat1*toRad, lat2*toRad
	dp, dl := (lat2-lat1)*toRad, (lon2-lon1)*toRad
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return earthRadiusNM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func downsampleTrack(points []flightTrackPoint, max int) []flightTrackPoint {
	if len(points) <= max || max <= 0 {
		return append([]flightTrackPoint(nil), points...)
	}
	out := make([]flightTrackPoint, 0, max)
	step := float64(len(points)-1) / float64(max-1)
	for i := 0; i < max; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx >= len(points) {
			idx = len(points) - 1
		}
		out = append(out, points[idx])
	}
	return out
}

func handleFlightLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	initializeFlightLog()
	gps := readFlightGPS()
	flightLogMu.Lock()
	phase := flightPhaseParked
	var current *flightSummary
	track := []flightTrackPoint{}
	if flightCurrent != nil {
		updateFlightDurationsLocked(flightCurrent, parseFlightTime(gps.TimeUTC))
		s := summarizeFlight(*flightCurrent)
		current = &s
		phase = flightCurrent.Phase
		track = downsampleTrack(flightCurrent.Track, 400)
	}
	history := append([]flightSummary(nil), flightHistory...)
	if len(history) > 100 {
		history = history[:100]
	}
	settings := flightSettings
	var airportCopy *flightAirport
	if flightCurrentAirport != nil {
		c := *flightCurrentAirport
		airportCopy = &c
	}
	syncState := flightSyncPublicStateLocked()
	resp := flightLogResponse{Phase: phase, GPS: gps, Current: current, CurrentTrack: track, CurrentAirport: airportCopy, Flights: history, Settings: settings, AirportDatabaseReady: flightAirportDBReady, AirportCount: len(flightAirports), StoragePath: flightStorageDir, Sync: syncState}
	flightLogMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleFlightLogSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	initializeFlightLog()
	var cfg flightLogSettings
	if json.NewDecoder(r.Body).Decode(&cfg) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	if cfg.Timezone == "" {
		cfg.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		http.Error(w, "unknown IANA timezone", http.StatusBadRequest)
		return
	}
	flightLogMu.Lock()
	flightSettings = cfg
	saveFlightSettingsLocked()
	flightLogMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleFlightLogFlight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	initializeFlightLog()
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		http.Error(w, "invalid flight id", http.StatusBadRequest)
		return
	}
	b, err := os.ReadFile(filepath.Join(flightFilesDir, id+".json"))
	if err != nil {
		http.Error(w, "flight not found", http.StatusNotFound)
		return
	}
	var record flightRecord
	if json.Unmarshal(b, &record) != nil {
		http.Error(w, "invalid flight record", http.StatusInternalServerError)
		return
	}
	flightLogMu.Lock()
	for i := range flightHistory {
		if flightHistory[i].ID == id {
			record.SyncStatus = flightHistory[i].SyncStatus
			record.RemoteID = flightHistory[i].RemoteID
			record.UploadedAtUTC = flightHistory[i].UploadedAtUTC
			record.SyncError = flightHistory[i].SyncError
			record.ContentHash = flightHistory[i].ContentHash
			break
		}
	}
	flightLogMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(record)
}

func handleFlightLogAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	initializeFlightLog()
	var req struct{ Action string `json:"action"` }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	gps := readFlightGPS()
	now := parseFlightTime(gps.TimeUTC)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	flightLogMu.Lock()
	switch req.Action {
	case "finish":
		if flightCurrent == nil {
			flightLogMu.Unlock()
			http.Error(w, "no active flight", http.StatusBadRequest)
			return
		}
		if flightCurrent.LandingUTC == "" {
			flightCurrent.LandingUTC = now.Format(time.RFC3339)
		}
		if !finishFlightLocked(now, gps) {
			flightLogMu.Unlock()
			http.Error(w, "flight could not be saved; the recoverable current record was retained", http.StatusInsufficientStorage)
			return
		}
	case "discard":
		if flightCurrent != nil {
			stopFlightWeatherCapture(flightCurrent.ID)
		}
		flightCurrent = nil
		flightLastPosition = nil
		_ = os.Remove(flightCurrentPath)
	default:
		flightLogMu.Unlock()
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	flightLogMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
