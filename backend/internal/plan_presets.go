package internal

// ----------------------------------------------------------------------------
//  Plan preset library — premise-type-specific starter plans.
//
//  Each premise type ships with a curated set of plans that match the way
//  real-world businesses sell access. The admin picks a premise type when
//  creating a tenant; later, in the Plans tab, they can "Install preset"
//  to materialise these plans (and rules) in seconds.
//
//  Every preset is fully editable after install — they're just convenient
//  templates, nothing structural.
// ----------------------------------------------------------------------------

import (
	"context"
	"fmt"
)

// PremiseType describes a known business pattern.
type PremiseType struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Presets     []PlanPreset   `json:"presets"`
}

// PlanPreset is a serializable plan template; rules carry the weekday + time
// windows verbatim from the design (e.g. "06:00-22:00 every day").
type PlanPreset struct {
	Name                  string       `json:"name"`
	Description           string       `json:"description"`
	Type                  string       `json:"type"`           // unlimited | credit | rule
	DefaultCredits        int          `json:"defaultCredits"`
	MustExitBeforeReentry bool         `json:"mustExitBeforeReentry"`
	Rules                 []RulePreset `json:"rules,omitempty"`
}

type RulePreset struct {
	Label     string `json:"label"`
	Weekdays  int    `json:"weekdays"`  // bitmask: bit0=Mon..bit6=Sun
	StartTime string `json:"startTime"` // HH:MM
	EndTime   string `json:"endTime"`
}

// Convenience weekday bitmask constants
const (
	wkAll      = 0b1111111 // Mon-Sun (127)
	wkWeekdays = 0b0011111 // Mon-Fri (31)
	wkSatSun   = 0b1100000 // Sat-Sun (96)
	wkMonSat   = 0b0111111 // Mon-Sat (63)
)

// PremiseTypes is the canonical list of supported premise patterns. Add a new
// entry here to expose a new option in the HQ "premise type" dropdown.
var PremiseTypes = []PremiseType{
	{
		Key:         "gym",
		Label:       "Gym / Fitness",
		Description: "Members buy access by visits, peak/off-peak windows, or unlimited monthly.",
		Presets: []PlanPreset{
			{
				Name: "Unlimited Member", Type: PlanUnlimited,
				Description: "Walk in any time, no caps.",
			},
			{
				Name: "10-Visit Pass", Type: PlanCredit, DefaultCredits: 10,
				Description: "Each entry burns 1 credit.",
			},
			{
				Name: "30-Visit Pass", Type: PlanCredit, DefaultCredits: 30,
				Description: "Bigger bundle; usually 3-month validity.",
			},
			{
				Name: "Off-Peak (Daytime)", Type: PlanRuleType,
				MustExitBeforeReentry: true,
				Description: "Weekday 10:00–16:00 only — cheaper tier for retirees / students.",
				Rules: []RulePreset{
					{Label: "Daytime", Weekdays: wkWeekdays, StartTime: "10:00", EndTime: "16:00"},
				},
			},
			{
				Name: "Morning + Evening", Type: PlanRuleType,
				Description: "Two daily windows — typical commuter member.",
				Rules: []RulePreset{
					{Label: "Morning", Weekdays: wkMonSat, StartTime: "06:00", EndTime: "09:00"},
					{Label: "Evening", Weekdays: wkAll, StartTime: "19:00", EndTime: "22:00"},
				},
			},
		},
	},
	{
		Key:         "property",
		Label:       "Property / Condo",
		Description: "Residents are unlimited; visitors/contractors are restricted by day/time.",
		Presets: []PlanPreset{
			{
				Name: "Resident", Type: PlanUnlimited,
				Description: "Owners + tenants; 24/7 access.",
			},
			{
				Name: "Family / Helper", Type: PlanUnlimited,
				MustExitBeforeReentry: true,
				Description: "Same as resident, but must exit before re-entry (anti-passback).",
			},
			{
				Name: "Contractor (Daytime)", Type: PlanRuleType,
				MustExitBeforeReentry: true,
				Description: "Mon–Sat 08:00–18:00 — renovators, technicians, cleaners.",
				Rules: []RulePreset{
					{Label: "Working hours", Weekdays: wkMonSat, StartTime: "08:00", EndTime: "18:00"},
				},
			},
			{
				Name: "Visitor (24h pass)", Type: PlanCredit, DefaultCredits: 2,
				Description: "Single-day visitor: one in, one out.",
			},
		},
	},
	{
		Key:         "office",
		Label:       "Office / Co-working",
		Description: "Staff weekday access; members buy day-pass credits or office hours.",
		Presets: []PlanPreset{
			{
				Name: "Staff", Type: PlanRuleType,
				Description: "Mon-Fri 07:00–22:00.",
				Rules: []RulePreset{
					{Label: "Weekday office hours", Weekdays: wkWeekdays, StartTime: "07:00", EndTime: "22:00"},
				},
			},
			{
				Name: "Hot-desk Day Pass", Type: PlanCredit, DefaultCredits: 5,
				Description: "5-pack of single-day passes.",
			},
			{
				Name: "24/7 Founder Plan", Type: PlanUnlimited,
				Description: "All-access for founders / executives.",
			},
			{
				Name: "Cleaning Crew (Late Night)", Type: PlanRuleType,
				MustExitBeforeReentry: true,
				Description: "Cleaners after-hours window.",
				Rules: []RulePreset{
					{Label: "Late night", Weekdays: wkAll, StartTime: "22:00", EndTime: "05:00"},
				},
			},
		},
	},
	{
		Key:         "parking",
		Label:       "Parking",
		Description: "Season passes vs. visitor credits.",
		Presets: []PlanPreset{
			{
				Name: "Season Pass", Type: PlanUnlimited,
				MustExitBeforeReentry: true,
				Description: "Monthly subscriber; must exit before re-entry.",
			},
			{
				Name: "Daily Visitor (5x)", Type: PlanCredit, DefaultCredits: 5,
				MustExitBeforeReentry: true,
				Description: "Five entries; counted by entry, not duration.",
			},
			{
				Name: "Weekend Only", Type: PlanRuleType,
				MustExitBeforeReentry: true,
				Description: "Sat–Sun all day.",
				Rules: []RulePreset{
					{Label: "Weekend", Weekdays: wkSatSun, StartTime: "00:00", EndTime: "23:59"},
				},
			},
		},
	},
	{
		Key:         "club",
		Label:       "Club / Lounge",
		Description: "Members have rule-based hours; guests buy single-entry credits.",
		Presets: []PlanPreset{
			{
				Name: "Standard Member", Type: PlanRuleType,
				Description: "Open hours, every day.",
				Rules: []RulePreset{
					{Label: "Open hours", Weekdays: wkAll, StartTime: "17:00", EndTime: "02:00"},
				},
			},
			{
				Name: "VIP Member", Type: PlanUnlimited,
				Description: "All-hours access including private rooms.",
			},
			{
				Name: "Guest (1 entry)", Type: PlanCredit, DefaultCredits: 1,
				Description: "Single-night pass.",
			},
		},
	},
	{
		Key:         "school",
		Label:       "School / Campus",
		Description: "Students access during class hours; staff longer; weekends restricted.",
		Presets: []PlanPreset{
			{
				Name: "Student", Type: PlanRuleType,
				MustExitBeforeReentry: true,
				Description: "Mon–Fri 07:00–18:00.",
				Rules: []RulePreset{
					{Label: "School hours", Weekdays: wkWeekdays, StartTime: "07:00", EndTime: "18:00"},
				},
			},
			{
				Name: "Teacher / Staff", Type: PlanRuleType,
				Description: "Mon–Sat 06:00–21:00.",
				Rules: []RulePreset{
					{Label: "Extended", Weekdays: wkMonSat, StartTime: "06:00", EndTime: "21:00"},
				},
			},
			{
				Name: "Maintenance (Weekend)", Type: PlanRuleType,
				MustExitBeforeReentry: true,
				Description: "Weekend maintenance access.",
				Rules: []RulePreset{
					{Label: "Weekend maint", Weekdays: wkSatSun, StartTime: "08:00", EndTime: "17:00"},
				},
			},
		},
	},
	{
		Key:         "hotel",
		Label:       "Hotel / Hostel",
		Description: "Guests get unlimited access while their booking is valid; staff have shifts.",
		Presets: []PlanPreset{
			{
				Name: "Guest", Type: PlanUnlimited,
				Description: "Unlimited for the booking window (manage validity via person record).",
			},
			{
				Name: "Day Shift Staff", Type: PlanRuleType,
				Description: "Mon-Sun 06:00-15:00.",
				Rules: []RulePreset{
					{Label: "Day shift", Weekdays: wkAll, StartTime: "06:00", EndTime: "15:00"},
				},
			},
			{
				Name: "Night Shift Staff", Type: PlanRuleType,
				Description: "Mon-Sun 22:00-07:00.",
				Rules: []RulePreset{
					{Label: "Night shift", Weekdays: wkAll, StartTime: "22:00", EndTime: "07:00"},
				},
			},
		},
	},
	{
		Key:         "generic",
		Label:       "Generic / Custom",
		Description: "Blank slate with the three core plan types — build from scratch.",
		Presets: []PlanPreset{
			{Name: "Unlimited", Type: PlanUnlimited, Description: "Every entry allowed."},
			{Name: "Credit Pass", Type: PlanCredit, DefaultCredits: 10, Description: "Each entry uses one credit."},
			{
				Name: "Scheduled", Type: PlanRuleType,
				Description: "Allowed during one or more weekday + time windows.",
				Rules: []RulePreset{
					{Label: "Default window", Weekdays: wkWeekdays, StartTime: "09:00", EndTime: "18:00"},
				},
			},
		},
	},
}

// FindPremiseType returns the preset bundle for a given key.
func FindPremiseType(key string) *PremiseType {
	for i := range PremiseTypes {
		if PremiseTypes[i].Key == key {
			return &PremiseTypes[i]
		}
	}
	return nil
}

// InstallPresets materialises one or more preset plans for a tenant.
// Returns the IDs of the plans created. Existing plans with the same name in
// the tenant are skipped (idempotent — running install twice doesn't duplicate).
func (s *Store) InstallPresets(ctx context.Context, tenantID string, presets []PlanPreset) ([]Plan, error) {
	existing, _ := s.ListPlans(ctx, tenantID)
	byName := map[string]bool{}
	for _, p := range existing {
		byName[p.Name] = true
	}

	out := []Plan{}
	for _, p := range presets {
		if byName[p.Name] {
			continue
		}
		plan, err := s.CreatePlan(ctx, Plan{
			TenantID:              tenantID,
			Name:                  p.Name,
			Type:                  p.Type,
			DefaultCredits:        p.DefaultCredits,
			MustExitBeforeReentry: p.MustExitBeforeReentry,
			Active:                true,
		})
		if err != nil {
			return out, fmt.Errorf("create %s: %w", p.Name, err)
		}
		for _, r := range p.Rules {
			_, _ = s.UpsertPlanRule(ctx, PlanRule{
				PlanID:    plan.ID,
				Weekdays:  r.Weekdays,
				StartTime: r.StartTime,
				EndTime:   r.EndTime,
				Label:     r.Label,
			})
		}
		full, _ := s.GetPlan(ctx, plan.ID)
		if full != nil {
			out = append(out, *full)
		}
	}
	return out, nil
}
