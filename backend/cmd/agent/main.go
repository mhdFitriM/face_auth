package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"face_auth/internal"

	"github.com/gorilla/websocket"
)

func main() {
	// Install ring-buffer log capture so the dashboard can stream logs.
	log.SetOutput(logBuf)

	isService := false
	// CLI flag dispatch
	for _, a := range os.Args[1:] {
		switch a {
		case "--service":
			isService = true
		case "--help", "-h":
			fmt.Println("face_auth-agent: LAN-side bridge for face_auth cloud.")
			fmt.Println("  Run with no args            → opens the dashboard in your browser and connects")
			fmt.Println("  --service                   → run silently as installed system service (no browser)")
			fmt.Println("  Env vars override config:   CLOUD_URL, AGENT_ID, AGENT_TOKEN, AGENT_NAME,")
			fmt.Println("                              QR_STRIP_PREFIX, QR_STRIP_SUFFIX, QR_LISTEN_PORT, QR_DEVICE")
			return
		}
	}

	// Dashboard always runs — config wizard or status view depending on state.
	reconnect := make(chan struct{}, 4)
	runDashboardServer(reconnect)
	if !isService {
		waitAndOpenBrowser("http://127.0.0.1:7780")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down...")
		cancel()
	}()

	// Outer loop: load config, run connect loop until reconnect signal or ctx done.
	for ctx.Err() == nil {
		if err := runConnectionLoop(ctx, reconnect); err != nil {
			log.Printf("connection loop ended: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		default:
			// loop and reload config
		}
	}
}

// runConnectionLoop loads config, dials the cloud with exponential backoff,
// and returns when either the parent ctx is cancelled or a reconnect signal
// fires (config changed via the dashboard).
func runConnectionLoop(parent context.Context, reconnect <-chan struct{}) error {
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = &AgentConfig{}
	}
	if v := getenv("CLOUD_URL", ""); v != "" {
		cfg.CloudURL = v
	}
	if v := getenv("AGENT_ID", ""); v != "" {
		cfg.AgentID = v
	}
	if v := getenv("AGENT_TOKEN", ""); v != "" {
		cfg.AgentToken = v
	}
	if v := getenv("AGENT_NAME", ""); v != "" {
		cfg.AgentName = v
	}
	if v := getenv("QR_STRIP_PREFIX", ""); v != "" {
		cfg.QRStripPrefix = v
	}
	if v := getenv("QR_STRIP_SUFFIX", ""); v != "" {
		cfg.QRStripSuffix = v
	}
	if v := getenv("QR_LISTEN_PORT", ""); v != "" {
		cfg.QRListenPort = v
	}
	if v := getenv("QR_DEVICE", ""); v != "" {
		cfg.QRDevice = v
	}

	// If still no usable config, wait for the user to fill in the dashboard's
	// wizard. The dashboard fires `reconnect` once /api/config is saved.
	if !cfg.Valid() {
		SetStatus("disconnected", "no config — open the dashboard to configure")
		log.Printf("no config yet — open http://127.0.0.1:7780 to set up")
		select {
		case <-parent.Done():
			return parent.Err()
		case <-reconnect:
			return nil // outer loop will reload config
		}
	}

	cloudURL := cfg.CloudURL
	agentID := cfg.AgentID
	agentName := cfg.AgentName
	if agentName == "" {
		agentName = agentID
	}
	agentToken := cfg.AgentToken

	// Expose config values to the rest of the agent via env (the QR/HID
	// listeners read these). Cheap way to avoid threading the config through.
	_ = os.Setenv("QR_STRIP_PREFIX", cfg.QRStripPrefix)
	_ = os.Setenv("QR_STRIP_SUFFIX", cfg.QRStripSuffix)
	if cfg.QRListenPort != "" {
		_ = os.Setenv("QR_LISTEN_PORT", cfg.QRListenPort)
	}
	if cfg.QRDevice != "" {
		_ = os.Setenv("QR_DEVICE", cfg.QRDevice)
	}
	if cfg.QRDeviceAuto {
		_ = os.Setenv("QR_DEVICE_AUTO", "true")
	}

	u, err := url.Parse(cloudURL)
	if err != nil {
		log.Fatalf("bad CLOUD_URL: %v", err)
	}
	// Normalise scheme: http -> ws, https -> wss
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
	q.Set("id", agentID)
	q.Set("token", agentToken)
	u.RawQuery = q.Encode()
	dialURL := u.String()

	log.Printf("face_auth-agent %s starting; cloud=%s", agentID, redactToken(dialURL))

	// Local HTTP listener for QR scans (USB scanner → POST here).
	// The scanner's helper writes incoming HID lines to this endpoint.
	qrPort := getenv("QR_LISTEN_PORT", "7771")
	cloudHTTPBase := strings.TrimSuffix(strings.TrimSuffix(cloudURL, "/"), "/agent/ws")
	if strings.HasPrefix(cloudHTTPBase, "ws://") {
		cloudHTTPBase = "http://" + cloudHTTPBase[len("ws://"):]
	} else if strings.HasPrefix(cloudHTTPBase, "wss://") {
		cloudHTTPBase = "https://" + cloudHTTPBase[len("wss://"):]
	}
	scanFn := startQRListener(qrPort, cloudHTTPBase, agentID)
	log.Printf("QR scan listener on http://127.0.0.1:%s/scan", qrPort)

	// On Linux, also read directly from /dev/input/eventN when QR_DEVICE
	// (or QR_DEVICE_AUTO) is set. Same scanner-side flow, just no userspace
	// helper needed.
	startNativeHID(func(line string) {
		status, body := scanFn(line)
		log.Printf("HID scan -> cloud: %d %s", status, strings.TrimSpace(body)[:min(120, len(strings.TrimSpace(body)))])
	})

	backoff := time.Second
	attempt := 0
	for parent.Err() == nil {
		attempt++
		SetStatus("connecting")
		log.Printf("[ws] connecting (attempt %d) → %s", attempt, redactToken(dialURL))
		start := time.Now()
		err := connect(parent, dialURL, agentID, agentName)
		if parent.Err() != nil {
			SetStatus("disconnected")
			return nil
		}
		uptime := time.Since(start).Round(time.Second)
		SetStatus("disconnected", err.Error())
		// Classify the error so the log is actionable.
		reason := err.Error()
		switch {
		case strings.Contains(reason, "i/o timeout"):
			log.Printf("[ws] lost — no ping from server for 60s (uptime %s) — retrying in %s", uptime, backoff)
		case strings.Contains(reason, "use of closed"):
			log.Printf("[ws] lost — connection closed unexpectedly (uptime %s) — retrying in %s", uptime, backoff)
		case strings.Contains(reason, "EOF"):
			log.Printf("[ws] lost — server closed connection (uptime %s) — retrying in %s", uptime, backoff)
		default:
			log.Printf("[ws] lost — %v (uptime %s) — retrying in %s", err, uptime, backoff)
		}
		select {
		case <-parent.Done():
			return nil
		case <-reconnect:
			log.Printf("[ws] config changed — reconnecting")
			return nil // outer loop reloads config
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
	return nil
}

const (
	agentWriteTimeout = 10 * time.Second
	agentReadTimeout  = 60 * time.Second
)

func connect(ctx context.Context, dialURL, agentID, agentName string) error {
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 10 * time.Second,
	}
	c, _, err := dialer.DialContext(ctx, dialURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	log.Printf("connected as %s", agentID)
	SetStatus("connected")

	// Send hello
	c.SetWriteDeadline(time.Now().Add(agentWriteTimeout))
	hello := internal.Frame{Type: "hello", AgentID: agentID, AgentName: agentName, Version: "1.0"}
	if err := c.WriteJSON(hello); err != nil {
		return err
	}

	// Writer pump: serialize writes through a channel
	out := make(chan internal.Frame, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.SetReadDeadline(time.Now().Add(agentReadTimeout))
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			// Refresh deadline on every received message (including pong).
			c.SetReadDeadline(time.Now().Add(agentReadTimeout))
			var f internal.Frame
			if err := json.Unmarshal(data, &f); err != nil {
				continue
			}
			switch f.Type {
			case "req":
				go handleRequest(f, out)
			case "ping":
				out <- internal.Frame{Type: "pong"}
			}
		}
	}()

	for {
		select {
		case f := <-out:
			c.SetWriteDeadline(time.Now().Add(agentWriteTimeout))
			if err := c.WriteJSON(f); err != nil {
				return err
			}
		case <-done:
			return errors.New("read loop ended")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func handleRequest(req internal.Frame, out chan<- internal.Frame) {
	resp := internal.Frame{Type: "resp", ID: req.ID}

	if req.BaseURL == "" || req.Path == "" {
		resp.Error = "missing baseUrl or path"
		out <- resp
		return
	}

	// Parse baseUrl to extract IP/port/scheme
	u, err := url.Parse(req.BaseURL)
	if err != nil {
		resp.Error = "bad baseUrl: " + err.Error()
		out <- resp
		return
	}
	host := u.Hostname()
	port := atoi(u.Port())
	useHTTPS := u.Scheme == "https"

	client := internal.NewISAPIClient(host, port, useHTTPS, req.Username, req.Password)
	httpResp, body, err := client.Do(req.Method, req.Path, req.ContentType, req.Body)
	if err != nil {
		resp.Error = err.Error()
		out <- resp
		return
	}
	resp.Status = httpResp.StatusCode
	resp.RespBody = body
	resp.RespHeaders = map[string]string{}
	for k, v := range httpResp.Header {
		if len(v) > 0 {
			resp.RespHeaders[k] = v[0]
		}
	}
	out <- resp
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return n
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// scanFunc is what HTTP handlers AND the native HID reader both call.
type scanFunc = func(qr string) (status int, body string)

// qrScan holds the live scan function. The HTTP server always calls through
// this pointer so that reconnects with new config don't need to rebind the port.
var qrScan atomic.Pointer[scanFunc]

// qrHTTPStarted tracks whether the QR HTTP server is already bound to its port.
var qrHTTPStarted atomic.Bool

// startQRListener builds a scan function for the given cloud target and ensures
// the QR HTTP server is running. On reconnects the port stays bound (only one
// server ever runs); the new scan function is stored in qrScan so the server
// always forwards to the current cloud URL without rebinding.
func startQRListener(port, cloudBase, agentID string) scanFunc {
	httpClient := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	stripPrefix := getenv("QR_STRIP_PREFIX", "")
	stripSuffix := getenv("QR_STRIP_SUFFIX", "")
	if stripPrefix != "" || stripSuffix != "" {
		log.Printf("QR strip: prefix=%q suffix=%q", stripPrefix, stripSuffix)
	}

	clean := func(qr string) string {
		qr = strings.TrimFunc(qr, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\x00' || r < 0x20 || r == 0x7f
		})
		if stripPrefix != "" && strings.HasPrefix(qr, stripPrefix) {
			qr = qr[len(stripPrefix):]
		}
		if stripSuffix != "" && strings.HasSuffix(qr, stripSuffix) {
			qr = qr[:len(qr)-len(stripSuffix)]
		}
		return strings.TrimSpace(qr)
	}

	scan := func(qr string) (status int, body string) {
		qr = clean(qr)
		if qr == "" {
			return 400, `{"error":"empty qr"}`
		}
		payload, _ := json.Marshal(map[string]string{"qrToken": qr, "agentId": agentID})
		req, err := http.NewRequest("POST", cloudBase+"/api/qr-auth/scan", bytes.NewReader(payload))
		if err != nil {
			return 500, fmt.Sprintf(`{"error":%q}`, err.Error())
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return 502, fmt.Sprintf(`{"error":%q}`, err.Error())
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// Update the global pointer so the already-running HTTP server picks up the
	// new cloud target without needing to rebind the port.
	qrScan.Store(&scan)

	// Start the HTTP server only once for the lifetime of the process.
	if qrHTTPStarted.CompareAndSwap(false, true) {
		mux := http.NewServeMux()

		// Dispatch through qrScan so reconnects with new config work without restart.
		dispatch := func(qr string) (int, string) {
			if fn := qrScan.Load(); fn != nil {
				return (*fn)(qr)
			}
			return 503, `{"error":"scanner not ready"}`
		}

		mux.HandleFunc("/scan", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", 405)
				return
			}
			var body struct{ QR string `json:"qr"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			s, b := dispatch(body.QR)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s)
			_, _ = w.Write([]byte(b))
		})

		mux.HandleFunc("/scan/q", func(w http.ResponseWriter, r *http.Request) {
			s, b := dispatch(r.URL.Query().Get("q"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s)
			_, _ = w.Write([]byte(b))
		})

		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

		go func() {
			srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("QR listener: %v", err)
			}
		}()
	}

	return scan
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func redactToken(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	if t := q.Get("token"); t != "" {
		q.Set("token", "***")
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}

