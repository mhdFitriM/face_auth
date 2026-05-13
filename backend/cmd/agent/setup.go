package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// setupServerAddr is the loopback address the first-run UI binds to. We use
// 0 to let the OS pick a free port, then read it back from the listener.
const setupBindAddr = "127.0.0.1:7780"

// runSetupUI starts a one-page setup wizard on http://127.0.0.1:7780 and
// opens the user's default browser. It blocks until the user saves config
// and clicks Install (or closes the wizard manually). Returns the saved
// config on success.
func runSetupUI() (*AgentConfig, bool, error) {
	saved := make(chan *AgentConfig, 1)
	install := make(chan bool, 1)
	done := make(chan struct{})

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		current, _ := loadConfig()
		if current == nil {
			current = &AgentConfig{
				CloudURL:      "https://face_auth.example.com",
				AgentName:     hostnameOr("LAN agent"),
				QRStripPrefix: "in#",
				QRListenPort:  "7771",
			}
		}
		_ = setupTpl.Execute(w, map[string]any{
			"Current":  current,
			"OS":       runtime.GOOS,
			"Hostname": hostnameOr(""),
			"ConfPath": configPath(),
		})
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

	mux.HandleFunc("/api/save", func(w http.ResponseWriter, r *http.Request) {
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
		saved <- &c
		httpJSON(w, 200, map[string]any{"ok": true, "configPath": configPath()})
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
		install <- true
		httpJSON(w, 200, map[string]any{"ok": true, "output": out})
	})

	mux.HandleFunc("/api/skip-install", func(w http.ResponseWriter, r *http.Request) {
		install <- false
		httpJSON(w, 200, map[string]any{"ok": true})
	})

	srv := &http.Server{Addr: setupBindAddr, Handler: mux}
	go func() {
		log.Printf("setup UI: http://%s", setupBindAddr)
		_ = srv.ListenAndServe()
	}()

	// Open the browser
	go func() {
		time.Sleep(400 * time.Millisecond)
		openBrowser("http://" + setupBindAddr)
	}()

	var cfg *AgentConfig
	var didInstall bool

	// Wait: first save, then install/skip
	select {
	case cfg = <-saved:
	case <-time.After(30 * time.Minute):
		_ = srv.Shutdown(context.Background())
		return nil, false, fmt.Errorf("setup UI timeout")
	}
	select {
	case didInstall = <-install:
	case <-time.After(5 * time.Minute):
	}

	// Give browser ~1 s to receive the final response, then close server
	go func() {
		time.Sleep(1 * time.Second)
		_ = srv.Shutdown(context.Background())
		close(done)
	}()
	<-done
	return cfg, didInstall, nil
}

// testConnect attempts a brief WebSocket handshake against the cloud to
// confirm the URL + token are valid.
func testConnect(c *AgentConfig) (bool, error) {
	u, err := url.Parse(c.CloudURL)
	if err != nil {
		return false, err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	if !strings.HasSuffix(u.Path, "/agent/ws") {
		u.Path = strings.TrimRight(u.Path, "/") + "/agent/ws"
	}
	q := u.Query()
	q.Set("id", c.AgentID)
	q.Set("token", c.AgentToken)
	u.RawQuery = q.Encode()

	d := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 6 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, _, err := d.DialContext(ctx, u.String(), nil)
	if err != nil {
		return false, err
	}
	_ = conn.Close()
	return true, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func httpJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func hostnameOr(fallback string) string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return fallback
	}
	return h
}
