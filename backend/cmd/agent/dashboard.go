package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
			"status":  GetStatus(),
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
			if !c.Valid() {
				httpJSON(w, 400, map[string]any{"ok": false, "error": "cloud_url, agent_id, agent_token are required"})
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

// Suppress unused-import lint in some builds
var _ = context.Background
