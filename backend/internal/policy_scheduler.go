package internal

// ----------------------------------------------------------------------------
//  Policy scheduler — proactive enforcement of plan rules on the camera.
//
//  Without this, the rule evaluator in policy_runner.go is purely REACTIVE:
//  it only decides allow/deny *after* the camera has already opened the door
//  on a face match. So the first scan after a window closes still succeeds.
//
//  This scheduler runs every minute and, for every person with an assigned
//  plan, computes whether they should *currently* be in face-mode or locked,
//  then pushes the corresponding userVerifyMode to every device in their
//  tenant. State changes are cached in-memory so repeated ticks don't spam
//  the ISAPI endpoint with redundant PUTs.
//
//  Combined with the rule evaluator (which still runs on face-match events
//  to log allow/deny + decrement credits), this gives:
//
//    - Pre-flight enforcement   — door physically refuses outside the window
//    - Audit log                — every attempt is logged with reason + dir
//    - Credit decrement         — on every "in" event
//    - Anti-passback flag       — person_plans.inside is toggled
// ----------------------------------------------------------------------------

import (
	"context"
	"log"
	"sync"
	"time"
)

// schedulerTick is how often the policy scheduler re-evaluates every person.
// One minute is the natural cadence for HH:MM-granularity rules.
const schedulerTick = 60 * time.Second

type policyState struct {
	// key = personID + "|" + deviceID, value = last verify mode pushed
	mu       sync.Mutex
	lastMode map[string]string
}

func newPolicyState() *policyState {
	return &policyState{lastMode: map[string]string{}}
}

func (p *policyState) get(personID, deviceID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastMode[personID+"|"+deviceID]
}

func (p *policyState) set(personID, deviceID, mode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastMode[personID+"|"+deviceID] = mode
}

// StartPolicyScheduler kicks off the periodic enforcement goroutine. Returns
// a cancel func — call it to stop the scheduler on shutdown.
func StartPolicyScheduler(store *Store, hub *AgentHub) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	state := newPolicyState()
	go runScheduler(ctx, store, hub, state)
	return cancel
}

func runScheduler(ctx context.Context, store *Store, hub *AgentHub, state *policyState) {
	// One immediate tick so the system is consistent right after restart,
	// then settle into the periodic cadence.
	tick(ctx, store, hub, state)
	t := time.NewTicker(schedulerTick)
	defer t.Stop()
	log.Printf("[policy-scheduler] started (every %s)", schedulerTick)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[policy-scheduler] stopped")
			return
		case <-t.C:
			tick(ctx, store, hub, state)
		}
	}
}

// tick is one pass over every person→plan assignment.
func tick(ctx context.Context, store *Store, hub *AgentHub, state *policyState) {
	rows, err := store.PG.Query(ctx, `
		SELECT pp.person_id, pp.plan_id, p.tenant_id, p.employee_no, p.name,
		       p.person_type, COALESCE(p.gender,''), p.long_term,
		       COALESCE(p.door_right,'1'), COALESCE(p.plan_template,'1'),
		       (p.person_role='administrator') AS local_ui_right,
		       p.attendance_only
		FROM person_plans pp
		JOIN persons p ON p.id = pp.person_id
		WHERE pp.plan_id IS NOT NULL AND pp.plan_id <> ''
	`)
	if err != nil {
		log.Printf("[policy-scheduler] list persons: %v", err)
		return
	}
	defer rows.Close()

	type personRow struct {
		PersonID, PlanID, TenantID                 string
		EmployeeNo, Name, PersonType, Gender       string
		LongTerm, LocalUIRight, AttendanceOnly     bool
		DoorRight, PlanTemplate                    string
	}
	persons := []personRow{}
	for rows.Next() {
		var pr personRow
		var tenantID *string
		if err := rows.Scan(&pr.PersonID, &pr.PlanID, &tenantID,
			&pr.EmployeeNo, &pr.Name, &pr.PersonType, &pr.Gender, &pr.LongTerm,
			&pr.DoorRight, &pr.PlanTemplate, &pr.LocalUIRight, &pr.AttendanceOnly); err != nil {
			continue
		}
		if tenantID != nil {
			pr.TenantID = *tenantID
		}
		if pr.EmployeeNo == "" || pr.TenantID == "" {
			continue
		}
		persons = append(persons, pr)
	}

	if len(persons) == 0 {
		return
	}

	// Cache device lists per tenant — typically the same tenant has a small
	// number of devices and many persons.
	devicesCache := map[string][]Device{}
	now := time.Now()

	pushed := 0
	for _, pr := range persons {
		// Decide what mode the person SHOULD be in right now.
		decision := store.EvaluateAccess(ctx, pr.PersonID, now)
		want := lockedVerifyMode
		if decision.Allow {
			want = unlockedVerifyMode
		}

		// Resolve devices for this person's tenant (filter to those with IP).
		devs, ok := devicesCache[pr.TenantID]
		if !ok {
			all, err := store.ListDevicesByTenant(ctx, pr.TenantID)
			if err != nil {
				continue
			}
			devs = make([]Device, 0, len(all))
			for _, d := range all {
				full, _ := store.GetDevice(ctx, d.DeviceID)
				if full != nil && full.IP != "" {
					devs = append(devs, *full)
				}
			}
			devicesCache[pr.TenantID] = devs
		}

		for _, d := range devs {
			if state.get(pr.PersonID, d.DeviceID) == want {
				continue // already in desired state, skip the ISAPI call
			}
			client := NewISAPIClientForDevice(&d, hub)
			// IMPORTANT: keep userVerifyMode=face in both states so the camera
			// actually *matches* the user's face and produces visible/audible
			// feedback. Distinguish allow/deny via the Valid window:
			//
			//   allow → Valid 2020-01-01 .. 2037-12-31 (always valid)
			//   deny  → Valid 2020-01-01 .. 2020-01-01 (expired in 2020)
			//
			// With an expired window, Hik recognises the face, checks Valid,
			// sees the user is out of validity, plays the denial sound, shows
			// "Invalid period" / "Authentication failed" on the LCD, and sends
			// a denial event to the alarm host (which face_auth ingests).
			// That is the feedback the operator and the visitor were missing
			// with the silent cardAndPw lockout.
			beginTime := "2020-01-01T00:00:00"
			endTime := "2037-12-31T23:59:59"
			longTerm := pr.LongTerm
			if want == lockedVerifyMode {
				// Currently denied — set a fully-expired Valid window in the
				// year 2000. Hik rejects zero-length windows (begin == end),
				// so use a 1-year range to be safe.
				beginTime = "2000-01-01T00:00:00"
				endTime = "2000-12-31T23:59:59"
				longTerm = false // Valid.enable must be true so Hik checks the dates
			}
			hikUser := HikUserInfo{
				EmployeeNo:     pr.EmployeeNo,
				Name:           pr.Name,
				UserType:       pr.PersonType,
				Gender:         pr.Gender,
				LongTerm:       longTerm,
				ValidBegin:     beginTime,
				ValidEnd:       endTime,
				DoorRight:      pr.DoorRight,
				PlanTemplate:   pr.PlanTemplate,
				LocalUIRight:   pr.LocalUIRight,
				CheckUser:      pr.AttendanceOnly,
				UserVerifyMode: unlockedVerifyMode, // always "face" — feedback path
			}
			if _, err := client.UpsertUserOnDevice(hikUser); err != nil {
				log.Printf("[policy-scheduler] %s on %s → %s: %v", pr.EmployeeNo, d.DeviceID, want, err)
				continue
			}
			state.set(pr.PersonID, d.DeviceID, want)
			pushed++
		}
	}
	if pushed > 0 {
		log.Printf("[policy-scheduler] tick: %d person×device verify-mode updates pushed", pushed)
	}
}
