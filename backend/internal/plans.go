package internal

// ----------------------------------------------------------------------------
//  Membership plans + rule evaluation.
//
//  Three plan types:
//    unlimited  — every entry allowed
//    credit     — uses credits_remaining on person_plans; deduct on entry
//    rule       — passes only when current local time falls into ANY plan_rule
//                  row (weekday + start/end time).
//
//  Plans also carry must_exit_before_reentry. When true, a person who entered
//  must trigger an "out" event before being allowed back in.
//
//  We don't actually open the door — the camera does that based on its own
//  configured verify mode. Our job is to:
//    1. evaluate every face-match event (log allow/deny + reason)
//    2. for "deny" decisions, push verify mode = cardAndPw to the person on
//       the camera so the NEXT swipe doesn't succeed.
//    3. for "allow" decisions on credit plans, decrement credits.
//    4. flip the "inside" flag for must_exit tracking.
// ----------------------------------------------------------------------------

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	PlanUnlimited = "unlimited"
	PlanCredit    = "credit"
	PlanRuleType  = "rule"
)

// ---------------- Plan store ----------------

func (s *Store) CreatePlan(ctx context.Context, p Plan) (*Plan, error) {
	if p.ID == "" {
		p.ID = "pln_" + uuid.NewString()[:10]
	}
	if p.Type == "" {
		p.Type = PlanUnlimited
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO plans (id, tenant_id, name, type, default_credits, must_exit_before_reentry, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE
		SET name=$3, type=$4, default_credits=$5, must_exit_before_reentry=$6, active=$7
	`, p.ID, p.TenantID, p.Name, p.Type, p.DefaultCredits, p.MustExitBeforeReentry, p.Active)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) GetPlan(ctx context.Context, id string) (*Plan, error) {
	p := &Plan{}
	err := s.PG.QueryRow(ctx, `
		SELECT id, tenant_id, name, type, default_credits, must_exit_before_reentry, active, created_at
		FROM plans WHERE id=$1
	`, id).Scan(&p.ID, &p.TenantID, &p.Name, &p.Type, &p.DefaultCredits, &p.MustExitBeforeReentry, &p.Active, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Rules, _ = s.ListPlanRules(ctx, p.ID)
	return p, nil
}

func (s *Store) ListPlans(ctx context.Context, tenantID string) ([]Plan, error) {
	q := `SELECT id, tenant_id, name, type, default_credits, must_exit_before_reentry, active, created_at FROM plans`
	args := []any{}
	if tenantID != "" {
		q += ` WHERE tenant_id=$1`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Type, &p.DefaultCredits, &p.MustExitBeforeReentry, &p.Active, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	// Attach rules — small data sets, one extra query per plan is fine.
	for i := range out {
		out[i].Rules, _ = s.ListPlanRules(ctx, out[i].ID)
	}
	return out, rows.Err()
}

func (s *Store) DeletePlan(ctx context.Context, id string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM plans WHERE id=$1`, id)
	return err
}

// ---------------- Plan rules ----------------

func (s *Store) UpsertPlanRule(ctx context.Context, r PlanRule) (*PlanRule, error) {
	if r.ID == "" {
		r.ID = "rul_" + uuid.NewString()[:10]
	}
	if r.Weekdays == 0 {
		r.Weekdays = 127
	}
	if r.StartTime == "" {
		r.StartTime = "00:00"
	}
	if r.EndTime == "" {
		r.EndTime = "23:59"
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO plan_rules (id, plan_id, weekdays, start_time, end_time, label)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE
		SET weekdays=$3, start_time=$4, end_time=$5, label=$6
	`, r.ID, r.PlanID, r.Weekdays, r.StartTime, r.EndTime, r.Label)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) ListPlanRules(ctx context.Context, planID string) ([]PlanRule, error) {
	rows, err := s.PG.Query(ctx, `SELECT id, plan_id, weekdays, start_time, end_time, COALESCE(label,'') FROM plan_rules WHERE plan_id=$1 ORDER BY id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanRule{}
	for rows.Next() {
		var r PlanRule
		if err := rows.Scan(&r.ID, &r.PlanID, &r.Weekdays, &r.StartTime, &r.EndTime, &r.Label); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeletePlanRule(ctx context.Context, id string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM plan_rules WHERE id=$1`, id)
	return err
}

// ---------------- Person → plan ----------------

func (s *Store) AssignPersonPlan(ctx context.Context, personID, planID string, credits int) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO person_plans (person_id, plan_id, credits_remaining)
		VALUES ($1,$2,$3)
		ON CONFLICT (person_id) DO UPDATE
		SET plan_id=$2, credits_remaining=$3, assigned_at=NOW()
	`, personID, planID, credits)
	return err
}

func (s *Store) GetPersonPlan(ctx context.Context, personID string) (*PersonPlan, error) {
	pp := &PersonPlan{}
	var planID *string
	err := s.PG.QueryRow(ctx, `
		SELECT person_id, plan_id, credits_remaining, inside, last_event_at, assigned_at
		FROM person_plans WHERE person_id=$1
	`, personID).Scan(&pp.PersonID, &planID, &pp.CreditsRemaining, &pp.Inside, &pp.LastEventAt, &pp.AssignedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if planID != nil {
		pp.PlanID = *planID
	}
	return pp, nil
}

func (s *Store) DecrementCredit(ctx context.Context, personID string) (int, error) {
	var remaining int
	err := s.PG.QueryRow(ctx, `
		UPDATE person_plans SET credits_remaining = GREATEST(credits_remaining - 1, 0), last_event_at = NOW()
		WHERE person_id=$1 RETURNING credits_remaining
	`, personID).Scan(&remaining)
	return remaining, err
}

func (s *Store) SetPersonInside(ctx context.Context, personID string, inside bool) error {
	_, err := s.PG.Exec(ctx, `
		UPDATE person_plans SET inside=$1, last_event_at=NOW() WHERE person_id=$2
	`, inside, personID)
	return err
}

// ---------------- Device → plan ----------------

func (s *Store) AssignDeviceToPlan(ctx context.Context, deviceID, planID string) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO device_plans (device_id, plan_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING
	`, deviceID, planID)
	return err
}

func (s *Store) UnassignDeviceFromPlan(ctx context.Context, deviceID, planID string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM device_plans WHERE device_id=$1 AND plan_id=$2`, deviceID, planID)
	return err
}

func (s *Store) ListDevicePlans(ctx context.Context, deviceID string) ([]string, error) {
	rows, err := s.PG.Query(ctx, `SELECT plan_id FROM device_plans WHERE device_id=$1`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------------- Access log ----------------

func (s *Store) WriteAccessLog(ctx context.Context, e AccessLog) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO access_log (tenant_id, person_id, employee_no, device_id, decision, reason, direction)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, e.TenantID, nullIfEmpty(e.PersonID), e.EmployeeNo, e.DeviceID, e.Decision, e.Reason, e.Direction)
	return err
}

func (s *Store) ListAccessLog(ctx context.Context, tenantID string, limit int) ([]AccessLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT id, COALESCE(tenant_id,''), COALESCE(person_id,''), COALESCE(employee_no,''), COALESCE(device_id,''), decision, COALESCE(reason,''), COALESCE(direction,''), created_at
	      FROM access_log`
	args := []any{}
	if tenantID != "" {
		q += ` WHERE tenant_id=$1`
		args = append(args, tenantID)
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, limit)
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessLog{}
	for rows.Next() {
		var a AccessLog
		if err := rows.Scan(&a.ID, &a.TenantID, &a.PersonID, &a.EmployeeNo, &a.DeviceID, &a.Decision, &a.Reason, &a.Direction, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---------------- Rule evaluator ----------------

// AccessDecision is the result of evaluating a person's plan against a moment in time.
type AccessDecision struct {
	Allow     bool   `json:"allow"`
	Reason    string `json:"reason"`
	PlanID    string `json:"planId,omitempty"`
	PlanType  string `json:"planType,omitempty"`
	Direction string `json:"direction,omitempty"` // "in" | "out"
}

// EvaluateAccess decides whether a person should be allowed to enter a device
// right now. It does NOT mutate state — callers commit consequences via
// CommitAccess() once the decision is final.
func (s *Store) EvaluateAccess(ctx context.Context, personID string, when time.Time) AccessDecision {
	pp, _ := s.GetPersonPlan(ctx, personID)
	if pp == nil || pp.PlanID == "" {
		// No plan assigned → default behaviour is OBSERVE (don't block).
		return AccessDecision{Allow: true, Reason: "no plan assigned"}
	}
	plan, _ := s.GetPlan(ctx, pp.PlanID)
	if plan == nil {
		return AccessDecision{Allow: true, Reason: "plan missing"}
	}
	if !plan.Active {
		return AccessDecision{Allow: false, PlanID: plan.ID, PlanType: plan.Type, Reason: "plan inactive"}
	}

	// Rules are specified in HH:MM *local* time as the operator types them
	// in the UI — but `when` arrives in UTC (the container clock). Convert
	// to the tenant's configured timezone (default "Asia/Kuala_Lumpur") so
	// "15:00-15:25" means 15:00-15:25 in the tenant's locale, not UTC.
	if plan.Type == PlanRuleType {
		if t, _ := s.GetTenant(ctx, plan.TenantID); t != nil && t.Timezone != "" {
			if loc, err := time.LoadLocation(t.Timezone); err == nil {
				when = when.In(loc)
			}
		}
	}

	// Must-exit-before-reenter check: if already inside, this is an "out" event.
	direction := "in"
	if plan.MustExitBeforeReentry && pp.Inside {
		direction = "out"
	}

	switch plan.Type {
	case PlanUnlimited:
		return AccessDecision{Allow: true, PlanID: plan.ID, PlanType: plan.Type, Direction: direction, Reason: "unlimited"}
	case PlanCredit:
		if direction == "out" {
			return AccessDecision{Allow: true, PlanID: plan.ID, PlanType: plan.Type, Direction: "out", Reason: "exit"}
		}
		if pp.CreditsRemaining <= 0 {
			return AccessDecision{Allow: false, PlanID: plan.ID, PlanType: plan.Type, Reason: "no credits remaining"}
		}
		return AccessDecision{Allow: true, PlanID: plan.ID, PlanType: plan.Type, Direction: direction, Reason: fmt.Sprintf("credit (%d remaining)", pp.CreditsRemaining-1)}
	case PlanRuleType:
		if direction == "out" {
			return AccessDecision{Allow: true, PlanID: plan.ID, PlanType: plan.Type, Direction: "out", Reason: "exit"}
		}
		if matched, lbl := matchAnyRule(plan.Rules, when); matched {
			return AccessDecision{Allow: true, PlanID: plan.ID, PlanType: plan.Type, Direction: direction, Reason: "rule: " + lbl}
		}
		return AccessDecision{Allow: false, PlanID: plan.ID, PlanType: plan.Type, Reason: "outside allowed time window"}
	}
	return AccessDecision{Allow: false, PlanID: plan.ID, Reason: "unknown plan type"}
}

// CommitAccess updates state after a decision: credit decrement, inside flag,
// and the access_log row. Idempotent — safe to call on duplicate events from
// the camera (though we don't dedupe internally).
func (s *Store) CommitAccess(ctx context.Context, decision AccessDecision, personID, employeeNo, deviceID, tenantID string) error {
	if decision.Allow {
		if decision.PlanType == PlanCredit && decision.Direction == "in" {
			_, _ = s.DecrementCredit(ctx, personID)
		}
		// Toggle inside state
		if decision.Direction == "in" {
			_ = s.SetPersonInside(ctx, personID, true)
		} else if decision.Direction == "out" {
			_ = s.SetPersonInside(ctx, personID, false)
		}
	}
	return s.WriteAccessLog(ctx, AccessLog{
		TenantID:   tenantID,
		PersonID:   personID,
		EmployeeNo: employeeNo,
		DeviceID:   deviceID,
		Decision:   ifElse(decision.Allow, "allow", "deny"),
		Reason:     decision.Reason,
		Direction:  decision.Direction,
	})
}

// matchAnyRule returns true if `when` falls into at least one rule's
// weekday + time window. Weekday bitmask: bit0=Mon..bit6=Sun.
//
// Boundaries are [start, end) — start is inclusive, end is EXCLUSIVE. So a
// rule "15:00–15:15" allows entries from 15:00:00 through 15:14:59 and the
// gate snaps shut at exactly 15:15:00. This matches the common-sense reading
// of "the window ends at 15:15".
func matchAnyRule(rules []PlanRule, when time.Time) (bool, string) {
	wd := int(when.Weekday())       // 0=Sun..6=Sat
	wdBit := ((wd + 6) % 7)          // remap so Mon=0
	mask := 1 << uint(wdBit)
	cur := when.Hour()*60 + when.Minute()
	for _, r := range rules {
		if r.Weekdays&mask == 0 {
			continue
		}
		s := parseHMM(r.StartTime)
		e := parseHMM(r.EndTime)
		// Treat end < start as "crosses midnight" (e.g. 22:00 - 02:00).
		if e < s {
			if cur >= s || cur < e {
				return true, ruleLabel(r)
			}
		} else {
			if cur >= s && cur < e {
				return true, ruleLabel(r)
			}
		}
	}
	return false, ""
}

func ruleLabel(r PlanRule) string {
	if r.Label != "" {
		return r.Label
	}
	return r.StartTime + "-" + r.EndTime
}

func parseHMM(s string) int {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0
	}
	h, _ := atoiSafe(parts[0])
	m, _ := atoiSafe(parts[1])
	return h*60 + m
}

func atoiSafe(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("nan")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
