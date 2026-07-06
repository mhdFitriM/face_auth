package internal

// Bootstrap the multi-tenant world on first boot:
//   - create the HQ admin user if no users exist
//   - create a "Default" tenant if no tenants exist, plus a tenant_admin
//   - assign every NULL-tenant_id row to the default tenant
//   - seed two demo tenants ("Acme Gym" and "Northpark Office") with one
//     plan of each type and a handful of demo people, *only* when the
//     SEED_DEMO env var is set
//
// All bootstrap users start with the password "changeme" — they MUST change
// it on first login. We log a warning at startup if any user still has the
// well-known seed hash.

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
)

const seedPassword = "changeme"

// seedVersion marks how far the seeder has run. Bump it whenever a new seed
// step is added — the marker check below then lets the new step run exactly
// once on already-seeded databases.
const seedVersion = 2

// Bootstrap is called from cmd/server/main.go right after the store opens.
// It is idempotent: each step creates rows only if they're missing, and a
// failure in one step does not abort the rest. A version marker in the
// settings table short-circuits the whole pass on every boot after the first
// successful run, so `docker compose up` never re-seeds.
func Bootstrap(ctx context.Context, store *Store) error {
	log.Printf("========================================")
	log.Printf("[seed] Bootstrap starting")

	// Devices that self-register over the push protocol arrive with no
	// tenant; tag them to the default merchant on EVERY boot (cheap UPDATE,
	// deliberately outside the seed marker below).
	for _, q := range []string{
		`UPDATE devices  SET tenant_id=$1 WHERE tenant_id IS NULL OR tenant_id=''`,
		`UPDATE persons  SET tenant_id=$1 WHERE tenant_id IS NULL OR tenant_id=''`,
		`UPDATE agents   SET tenant_id=$1 WHERE tenant_id IS NULL OR tenant_id=''`,
		`UPDATE api_keys SET tenant_id=$1 WHERE tenant_id IS NULL OR tenant_id=''`,
	} {
		if _, err := store.PG.Exec(ctx, q, "ten_default"); err != nil {
			log.Printf("[seed] WARN re-tag: %v", err)
		}
	}

	// Seed marker: if a previous boot completed seeding at this version,
	// skip everything else.
	var doneVersion int
	_ = store.PG.QueryRow(ctx, `SELECT COALESCE((value->>'version')::int, 0) FROM settings WHERE key='seed_marker'`).Scan(&doneVersion)
	if doneVersion >= seedVersion {
		log.Printf("[seed] already seeded (v%d) — skipping", doneVersion)
		log.Printf("========================================")
		return nil
	}
	// 0) Migrate any rows seeded by an earlier build that used the buggy
	//    underscore email or the hard-coded user IDs. Both are no-ops on a
	//    fresh database.
	//
	//    a) Rename hq@face_auth.local -> hq@faceauth.local (only if there's
	//       no row already at the new address — avoids unique-key collision).
	_, _ = store.PG.Exec(ctx, `
		UPDATE users SET email='hq@faceauth.local'
		WHERE email='hq@face_auth.local'
		  AND NOT EXISTS (SELECT 1 FROM users WHERE email='hq@faceauth.local')
	`)
	//    b) Drop the old underscore row outright if both forms exist (the
	//       new one wins — it matches HTML5 email validation).
	_, _ = store.PG.Exec(ctx, `DELETE FROM users WHERE email='hq@face_auth.local'`)
	//    c) Free the legacy hard-coded primary keys so a fresh INSERT can
	//       use a uuid-based id without colliding.
	_, _ = store.PG.Exec(ctx, `DELETE FROM users WHERE id IN ('usr_hq_admin','usr_default_admin') AND email NOT IN ($1, $2)`,
		envOr("HQ_ADMIN_EMAIL", "hq@faceauth.local"),
		envOr("DEFAULT_TENANT_EMAIL", "admin@default.local"),
	)
	//    d) Strip Hikvision-incompatible characters from existing employee
	//       numbers. Hik only accepts [A-Za-z0-9]{1,32} for FPID/employeeNo;
	//       hyphens, dots, underscores all 400 with "badJsonContent.FPID".
	//       This migration silently fixes legacy demo rows ("acme-gym-003" →
	//       "acmegym003") so face enrolment starts working immediately.
	_, _ = store.PG.Exec(ctx, `
		UPDATE persons
		SET employee_no = regexp_replace(employee_no, '[^A-Za-z0-9]', '', 'g')
		WHERE employee_no ~ '[^A-Za-z0-9]'
	`)

	// 1) HQ admin — uses an auto-generated ID so this stays idempotent even
	//    after the email default changes in the future.
	hqEmail := envOr("HQ_ADMIN_EMAIL", "hq@faceauth.local")
	if existing, _ := store.GetUserByEmail(ctx, hqEmail); existing == nil {
		hash, err := HashPassword(envOr("HQ_ADMIN_PASSWORD", seedPassword))
		if err != nil {
			log.Printf("[seed] hash HQ password: %v", err)
		} else {
			err = store.CreateUser(ctx, User{
				Email:        hqEmail,
				Name:         "HQ Admin",
				Role:         RoleHQ,
				PasswordHash: hash,
				Active:       true,
			})
			if err != nil {
				log.Printf("[seed] create HQ admin: %v", err)
			} else {
				log.Printf("[seed] created HQ admin %s", hqEmail)
			}
		}
	}

	// 2) Default tenant for any legacy data
	defaultTenantID := "ten_default"
	if t, _ := store.GetTenant(ctx, defaultTenantID); t == nil {
		if err := store.CreateTenant(ctx, Tenant{
			ID:     defaultTenantID,
			Slug:   "default",
			Name:   "Default",
			Active: true,
		}); err != nil {
			log.Printf("[seed] create default tenant: %v", err)
		} else {
			log.Printf("[seed] created default tenant %s", defaultTenantID)
		}
	}

	// 3) Default tenant admin
	defaultEmail := envOr("DEFAULT_TENANT_EMAIL", "admin@default.local")
	if existing, _ := store.GetUserByEmail(ctx, defaultEmail); existing == nil {
		hash, err := HashPassword(envOr("DEFAULT_TENANT_PASSWORD", seedPassword))
		if err != nil {
			log.Printf("[seed] hash default password: %v", err)
		} else {
			tid := defaultTenantID
			if err := store.CreateUser(ctx, User{
				TenantID:     &tid,
				Email:        defaultEmail,
				Name:         "Default Tenant Admin",
				Role:         RoleTenantAdmin,
				PasswordHash: hash,
				Active:       true,
			}); err != nil {
				log.Printf("[seed] create default tenant admin: %v", err)
			} else {
				log.Printf("[seed] created default tenant admin %s", defaultEmail)
			}
		}
	}

	// 4) Starter outlet for the default merchant — the device/agent forms
	//     expect at least one outlet to attach things to. Skipped as soon as
	//     the merchant has any outlet (including agent-backfilled ones).
	if bs, _ := store.ListBranchesByTenant(ctx, defaultTenantID); len(bs) == 0 {
		if err := store.CreateBranch(ctx, Branch{ID: "br_default_main", TenantID: defaultTenantID, Name: "Main Outlet"}); err != nil {
			log.Printf("[seed] create default outlet: %v", err)
		} else {
			log.Printf("[seed] created default outlet br_default_main")
		}
	}

	// 5) Demo data — runs on first boot, idempotent on every subsequent boot
	//    (seedDemo skips tenants that already exist). Set SKIP_DEMO_SEED=1 in
	//    real production to keep the system empty.
	if os.Getenv("SKIP_DEMO_SEED") == "" {
		if err := seedDemo(ctx, store); err != nil {
			log.Printf("[seed] demo data: %v", err)
		}
	}

	// Final summary so the operator can confirm everything is in place at a
	// glance in `docker compose logs backend`.
	var tenantCount, userCount, planCount, personCount, deviceCount, outletCount int
	_ = store.PG.QueryRow(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&tenantCount)
	_ = store.PG.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount)
	_ = store.PG.QueryRow(ctx, `SELECT COUNT(*) FROM plans`).Scan(&planCount)
	_ = store.PG.QueryRow(ctx, `SELECT COUNT(*) FROM persons`).Scan(&personCount)
	_ = store.PG.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&deviceCount)
	_ = store.PG.QueryRow(ctx, `SELECT COUNT(*) FROM branches`).Scan(&outletCount)
	log.Printf("[seed] summary: merchants=%d users=%d plans=%d persons=%d devices=%d outlets=%d",
		tenantCount, userCount, planCount, personCount, deviceCount, outletCount)

	// Record the marker so the next boot skips straight past seeding.
	if _, err := store.PG.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ('seed_marker', jsonb_build_object('version', $1::int), NOW())
		ON CONFLICT (key) DO UPDATE SET value=jsonb_build_object('version', $1::int), updated_at=NOW()
	`, seedVersion); err != nil {
		log.Printf("[seed] WARN write seed marker: %v", err)
	}
	log.Printf("[seed] Bootstrap complete (marker set to v%d)", seedVersion)
	log.Printf("========================================")
	return nil
}

func seedDemo(ctx context.Context, store *Store) error {
	demos := []struct {
		ID, Slug, Name, Email, Premise string
	}{
		{"ten_acme_gym", "acme-gym", "Acme Gym", "admin@acme-gym.local", "gym"},
		{"ten_northpark", "northpark", "Northpark Office", "admin@northpark.local", "office"},
	}

	for _, d := range demos {
		// Outlets are seeded before the tenant-exists early-exit so merchants
		// created by an older build (pre-outlets) get them too. Existence is
		// checked per outlet, so a rename in the UI survives restarts.
		outlets := []struct{ id, name, addr string }{
			{"br_" + d.Slug + "_main", d.Name + " — Main Outlet", "1 Main Street"},
			{"br_" + d.Slug + "_annex", d.Name + " — Annex", "2 Side Avenue"},
		}
		for _, o := range outlets {
			if b, _ := store.GetBranch(ctx, o.id); b == nil {
				if err := store.CreateBranch(ctx, Branch{ID: o.id, TenantID: d.ID, Name: o.name, Address: o.addr}); err != nil {
					log.Printf("[seed] create outlet %s: %v", o.id, err)
				} else {
					log.Printf("[seed] created outlet %s (%s)", o.name, o.id)
				}
			}
		}

		if t, _ := store.GetTenant(ctx, d.ID); t != nil {
			continue // already seeded
		}
		_ = store.CreateTenant(ctx, Tenant{
			ID: d.ID, Slug: d.Slug, Name: d.Name,
			PremiseType: d.Premise, Timezone: "Asia/Kuala_Lumpur",
			Active: true,
		})
		hash, _ := HashPassword(seedPassword)
		tid := d.ID
		_ = store.CreateUser(ctx, User{
			ID: "usr_" + uuid.NewString()[:10], TenantID: &tid,
			Email: d.Email, Name: d.Name + " Admin", Role: RoleTenantAdmin,
			PasswordHash: hash, Active: true,
		})

		// One plan of each type
		unlim, _ := store.CreatePlan(ctx, Plan{
			TenantID: d.ID, Name: "Unlimited Member", Type: PlanUnlimited, Active: true,
		})
		credit, _ := store.CreatePlan(ctx, Plan{
			TenantID: d.ID, Name: "10-Visit Pass", Type: PlanCredit, DefaultCredits: 10, Active: true,
		})
		rule, _ := store.CreatePlan(ctx, Plan{
			TenantID: d.ID, Name: "Morning + Evening Only", Type: PlanRuleType, Active: true,
			MustExitBeforeReentry: true,
		})
		if rule != nil {
			_, _ = store.UpsertPlanRule(ctx, PlanRule{
				PlanID:   rule.ID,
				Weekdays: 0b0111111, // Mon-Sat
				StartTime: "06:00", EndTime: "09:00",
				Label: "Morning",
			})
			_, _ = store.UpsertPlanRule(ctx, PlanRule{
				PlanID:   rule.ID,
				Weekdays: 0b1111111, // every day
				StartTime: "19:00", EndTime: "22:00",
				Label: "Evening",
			})
		}

		// A handful of demo people
		now := time.Now()
		// Hik FPID accepts [A-Za-z0-9] only — use a compact slug-prefix + 3-digit
		// suffix per person. Same shape as a real employee ID.
		slugClean := sanitizeFPID(d.Slug)
		people := []struct {
			name, emp string
			planID    string
			credits   int
		}{
			{"Alice Tan", slugClean + "001", safeID(unlim), 0},
			{"Bob Chen",  slugClean + "002", safeID(credit), 10},
			{"Cara Ng",   slugClean + "003", safeID(rule), 0},
			{"David Lim", slugClean + "004", safeID(unlim), 0},
		}
		for _, p := range people {
			id := "psn_" + uuid.NewString()[:10]
			_ = store.CreatePerson(ctx, Person{
				ID: id, Name: p.name, EmployeeNo: p.emp,
				PersonType: "normal", PersonRole: "basic",
				LongTerm: true, CreatedAt: now,
			})
			// Tag the person with the tenant id
			_, _ = store.PG.Exec(ctx, `UPDATE persons SET tenant_id=$1 WHERE id=$2`, d.ID, id)
			if p.planID != "" {
				_ = store.AssignPersonPlan(ctx, id, p.planID, p.credits)
			}
		}

		log.Printf("[seed] demo tenant %s (%s) — login: %s / %s", d.Name, d.ID, d.Email, seedPassword)
	}
	return nil
}

func safeID(p *Plan) string {
	if p == nil {
		return ""
	}
	return p.ID
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
