package internal

// ----------------------------------------------------------------------------
//  Authentication + session management.
//
//  Dashboard users log in with email + password, get a session token, and
//  pass it back as `Authorization: Bearer <token>`. Sessions live in the
//  `sessions` table so the system survives restarts.
//
//  Role model
//    hq_admin         — no tenantId; sees every tenant via /api/hq/*
//    tenant_admin     — full control of one tenant
//    tenant_operator  — limited (cannot delete tenant-level things)
//
//  Tenant scoping is enforced by:
//    - sessionAuth() middleware loads the user and sets Locals("user")
//    - tenantScope(c) helper returns the user's tenantId, refusing if absent.
//      HQ users can pass ?tenantId=... to operate on a specific tenant.
// ----------------------------------------------------------------------------

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleHQ            = "hq_admin"
	RoleTenantAdmin   = "tenant_admin"
	RoleTenantOperator = "tenant_operator"

	sessionTTL = 14 * 24 * time.Hour
)

// HashPassword wraps bcrypt for the seed and user-create paths.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword compares plain against a stored hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// ---------------- Store helpers ----------------

func (s *Store) CreateTenant(ctx context.Context, t Tenant) error {
	if t.ID == "" {
		t.ID = "ten_" + uuid.NewString()[:12]
	}
	if t.Slug == "" {
		t.Slug = strings.ToLower(strings.ReplaceAll(t.Name, " ", "-"))
	}
	if t.PremiseType == "" {
		t.PremiseType = "generic"
	}
	if t.Timezone == "" {
		t.Timezone = "Asia/Kuala_Lumpur"
	}
	settings := t.Settings
	if len(settings) == 0 {
		settings = []byte("{}")
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO tenants (id, slug, name, premise_type, timezone, contact_email, contact_phone, address, settings, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE
		SET slug=$2, name=$3, premise_type=$4, timezone=$5,
		    contact_email=$6, contact_phone=$7, address=$8, settings=$9, active=$10
	`, t.ID, t.Slug, t.Name, t.PremiseType, t.Timezone, t.ContactEmail, t.ContactPhone, t.Address, settings, t.Active)
	return err
}

const tenantSelect = `
	SELECT id, slug, name,
	       COALESCE(premise_type,'generic'),
	       COALESCE(timezone,'Asia/Kuala_Lumpur'),
	       COALESCE(contact_email,''),
	       COALESCE(contact_phone,''),
	       COALESCE(address,''),
	       COALESCE(settings,'{}'::jsonb),
	       active, created_at
	FROM tenants
`

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.PG.Query(ctx, tenantSelect+` ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tenant{}
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.PremiseType, &t.Timezone, &t.ContactEmail, &t.ContactPhone, &t.Address, &t.Settings, &t.Active, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	t := &Tenant{}
	err := s.PG.QueryRow(ctx, tenantSelect+` WHERE id=$1`, id).
		Scan(&t.ID, &t.Slug, &t.Name, &t.PremiseType, &t.Timezone, &t.ContactEmail, &t.ContactPhone, &t.Address, &t.Settings, &t.Active, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, id)
	return err
}

func (s *Store) CreateUser(ctx context.Context, u User) error {
	if u.ID == "" {
		u.ID = "usr_" + uuid.NewString()[:12]
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO users (id, tenant_id, email, password_hash, role, name, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (email) DO UPDATE
		SET tenant_id=$2, password_hash=$4, role=$5, name=$6, active=$7
	`, u.ID, u.TenantID, strings.ToLower(u.Email), u.PasswordHash, u.Role, u.Name, u.Active)
	return err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := s.PG.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, role, COALESCE(name,''), active, last_login_at, created_at
		FROM users WHERE email=$1
	`, strings.ToLower(email)).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name, &u.Active, &u.LastLoginAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := s.PG.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, role, COALESCE(name,''), active, last_login_at, created_at
		FROM users WHERE id=$1
	`, id).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name, &u.Active, &u.LastLoginAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (s *Store) ListUsers(ctx context.Context, tenantID string) ([]User, error) {
	q := `SELECT id, tenant_id, email, role, COALESCE(name,''), active, last_login_at, created_at FROM users`
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
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Name, &u.Active, &u.LastLoginAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

func (s *Store) CreateSession(ctx context.Context, userID string) (*Session, error) {
	tok := "fas_" + RandomString(48, charset+hexCharset)
	exp := time.Now().Add(sessionTTL)
	_, err := s.PG.Exec(ctx, `INSERT INTO sessions (token, user_id, expires_at) VALUES ($1,$2,$3)`, tok, userID, exp)
	if err != nil {
		return nil, err
	}
	_, _ = s.PG.Exec(ctx, `UPDATE users SET last_login_at=NOW() WHERE id=$1`, userID)
	return &Session{Token: tok, UserID: userID, ExpiresAt: exp}, nil
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	sess := &Session{}
	err := s.PG.QueryRow(ctx, `SELECT token, user_id, expires_at, created_at FROM sessions WHERE token=$1`, token).
		Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return sess, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM sessions WHERE token=$1`, token)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM sessions WHERE expires_at < NOW()`)
	return err
}

// ---------------- Middleware ----------------

// sessionAuth verifies the session token in the Authorization header and
// attaches the User + Tenant to Fiber locals. Routes that need a tenant scope
// call tenantScope(c).
func sessionAuth(store *Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tok := ""
		if h := c.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
			tok = strings.TrimSpace(h[7:])
		}
		if tok == "" {
			tok = c.Get("X-Session-Token")
		}
		if tok == "" {
			return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
		}
		sess, err := store.GetSession(c.Context(), tok)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if sess == nil || sess.ExpiresAt.Before(time.Now()) {
			return c.Status(401).JSON(fiber.Map{"error": "session expired"})
		}
		u, err := store.GetUser(c.Context(), sess.UserID)
		if err != nil || u == nil || !u.Active {
			return c.Status(401).JSON(fiber.Map{"error": "user inactive"})
		}
		c.Locals("user", u)
		c.Locals("session", sess)
		return c.Next()
	}
}

// requireRole gates a route by one or more allowed roles.
func requireRole(roles ...string) fiber.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *fiber.Ctx) error {
		u, ok := c.Locals("user").(*User)
		if !ok || u == nil {
			return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
		}
		if !allowed[u.Role] {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden for role " + u.Role})
		}
		return c.Next()
	}
}

// tenantScope returns the tenantId for the current request:
//   - tenant_admin / tenant_operator: their own tenant (query overrides forbidden)
//   - hq_admin: tenant from ?tenantId= query (required for tenant-scoped endpoints)
//
// Returns ("", error) if the caller is HQ and didn't pass a tenant.
func tenantScope(c *fiber.Ctx) (string, error) {
	u, _ := c.Locals("user").(*User)
	if u == nil {
		return "", fmt.Errorf("not authenticated")
	}
	if u.Role == RoleHQ {
		t := c.Query("tenantId")
		if t == "" {
			t = c.Get("X-Tenant-Id")
		}
		if t == "" {
			return "", fmt.Errorf("HQ user must pass tenantId query param or X-Tenant-Id header")
		}
		return t, nil
	}
	if u.TenantID == nil || *u.TenantID == "" {
		return "", fmt.Errorf("user has no tenant scope")
	}
	return *u.TenantID, nil
}

func currentUser(c *fiber.Ctx) *User {
	u, _ := c.Locals("user").(*User)
	return u
}
