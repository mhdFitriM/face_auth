package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// QRAuth manages "card-and-face equivalent" sessions.
//
// Flow:
//  1. User scans a QR (containing their qr_token).
//  2. Agent posts the token to /api/qr-auth/scan.
//  3. We look up the Person, call ISAPI to set their userVerifyMode = "face"
//     on the device (so only face works, briefly).
//  4. A watchdog waits up to `Window` for either a face-match event for that
//     user OR a timeout. Either way, we re-lock the user (userVerifyMode set
//     to a mode they can't satisfy with their enrolled credentials).
//
// Locked mode is "cardAndPw" — requires card AND a password. If users have
// no password set (which is the default), this combination is unsatisfiable
// and the device rejects every credential attempt for that user.
const (
	lockedVerifyMode   = "cardAndPw"
	unlockedVerifyMode = "face"
)

type QRSession struct {
	ID         string    `json:"id"`
	PersonID   string    `json:"personId"`
	EmployeeNo string    `json:"employeeNo"`
	Name       string    `json:"name"`
	DeviceID   string    `json:"deviceId"`
	OpenedAt   time.Time `json:"openedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	// Mode describes how the session was opened.
	//   qr        — user scanned a QR code that mapped to a person
	//   face-only — caller asked us to allow face for a specific person without QR
	//   face-any  — caller asked us to allow face for ANY enrolled person on the device
	Mode   string `json:"mode"`
	Status string `json:"status"` // open | face_matched | timed_out | cancelled
	// MatchedEmployeeNo is set when Mode=="face-any" and the device reports
	// which user authenticated. For person-scoped sessions it equals EmployeeNo.
	MatchedEmployeeNo string `json:"matchedEmployeeNo,omitempty"`
	Source            string `json:"source,omitempty"` // ui | api | agent

	cancel context.CancelFunc
}

type QRAuth struct {
	store    *Store
	cfg      Config
	hub      *AgentHub
	settings *SettingsStore

	mu       sync.Mutex
	sessions map[string]*QRSession // by sessionID
	history  []QRSession           // ring buffer for the live panel
}

func NewQRAuth(store *Store, cfg Config, hub *AgentHub, settings *SettingsStore) *QRAuth {
	return &QRAuth{
		store:    store,
		cfg:      cfg,
		hub:      hub,
		settings: settings,
		sessions: map[string]*QRSession{},
	}
}

// window returns the configured face-auth window (settings-controlled).
func (q *QRAuth) window() time.Duration {
	sec := 10
	if q.settings != nil {
		sec = q.settings.Get().FaceAuthWindowSec
	}
	if sec <= 0 {
		sec = 10
	}
	return time.Duration(sec) * time.Second
}

// Scan is the legacy QR entry point — agent posts QR payload here.
// Maps QR token → Person → calls openSession(qr) on every device linked to
// the agent.
func (q *QRAuth) Scan(ctx context.Context, qrToken, agentID string) (*QRSession, error) {
	candidates := tokenCandidates(qrToken)
	var p *Person
	for _, t := range candidates {
		pp, err := q.store.GetPersonByQRToken(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("lookup: %w", err)
		}
		if pp != nil {
			p = pp
			break
		}
	}
	if p == nil {
		return nil, errors.New("unknown QR token")
	}
	if p.EmployeeNo == "" {
		return nil, errors.New("person has no employeeNo to authenticate against the device")
	}
	// Scope to the person's tenant — never touch devices belonging to other
	// tenants. This is the multi-tenant safety net for QR scanning.
	tenantID, _ := q.store.GetPersonTenant(ctx, p.ID)
	if tenantID == "" {
		return nil, errors.New("person has no tenant — assign one before QR-auth can run")
	}
	devices, err := q.devicesForAgent(ctx, tenantID, agentID)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New("no reachable devices for this tenant (no IP / no agent match)")
	}
	return q.openSession(ctx, p, devices, "qr", "agent")
}

// StartFaceAuth opens a face-auth window on a specific device. Behavior depends
// on the QR-required toggle:
//
//   - If the device requires QR (global or per-device override): the caller
//     MUST supply qrToken OR (personId/employeeNo) so we know which user to
//     unlock. If only a device is given, we refuse with ErrQRRequired so the
//     caller knows to prompt for QR.
//   - If the device does NOT require QR: anyone enrolled on the device can
//     authenticate. We open a "face-any" session that just watches for the
//     next face-match event on that device.
//
// If a specific personId/employeeNo is provided we open a "face-only" session
// scoped to that user, regardless of toggle (third parties may want strict
// identity binding).
func (q *QRAuth) StartFaceAuth(ctx context.Context, req FaceAuthRequest) (*QRSession, error) {
	if req.DeviceID == "" {
		return nil, errors.New("deviceId required")
	}
	d, err := q.store.GetDevice(ctx, req.DeviceID)
	if err != nil || d == nil {
		return nil, errors.New("device not found")
	}
	if d.IP == "" {
		return nil, errors.New("device has no LAN address")
	}

	needsQR, _ := q.settings.DeviceRequiresQR(ctx, req.DeviceID)

	// Resolve a person if any identifier was supplied.
	var p *Person
	switch {
	case strings.TrimSpace(req.QRToken) != "":
		for _, t := range tokenCandidates(req.QRToken) {
			if pp, _ := q.store.GetPersonByQRToken(ctx, t); pp != nil {
				p = pp
				break
			}
		}
		if p == nil {
			return nil, errors.New("unknown QR token")
		}
	case req.PersonID != "":
		p, _ = q.store.GetPerson(ctx, req.PersonID)
		if p == nil {
			return nil, errors.New("person not found")
		}
	case req.EmployeeNo != "":
		p, _ = q.store.GetPersonByEmployeeNo(ctx, req.EmployeeNo)
		if p == nil {
			return nil, errors.New("employeeNo not found")
		}
	}

	if p != nil && p.EmployeeNo == "" {
		return nil, errors.New("person has no employeeNo")
	}

	if p == nil && needsQR {
		return nil, ErrQRRequired
	}

	mode := "face-only"
	if p == nil {
		mode = "face-any"
	} else if req.QRToken != "" {
		mode = "qr"
	}

	src := req.Source
	if src == "" {
		src = "api"
	}
	return q.openSessionTagged(ctx, p, []Device{*d}, mode, src)
}

// FaceAuthRequest is the inbound payload for StartFaceAuth.
type FaceAuthRequest struct {
	DeviceID   string `json:"deviceId"`
	PersonID   string `json:"personId,omitempty"`
	EmployeeNo string `json:"employeeNo,omitempty"`
	QRToken    string `json:"qrToken,omitempty"`
	Source     string `json:"-"` // "ui" | "api" | "agent"
}

// ErrQRRequired signals to the caller that this device requires a QR scan
// before face auth can start.
var ErrQRRequired = errors.New("QR scan required before face auth on this device")

// openSession (legacy) used by Scan() — keeps the historic name.
func (q *QRAuth) openSession(ctx context.Context, p *Person, devices []Device, mode, source string) (*QRSession, error) {
	return q.openSessionTagged(ctx, p, devices, mode, source)
}

func (q *QRAuth) openSessionTagged(ctx context.Context, p *Person, devices []Device, mode, source string) (*QRSession, error) {
	if len(devices) == 0 {
		return nil, errors.New("no devices to open session on")
	}

	// Cancel any existing session covering the same person (avoids two
	// concurrent watchdogs fighting over verify-mode).
	if p != nil {
		q.mu.Lock()
		for k, old := range q.sessions {
			if old.EmployeeNo == p.EmployeeNo && old.cancel != nil {
				old.cancel()
				delete(q.sessions, k)
			}
		}
		q.mu.Unlock()
	}

	// For person-scoped sessions on devices that REQUIRE QR (toggle ON), we
	// briefly flip that user's verify-mode to "face" so the camera will accept
	// a face match. On devices that don't require QR, users are already in
	// "face" mode permanently — we leave them alone.
	if p != nil {
		for _, d := range devices {
			needsQR, _ := q.settings.DeviceRequiresQR(ctx, d.DeviceID)
			if !needsQR {
				continue
			}
			if err := q.setUserVerifyMode(ctx, &d, p, unlockedVerifyMode); err != nil {
				log.Printf("[face-auth] unlock %s on %s: %v", p.EmployeeNo, d.DeviceID, err)
				return nil, fmt.Errorf("unlock failed: %w", err)
			}
		}
	}

	win := q.window()
	sessCtx, cancel := context.WithTimeout(context.Background(), win)
	sess := &QRSession{
		ID:        "fa-" + RandomString(10, hexCharset),
		DeviceID:  devices[0].DeviceID,
		OpenedAt:  time.Now(),
		ExpiresAt: time.Now().Add(win),
		Mode:      mode,
		Status:    "open",
		Source:    source,
		cancel:    cancel,
	}
	if p != nil {
		sess.PersonID = p.ID
		sess.EmployeeNo = p.EmployeeNo
		sess.Name = p.Name
	}
	q.mu.Lock()
	q.sessions[sess.ID] = sess
	q.mu.Unlock()

	go q.watchdog(sessCtx, sess, p, devices)
	if p != nil {
		log.Printf("[face-auth] session %s OPENED for %s (%s) mode=%s window=%s", sess.ID, p.EmployeeNo, p.Name, mode, win)
	} else {
		log.Printf("[face-auth] session %s OPENED any-user on %s mode=%s window=%s", sess.ID, sess.DeviceID, mode, win)
	}
	return sess, nil
}

// GetSession returns a snapshot of a session (active or recently finished).
func (q *QRAuth) GetSession(id string) *QRSession {
	q.mu.Lock()
	defer q.mu.Unlock()
	if s, ok := q.sessions[id]; ok {
		cp := *s
		return &cp
	}
	for i := len(q.history) - 1; i >= 0; i-- {
		if q.history[i].ID == id {
			cp := q.history[i]
			return &cp
		}
	}
	return nil
}

// CancelSession aborts an open session. Returns true if it was open.
func (q *QRAuth) CancelSession(id string) bool {
	q.mu.Lock()
	s, ok := q.sessions[id]
	if !ok || s.cancel == nil {
		q.mu.Unlock()
		return false
	}
	s.cancel()
	q.mu.Unlock()
	return true
}

// watchdog locks the user back as soon as a matching face event arrives,
// or when the timer expires.
func (q *QRAuth) watchdog(ctx context.Context, sess *QRSession, p *Person, devices []Device) {
	ch := q.store.Subscribe()
	defer q.store.Unsubscribe(ch)

	var status string
	var matchedEmpNo string
	wantDevice := sess.DeviceID
	personScoped := p != nil

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				status = "cancelled"
				goto done
			}
			if wantDevice != "" && e.DeviceID != "" && e.DeviceID != wantDevice {
				continue
			}
			emp, faceMatched := extractFaceMatchFromEvent(e)
			if !faceMatched {
				continue
			}
			if personScoped {
				if emp == p.EmployeeNo {
					matchedEmpNo = emp
					status = "face_matched"
					goto done
				}
				// Different user matched on the device — ignore, keep waiting
				continue
			}
			// face-any: any successful match closes the session
			matchedEmpNo = emp
			status = "face_matched"
			goto done
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				status = "timed_out"
			} else {
				status = "cancelled"
			}
			goto done
		}
	}
done:
	// Re-lock the person — but only on devices that require QR. Devices in
	// face-only mode keep their users permanently armed; we never touched them
	// when opening the session, so don't touch them on close either.
	if personScoped {
		bg := context.Background()
		for _, d := range devices {
			needsQR, _ := q.settings.DeviceRequiresQR(bg, d.DeviceID)
			if !needsQR {
				continue
			}
			if err := q.setUserVerifyMode(bg, &d, p, lockedVerifyMode); err != nil {
				log.Printf("[face-auth] LOCK %s on %s failed: %v", p.EmployeeNo, d.DeviceID, err)
			}
		}
	}

	q.mu.Lock()
	sess.Status = status
	sess.MatchedEmployeeNo = matchedEmpNo
	q.history = append(q.history, *sess)
	if len(q.history) > 200 {
		q.history = q.history[len(q.history)-200:]
	}
	delete(q.sessions, sess.ID)
	q.mu.Unlock()
	log.Printf("[face-auth] session %s -> %s (matched=%q)", sess.ID, status, matchedEmpNo)
}

// extractFaceMatchFromEvent inspects a device event payload and returns
// (employeeNo, isFaceMatch). It tolerates both Hikvision AccessControllerEvent
// schemas and a few flatter variants emitted by older firmware.
func extractFaceMatchFromEvent(e Event) (string, bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(e.Raw, &probe); err != nil {
		return "", false
	}
	if v, ok := probe["AccessControllerEvent"]; ok {
		var ace struct {
			EmployeeNoString string `json:"employeeNoString"`
			MajorEventType   int    `json:"majorEventType"`
			SubEventType     int    `json:"subEventType"`
			Name             string `json:"name"`
			CurrentVerifyMode string `json:"currentVerifyMode"`
		}
		if err := json.Unmarshal(v, &ace); err == nil {
			// Hik major=5 / sub=75-78 covers face-related access events. We also
			// accept any event whose currentVerifyMode contains "face" — that's
			// the firmware confirming a face was used to authenticate.
			isFace := strings.Contains(strings.ToLower(ace.CurrentVerifyMode), "face") ||
				(ace.MajorEventType == 5 && ace.SubEventType >= 75 && ace.SubEventType <= 78)
			if ace.EmployeeNoString != "" && isFace {
				return ace.EmployeeNoString, true
			}
		}
	}
	for _, k := range []string{"employeeNoString", "EmployeeNoString"} {
		if v, ok := probe[k]; ok {
			var s string
			_ = json.Unmarshal(v, &s)
			if s != "" {
				return s, true
			}
		}
	}
	return "", false
}

func (q *QRAuth) setUserVerifyMode(ctx context.Context, d *Device, p *Person, mode string) error {
	client := NewISAPIClientForDevice(d, q.hub)
	// Always keep verifyMode=face so the camera produces visible/audible
	// feedback on every attempt. Distinguish allow vs deny via the Valid
	// window — an expired window makes Hik emit a "user invalid" denial.
	beginTime := "2020-01-01T00:00:00"
	endTime := "2037-12-31T23:59:59"
	if mode == lockedVerifyMode {
		beginTime = "2000-01-01T00:00:00"
		endTime = "2000-12-31T23:59:59"
	}
	body, _ := json.Marshal(map[string]any{
		"UserInfo": map[string]any{
			"employeeNo":     sanitizeFPID(p.EmployeeNo),
			"name":           p.Name,
			"userType":       mapDeviceUserType(p.PersonType),
			"localUIRight":   p.PersonRole == "administrator",
			"userVerifyMode": unlockedVerifyMode,
			"gender":         ifElse(p.Gender == "", "unknown", p.Gender),
			"doorRight":      ifElse(p.DoorRight == "", "1", p.DoorRight),
			"RightPlan": []map[string]any{
				{"doorNo": 1, "planTemplateNo": ifElse(p.PlanTemplate == "", "1", p.PlanTemplate)},
			},
			"Valid": map[string]any{
				"enable":    true, // must be true so Hik checks the dates
				"beginTime": beginTime,
				"endTime":   endTime,
				"timeType":  "local",
			},
		},
	})
	resp, respBody, err := client.Do("PUT", "/ISAPI/AccessControl/UserInfo/Modify?format=json", "application/json", body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("modify user: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// LockAllUsersOnDevice sweeps every user on a device into the locked state.
// Kept for backwards-compatible /api/devices/:id/lock-all-users.
func (q *QRAuth) LockAllUsersOnDevice(ctx context.Context, deviceID string) (int, error) {
	return q.SetAllUsersVerifyMode(ctx, deviceID, lockedVerifyMode)
}

// SetAllUsersVerifyMode bulk-updates every user record on a device to the
// given verify mode. Used to enforce the QR-2FA toggle at device level:
//   - mode = "cardAndPw"  → device requires QR + face (toggle ON)
//   - mode = "face"       → device accepts face alone   (toggle OFF)
func (q *QRAuth) SetAllUsersVerifyMode(ctx context.Context, deviceID, mode string) (int, error) {
	d, err := q.store.GetDevice(ctx, deviceID)
	if err != nil || d == nil {
		return 0, fmt.Errorf("device not found")
	}
	if d.IP == "" {
		return 0, fmt.Errorf("device has no LAN address")
	}
	client := NewISAPIClientForDevice(d, q.hub)
	users, err := client.ListUsers()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		body, _ := json.Marshal(map[string]any{
			"UserInfo": map[string]any{
				"employeeNo":     sanitizeFPID(u.EmployeeNo),
				"name":           u.Name,
				"userType":       firstNonEmpty(u.UserType, "normal"),
				"localUIRight":   u.LocalUIRight,
				"userVerifyMode": mode,
				"gender":         firstNonEmpty(u.Gender, "unknown"),
				"doorRight":      firstNonEmpty(u.DoorRight, "1"),
				"RightPlan":      []map[string]any{{"doorNo": 1, "planTemplateNo": "1"}},
				"Valid": map[string]any{
					"enable":    true,
					"beginTime": "2020-01-01T00:00:00",
					"endTime":   "2037-12-31T23:59:59",
					"timeType":  "local",
				},
			},
		})
		resp, _, err := client.Do("PUT", "/ISAPI/AccessControl/UserInfo/Modify?format=json", "application/json", body)
		if err == nil && resp.StatusCode == 200 {
			n++
		}
	}
	return n, nil
}

// ApplyDeviceMode reads the effective requireQR2FA setting for a device and
// pushes the corresponding verify mode to every user on it. Idempotent.
func (q *QRAuth) ApplyDeviceMode(ctx context.Context, deviceID string) (int, string, error) {
	needsQR, err := q.settings.DeviceRequiresQR(ctx, deviceID)
	if err != nil {
		return 0, "", err
	}
	mode := unlockedVerifyMode
	if needsQR {
		mode = lockedVerifyMode
	}
	n, err := q.SetAllUsersVerifyMode(ctx, deviceID, mode)
	return n, mode, err
}

// ApplyAllDeviceModes loops over every registered device and calls
// ApplyDeviceMode. Used right after the global settings toggle changes so the
// new behavior takes effect on all devices.
func (q *QRAuth) ApplyAllDeviceModes(ctx context.Context) []map[string]any {
	out := []map[string]any{}
	devices, err := q.store.ListDevices(ctx)
	if err != nil {
		return out
	}
	for _, d := range devices {
		if d.IP == "" {
			continue
		}
		n, mode, err := q.ApplyDeviceMode(ctx, d.DeviceID)
		entry := map[string]any{"deviceId": d.DeviceID, "updated": n, "mode": mode}
		if err != nil {
			entry["error"] = err.Error()
		}
		out = append(out, entry)
	}
	return out
}

// ModeForDevice returns the baseline verify mode that should be on the device
// right now (used by enrolment paths so new users land in the right state).
func (q *QRAuth) ModeForDevice(ctx context.Context, deviceID string) string {
	needsQR, _ := q.settings.DeviceRequiresQR(ctx, deviceID)
	if needsQR {
		return lockedVerifyMode
	}
	return unlockedVerifyMode
}

// ActiveSessions returns currently-open windows for the live panel.
func (q *QRAuth) ActiveSessions() []QRSession {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QRSession, 0, len(q.sessions))
	for _, s := range q.sessions {
		out = append(out, *s)
	}
	// newest first
	sortSessions(out)
	return out
}

func sortSessions(s []QRSession) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].OpenedAt.After(s[j-1].OpenedAt); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// History returns the recent finished sessions for audit.
func (q *QRAuth) History() []QRSession {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QRSession, len(q.history))
	copy(out, q.history)
	// reverse so newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// devicesForAgent returns the devices a QR scan should operate on.
//
//   - tenantID: REQUIRED. Multi-tenant scoping — never reach into another
//     tenant's devices even if the agent or qr-token resolution somehow
//     leaked an unscoped lookup.
//   - agentID: optional. If non-empty, restricts to devices linked to that
//     agent (the LAN bridge that the scanner sent the token through).
//
// Devices with no LAN address are skipped — they're typically stubs created
// in development. Operating on them would crash with "dial tcp :80: connect:
// connection refused".
func (q *QRAuth) devicesForAgent(ctx context.Context, tenantID, agentID string) ([]Device, error) {
	if tenantID == "" {
		return nil, errors.New("internal: tenantID required for QR device lookup")
	}
	all, err := q.store.ListDevicesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := []Device{}
	for _, d := range all {
		if agentID != "" && d.AgentID != agentID {
			continue
		}
		full, _ := q.store.GetDevice(ctx, d.DeviceID)
		if full == nil || full.IP == "" {
			continue
		}
		out = append(out, *full)
	}
	return out, nil
}

// tokenCandidates returns the set of strings to try when matching against
// qr_token. We always try the trimmed input first, then fallbacks that strip
// common scanner-programmed prefixes ("in#", "F1:", "qr:", etc).
func tokenCandidates(raw string) []string {
	// Trim leading/trailing whitespace + control bytes (incl. CR, LF, NUL)
	trimmed := strings.TrimFunc(raw, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\x00' || r < 0x20 || r == 0x7f
	})
	out := []string{trimmed}
	seen := map[string]bool{trimmed: true}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// After last "#" — handles "in#TOKEN", "anything#TOKEN"
	if i := strings.LastIndex(trimmed, "#"); i >= 0 && i < len(trimmed)-1 {
		add(trimmed[i+1:])
	}
	// After last ":" — handles "qr:TOKEN", "reader2:TOKEN"
	if i := strings.LastIndex(trimmed, ":"); i >= 0 && i < len(trimmed)-1 {
		add(trimmed[i+1:])
	}
	// Before first "#" — in case prefix is at the END (suffix scanners)
	if i := strings.Index(trimmed, "#"); i > 0 {
		add(trimmed[:i])
	}
	return out
}

func validOr(t *time.Time, fallback string) string {
	if t == nil {
		return fallback
	}
	return t.Format("2006-01-02T15:04:05")
}
