package internal

// Background goroutine that watches the live event stream, evaluates each
// face-match against the person's plan, writes an access_log row, and
// (for deny decisions) flips the user's verify mode to cardAndPw so the
// next face attempt fails at the camera.

import (
	"context"
	"log"
	"time"
)

// StartPolicyRunner subscribes to the store's event fanout and processes
// face-match events. Stops when the returned cancel func is called.
func StartPolicyRunner(store *Store, hub *AgentHub) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go runPolicy(ctx, store, hub)
	return cancel
}

func runPolicy(ctx context.Context, store *Store, hub *AgentHub) {
	ch := store.Subscribe()
	defer store.Unsubscribe(ch)
	log.Printf("[policy] runner started")
	for {
		select {
		case <-ctx.Done():
			log.Printf("[policy] runner stopped")
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			emp, isFace := extractFaceMatchFromEvent(e)
			if !isFace || emp == "" {
				continue
			}
			processFaceMatch(store, hub, e, emp)
		}
	}
}

func processFaceMatch(store *Store, hub *AgentHub, e Event, employeeNo string) {
	bg := context.Background()

	tenantID, _ := store.GetDeviceTenant(bg, e.DeviceID)
	if tenantID == "" {
		// Device has no tenant — treat as legacy / no policy.
		_ = store.WriteAccessLog(bg, AccessLog{
			TenantID:   tenantID,
			EmployeeNo: employeeNo,
			DeviceID:   e.DeviceID,
			Decision:   "observed",
			Reason:     "device has no tenant",
		})
		return
	}

	person, _ := store.FindPersonByEmployeeNoInTenant(bg, tenantID, employeeNo)
	if person == nil {
		_ = store.WriteAccessLog(bg, AccessLog{
			TenantID:   tenantID,
			EmployeeNo: employeeNo,
			DeviceID:   e.DeviceID,
			Decision:   "observed",
			Reason:     "unknown person",
		})
		return
	}

	now := e.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}
	decision := store.EvaluateAccess(bg, person.ID, now)
	_ = store.CommitAccess(bg, decision, person.ID, employeeNo, e.DeviceID, tenantID)

	// If the decision was deny, push verify_mode = cardAndPw to the user on the
	// device so the very next swipe is refused. This is best-effort.
	if !decision.Allow {
		d, _ := store.GetDevice(bg, e.DeviceID)
		if d != nil && d.IP != "" {
			client := NewISAPIClientForDevice(d, hub)
			// Same "expired Valid window" trick as the scheduler — the next
			// face match will hit a denial sound + on-screen "user invalid"
			// instead of being silently ignored.
			hikUser := HikUserInfo{
				EmployeeNo:     person.EmployeeNo,
				Name:           person.Name,
				UserType:       person.PersonType,
				Gender:         person.Gender,
				LongTerm:       false,
				ValidBegin:     "2000-01-01T00:00:00",
				ValidEnd:       "2000-12-31T23:59:59",
				DoorRight:      person.DoorRight,
				PlanTemplate:   person.PlanTemplate,
				LocalUIRight:   person.PersonRole == "administrator",
				CheckUser:      person.AttendanceOnly,
				UserVerifyMode: unlockedVerifyMode, // keep face matching active so denial is audible
			}
			if _, err := client.UpsertUserOnDevice(hikUser); err != nil {
				log.Printf("[policy] lock %s on %s after deny: %v", employeeNo, e.DeviceID, err)
			}
		}
	}

	log.Printf("[policy] device=%s emp=%s decision=%s reason=%q dir=%s",
		e.DeviceID, employeeNo, ifElse(decision.Allow, "allow", "deny"), decision.Reason, decision.Direction)
}
