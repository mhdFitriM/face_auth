package internal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type PushServer struct {
	app   *fiber.App
	store *Store
	cfg   Config
	hub   *AgentHub // used for snapshot-on-event capture (direct/agent reach)
}

func NewPushServer(store *Store, cfg Config, hub *AgentHub) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             50 * 1024 * 1024, // 50MB for face image events
		ReadBufferSize:        16 * 1024,
		AppName:               "face_auth-push",
	})

	ps := &PushServer{app: app, store: store, cfg: cfg, hub: hub}

	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		dur := time.Since(start)
		status := c.Response().StatusCode()
		log.Printf("[push] %s %s -> %d (%s)", c.Method(), c.Path(), status, dur)
		// Skip self-pings to keep the bridge log signal-rich
		if c.Path() != "/healthz" {
			devID := c.Params("deviceID")
			BridgeLogPush(ConnectionHit{
				At:         time.Now(),
				Port:       cfg.PushPort,
				RemoteAddr: c.IP(),
				Protocol:   classifyHTTPPath(c.Path()),
				Path:       c.Path(),
				Method:     c.Method(),
				Status:     status,
				Bytes:      len(c.Body()),
				DeviceID:   devID,
			})
		}
		return err
	})

	// Device PUSH endpoint (HTTP Push SDK for newer firmware). URL format:
	//   POST /iot/{deviceID}/global/0-global/model/service/operate/PUSH/{Action}
	app.Post("/iot/:deviceID/global/0-global/model/service/operate/PUSH/:action", ps.handlePush)

	// Hik ISAPI HTTP-host alarm sink. Devices configured via
	// /ISAPI/Event/notification/httpHosts push events (JSON or multipart) here.
	app.Post("/hik-event", ps.handleHikISAPIEvent)
	app.Post("/hik-event/*", ps.handleHikISAPIEvent)

	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })

	return app
}

// handleHikISAPIEvent receives events the device pushes via its built-in
// HTTP alarm host feature. Body is either JSON or multipart/form-data with
// a JSON metadata part + JPEG snapshot part.
func (ps *PushServer) handleHikISAPIEvent(c *fiber.Ctx) error {
	ctx := c.Context()
	ct := strings.ToLower(c.Get("Content-Type"))
	body := c.Body()

	deviceID := ""
	eventType := ""
	var jsonPayload []byte
	var imageBytes []byte

	if strings.HasPrefix(ct, "multipart/") {
		parts, err := parseHTTPMultipart(ct, body)
		if err != nil {
			log.Printf("hik-event multipart parse: %v", err)
		}
		for _, p := range parts {
			pct := strings.ToLower(p.ContentType)
			if strings.HasPrefix(pct, "image/") {
				imageBytes = p.Body
			} else if strings.Contains(pct, "json") || strings.Contains(pct, "xml") || pct == "" {
				if len(jsonPayload) == 0 {
					jsonPayload = p.Body
				}
			}
		}
	} else {
		jsonPayload = bytes.TrimSpace(body)
	}

	// Extract metadata from the JSON
	if len(jsonPayload) > 0 {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(jsonPayload, &probe); err == nil {
			unquote := func(k string) string {
				if v, ok := probe[k]; ok {
					var s string
					_ = json.Unmarshal(v, &s)
					return s
				}
				return ""
			}
			eventType = unquote("eventType")
			deviceID = unquote("serialNo")
			if deviceID == "" {
				deviceID = unquote("macAddress")
			}
			// Drill into AccessControllerEvent for richer fields if present
			if v, ok := probe["AccessControllerEvent"]; ok {
				var ace struct {
					SerialNo  string `json:"serialNo"`
					DeviceID  string `json:"deviceID"`
					EventType string `json:"eventType"`
				}
				if err := json.Unmarshal(v, &ace); err == nil {
					if deviceID == "" {
						deviceID = firstNonEmptyStr(ace.SerialNo, ace.DeviceID)
					}
					if eventType == "" {
						eventType = ace.EventType
					}
				}
			}
		}
	}

	// Fall back to remote IP lookup if device didn't include serial
	if deviceID == "" {
		remoteIP := c.IP()
		if d, _ := ps.store.FindDeviceByIP(ctx, remoteIP); d != nil {
			deviceID = d.DeviceID
		}
	}

	// Drop noise: heartbeats / empty-body pings carry no event info — don't pollute the event log.
	hasContent := len(jsonPayload) > 2 || imageBytes != nil // ">2" filters "{}" and empty
	if !hasContent {
		return c.Status(200).SendString("OK")
	}

	if eventType == "" {
		eventType = sniffEventType(jsonPayload)
	}
	if deviceID == "" {
		deviceID = "unknown"
	}

	var imageKey string
	if imageBytes != nil {
		key := fmt.Sprintf("events/%s/%s.jpg", deviceID, uuid.NewString())
		if err := ps.store.PutObject(ctx, key, "image/jpeg", imageBytes); err == nil {
			imageKey = key
		}
	}

	_, _ = ps.store.InsertEvent(ctx, Event{
		DeviceID:  deviceID,
		EventType: eventType,
		Raw:       json.RawMessage(safeJSONFromBytes(jsonPayload)),
		ImageKey:  imageKey,
	})

	return c.Status(200).SendString("OK")
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (ps *PushServer) handlePush(c *fiber.Ctx) error {
	deviceID := c.Params("deviceID")
	action := c.Params("action")
	ctx := c.Context()

	switch action {
	case "AuthInfo":
		return ps.handleAuthInfo(c, deviceID)
	case "Login":
		return ps.handleLogin(c, deviceID)
	case "Logout":
		if !ps.checkAuth(c, deviceID) {
			return ps.respondInvalid(c)
		}
		return ps.handleLogout(c, deviceID)
	case "CommandRequest":
		if !ps.checkAuth(c, deviceID) {
			return ps.respondInvalid(c)
		}
		return ps.handleCommandRequest(c, deviceID)
	case "CommandResult":
		if !ps.checkAuth(c, deviceID) {
			return ps.respondInvalid(c)
		}
		return ps.handleCommandResult(c, deviceID)
	case "Event":
		if !ps.checkAuth(c, deviceID) {
			return ps.respondInvalid(c)
		}
		return ps.handleEvent(c, deviceID)
	case "UpgradeData", "UpgradeStatus":
		if !ps.checkAuth(c, deviceID) {
			return ps.respondInvalid(c)
		}
		return ps.respondOK(c, deviceID)
	default:
		_ = ctx
		return c.Status(fiber.StatusNotFound).SendString("unknown action")
	}
}

// ---------- Auth handlers ----------

func (ps *PushServer) handleAuthInfo(c *fiber.Ctx, deviceID string) error {
	ctx := c.Context()

	salt := GenerateSalt()
	challenge := GenerateChallenge()
	iterations := GenerateIterations()

	isAuth := !ps.cfg.NoAuthMode

	if err := ps.store.UpsertDeviceAuth(ctx, deviceID, salt, challenge, iterations, isAuth); err != nil {
		log.Printf("AuthInfo upsert error: %v", err)
	}

	resp := hikAuthInfoResponse{
		Data: hikAuthInfoData{
			Challenge:       challenge,
			Salt:            salt,
			Iterations:      iterations,
			IsDataEncrypt:   true,
			SecurityVersion: []int{3, 4},
			IsAuth:          isAuth,
		},
	}
	return c.Status(200).JSON(resp)
}

func (ps *PushServer) handleLogin(c *fiber.Ctx, deviceID string) error {
	ctx := c.Context()

	var req hikLoginRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return ps.respondInvalid(c)
	}

	dev, err := ps.store.GetDevice(ctx, deviceID)
	if err != nil || dev == nil {
		return ps.respondInvalid(c)
	}

	password := ps.cfg.DefaultPassword
	if pw, _ := ps.store.GetDevicePassword(ctx, deviceID); pw != "" {
		password = pw
	}

	digestType, ok := VerifyDeviceLoginPassword(
		req.Data.Username, password, dev.Salt, dev.Challenge, dev.Iterations, req.Data.LoginPassword,
	)
	if !ok {
		log.Printf("login failed for %s (user=%s)", deviceID, req.Data.Username)
		return ps.respondInvalid(c)
	}

	if err := ps.store.SetDeviceLogin(ctx, deviceID, req.Data.Username, digestType); err != nil {
		log.Printf("SetDeviceLogin error: %v", err)
	}

	resp := hikLoginResponse{
		Status:   200,
		Code:     "0x00000000",
		ErrorMsg: "Succeeded.",
	}
	resp.Data.CommandInterval = ps.cfg.CommandInterval
	resp.Data.ErrorDelay = 30

	ps.rotateChallenge(c, deviceID, true)
	return c.Status(200).JSON(resp)
}

func (ps *PushServer) handleLogout(c *fiber.Ctx, deviceID string) error {
	_ = ps.store.SetDeviceOffline(c.Context(), deviceID)
	return ps.respondOK(c, deviceID)
}

// ---------- Command handlers ----------

func (ps *PushServer) handleCommandRequest(c *fiber.Ctx, deviceID string) error {
	ctx := c.Context()
	cmds, err := ps.store.PopPendingCommands(ctx, deviceID, 5)
	if err != nil {
		log.Printf("PopPendingCommands: %v", err)
	}

	if len(cmds) == 0 {
		resp := hikCommandRequestResponse{
			Status:     200,
			Code:       "0x00000000",
			ErrorMsg:   "Succeeded.",
			CommandNum: 0,
		}
		ps.rotateChallenge(c, deviceID, true)
		return c.Status(200).JSON(resp)
	}

	items := make([]hikCommandItem, 0, len(cmds))
	for _, cmd := range cmds {
		// "data" field is base64 of the request body (per Hik protocol)
		data := cmd.BodyBase64
		if data == "" && cmd.DataFormat == "json" {
			data = base64.StdEncoding.EncodeToString([]byte("{}"))
		}
		items = append(items, hikCommandItem{
			UUID:       cmd.ID,
			URL:        cmd.Method + " " + cmd.URL,
			DataFormat: cmd.DataFormat,
			Data:       data,
		})
	}

	resp := hikCommandRequestResponse{
		Status:      200,
		Code:        "0x00000000",
		ErrorMsg:    "Succeeded.",
		CommandNum:  len(items),
		CommandList: items,
	}

	ps.rotateChallenge(c, deviceID, true)
	return c.Status(200).JSON(resp)
}

func (ps *PushServer) handleCommandResult(c *fiber.Ctx, deviceID string) error {
	ctx := c.Context()

	var req hikCommandResultRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		log.Printf("CommandResult decode: %v", err)
	}

	for _, item := range req.CommandList {
		if item.UUID == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(item.Data)
		if err != nil {
			log.Printf("CommandResult base64: %v", err)
			continue
		}
		responseBody := parseCommandResultBody(decoded)
		status := extractStatusFromJSON(responseBody)
		if err := ps.store.CompleteCommand(ctx, item.UUID, responseBody, status); err != nil {
			log.Printf("CompleteCommand: %v", err)
		}
	}

	pending, _ := ps.store.PendingCommandCount(ctx, deviceID)

	resp := hikCommandResultResponse{
		Status:           200,
		Code:             "0x00000000",
		ErrorMsg:         "Succeeded.",
		IsPendingCommand: pending > 0,
	}
	ps.rotateChallenge(c, deviceID, true)
	return c.Status(200).JSON(resp)
}

// ---------- Event handler ----------

func (ps *PushServer) handleEvent(c *fiber.Ctx, deviceID string) error {
	ctx := c.Context()

	ct := strings.ToLower(c.Get("Content-Type"))
	body := c.Body()

	var eventReq hikEventListRequest

	if strings.HasPrefix(ct, "multipart/") {
		// Event sent as multipart: JSON part(s) named "eventLog" + image part(s).
		parts, err := parseHTTPMultipart(ct, body)
		if err != nil {
			log.Printf("event multipart parse: %v", err)
		}
		var imageKey string
		var jsonBody []byte
		for _, p := range parts {
			pct := strings.ToLower(p.ContentType)
			if strings.HasPrefix(pct, "image/") || strings.Contains(strings.ToLower(p.Name), "picture") || strings.Contains(strings.ToLower(p.Filename), ".jpg") || strings.Contains(strings.ToLower(p.Filename), ".jpeg") {
				key := fmt.Sprintf("events/%s/%s.jpg", deviceID, uuid.NewString())
				if err := ps.store.PutObject(ctx, key, "image/jpeg", p.Body); err != nil {
					log.Printf("event image put: %v", err)
				} else {
					imageKey = key
				}
			} else if strings.Contains(pct, "json") {
				jsonBody = p.Body
			}
		}
		if len(jsonBody) > 0 {
			_ = json.Unmarshal(jsonBody, &eventReq)
			if len(eventReq.EventList) == 0 {
				// Some firmwares send a single event object directly
				var single hikEventItem
				if err := json.Unmarshal(jsonBody, &single); err == nil && single.UUID != "" {
					eventReq.EventList = []hikEventItem{single}
				}
			}
		}
		// Inject image into stored event if there was one
		ps.persistEventList(ctx, deviceID, eventReq.EventList, imageKey, body)
	} else {
		// Pure JSON body
		_ = json.Unmarshal(body, &eventReq)
		ps.persistEventList(ctx, deviceID, eventReq.EventList, "", body)
	}

	// Response: array of acks
	out := hikEventListResponse{}
	for _, e := range eventReq.EventList {
		out = append(out, hikEventResponseItem{UUID: e.UUID, Status: 200, Code: "0x00000000", ErrorMsg: "Succeeded."})
	}
	ps.rotateChallenge(c, deviceID, true)
	return c.Status(200).JSON(out)
}

func (ps *PushServer) persistEventList(ctx context.Context, deviceID string, list []hikEventItem, imageKey string, rawBody []byte) {
	if len(list) == 0 {
		// Still record the raw body for debugging
		_, err := ps.store.InsertEvent(ctx, Event{
			DeviceID:  deviceID,
			EventType: "unknown",
			Raw:       json.RawMessage(safeJSONFromBytes(rawBody)),
			ImageKey:  imageKey,
		})
		if err != nil {
			log.Printf("InsertEvent raw: %v", err)
		}
		return
	}

	for _, item := range list {
		var payload []byte
		if item.Data != "" {
			if decoded, err := base64.StdEncoding.DecodeString(item.Data); err == nil {
				// data may be JSON or boundary multipart with embedded image
				if isMultipartPayload(decoded) {
					parts, perr := ParseEmbeddedMultipart(decoded)
					if perr == nil {
						for _, pt := range parts {
							pct := strings.ToLower(pt.ContentType)
							if strings.HasPrefix(pct, "image/") && imageKey == "" {
								key := fmt.Sprintf("events/%s/%s.jpg", deviceID, uuid.NewString())
								if err := ps.store.PutObject(ctx, key, "image/jpeg", pt.Body); err == nil {
									imageKey = key
								}
							} else if strings.Contains(pct, "json") {
								payload = pt.Body
							}
						}
					}
				} else {
					payload = decoded
				}
			}
		}
		if len(payload) == 0 {
			payload = []byte("{}")
		}

		eventType := item.EventType
		if eventType == "" {
			eventType = sniffEventType(payload)
		}

		id, err := ps.store.InsertEvent(ctx, Event{
			DeviceID:  deviceID,
			EventType: eventType,
			Raw:       json.RawMessage(safeJSONFromBytes(payload)),
			ImageKey:  imageKey,
		})
		if err != nil {
			log.Printf("InsertEvent: %v", err)
			continue
		}
		// Snapshot-on-event: if the event carried no image, try to grab a live
		// frame so door/face events have a picture. Runs async so it never
		// delays the device's event POST.
		if imageKey == "" {
			go ps.captureSnapshotForEvent(deviceID, id)
		}
	}
}

// captureSnapshotForEvent pulls a live snapshot and attaches it to an event
// that arrived without an image. Only meaningful for devices we can reach for a
// binary pull (direct/agent) — OTAP's command queue can't carry binary frames.
func (ps *PushServer) captureSnapshotForEvent(deviceID string, eventID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := ps.store.GetDevice(ctx, deviceID)
	if err != nil || d == nil || d.Reach == "otap" {
		return
	}
	if d.IP == "" && d.AgentID == "" {
		return
	}
	client := NewISAPIClientForDevice(d, ps.hub)
	jpeg, _, err := client.GetSnapshot()
	if err != nil || len(jpeg) == 0 {
		return
	}
	key := fmt.Sprintf("events/%s/%s.jpg", deviceID, uuid.NewString())
	if err := ps.store.PutObject(ctx, key, "image/jpeg", jpeg); err != nil {
		return
	}
	if err := ps.store.UpdateEventImage(ctx, eventID, key); err != nil {
		log.Printf("UpdateEventImage: %v", err)
	}
}

// ---------- Helpers ----------

func (ps *PushServer) checkAuth(c *fiber.Ctx, deviceID string) bool {
	ctx := c.Context()
	customAuth := c.Get("My-Custom-Auth")

	if customAuth == "" {
		// No-auth path: auto-register the device if absent.
		dev, _ := ps.store.GetDevice(ctx, deviceID)
		if dev == nil {
			if ps.cfg.NoAuthMode {
				_ = ps.store.UpsertDeviceNoAuth(ctx, deviceID)
				return true
			}
			return false
		}
		ps.store.TouchDevice(ctx, deviceID)
		return !dev.IsAuth
	}

	dev, err := ps.store.GetDevice(ctx, deviceID)
	if err != nil || dev == nil {
		return false
	}

	// Device is in no-auth mode: accept without verifying the header.
	if !dev.IsAuth {
		ps.store.TouchDevice(ctx, deviceID)
		return true
	}

	password := ps.cfg.DefaultPassword
	if pw, _ := ps.store.GetDevicePassword(ctx, deviceID); pw != "" {
		password = pw
	}

	if !VerifyCustomAuth(dev.Username, password, dev.Salt, dev.Challenge, customAuth) {
		return false
	}
	ps.store.TouchDevice(ctx, deviceID)
	return true
}

func (ps *PushServer) rotateChallenge(c *fiber.Ctx, deviceID string, withHeader bool) {
	ctx := c.Context()
	dev, _ := ps.store.GetDevice(ctx, deviceID)
	if dev == nil || !dev.IsAuth {
		return
	}
	ch := GenerateChallenge()
	if err := ps.store.UpdateDeviceChallenge(ctx, deviceID, ch); err != nil {
		log.Printf("rotateChallenge: %v", err)
	}
	if withHeader {
		c.Set("My-Custom-Challenge", ch)
	}
}

func (ps *PushServer) respondOK(c *fiber.Ctx, deviceID string) error {
	ps.rotateChallenge(c, deviceID, true)
	return c.Status(200).JSON(hikStatusResponse{
		Status: 200, Code: "0x00000000", ErrorMsg: "Succeeded.",
	})
}

func (ps *PushServer) respondInvalid(c *fiber.Ctx) error {
	return c.Status(401).JSON(hikStatusResponse{
		Status: 401, Code: "0x0020000f", ErrorMsg: "Invalid SessionID.",
	})
}

func parseHTTPMultipart(contentType string, body []byte) ([]MultipartPart, error) {
	hdr := fmt.Sprintf("Content-Type: %s\r\nContent-Length: %d\r\n\r\n", contentType, len(body))
	combined := append([]byte(hdr), body...)
	return ParseEmbeddedMultipart(combined)
}

func isMultipartPayload(p []byte) bool {
	head := p
	if len(head) > 400 {
		head = head[:400]
	}
	low := strings.ToLower(string(head))
	return strings.Contains(low, "content-type:") && strings.Contains(low, "multipart/")
}

func parseCommandResultBody(decoded []byte) string {
	if isMultipartPayload(decoded) {
		parts, err := ParseEmbeddedMultipart(decoded)
		if err == nil {
			for _, p := range parts {
				pct := strings.ToLower(p.ContentType)
				if strings.Contains(pct, "json") || strings.Contains(pct, "xml") {
					return string(p.Body)
				}
			}
		}
	}
	return string(decoded)
}

func extractStatusFromJSON(body string) int {
	if body == "" {
		return 0
	}
	var probe struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &probe); err == nil {
		return probe.Status
	}
	return 0
}

func sniffEventType(payload []byte) string {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		return "unknown"
	}
	for _, k := range []string{"AccessControllerEvent", "VoiceTalkEvent", "VideoIntercomEvent", "FaceCaptureEvent", "AlarmEvent"} {
		if _, ok := probe[k]; ok {
			return k
		}
	}
	return "unknown"
}

func classifyHTTPPath(path string) string {
	if strings.Contains(path, "/iot/") && strings.Contains(path, "/PUSH/") {
		return "http_push_sdk"
	}
	if strings.Contains(path, "/hik-event") {
		return "isapi_event_callback"
	}
	return "http_other"
}

func safeJSONFromBytes(b []byte) []byte {
	if json.Valid(b) {
		return b
	}
	// Wrap raw bytes as a string field so the JSONB column accepts it
	wrapped, _ := json.Marshal(map[string]string{"raw": string(b)})
	return wrapped
}
