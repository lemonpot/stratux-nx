package main

import (
	"sync"
	"time"
)

const (
	flightContextInterval = 45 * time.Second
	flightContextMaximum  = 400
)

type flightTrafficContext struct {
	SourceCode      int     `json:"SourceCode"`
	Latitude        float64 `json:"Latitude"`
	Longitude       float64 `json:"Longitude"`
	AltitudeFt      int     `json:"AltitudeFt"`
	GroundspeedKt   int     `json:"GroundspeedKt"`
	BearingDeg      float64 `json:"BearingDeg"`
	DistanceNM      float64 `json:"DistanceNM"`
	AltitudeDeltaFt int     `json:"AltitudeDeltaFt"`
	SignalLevel     float64 `json:"SignalLevel"`
	AgeSeconds      float64 `json:"AgeSeconds"`
}

type flightContextSnapshot struct {
	TimeUTC             string                `json:"TimeUTC"`
	Phase               string                `json:"Phase"`
	GPSValid            bool                  `json:"GPSValid"`
	GPSSolution         string                `json:"GPSSolution"`
	SatellitesLocked    int                   `json:"SatellitesLocked"`
	HorizontalAccuracyM float64               `json:"HorizontalAccuracyM"`
	UATMsgPerMinute     int                   `json:"UATMsgPerMinute"`
	ESMsgPerMinute      int                   `json:"ESMsgPerMinute"`
	WANOnline           bool                  `json:"WANOnline"`
	SessionDataMiB      float64               `json:"SessionDataMiB"`
	SessionDownBps      float64               `json:"SessionDownBps"`
	SessionUpBps        float64               `json:"SessionUpBps"`
	InternetClients     int                   `json:"InternetClients"`
	ActiveTraffic       int                   `json:"ActiveTraffic"`
	NearestTraffic      *flightTrafficContext `json:"NearestTraffic,omitempty"`
}

type flightContextStatus struct {
	GPSSolution       string
	SatellitesLocked  uint16
	UATMsgPerMinute   uint
	ESMsgPerMinute    uint
}

var (
	flightContextStatusMu sync.Mutex
	flightContextLatest   flightContextStatus
)

func captureFlightGPSStatusSnapshot(solution string, satellites uint16) {
	flightContextStatusMu.Lock()
	flightContextLatest.GPSSolution = solution
	flightContextLatest.SatellitesLocked = satellites
	flightContextStatusMu.Unlock()
}

func captureFlightReceptionSnapshot(uatMessages, esMessages uint) {
	flightContextStatusMu.Lock()
	flightContextLatest.UATMsgPerMinute = uatMessages
	flightContextLatest.ESMsgPerMinute = esMessages
	flightContextStatusMu.Unlock()
}

func readFlightStatusSnapshot() flightContextStatus {
	flightContextStatusMu.Lock()
	snapshot := flightContextLatest
	flightContextStatusMu.Unlock()
	return snapshot
}

func captureFlightContext(sample flightLiveGPS, phase string) flightContextSnapshot {
	status := readFlightStatusSnapshot()
	snapshot := flightContextSnapshot{
		TimeUTC:             sample.TimeUTC,
		Phase:               phase,
		GPSValid:            sample.Valid,
		GPSSolution:         status.GPSSolution,
		SatellitesLocked:    int(status.SatellitesLocked),
		HorizontalAccuracyM: sample.HorizontalAccuracyM,
		UATMsgPerMinute:     int(status.UATMsgPerMinute),
		ESMsgPerMinute:      int(status.ESMsgPerMinute),
	}
	if snapshot.TimeUTC == "" {
		snapshot.TimeUTC = flightNowUTC().Format(time.RFC3339)
	}

	dataUsageMu.Lock()
	for _, client := range dataUsageClients {
		snapshot.SessionDataMiB += float64(client.TotalBytes) / 1048576
		snapshot.SessionDownBps += client.DownloadBps
		snapshot.SessionUpBps += client.UploadBps
		if client.Connected {
			snapshot.InternetClients++
		}
	}
	snapshot.WANOnline = dataUsageWANOnline
	dataUsageMu.Unlock()

	trafficMutex.Lock()
	var nearest TrafficInfo
	nearestSet := false
	for _, target := range traffic {
		age := stratuxClock.Since(target.Last_seen).Seconds()
		if age < 0 || age > 60 {
			continue
		}
		snapshot.ActiveTraffic++
		if target.Position_valid && target.BearingDist_valid && target.Distance >= 0 &&
			(!nearestSet || target.Distance < nearest.Distance) {
			nearest = target
			nearestSet = true
		}
	}
	trafficMutex.Unlock()

	if nearestSet {
		snapshot.NearestTraffic = &flightTrafficContext{
			SourceCode:      int(nearest.Last_source),
			Latitude:        float64(nearest.Lat),
			Longitude:       float64(nearest.Lng),
			AltitudeFt:      int(nearest.Alt),
			GroundspeedKt:   int(nearest.Speed),
			BearingDeg:      nearest.Bearing,
			DistanceNM:      nearest.Distance / 1852.0,
			AltitudeDeltaFt: int(nearest.Alt) - int(sample.AltitudeFt),
			SignalLevel:     nearest.SignalLevel,
			AgeSeconds:      stratuxClock.Since(nearest.Last_seen).Seconds(),
		}
	}
	return snapshot
}

func appendFlightContextLocked(snapshot flightContextSnapshot) {
	if flightCurrent == nil {
		return
	}
	flightCurrent.Context = append(flightCurrent.Context, snapshot)
	if len(flightCurrent.Context) > flightContextMaximum {
		flightCurrent.Context = append([]flightContextSnapshot(nil), flightCurrent.Context[len(flightCurrent.Context)-flightContextMaximum:]...)
	}
}
