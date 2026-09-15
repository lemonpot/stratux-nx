package main

import (
	"strings"
	"sync"
	"time"
)

const (
	flightWeatherTextMaximum   = 200
	flightWeatherRadarMaximum  = 60
	flightWeatherRadarInterval = 5 * time.Minute
)

type flightWeatherTextReport struct {
	ReceivedUTC string `json:"ReceivedUTC"`
	Type        string `json:"Type"`
	Location    string `json:"Location"`
	ReportTime  string `json:"ReportTime"`
	Data        string `json:"Data"`
}

type flightWeatherRadarRollup struct {
	ReceivedUTC string `json:"ReceivedUTC"`
	ProductID   uint32 `json:"ProductID"`
	FrameCount  int    `json:"FrameCount"`
	BlockCount  int    `json:"BlockCount"`
}

type flightWeatherMemory struct {
	CaptureVersion  int                        `json:"CaptureVersion"`
	TextReports     []flightWeatherTextReport  `json:"TextReports,omitempty"`
	NEXRADSnapshots []flightWeatherRadarRollup `json:"NEXRADSnapshots,omitempty"`
	Summary         map[string]int             `json:"Summary"`
}

var (
	flightWeatherMu       sync.Mutex
	flightWeatherFlightID string
	flightWeatherCurrent  flightWeatherMemory
	flightWeatherSeen     map[string]bool
)

func startFlightWeatherCapture(flightID string, existing *flightWeatherMemory) {
	flightWeatherMu.Lock()
	flightWeatherFlightID = flightID
	flightWeatherCurrent = flightWeatherMemory{CaptureVersion: 1, Summary: make(map[string]int)}
	flightWeatherSeen = make(map[string]bool)
	if existing != nil {
		flightWeatherCurrent.TextReports = append([]flightWeatherTextReport(nil), existing.TextReports...)
		flightWeatherCurrent.NEXRADSnapshots = append([]flightWeatherRadarRollup(nil), existing.NEXRADSnapshots...)
		for key, value := range existing.Summary {
			flightWeatherCurrent.Summary[key] = value
		}
		for _, report := range existing.TextReports {
			key := report.Type + "|" + report.Location + "|" + report.ReportTime + "|" + report.Data
			flightWeatherSeen[key] = true
		}
	}
	flightWeatherMu.Unlock()
}

func stopFlightWeatherCapture(flightID string) {
	flightWeatherMu.Lock()
	if flightWeatherFlightID == flightID {
		flightWeatherFlightID = ""
		flightWeatherSeen = nil
	}
	flightWeatherMu.Unlock()
}

func captureFlightWeatherText(message WeatherMessage) {
	weatherType := strings.ToUpper(strings.TrimSpace(message.Type))
	location := strings.ToUpper(strings.TrimSpace(message.Location))
	reportTime := strings.TrimSpace(message.Time)
	data := strings.TrimSpace(message.Data)
	if weatherType == "" || location == "" || data == "" {
		return
	}
	flightWeatherMu.Lock()
	defer flightWeatherMu.Unlock()
	if flightWeatherFlightID == "" {
		return
	}
	flightWeatherCurrent.Summary[weatherType]++
	key := weatherType + "|" + location + "|" + reportTime + "|" + data
	if flightWeatherSeen[key] || len(flightWeatherCurrent.TextReports) >= flightWeatherTextMaximum {
		return
	}
	flightWeatherSeen[key] = true
	flightWeatherCurrent.TextReports = append(flightWeatherCurrent.TextReports, flightWeatherTextReport{
		ReceivedUTC: time.Now().UTC().Format(time.RFC3339),
		Type:        weatherType,
		Location:    location,
		ReportTime:  reportTime,
		Data:        data,
	})
}

func captureFlightNEXRADFrame(productID uint32, blockCount int) {
	if (productID != 63 && productID != 64) || blockCount < 1 {
		return
	}
	now := time.Now().UTC()
	flightWeatherMu.Lock()
	defer flightWeatherMu.Unlock()
	if flightWeatherFlightID == "" {
		return
	}
	flightWeatherCurrent.Summary["NEXRAD"]++
	count := len(flightWeatherCurrent.NEXRADSnapshots)
	for index := count - 1; index >= 0; index-- {
		current := &flightWeatherCurrent.NEXRADSnapshots[index]
		received, _ := time.Parse(time.RFC3339, current.ReceivedUTC)
		if current.ProductID == productID && now.Sub(received) < flightWeatherRadarInterval {
			current.FrameCount++
			current.BlockCount += blockCount
			return
		}
	}
	if count >= flightWeatherRadarMaximum {
		flightWeatherCurrent.NEXRADSnapshots = append([]flightWeatherRadarRollup(nil), flightWeatherCurrent.NEXRADSnapshots[1:]...)
	}
	flightWeatherCurrent.NEXRADSnapshots = append(flightWeatherCurrent.NEXRADSnapshots, flightWeatherRadarRollup{
		ReceivedUTC: now.Format(time.RFC3339),
		ProductID:   productID,
		FrameCount:  1,
		BlockCount:  blockCount,
	})
}

func snapshotFlightWeather(flightID string) *flightWeatherMemory {
	flightWeatherMu.Lock()
	defer flightWeatherMu.Unlock()
	if flightWeatherFlightID != flightID {
		return nil
	}
	snapshot := flightWeatherMemory{
		CaptureVersion:  flightWeatherCurrent.CaptureVersion,
		TextReports:     append([]flightWeatherTextReport(nil), flightWeatherCurrent.TextReports...),
		NEXRADSnapshots: append([]flightWeatherRadarRollup(nil), flightWeatherCurrent.NEXRADSnapshots...),
		Summary:         make(map[string]int, len(flightWeatherCurrent.Summary)),
	}
	for key, value := range flightWeatherCurrent.Summary {
		snapshot.Summary[key] = value
	}
	return &snapshot
}

func syncFlightWeatherLocked() {
	if flightCurrent == nil {
		return
	}
	flightCurrent.Weather = snapshotFlightWeather(flightCurrent.ID)
}
