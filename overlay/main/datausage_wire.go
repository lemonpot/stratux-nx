package main

import "encoding/json"

// The Stratux web UI historically consumes capitalized Go field names.
// These marshalers keep the dashboard API explicit while the internal structs
// retain lower-case JSON tags for backwards-compatible config decoding.

func (s dataUsageSettings) MarshalJSON() ([]byte, error) {
	type wire struct {
		WarningMB        uint64
		AutoBlockMB      uint64
		SessionLimitMB   uint64
		AutoBlockEnabled bool
	}
	return json.Marshal(wire{s.WarningMB, s.AutoBlockMB, s.SessionLimitMB, s.AutoBlockEnabled})
}

func (c dataUsageClient) MarshalJSON() ([]byte, error) {
	type wire struct {
		IP             string
		MAC            string
		Hostname       string
		Connected      bool
		Blocked        bool
		UploadBytes    uint64
		DownloadBytes  uint64
		TotalBytes     uint64
		UploadBps      float64
		DownloadBps    float64
		LastSeen       string
		WarningReached bool
	}
	return json.Marshal(wire{c.IP, c.MAC, c.Hostname, c.Connected, c.Blocked, c.UploadBytes, c.DownloadBytes, c.TotalBytes, c.UploadBps, c.DownloadBps, c.LastSeen, c.WarningReached})
}

func (e dataUsageLogRecord) MarshalJSON() ([]byte, error) {
	type wire struct {
		Time     string
		Type     string
		IP       string `json:",omitempty"`
		MAC      string `json:",omitempty"`
		Hostname string `json:",omitempty"`
		Message  string
		Bytes    uint64 `json:",omitempty"`
	}
	return json.Marshal(wire{e.Time, e.Type, e.IP, e.MAC, e.Hostname, e.Message, e.Bytes})
}

func (r dataUsageResponse) MarshalJSON() ([]byte, error) {
	type wire struct {
		SessionStart        string
		SessionSeconds      int64
		TotalBytes          uint64
		UploadBytes         uint64
		DownloadBytes       uint64
		UploadBps           float64
		DownloadBps         float64
		WANOnline           bool
		APInterface         string
		WANInterface        string
		Settings            dataUsageSettings
		Clients             []dataUsageClient
		RecentEvents        []dataUsageLogRecord
		PersistentLogPath   string
		MonitoringActive    bool
		SessionLimitReached bool
	}
	return json.Marshal(wire{
		r.SessionStart, r.SessionSeconds, r.TotalBytes, r.UploadBytes, r.DownloadBytes,
		r.UploadBps, r.DownloadBps, r.WANOnline, r.APInterface, r.WANInterface,
		r.Settings, r.Clients, r.RecentEvents, r.PersistentLogPath, r.MonitoringActive,
		r.SessionLimitReached,
	})
}
