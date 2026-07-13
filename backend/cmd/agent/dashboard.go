package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// runDashboardServer starts the always-on local web server on 127.0.0.1:7780.
// It serves:
//   - GET  /              dashboard (status, logs, config) — or wizard if no config
//   - GET  /manifest.json + meta tags so the page is installable as a PWA
//   - GET  /api/status    current connection state
//   - GET  /api/logs      snapshot of recent log lines
//   - GET  /api/logs/stream   SSE feed of new log lines
//   - GET  /api/config    current config (token redacted)
//   - POST /api/config    update config + reconnect
//   - POST /api/test      test cloud connectivity
//   - POST /api/reset     wipe config and reload (back to wizard)
//   - POST /api/install   install as system service
//
// Returns an error chan that fires when the user clicks "Reset config" or
// changes config, so main() can decide what to do.
func runDashboardServer(reconnect chan<- struct{}) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := loadConfig()
		if cfg == nil || !cfg.Valid() {
			renderWizard(w, cfg)
			return
		}
		renderDashboard(w, cfg)
	})

	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		_, _ = w.Write([]byte(manifestJSON))
	})

	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(serviceWorkerJS))
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := loadConfig()
		out := map[string]any{
			"status":    GetStatus(),
			"hasConfig": cfg != nil && cfg.Valid(),
		}
		if cfg != nil {
			redacted := *cfg
			if len(redacted.AgentToken) > 6 {
				redacted.AgentToken = redacted.AgentToken[:6] + "…"
			}
			out["config"] = redacted
			out["configPath"] = configPath()
		}
		httpJSON(w, 200, out)
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		httpJSON(w, 200, logBuf.Snapshot())
	})

	mux.HandleFunc("/api/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Backfill recent
		for _, e := range logBuf.Snapshot() {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		flusher.Flush()

		ch := logBuf.Subscribe()
		defer logBuf.Unsubscribe(ch)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case e, ok := <-ch:
				if !ok {
					return
				}
				b, _ := json.Marshal(e)
				if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
					return
				}
				flusher.Flush()
			case <-ticker.C:
				_, _ = fmt.Fprintf(w, ": keep-alive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			cfg, _ := loadConfig()
			if cfg == nil {
				cfg = &AgentConfig{}
			}
			httpJSON(w, 200, cfg)
		case "POST", "PUT":
			var c AgentConfig
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				httpJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			// General settings forms predate hardware bindings. Preserve them when
			// the field is omitted so saving a cloud URL cannot unbind scanners.
			if c.ScannerBindings == nil {
				if old, _ := loadConfig(); old != nil {
					c.ScannerBindings = old.ScannerBindings
				}
			}
			if !c.Valid() {
				httpJSON(w, 400, map[string]any{"ok": false, "error": "cloud_url, agent_id, agent_token, and target device when direct opening is enabled are required"})
				return
			}
			if err := saveConfig(&c); err != nil {
				httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			httpJSON(w, 200, map[string]any{"ok": true})
			// Signal the agent loop to reconnect with the new config
			select {
			case reconnect <- struct{}{}:
			default:
			}
		default:
			http.Error(w, "GET or POST", 405)
		}
	})

	// The first-run wizard posts to /api/save. The always-on dashboard can
	// render that same wizard when no config exists, so keep this alias here.
	mux.HandleFunc("/api/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			httpJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
			return
		}
		var c AgentConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			httpJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if !c.Valid() {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "cloud_url, agent_id, agent_token, and target device when direct opening is enabled are required"})
			return
		}
		if err := saveConfig(&c); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true, "configPath": configPath()})
		select {
		case reconnect <- struct{}{}:
		default:
		}
	})

	mux.HandleFunc("/api/skip-install", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			httpJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		var c AgentConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			httpJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		ok, err := testConnect(&c)
		if !ok {
			httpJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true})
	})

	// Native scanner management. These endpoints are loopback-only because the
	// dashboard server itself binds to 127.0.0.1. A binding maps physical HID
	// hardware to an existing cloud scannerId; device and door stay cloud-owned.
	mux.HandleFunc("/api/scanners", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		cfg, _ := loadConfig()
		if cfg == nil {
			cfg = &AgentConfig{}
		}
		devices := refreshPhysicalScanners()
		points := []agentAccessPoint{}
		if cfg.Valid() {
			points = fetchAgentAccessPoints(strings.TrimRight(cfg.CloudURL, "/"), cfg.AgentID, cfg.AgentToken)
		}
		_, last := scannerSnapshot()
		httpJSON(w, 200, map[string]any{"devices": devices, "bindings": cfg.ScannerBindings, "accessPoints": points, "lastHardwareId": last, "nativeSupported": runtimeGOOS == "windows"})
	})

	mux.HandleFunc("/api/scanners/identify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if runtimeGOOS == "windows" {
			// Prevent a valid test QR from also passing through the legacy global
			// AHK hook and opening its old single-scanner target.
			_ = qrWatcherStop()
		}
		beginScannerIdentify()
		httpJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/scanners/bind", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body ScannerBinding
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ScannerID == "" || body.HardwareID == "" {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "scanner_id and hardware_id are required"})
			return
		}
		cfg, _ := loadConfig()
		if cfg == nil || !cfg.Valid() {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "configure the agent first"})
			return
		}
		points := fetchAgentAccessPoints(strings.TrimRight(cfg.CloudURL, "/"), cfg.AgentID, cfg.AgentToken)
		validScanner := false
		for _, p := range points {
			if p.ScannerID == body.ScannerID && p.Enabled && p.AuthMode == "faceauth_qr" {
				validScanner = true
				break
			}
		}
		if !validScanner {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "scanner ID is not an enabled FaceAuth QR access point for this agent"})
			return
		}
		connectedHardware := false
		for _, d := range refreshPhysicalScanners() {
			if strings.EqualFold(d.HardwareID, body.HardwareID) {
				connectedHardware = true
				body.HardwareID = d.HardwareID
				body.ReaderMode = d.ReaderMode
				break
			}
		}
		if !connectedHardware {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "physical scanner is no longer connected"})
			return
		}
		bindings := make([]ScannerBinding, 0, len(cfg.ScannerBindings)+1)
		for _, b := range cfg.ScannerBindings {
			if b.ScannerID != body.ScannerID && !strings.EqualFold(b.HardwareID, body.HardwareID) {
				bindings = append(bindings, b)
			}
		}
		cfg.ScannerBindings = append(bindings, body)
		if err := saveConfig(cfg); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true})
		select {
		case reconnect <- struct{}{}:
		default:
		}
	})

	mux.HandleFunc("/api/scanners/unbind", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ScannerID string `json:"scanner_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cfg, _ := loadConfig()
		if cfg == nil {
			httpJSON(w, 404, map[string]any{"ok": false, "error": "config not found"})
			return
		}
		bindings := cfg.ScannerBindings[:0]
		for _, b := range cfg.ScannerBindings {
			if b.ScannerID != body.ScannerID {
				bindings = append(bindings, b)
			}
		}
		cfg.ScannerBindings = bindings
		if err := saveConfig(cfg); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true})
		select {
		case reconnect <- struct{}{}:
		default:
		}
	})

	// ─── Scanner stations (local-only scanner→door config) ────────────────────
	// A station stores device ID + door number locally; no cloud access point
	// required. The agent opens the door directly after the cloud validates QR.

	mux.HandleFunc("/api/stations", func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := loadConfig()
		if cfg == nil {
			cfg = &AgentConfig{}
		}
		switch r.Method {
		case http.MethodGet:
			httpJSON(w, 200, map[string]any{"stations": cfg.ScannerStations})
		case http.MethodPost:
			var st ScannerStation
			if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
				httpJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if st.DeviceID == "" {
				httpJSON(w, 400, map[string]any{"ok": false, "error": "device_id is required"})
				return
			}
			if st.Port == 0 {
				st.Port = nextStationPort(cfg.ScannerStations)
			}
			if st.ID == "" {
				st.ID = newShortID()
			}
			// Replace if same ID exists, otherwise append.
			replaced := false
			for i, s := range cfg.ScannerStations {
				if s.ID == st.ID {
					cfg.ScannerStations[i] = st
					replaced = true
					break
				}
			}
			if !replaced {
				cfg.ScannerStations = append(cfg.ScannerStations, st)
			}
			if err := saveConfig(cfg); err != nil {
				httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			httpJSON(w, 200, map[string]any{"ok": true, "station": st})
			select {
			case reconnect <- struct{}{}:
			default:
			}
		default:
			http.Error(w, "GET or POST", 405)
		}
	})

	mux.HandleFunc("/api/stations/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "id is required"})
			return
		}
		cfg, _ := loadConfig()
		if cfg == nil {
			httpJSON(w, 404, map[string]any{"ok": false, "error": "no config"})
			return
		}
		stations := cfg.ScannerStations[:0]
		for _, s := range cfg.ScannerStations {
			if s.ID != body.ID {
				stations = append(stations, s)
			}
		}
		cfg.ScannerStations = stations
		if err := saveConfig(cfg); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true})
		select {
		case reconnect <- struct{}{}:
		default:
		}
	})

	mux.HandleFunc("/api/stations/assign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var body struct {
			StationID  string `json:"station_id"`
			HardwareID string `json:"hardware_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.StationID == "" {
			httpJSON(w, 400, map[string]any{"ok": false, "error": "station_id is required"})
			return
		}
		cfg, _ := loadConfig()
		if cfg == nil {
			httpJSON(w, 404, map[string]any{"ok": false, "error": "no config"})
			return
		}
		found := false
		for i, s := range cfg.ScannerStations {
			if s.ID == body.StationID {
				cfg.ScannerStations[i].HardwareID = body.HardwareID
				found = true
				break
			}
		}
		if !found {
			httpJSON(w, 404, map[string]any{"ok": false, "error": "station not found"})
			return
		}
		if err := saveConfig(cfg); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true})
		select {
		case reconnect <- struct{}{}:
		default:
		}
	})

	mux.HandleFunc("/api/reset", func(w http.ResponseWriter, r *http.Request) {
		if err := os.Remove(configPath()); err != nil && !os.IsNotExist(err) {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		log.Printf("dashboard: config reset by user")
		httpJSON(w, 200, map[string]any{"ok": true})
		// Trigger reconnect — current loop will see empty config and pause.
		go func() {
			time.Sleep(200 * time.Millisecond)
			select {
			case reconnect <- struct{}{}:
			default:
			}
		}()
	})

	mux.HandleFunc("/api/install", func(w http.ResponseWriter, r *http.Request) {
		exePath, err := os.Executable()
		if err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		out, err := installAsService(exePath)
		if err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error(), "output": out})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true, "output": out})
	})

	// ─── QR watcher (USB scanner → agent) ─────────────────────────
	// The watcher is an AutoHotkey v2 script. We bundle it inside the agent
	// binary so the user never has to download it separately; the dashboard
	// installs it into the user's Startup folder and starts it, so it runs
	// on every boot.

	mux.HandleFunc("/qr-watcher.ahk", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="face_auth-qr-watcher.ahk"`)
		_, _ = w.Write(renderedQRWatcherAHK())
	})

	mux.HandleFunc("/api/qr-watcher/status", func(w http.ResponseWriter, r *http.Request) {
		httpJSON(w, 200, qrWatcherStatus())
	})

	mux.HandleFunc("/api/qr-watcher/install", func(w http.ResponseWriter, r *http.Request) {
		st, err := qrWatcherInstall()
		if err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error(), "status": st})
			return
		}
		log.Printf("dashboard: QR watcher installed at %s (autohotkey=%v running=%v)", st.StartupPath, st.AutoHotkeyInstalled, st.Running)
		httpJSON(w, 200, map[string]any{"ok": true, "status": st})
	})

	mux.HandleFunc("/api/qr-watcher/start", func(w http.ResponseWriter, r *http.Request) {
		if err := qrWatcherStart(); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error(), "status": qrWatcherStatus()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true, "status": qrWatcherStatus()})
	})

	mux.HandleFunc("/api/qr-watcher/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := qrWatcherStop(); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error(), "status": qrWatcherStatus()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true, "status": qrWatcherStatus()})
	})

	mux.HandleFunc("/api/qr-watcher/uninstall", func(w http.ResponseWriter, r *http.Request) {
		if err := qrWatcherUninstall(); err != nil {
			httpJSON(w, 500, map[string]any{"ok": false, "error": err.Error(), "status": qrWatcherStatus()})
			return
		}
		httpJSON(w, 200, map[string]any{"ok": true, "status": qrWatcherStatus()})
	})

	addr := getenv("DASHBOARD_ADDR", "127.0.0.1:7780")
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("dashboard: http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("dashboard: %v", err)
		}
	}()
}

func renderWizard(w http.ResponseWriter, current *AgentConfig) {
	if current == nil {
		current = &AgentConfig{
			CloudURL:      "http://localhost:8080",
			AgentName:     hostnameOr("LAN agent"),
			QRStripPrefix: "in#",
			QRListenPort:  "7771",
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = setupTpl.Execute(w, map[string]any{
		"Current":  current,
		"OS":       osName(),
		"Hostname": hostnameOr(""),
		"ConfPath": configPath(),
	})
}

func renderDashboard(w http.ResponseWriter, cfg *AgentConfig) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTpl.Execute(w, map[string]any{
		"Config":   cfg,
		"OS":       osName(),
		"ConfPath": configPath(),
		"Hostname": hostnameOr(""),
	})
}

func osName() string {
	return runtimeGOOS
}

// helper so the wizard template can also call openBrowser via ctx wait elsewhere
func waitAndOpenBrowser(url string) {
	go func() {
		time.Sleep(400 * time.Millisecond)
		openBrowser(url)
	}()
}

// nextStationPort returns the lowest unused port starting from 7771.
func nextStationPort(stations []ScannerStation) int {
	used := map[int]bool{}
	for _, s := range stations {
		used[s.Port] = true
	}
	for p := 7771; ; p++ {
		if !used[p] {
			return p
		}
	}
}

// newShortID returns an 8-char random hex string suitable for station IDs.
func newShortID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Suppress unused-import lint in some builds
var _ = context.Background
