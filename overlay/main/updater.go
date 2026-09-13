package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Update server configuration.
const (
	updateCheckURL   = "https://api.stratuxnx.com/v1/update/check"
	updateDownloadURL = "https://api.stratuxnx.com/v1/update/download"
	updateStageDir   = "/boot/firmware/StratuxUpdates"
	updateCheckInterval = 30 * time.Minute
)

// updateInfo is the JSON returned by the update check endpoint.
type updateInfo struct {
	Status          string `json:"status"`
	UpdateAvailable bool   `json:"update_available"`
	Version         string `json:"version"`
	Filename        string `json:"filename"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	PublishedAt     string `json:"published_at"`
	DownloadURL     string `json:"download_url"`
	Message         string `json:"message"`
}

// updateState tracks the current update status for the web UI.
type updateState struct {
	mu              sync.RWMutex
	LastCheck       time.Time   `json:"last_check"`
	CheckError      string      `json:"check_error"`
	Available       *updateInfo `json:"available"`
	Downloading     bool        `json:"downloading"`
	DownloadPercent int         `json:"download_percent"`
	DownloadError   string      `json:"download_error"`
	Staged          bool        `json:"staged"`
	StagedFile      string      `json:"staged_file"`
	CurrentVersion  string      `json:"current_version"`
}

var updaterState = &updateState{}

func initUpdater() {
	updaterState.mu.Lock()
	updaterState.CurrentVersion = stratuxVersion
	updaterState.mu.Unlock()

	go updaterLoop()
}

func updaterLoop() {
	// Check once shortly after boot.
	time.Sleep(60 * time.Second)
	checkForUpdate()

	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		checkForUpdate()
	}
}

func checkForUpdate() {
	updaterState.mu.Lock()
	updaterState.CheckError = ""
	updaterState.mu.Unlock()

	url := fmt.Sprintf("%s?v=%s", updateCheckURL, stratuxVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		updaterState.mu.Lock()
		updaterState.CheckError = fmt.Sprintf("Connection failed: %s", err.Error())
		updaterState.LastCheck = time.Now()
		updaterState.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		updaterState.mu.Lock()
		updaterState.CheckError = fmt.Sprintf("Server returned %d", resp.StatusCode)
		updaterState.LastCheck = time.Now()
		updaterState.mu.Unlock()
		return
	}

	var info updateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		updaterState.mu.Lock()
		updaterState.CheckError = "Invalid response from update server"
		updaterState.LastCheck = time.Now()
		updaterState.mu.Unlock()
		return
	}

	updaterState.mu.Lock()
	updaterState.LastCheck = time.Now()
	if info.UpdateAvailable {
		updaterState.Available = &info
	} else {
		updaterState.Available = nil
	}
	updaterState.mu.Unlock()

	if info.UpdateAvailable {
		log.Printf("UPDATER: new version available: %s (current: %s)\n", info.Version, stratuxVersion)
	}
}

func downloadAndStageUpdate() error {
	updaterState.mu.RLock()
	info := updaterState.Available
	updaterState.mu.RUnlock()

	if info == nil {
		return fmt.Errorf("no update available")
	}

	updaterState.mu.Lock()
	if updaterState.Downloading {
		updaterState.mu.Unlock()
		return fmt.Errorf("download already in progress")
	}
	updaterState.Downloading = true
	updaterState.DownloadPercent = 0
	updaterState.DownloadError = ""
	updaterState.Staged = false
	updaterState.StagedFile = ""
	updaterState.mu.Unlock()

	defer func() {
		updaterState.mu.Lock()
		updaterState.Downloading = false
		updaterState.mu.Unlock()
	}()

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(updateDownloadURL)
	if err != nil {
		updaterState.mu.Lock()
		updaterState.DownloadError = fmt.Sprintf("Download failed: %s", err.Error())
		updaterState.mu.Unlock()
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		msg := fmt.Sprintf("Download returned HTTP %d", resp.StatusCode)
		updaterState.mu.Lock()
		updaterState.DownloadError = msg
		updaterState.mu.Unlock()
		return fmt.Errorf(msg)
	}

	if err := os.MkdirAll(updateStageDir, 0755); err != nil {
		updaterState.mu.Lock()
		updaterState.DownloadError = "Cannot create update directory"
		updaterState.mu.Unlock()
		return err
	}

	filename := info.Filename
	if filename == "" {
		filename = "stratux-nx-update.deb"
	}
	destPath := filepath.Join(updateStageDir, filename)
	tmpPath := destPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		updaterState.mu.Lock()
		updaterState.DownloadError = "Cannot create temp file"
		updaterState.mu.Unlock()
		return err
	}

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	totalSize := info.Size
	if totalSize <= 0 {
		totalSize = resp.ContentLength
	}

	var downloaded int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := writer.Write(buf[:n]); wErr != nil {
				f.Close()
				os.Remove(tmpPath)
				updaterState.mu.Lock()
				updaterState.DownloadError = "Write error"
				updaterState.mu.Unlock()
				return wErr
			}
			downloaded += int64(n)
			if totalSize > 0 {
				pct := int(downloaded * 100 / totalSize)
				if pct > 100 {
					pct = 100
				}
				updaterState.mu.Lock()
				updaterState.DownloadPercent = pct
				updaterState.mu.Unlock()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			f.Close()
			os.Remove(tmpPath)
			updaterState.mu.Lock()
			updaterState.DownloadError = fmt.Sprintf("Download interrupted: %s", readErr.Error())
			updaterState.mu.Unlock()
			return readErr
		}
	}
	f.Close()

	// Verify SHA256 if provided.
	if info.SHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, info.SHA256) {
			os.Remove(tmpPath)
			msg := fmt.Sprintf("SHA256 mismatch: expected %s, got %s", info.SHA256, got)
			updaterState.mu.Lock()
			updaterState.DownloadError = msg
			updaterState.mu.Unlock()
			return fmt.Errorf(msg)
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		updaterState.mu.Lock()
		updaterState.DownloadError = "Failed to stage update file"
		updaterState.mu.Unlock()
		return err
	}

	updaterState.mu.Lock()
	updaterState.DownloadPercent = 100
	updaterState.Staged = true
	updaterState.StagedFile = destPath
	updaterState.mu.Unlock()

	log.Printf("UPDATER: update %s staged at %s\n", info.Version, destPath)
	return nil
}

// HTTP handlers for the web UI.

func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	go checkForUpdate()

	// Return current state immediately.
	time.Sleep(200 * time.Millisecond)

	updaterState.mu.RLock()
	defer updaterState.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updaterState)
}

func handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	updaterState.mu.RLock()
	defer updaterState.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updaterState)
}

func handleUpdateInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		if err := downloadAndStageUpdate(); err != nil {
			log.Printf("UPDATER: install failed: %s\n", err.Error())
			return
		}
		// Trigger reboot after staging.
		log.Println("UPDATER: update staged, rebooting in 3 seconds...")
		time.Sleep(3 * time.Second)
		reboot()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Update download started. The device will reboot automatically.",
	})
}
