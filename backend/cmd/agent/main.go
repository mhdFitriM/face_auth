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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"face_auth/internal"

	"github.com/gorilla/websocket"
)

const agentVersion = internal.AgentVersion

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

	// A downloaded Windows build must replace an existing service before it
	// opens the dashboard. Otherwise the old service still owns port 7780 and
	// the browser displays the old version even though the new file is running.
	if !isService {
		exePath, _ := os.Executable()
		if attempted, out, err := autoUpgradeExistingWindowsService(exePath); attempted {
			if err != nil {
				log.Printf("automatic service upgrade failed: %v\n%s", err, out)
				failurePath := filepath.Join(os.TempDir(), "face_auth-agent-upgrade-error.txt")
				_ = os.WriteFile(failurePath, []byte(fmt.Sprintf("face_auth agent v%s automatic upgrade failed:\r\n\r\n%v\r\n\r\n%s", agentVersion, err, out)), 0o600)
				_ = exec.Command("notepad.exe", failurePath).Start()
				return
			}
			time.Sleep(800 * time.Millisecond)
			openBrowser("http://127.0.0.1:7780")
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
	if v := getenv("QR_TARGET_DEVICE", ""); v != "" {
		cfg.QRTargetDevice = v
	}
	if v := getenv("QR_DIRECT_OPEN", ""); v != "" {
		cfg.QRDirectOpen = strings.EqualFold(v, "true") || v == "1"
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
	_ = os.Setenv("QR_TARGET_DEVICE", cfg.QRTargetDevice)
	_ = os.Setenv("QR_DIRECT_OPEN", fmt.Sprintf("%t", cfg.QRDirectOpen))

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
	cloudHTTPBase := normalizeCloudHTTPBase(cloudURL)
	var scanFn scanFunc
	nativeRoutes := []scannerRoute{}
	activeScannerCount := 0
	activeQRPorts := map[string]bool{}
	stationMode := len(cfg.ScannerStations) > 0

	// Local scanner stations take priority over cloud access points.
	// They are fully self-contained: device ID and door are stored in the
	// agent config, so no cloud access point row is needed.
	if stationMode {
		for _, st := range cfg.ScannerStations {
			if st.ScannerSource != "" && st.ScannerSource != "third_party_usb" {
				log.Printf("QR station %q ignored: scanner source %q is not third_party_usb", st.Name, st.ScannerSource)
				continue
			}
			if st.DeviceID == "" || st.Port == 0 {
				continue
			}
			activeScannerCount++
			fn := startQRListener(strconv.Itoa(st.Port), cloudHTTPBase, agentID, agentToken, scanRouteInfo{
				ScannerName: st.Name,
				ScannerID:   st.ID,
				HardwareID:  st.HardwareID,
				DeviceID:    st.DeviceID,
				DoorNo:      st.DoorNo,
				Direction:   st.Direction,
			})
			activeQRPorts[strconv.Itoa(st.Port)] = true
			if st.HardwareID != "" {
				nativeRoutes = append(nativeRoutes, scannerRoute{ScannerID: st.ID, HardwareID: st.HardwareID, Scan: fn})
			}
			if scanFn == nil {
				scanFn = fn
			}
			log.Printf("QR local station %q on http://127.0.0.1:%d/scan -> device %s door %d", st.Name, st.Port, st.DeviceID, st.DoorNo)
		}
	} else {
		points := fetchAgentAccessPoints(cloudHTTPBase, agentID, agentToken)
		if len(points) == 0 {
			scanFn = startQRListener(qrPort, cloudHTTPBase, agentID, agentToken, scanRouteInfo{ScannerName: "Legacy scanner", DoorNo: 1})
			activeQRPorts[qrPort] = true
			log.Printf("QR legacy listener on http://127.0.0.1:%s/scan", qrPort)
		} else {
			for _, point := range points {
				if point.AuthMode != "faceauth_qr" || !point.Enabled {
					continue
				}
				activeScannerCount++
				hardwareID := ""
				for _, binding := range cfg.ScannerBindings {
					if binding.ScannerID == point.ScannerID {
						hardwareID = binding.HardwareID
						break
					}
				}
				fn := startQRListener(strconv.Itoa(point.ScannerPort), cloudHTTPBase, agentID, agentToken, scanRouteInfo{
					ScannerName:   firstNonEmpty(point.Name, point.ScannerID),
					ScannerID:     point.ScannerID,
					HardwareID:    hardwareID,
					DeviceID:      point.DeviceID,
					DoorNo:        point.DoorNo,
					Direction:     point.Direction,
					AccessPointID: point.ID,
				})
				activeQRPorts[strconv.Itoa(point.ScannerPort)] = true
				for _, binding := range cfg.ScannerBindings {
					if binding.ScannerID == point.ScannerID && binding.HardwareID != "" {
						nativeRoutes = append(nativeRoutes, scannerRoute{ScannerID: point.ScannerID, HardwareID: binding.HardwareID, Scan: fn})
					}
				}
				if scanFn == nil {
					scanFn = fn
				}
				log.Printf("QR access point %s listener on http://127.0.0.1:%d/scan -> %s door %d", point.ID, point.ScannerPort, point.DeviceID, point.DoorNo)
			}
		}
	}
	disableQRListenersExcept(activeQRPorts)

	// On Linux, also read directly from /dev/input/eventN when QR_DEVICE
	// (or QR_DEVICE_AUTO) is set. Same scanner-side flow, just no userspace
	// helper needed.
	if !(runtimeGOOS == "windows" && (stationMode || len(nativeRoutes) > 0)) {
		startQRWatchdog(parent)
	}
	if runtimeGOOS == "windows" {
		nativeExclusive := stationMode || len(nativeRoutes) > 0
		_ = os.Setenv("QR_NATIVE_BOUND", fmt.Sprintf("%t", nativeExclusive))
		if err := startNativeScannerManager(parent, nativeRoutes); err != nil {
			log.Printf("native scanner manager: %v", err)
		}
		if nativeExclusive {
			// The global keyboard hook cannot identify which scanner produced a
			// scan. Never allow it in station mode or every scanner could reach one
			// legacy port and open the same gate.
			// Native Raw Input owns approved scanners. Stop the global AHK hook so
			// the same scan cannot also reach the legacy single-scanner route.
			_ = qrWatcherStop()
		}
		if stationMode && len(nativeRoutes) == 0 {
			log.Printf("QR station mode is fail-closed: no USB scanner is bound; legacy global watcher disabled")
		}
	} else if scanFn != nil && activeScannerCount <= 1 {
		_ = os.Setenv("QR_NATIVE_BOUND", "false")
		startNativeHID(func(line string) {
			status, body := scanFn(line)
			log.Printf("HID scan -> cloud: %d %s", status, strings.TrimSpace(body)[:min(120, len(strings.TrimSpace(body)))])
		})
	} else if activeScannerCount > 1 {
		log.Printf("HID global capture disabled: %d scanner stations require device-bound or network scanner input", activeScannerCount)
	}

	backoff := time.Second
	attempt := 0
	for parent.Err() == nil {
		attempt++
		SetStatus("connecting")
		log.Printf("[ws] connecting (attempt %d) → %s", attempt, redactToken(dialURL))
		start := time.Now()
		// A station edit must take effect while the WebSocket is healthy. Run the
		// connection under a child context so a dashboard reconnect signal can
		// close it immediately and make the outer loop reload agent.json.
		connectCtx, cancelConnect := context.WithCancel(parent)
		connectResult := make(chan error, 1)
		go func() {
			connectResult <- connect(connectCtx, dialURL, agentID, agentName)
		}()
		var err error
		select {
		case err = <-connectResult:
			cancelConnect()
		case <-reconnect:
			log.Printf("[ws] config changed — reconnecting immediately")
			cancelConnect()
			<-connectResult
			return nil // outer loop reloads config and scanner routes
		case <-parent.Done():
			cancelConnect()
			<-connectResult
			SetStatus("disconnected")
			return nil
		}
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
	hello := internal.Frame{Type: "hello", AgentID: agentID, AgentName: agentName, Version: agentVersion}
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

type completedScan struct {
	status      int
	body        string
	completedAt time.Time
}

type pendingScan struct {
	done   chan struct{}
	status int
	body   string
}

// suppressDuplicateScans guarantees that repeated delivery of the same QR to
// one scanner route results in at most one cloud request during the debounce
// window. Concurrent duplicates wait for and reuse the first response instead
// of issuing a second OpenDoor command.
func suppressDuplicateScans(window time.Duration, forward scanFunc) scanFunc {
	var mu sync.Mutex
	recent := map[string]completedScan{}
	pending := map[string]*pendingScan{}

	return func(qr string) (int, string) {
		mu.Lock()
		if call := pending[qr]; call != nil {
			mu.Unlock()
			<-call.done
			return call.status, call.body
		}
		if result, ok := recent[qr]; ok && time.Since(result.completedAt) < window {
			mu.Unlock()
			return result.status, result.body
		}
		call := &pendingScan{done: make(chan struct{})}
		pending[qr] = call
		mu.Unlock()

		status, body := forward(qr)

		mu.Lock()
		call.status, call.body = status, body
		delete(pending, qr)
		recent[qr] = completedScan{status: status, body: body, completedAt: time.Now()}
		close(call.done)
		for key, result := range recent {
			if time.Since(result.completedAt) >= window {
				delete(recent, key)
			}
		}
		mu.Unlock()
		return status, body
	}
}

// qrScan holds the live scan function. The HTTP server always calls through
// this pointer so that reconnects with new config don't need to rebind the port.
var qrListeners = struct {
	sync.RWMutex
	scans   map[string]*scanFunc
	started map[string]bool
}{scans: map[string]*scanFunc{}, started: map[string]bool{}}

// ScanEntry is one record in the debug ring buffer.
type ScanEntry struct {
	Time          string `json:"time"`
	QR            string `json:"qr"` // first 32 chars only
	Member        string `json:"member,omitempty"`
	Status        int    `json:"status"`
	Body          string `json:"body"`
	Opened        bool   `json:"opened"`
	Scanner       string `json:"scanner,omitempty"`
	HardwareID    string `json:"hardwareId,omitempty"`
	LocalPort     string `json:"localPort,omitempty"`
	DeviceID      string `json:"deviceId,omitempty"`
	DoorNo        int    `json:"doorNo,omitempty"`
	Direction     string `json:"direction,omitempty"`
	AccessPointID string `json:"accessPointId,omitempty"`
}

type scanRouteInfo struct {
	ScannerName   string
	ScannerID     string
	HardwareID    string
	DeviceID      string
	DoorNo        int
	Direction     string
	AccessPointID string
	LocalPort     string
}

var (
	scanLogMu      sync.Mutex
	scanLog        []ScanEntry
	scanLogMaxSize = 50
)

func recordScan(qr string, status int, body string, opened bool, route scanRouteInfo) {
	preview := qr
	if len(preview) > 32 {
		preview = preview[:32] + "…"
	}
	var response struct {
		Person   string `json:"person"`
		DeviceID string `json:"deviceId"`
	}
	_ = json.Unmarshal([]byte(body), &response)
	deviceID := firstNonEmpty(route.DeviceID, response.DeviceID)
	e := ScanEntry{
		Time:          time.Now().Format("15:04:05"),
		QR:            preview,
		Member:        response.Person,
		Status:        status,
		Body:          body,
		Opened:        opened,
		Scanner:       route.ScannerName,
		HardwareID:    route.HardwareID,
		LocalPort:     route.LocalPort,
		DeviceID:      deviceID,
		DoorNo:        route.DoorNo,
		Direction:     route.Direction,
		AccessPointID: route.AccessPointID,
	}
	scanLogMu.Lock()
	scanLog = append([]ScanEntry{e}, scanLog...)
	if len(scanLog) > scanLogMaxSize {
		scanLog = scanLog[:scanLogMaxSize]
	}
	scanLogMu.Unlock()
}

// startQRListener builds a scan function for the given cloud target and ensures
// the QR HTTP server is running. On reconnects the port stays bound (only one
// server ever runs); the new scan function is stored in qrScan so the server
// always forwards to the current cloud URL without rebinding.
func startQRListener(port, cloudBase, agentID, agentToken string, route scanRouteInfo) scanFunc {
	route.LocalPort = port
	httpClient := &http.Client{
		Timeout:   8 * time.Second,
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

	forward := func(qr string) (status int, body string) {
		if qr == "" {
			return 400, `{"error":"empty qr"}`
		}
		devID := route.DeviceID
		if devID == "" {
			devID = getenv("QR_TARGET_DEVICE", "")
		}
		payload, _ := json.Marshal(map[string]any{
			"qrToken": qr, "agentId": agentID, "agentToken": agentToken,
			"deviceId":      devID,
			"doorNo":        route.DoorNo,
			"directOpen":    route.AccessPointID != "" || devID != "" || strings.EqualFold(getenv("QR_DIRECT_OPEN", "false"), "true"),
			"accessPointId": route.AccessPointID,
			"scannerId":     route.ScannerID,
			"scannerName":   route.ScannerName,
			"direction":     route.Direction,
			"correlationId": fmt.Sprintf("%s-%d", firstNonEmpty(route.AccessPointID, agentID), time.Now().UnixNano()),
		})
		req, err := http.NewRequest("POST", cloudBase+"/api/qr-auth/scan", bytes.NewReader(payload))
		if err != nil {
			return 500, fmt.Sprintf(`{"error":%q}`, err.Error())
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			recordScan(qr, 502, fmt.Sprintf(`{"error":%q}`, err.Error()), false, route)
			return 502, fmt.Sprintf(`{"error":%q}`, err.Error())
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var result struct {
			OK     bool `json:"ok"`
			Opened bool `json:"opened"`
		}
		_ = json.Unmarshal(b, &result)
		opened := resp.StatusCode == http.StatusOK && result.OK && result.Opened
		recordScan(qr, resp.StatusCode, string(b), opened, route)
		return resp.StatusCode, string(b)
	}
	deduped := suppressDuplicateScans(2*time.Second, forward)
	scan := func(qr string) (status int, body string) {
		return deduped(clean(qr))
	}

	// Update the global pointer so the already-running HTTP server picks up the
	// new cloud target without needing to rebind the port.
	qrListeners.Lock()
	qrListeners.scans[port] = &scan
	alreadyStarted := qrListeners.started[port]
	if !alreadyStarted {
		qrListeners.started[port] = true
	}
	qrListeners.Unlock()

	// Start the HTTP server only once for the lifetime of the process.
	if !alreadyStarted {
		mux := http.NewServeMux()

		// Dispatch through qrScan so reconnects with new config work without restart.
		dispatch := func(qr string) (int, string) {
			qrListeners.RLock()
			fn := qrListeners.scans[port]
			qrListeners.RUnlock()
			if fn != nil {
				return (*fn)(qr)
			}
			return 503, `{"error":"scanner not ready"}`
		}

		mux.HandleFunc("/scan", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", 405)
				return
			}
			var body struct {
				QR string `json:"qr"`
			}
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

		mux.HandleFunc("/qrdebug", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept") == "application/json" || r.URL.Query().Get("json") == "1" {
				scanLogMu.Lock()
				snap := make([]ScanEntry, len(scanLog))
				copy(snap, scanLog)
				scanLogMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				b, _ := json.Marshal(snap)
				_, _ = w.Write(b)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(qrDebugPage()))
		})

		go func() {
			srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("QR listener: %v", err)
			}
		}()
	}

	return scan
}

// disableQRListenersExcept makes removed or port-changed stations fail closed.
// Listener sockets remain allocated for process lifetime, but no stale route
// can continue authenticating or opening its former door.
func disableQRListenersExcept(active map[string]bool) {
	qrListeners.Lock()
	defer qrListeners.Unlock()
	for port := range qrListeners.started {
		if !active[port] {
			qrListeners.scans[port] = nil
			log.Printf("QR listener :%s disabled (station removed or port changed)", port)
		}
	}
}

type agentAccessPoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ScannerID   string `json:"scannerId"`
	ScannerPort int    `json:"scannerPort"`
	DeviceID    string `json:"deviceId"`
	DoorNo      int    `json:"doorNo"`
	Direction   string `json:"direction"`
	AuthMode    string `json:"authMode"`
	Enabled     bool   `json:"enabled"`
}

type agentDeviceOption struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Online   bool   `json:"online"`
}

func normalizeCloudHTTPBase(cloudURL string) string {
	base := strings.TrimSuffix(strings.TrimSpace(cloudURL), "/")
	base = strings.TrimSuffix(base, "/agent/ws")
	if strings.HasPrefix(base, "ws://") {
		return "http://" + base[len("ws://"):]
	}
	if strings.HasPrefix(base, "wss://") {
		return "https://" + base[len("wss://"):]
	}
	return base
}

func fetchAgentDevices(cloudBase, agentID, agentToken string) ([]agentDeviceOption, error) {
	u := cloudBase + "/api/agent/devices?agentId=" + url.QueryEscape(agentID) + "&token=" + url.QueryEscape(agentToken)
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("agent device list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent device list: HTTP %d", resp.StatusCode)
	}
	var devices []agentDeviceOption
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return nil, fmt.Errorf("agent device list decode: %w", err)
	}
	return devices, nil
}

func fetchAgentAccessPoints(cloudBase, agentID, agentToken string) []agentAccessPoint {
	u := cloudBase + "/api/agent/access-points?agentId=" + url.QueryEscape(agentID) + "&token=" + url.QueryEscape(agentToken)
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get(u)
	if err != nil {
		log.Printf("access-point config: %v; using legacy listener", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("access-point config: HTTP %d; using legacy listener", resp.StatusCode)
		return nil
	}
	var points []agentAccessPoint
	if err := json.NewDecoder(resp.Body).Decode(&points); err != nil {
		log.Printf("access-point config decode: %v", err)
		return nil
	}
	return points
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "scan"
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
