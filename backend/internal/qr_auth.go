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
	ID          string    `json:"id"`
	PersonID    string    `json:"personId"`
	EmployeeNo  string    `json:"employeeNo"`
	Name        string    `json:"name"`
	DeviceID    string    `json:"deviceId"`
	OpenedAt    time.Time `json:"openedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Status      string    `json:"status"` // open | face_matched | timed_out | cancelled
	cancel      context.CancelFunc
}

type QRAuth struct {
	store  *Store
	cfg    Config
	hub    *AgentHub
	window time.Duration

	mu       sync.Mutex
	sessions map[string]*QRSession // by employeeNo (one active session per user)
	history  []QRSession           // ring buffer for the live panel
}

func NewQRAuth(store *Store, cfg Config, hub *AgentHub) *QRAuth {
	return &QRAuth{
		store:    store,
		cfg:      cfg,
		hub:      hub,
		window:   5 * time.Second,
		sessions: map[string]*QRSession{},
	}
}

// Scan is the entry point — agent posts QR payload here.
// We find the person, unlock face on the device(s), spawn watchdog.
func (q *QRAuth) Scan(ctx context.Context, qrToken, agentID string) (*QRSession, error) {
	// Normalize: strip whitespace + control chars; try a few common scanner
	// prefix conventions if the exact token doesn't match.
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

	devices, err := q.devicesForAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New("no devices linked to this agent")
	}

	// Cancel any existing session for this user
	q.mu.Lock()
	if old, ok := q.sessions[p.EmployeeNo]; ok && old.cancel != nil {
		old.cancel()
	}
	q.mu.Unlock()

	// Unlock on every device served by the agent
	for _, d := range devices {
		if err := q.setUserVerifyMode(ctx, &d, p, unlockedVerifyMode); err != nil {
			log.Printf("[qr-auth] unlock %s on %s: %v", p.EmployeeNo, d.DeviceID, err)
			return nil, fmt.Errorf("unlock failed: %w", err)
		}
	}

	sessCtx, cancel := context.WithTimeout(context.Background(), q.window)
	sess := &QRSession{
		ID:         "qr-" + p.EmployeeNo + "-" + time.Now().Format("150405.000"),
		PersonID:   p.ID,
		EmployeeNo: p.EmployeeNo,
		Name:       p.Name,
		DeviceID:   devices[0].DeviceID,
		OpenedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(q.window),
		Status:     "open",
		cancel:     cancel,
	}
	q.mu.Lock()
	q.sessions[p.EmployeeNo] = sess
	q.mu.Unlock()

	go q.watchdog(sessCtx, sess, p, devices)
	log.Printf("[qr-auth] session %s OPENED for %s (%s) — window %s", sess.ID, p.EmployeeNo, p.Name, q.window)
	return sess, nil
}

// watchdog locks the user back as soon as a matching face event arrives,
// or when the timer expires.
func (q *QRAuth) watchdog(ctx context.Context, sess *QRSession, p *Person, devices []Device) {
	ch := q.store.Subscribe()
	defer q.store.Unsubscribe(ch)

	var status string
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				status = "cancelled"
				goto done
			}
			if q.eventMatchesPerson(e, p.EmployeeNo) {
				status = "face_matched"
				goto done
			}
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
	// Re-lock the user on every device
	bg := context.Background()
	for _, d := range devices {
		if err := q.setUserVerifyMode(bg, &d, p, lockedVerifyMode); err != nil {
			log.Printf("[qr-auth] LOCK %s on %s failed: %v", p.EmployeeNo, d.DeviceID, err)
		}
	}

	q.mu.Lock()
	sess.Status = status
	q.history = append(q.history, *sess)
	if len(q.history) > 200 {
		q.history = q.history[len(q.history)-200:]
	}
	if cur, ok := q.sessions[p.EmployeeNo]; ok && cur.ID == sess.ID {
		delete(q.sessions, p.EmployeeNo)
	}
	q.mu.Unlock()
	log.Printf("[qr-auth] session %s -> %s", sess.ID, status)
}

func (q *QRAuth) eventMatchesPerson(e Event, employeeNo string) bool {
	// Find employeeNoString anywhere in the raw payload
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(e.Raw, &probe); err != nil {
		return false
	}
	if v, ok := probe["AccessControllerEvent"]; ok {
		var ace struct {
			EmployeeNoString string `json:"employeeNoString"`
			MajorEventType   int    `json:"majorEventType"`
			SubEventType     int    `json:"subEventType"`
		}
		if err := json.Unmarshal(v, &ace); err == nil {
			if ace.EmployeeNoString == employeeNo {
				return true
			}
		}
	}
	// Some firmware reports differently
	for _, k := range []string{"employeeNoString", "EmployeeNoString"} {
		if v, ok := probe[k]; ok {
			var s string
			_ = json.Unmarshal(v, &s)
			if s == employeeNo {
				return true
			}
		}
	}
	return false
}

func (q *QRAuth) setUserVerifyMode(ctx context.Context, d *Device, p *Person, mode string) error {
	client := NewISAPIClientForDevice(d, q.hub)
	body, _ := json.Marshal(map[string]any{
		"UserInfo": map[string]any{
			"employeeNo":   p.EmployeeNo,
			"name":         p.Name,
			"userType":     mapDeviceUserType(p.PersonType),
			"localUIRight": p.PersonRole == "administrator",
			"userVerifyMode": mode,
			"gender":       ifElse(p.Gender == "", "unknown", p.Gender),
			"doorRight":    ifElse(p.DoorRight == "", "1", p.DoorRight),
			"RightPlan": []map[string]any{
				{"doorNo": 1, "planTemplateNo": ifElse(p.PlanTemplate == "", "1", p.PlanTemplate)},
			},
			"Valid": map[string]any{
				"enable":    !p.LongTerm,
				"beginTime": validOr(p.ValidBegin, "2020-01-01T00:00:00"),
				"endTime":   validOr(p.ValidEnd, "2037-12-31T23:59:59"),
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
// Run once when first enabling QR-auth on an existing site.
func (q *QRAuth) LockAllUsersOnDevice(ctx context.Context, deviceID string) (int, error) {
	d, err := q.store.GetDevice(ctx, deviceID)
	if err != nil || d == nil {
		return 0, fmt.Errorf("device not found")
	}
	client := NewISAPIClientForDevice(d, q.hub)
	users, err := client.ListUsers()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		// Build minimum body keeping device-side fields
		body, _ := json.Marshal(map[string]any{
			"UserInfo": map[string]any{
				"employeeNo":     u.EmployeeNo,
				"name":           u.Name,
				"userType":       firstNonEmpty(u.UserType, "normal"),
				"localUIRight":   false,
				"userVerifyMode": lockedVerifyMode,
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

// ActiveSessions returns currently-open windows for the live panel.
func (q *QRAuth) ActiveSessions() []QRSession {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QRSession, 0, len(q.sessions))
	for _, s := range q.sessions {
		out = append(out, *s)
	}
	return out
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

func (q *QRAuth) devicesForAgent(ctx context.Context, agentID string) ([]Device, error) {
	all, err := q.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := []Device{}
	for _, d := range all {
		if agentID == "" || d.AgentID == agentID {
			full, _ := q.store.GetDevice(ctx, d.DeviceID)
			if full != nil {
				out = append(out, *full)
			}
		}
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
