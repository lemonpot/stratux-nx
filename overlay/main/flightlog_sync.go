package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	flightCloudBaseURL      = "https://api.stratuxnx.com/v1"
	flightSyncRetryInterval = 30 * time.Second
	flightLinkCodeAlphabet  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	// The FAT copy is a legacy import source, so a lost ext4 identity is restored
	// from it instead of creating a new, unlinked installation.
	flightIdentityBackupDir = "/boot/firmware/stratux-flight-log"
)

type flightInstallationIdentity struct {
	InstallationID string `json:"InstallationID"`
	PublicKey      string `json:"PublicKey"`
	PrivateKey     string `json:"PrivateKey"`
	CreatedAtUTC   string `json:"CreatedAtUTC"`
	LinkCode       string `json:"LinkCode,omitempty"`
}

type flightSyncPublicState struct {
	Enabled        bool   `json:"Enabled"`
	InstallationID string `json:"InstallationID"`
	Online         bool   `json:"Online"`
	Pending        int    `json:"Pending"`
	LastAttemptUTC string `json:"LastAttemptUTC,omitempty"`
	LastError      string `json:"LastError,omitempty"`
	Linked         bool   `json:"Linked"`
	AccountName    string `json:"AccountName,omitempty"`
	AccountEmail   string `json:"AccountEmail,omitempty"`
	LinkCode       string `json:"LinkCode,omitempty"`
	LinkURL        string `json:"LinkURL,omitempty"`
}

type flightSyncConfig struct {
	Enabled      bool   `json:"Enabled"`
	Linked       bool   `json:"Linked,omitempty"`
	AccountName  string `json:"AccountName,omitempty"`
	AccountEmail string `json:"AccountEmail,omitempty"`
}

var (
	flightIdentity     flightInstallationIdentity
	flightSyncEnabled  bool
	flightSyncOnline   bool
	flightSyncLastTry  string
	flightSyncLastErr  string
	flightSyncLinked   bool
	flightSyncAccountName string
	flightSyncAccountEmail string
	flightIdentityPath string
	flightSyncPath     string
	flightSyncWake     = make(chan struct{}, 1)
	flightCloudClockMu sync.Mutex
	flightCloudOffset  int64
	flightCloudClockAt time.Time
)

func init() {
	http.HandleFunc("/flightLog/sync", handleFlightLogSync)
	go flightSyncLoop()
}

func flightSyncInitializeLocked() {
	flightIdentityPath = filepath.Join(flightStorageDir, "installation.json")
	flightSyncPath = filepath.Join(flightStorageDir, "cloud-sync.json")

	if b, err := os.ReadFile(flightIdentityPath); err == nil {
		_ = json.Unmarshal(b, &flightIdentity)
	}
	changed := false
	if flightIdentity.InstallationID == "" || flightIdentity.PublicKey == "" || flightIdentity.PrivateKey == "" {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err == nil {
			flightIdentity = flightInstallationIdentity{
				InstallationID: newFlightInstallationID(),
				PublicKey:      base64.RawURLEncoding.EncodeToString(publicKey),
				PrivateKey:     base64.RawURLEncoding.EncodeToString(privateKey),
				CreatedAtUTC:   time.Now().UTC().Format(time.RFC3339),
			}
			changed = true
		}
	}
	if flightIdentity.InstallationID != "" && !validFlightLinkCode(flightIdentity.LinkCode) {
		flightIdentity.LinkCode = newFlightLinkCode()
		changed = true
	}
	if b, err := json.MarshalIndent(flightIdentity, "", "  "); err == nil && flightIdentity.InstallationID != "" {
		if changed {
			if writeErr := writeFlightFileAtomic(flightIdentityPath, b, 0600); writeErr != nil {
				setFlightStorageErrorLocked("Cannot save device identity: " + writeErr.Error())
			}
		}
		backupFlightIdentityLocked(b)
	}

	var cfg flightSyncConfig
	if b, err := os.ReadFile(flightSyncPath); err == nil && json.Unmarshal(b, &cfg) == nil {
		flightSyncEnabled = cfg.Enabled
		flightSyncLinked = cfg.Linked
		flightSyncAccountName = cfg.AccountName
		flightSyncAccountEmail = cfg.AccountEmail
	}
}

func backupFlightIdentityLocked(body []byte) {
	if filepath.Clean(flightStorageDir) == flightIdentityBackupDir {
		return
	}
	if _, err := os.Stat(filepath.Dir(flightIdentityBackupDir)); err != nil {
		return
	}
	path := filepath.Join(flightIdentityBackupDir, "installation.json")
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, body) {
		return
	}
	if err := os.MkdirAll(flightIdentityBackupDir, 0755); err == nil {
		_ = writeFlightFileAtomic(path, body, 0600)
	}
}

func newFlightLinkCode() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	code := make([]byte, 0, 9)
	for i, value := range raw {
		if i == 4 {
			code = append(code, '-')
		}
		code = append(code, flightLinkCodeAlphabet[int(value)%len(flightLinkCodeAlphabet)])
	}
	return string(code)
}

func validFlightLinkCode(code string) bool {
	if len(code) != 9 || code[4] != '-' {
		return false
	}
	for i := 0; i < len(code); i++ {
		if i != 4 && !strings.ContainsRune(flightLinkCodeAlphabet, rune(code[i])) {
			return false
		}
	}
	return true
}

func newFlightInstallationID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("NX-%d", time.Now().UTC().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("NX-%s-%s-%s-%s-%s",
		hex.EncodeToString(raw[0:4]),
		hex.EncodeToString(raw[4:6]),
		hex.EncodeToString(raw[6:8]),
		hex.EncodeToString(raw[8:10]),
		hex.EncodeToString(raw[10:16]),
	))
}

func saveFlightSyncConfigLocked() {
	b, _ := json.MarshalIndent(flightSyncConfig{
		Enabled:      flightSyncEnabled,
		Linked:       flightSyncLinked,
		AccountName:  flightSyncAccountName,
		AccountEmail: flightSyncAccountEmail,
	}, "", "  ")
	if err := writeFlightFileAtomic(flightSyncPath, b, 0600); err != nil {
		setFlightStorageErrorLocked("Cannot save cloud sync settings: " + err.Error())
	}
}

func flightSyncCompletedStatusLocked() string {
	if flightSyncEnabled {
		return "pending"
	}
	return "local"
}

func flightSyncPublicStateLocked() flightSyncPublicState {
	pending := 0
	for i := range flightHistory {
		if flightHistory[i].SyncStatus == "pending" || flightHistory[i].SyncStatus == "failed" {
			pending++
		}
	}
	return flightSyncPublicState{
		Enabled:        flightSyncEnabled,
		InstallationID: flightIdentity.InstallationID,
		Online:         flightSyncOnline,
		Pending:        pending,
		LastAttemptUTC: flightSyncLastTry,
		LastError:      flightSyncLastErr,
		Linked:         flightSyncLinked,
		AccountName:    flightSyncAccountName,
		AccountEmail:   flightSyncAccountEmail,
		LinkCode:       flightIdentity.LinkCode,
		LinkURL:        flightLinkURL(flightIdentity.LinkCode),
	}
}

func flightLinkURL(code string) string {
	if code == "" {
		return ""
	}
	return "https://app.stratuxnx.com/?claim=" + code
}

func wakeFlightSync() {
	select {
	case flightSyncWake <- struct{}{}:
	default:
	}
}

func flightSyncLoop() {
	time.Sleep(20 * time.Second)
	ticker := time.NewTicker(flightSyncRetryInterval)
	defer ticker.Stop()
	for {
		syncPendingFlight()
		select {
		case <-ticker.C:
		case <-flightSyncWake:
		}
	}
}

func syncPendingFlight() {
	initializeFlightLog()
	flightLogMu.Lock()
	if !flightSyncEnabled || flightIdentity.InstallationID == "" {
		flightLogMu.Unlock()
		return
	}
	identity := flightIdentity
	flightSyncLastTry = time.Now().UTC().Format(time.RFC3339)
	flightLogMu.Unlock()

	if err := registerFlightInstallation(identity); err != nil {
		flightLogMu.Lock()
		flightSyncOnline = false
		flightSyncLastErr = compactFlightSyncError(err)
		flightLogMu.Unlock()
		return
	}

	flightLogMu.Lock()
	flightSyncOnline = true
	flightSyncLastErr = ""
	var summary *flightSummary
	for i := range flightHistory {
		if flightHistory[i].SyncStatus == "pending" || flightHistory[i].SyncStatus == "failed" || flightHistory[i].SyncStatus == "local" || flightHistory[i].SyncStatus == "" {
			copy := flightHistory[i]
			summary = &copy
			flightHistory[i].SyncStatus = "uploading"
			break
		}
	}
	if summary == nil {
		flightLogMu.Unlock()
		return
	}
	saveFlightIndexLocked()
	flightLogMu.Unlock()

	recordPath := filepath.Join(flightFilesDir, summary.ID+".json")
	body, err := os.ReadFile(recordPath)
	contentHash := ""
	if err == nil {
		sum := sha256.Sum256(body)
		contentHash = hex.EncodeToString(sum[:])
	}
	var remoteID string
	if err == nil {
		remoteID, err = uploadFlightRecord(identity, body)
	}

	flightLogMu.Lock()
	defer flightLogMu.Unlock()
	for i := range flightHistory {
		if flightHistory[i].ID != summary.ID {
			continue
		}
		if err != nil {
			flightHistory[i].SyncStatus = "failed"
			flightHistory[i].SyncError = compactFlightSyncError(err)
			flightSyncLastErr = flightHistory[i].SyncError
		} else {
			flightHistory[i].SyncStatus = "uploaded"
			flightHistory[i].SyncError = ""
			flightHistory[i].RemoteID = remoteID
			flightHistory[i].UploadedAtUTC = time.Now().UTC().Format(time.RFC3339)
			flightHistory[i].ContentHash = contentHash
		}
		break
	}
	saveFlightIndexLocked()
}

func compactFlightSyncError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 180 {
		message = message[:180]
	}
	return message
}

func registerFlightInstallation(identity flightInstallationIdentity) error {
	payload := map[string]string{
		"installation_id": identity.InstallationID,
		"public_key":      identity.PublicKey,
		"created_at_utc":  identity.CreatedAtUTC,
		"software_version": stratuxVersion,
		"link_code":       identity.LinkCode,
	}
	body, _ := json.Marshal(payload)
	resp, err := flightCloudRequest(http.MethodPost, flightCloudBaseURL+"/installations/register", identity, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return flightCloudHTTPError(resp)
	}
	var result struct {
		Account struct {
			Linked bool   `json:"linked"`
			Name   string `json:"name"`
			Email  string `json:"email"`
		} `json:"account"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return fmt.Errorf("cloud returned an invalid installation response")
	}
	name := strings.TrimSpace(result.Account.Name)
	email := strings.TrimSpace(result.Account.Email)
	flightLogMu.Lock()
	if flightSyncLinked != result.Account.Linked || flightSyncAccountName != name || flightSyncAccountEmail != email {
		flightSyncLinked = result.Account.Linked
		flightSyncAccountName = name
		flightSyncAccountEmail = email
		saveFlightSyncConfigLocked()
	}
	flightLogMu.Unlock()
	return nil
}

func uploadFlightRecord(identity flightInstallationIdentity, record []byte) (string, error) {
	resp, err := flightCloudRequest(http.MethodPost, flightCloudBaseURL+"/flights/upload", identity, record)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", flightCloudHTTPError(resp)
	}
	var result struct {
		FlightID string `json:"flight_id"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil || result.FlightID == "" {
		return "", fmt.Errorf("cloud returned an invalid flight response")
	}
	return result.FlightID, nil
}

func flightCloudRequest(method, url string, identity flightInstallationIdentity, body []byte) (*http.Response, error) {
	syncFlightCloudClock(false)
	resp, err := signedFlightCloudRequest(method, url, identity, body)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		syncFlightCloudClock(true)
		return signedFlightCloudRequest(method, url, identity, body)
	}
	return resp, err
}

func signedFlightCloudRequest(method, url string, identity flightInstallationIdentity, body []byte) (*http.Response, error) {
	privateKey, err := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid installation private key")
	}
	flightCloudClockMu.Lock()
	timestamp := fmt.Sprintf("%d", time.Now().UTC().Unix()+flightCloudOffset)
	flightCloudClockMu.Unlock()
	signed := append([]byte(timestamp+"\n"), body...)
	signature := ed25519.Sign(ed25519.PrivateKey(privateKey), signed)
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Installation-ID", identity.InstallationID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", base64.RawURLEncoding.EncodeToString(signature))
	return (&http.Client{Timeout: 20 * time.Second}).Do(req)
}

func syncFlightCloudClock(force bool) {
	flightCloudClockMu.Lock()
	if !force && !flightCloudClockAt.IsZero() && time.Since(flightCloudClockAt) < 5*time.Minute {
		flightCloudClockMu.Unlock()
		return
	}
	flightCloudClockMu.Unlock()

	req, err := http.NewRequest(http.MethodGet, flightCloudBaseURL+"/ping", nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
	serverTime, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return
	}

	flightCloudClockMu.Lock()
	flightCloudOffset = serverTime.Unix() - time.Now().UTC().Unix()
	flightCloudClockAt = time.Now()
	flightCloudClockMu.Unlock()
}

func flightCloudHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	message := strings.TrimSpace(string(body))
	if message == "" || strings.HasPrefix(message, "<") {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("cloud returned %d: %s", resp.StatusCode, message)
}

func handleFlightLogSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	initializeFlightLog()
	var request struct {
		Action string `json:"action"`
	}
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	flightLogMu.Lock()
	switch request.Action {
	case "enable":
		flightSyncEnabled = true
		for i := range flightHistory {
			if flightHistory[i].SyncStatus == "" || flightHistory[i].SyncStatus == "local" {
				flightHistory[i].SyncStatus = "pending"
			}
		}
		saveFlightSyncConfigLocked()
		saveFlightIndexLocked()
	case "disable":
		flightSyncEnabled = false
		saveFlightSyncConfigLocked()
	case "retry":
		for i := range flightHistory {
			if flightHistory[i].SyncStatus == "failed" {
				flightHistory[i].SyncStatus = "pending"
				flightHistory[i].SyncError = ""
			}
		}
		saveFlightIndexLocked()
	default:
		flightLogMu.Unlock()
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	state := flightSyncPublicStateLocked()
	flightLogMu.Unlock()
	if request.Action != "disable" {
		wakeFlightSync()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(state)
}
