package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
)

func NewAPIServer(store *Store, cfg Config, hub *AgentHub) *fiber.App {
	settings := NewSettingsStore(store)
	if err := settings.Load(context.Background()); err != nil {
		log.Printf("WARN: settings load: %v (using defaults)", err)
	}
	qrAuth := NewQRAuth(store, cfg, hub, settings)
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             50 * 1024 * 1024,
		AppName:               "face_auth-api",
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		// Every custom header we send from the SPA must be listed here OR the
		// browser silently kills the preflight. X-Tenant-Id is added on every
		// dashboard request once an active tenant is picked. X-Session-Token
		// is an alternate to the Authorization header. X-API-Key is used by
		// third-party /api/v1 callers.
		AllowHeaders:  "Content-Type, Authorization, X-Tenant-Id, X-Session-Token, X-API-Key",
		ExposeHeaders: "Content-Disposition",
	}))

	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		log.Printf("[api] %s %s -> %d (%s)", c.Method(), c.Path(), c.Response().StatusCode(), time.Since(start))
		return err
	})

	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })

	api := app.Group("/api")

	// ---------- Auth (unauthenticated) ----------

	api.Post("/auth/login", func(c *fiber.Ctx) error {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad body"})
		}
		u, err := store.GetUserByEmail(c.Context(), body.Email)
		if err != nil || u == nil || !u.Active || !CheckPassword(u.PasswordHash, body.Password) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid email or password"})
		}
		sess, err := store.CreateSession(c.Context(), u.ID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		var t *Tenant
		if u.TenantID != nil && *u.TenantID != "" {
			t, _ = store.GetTenant(c.Context(), *u.TenantID)
		}
		return c.JSON(fiber.Map{"token": sess.Token, "user": u, "tenant": t, "expiresAt": sess.ExpiresAt})
	})

	// Everything below this point requires a valid session token. The OLD
	// /api/devices, /api/persons etc. endpoints are now session-gated too.
	// Public third-party traffic still uses /api/v1/* with API keys.
	api.Use(func(c *fiber.Ctx) error {
		// ONLY /api/auth/login is unauthenticated. Everything else under
		// /api/auth/* (me, logout) needs the session middleware to populate
		// c.Locals("user") — otherwise /auth/me silently returns null and the
		// SPA bounces back to the login screen in an infinite loop.
		p := c.Path()
		// /api/qr-auth/scan is called by LAN agents (USB scanner → cloud) which
		// hold no dashboard session. The per-person QR token in the body IS the
		// credential, so this route authenticates itself — same model as the
		// /api/v1/qr-auth/scan API-key endpoint.
		if p == "/api/auth/login" || p == "/api/healthz" || p == "/api/qr-auth/scan" {
			return c.Next()
		}
		// Agent binaries and companion scripts are generic, secret-free downloads
		// (the per-agent token is generated separately and shown once in the UI),
		// so they're served without a session — no token needed in the URL.
		if strings.HasPrefix(p, "/api/agents/downloads") || strings.HasPrefix(p, "/api/agents/scripts/") {
			return c.Next()
		}
		return sessionAuth(store)(c)
	})

	api.Post("/auth/logout", func(c *fiber.Ctx) error {
		if sess, ok := c.Locals("session").(*Session); ok && sess != nil {
			_ = store.DeleteSession(c.Context(), sess.Token)
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	api.Get("/auth/me", func(c *fiber.Ctx) error {
		u := currentUser(c)
		var t *Tenant
		if u != nil && u.TenantID != nil && *u.TenantID != "" {
			t, _ = store.GetTenant(c.Context(), *u.TenantID)
		}
		// HQ users may have an active "viewing" tenant via header/query
		if u != nil && u.Role == RoleHQ {
			if tid := firstNonEmpty(c.Query("tenantId"), c.Get("X-Tenant-Id")); tid != "" {
				t, _ = store.GetTenant(c.Context(), tid)
			}
		}
		return c.JSON(fiber.Map{"user": u, "tenant": t})
	})

	// ---------- HQ: tenants + cross-tenant overview ----------
	//
	// NOTE: The Group prefix MUST be "/hq" (not "") because Fiber v2 treats
	// an empty-prefix Group with middleware as a Use() on the parent — every
	// subsequent route on `api` would inherit requireRole(HQ) and 403 for
	// tenant_admin users. The /hq prefix scopes the middleware exactly to
	// /api/hq/* paths.

	hq := api.Group("/hq", requireRole(RoleHQ))

	hq.Get("/tenants", func(c *fiber.Ctx) error {
		ts, err := store.ListTenants(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Decorate with quick counts
		out := []fiber.Map{}
		for _, t := range ts {
			var devCount, personCount, planCount int
			_ = store.PG.QueryRow(c.Context(), `SELECT COUNT(*) FROM devices WHERE tenant_id=$1`, t.ID).Scan(&devCount)
			_ = store.PG.QueryRow(c.Context(), `SELECT COUNT(*) FROM persons WHERE tenant_id=$1`, t.ID).Scan(&personCount)
			_ = store.PG.QueryRow(c.Context(), `SELECT COUNT(*) FROM plans WHERE tenant_id=$1`, t.ID).Scan(&planCount)
			var onlineCount int
			_ = store.PG.QueryRow(c.Context(), `SELECT COUNT(*) FROM devices WHERE tenant_id=$1 AND online=TRUE`, t.ID).Scan(&onlineCount)
			out = append(out, fiber.Map{
				"tenant":        t,
				"deviceCount":   devCount,
				"devicesOnline": onlineCount,
				"personCount":   personCount,
				"planCount":     planCount,
			})
		}
		return c.JSON(out)
	})

	hq.Post("/tenants", func(c *fiber.Ctx) error {
		var body struct {
			Name           string `json:"name"`
			Slug           string `json:"slug"`
			PremiseType    string `json:"premiseType"`
			Timezone       string `json:"timezone"`
			ContactEmail   string `json:"contactEmail"`
			ContactPhone   string `json:"contactPhone"`
			Address        string `json:"address"`
			InstallPresets bool   `json:"installPresets"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name required"})
		}
		t := Tenant{
			Name:         body.Name,
			Slug:         body.Slug,
			PremiseType:  body.PremiseType,
			Timezone:     body.Timezone,
			ContactEmail: body.ContactEmail,
			ContactPhone: body.ContactPhone,
			Address:      body.Address,
			Active:       true,
		}
		if err := store.CreateTenant(c.Context(), t); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Re-read so we get the assigned ID and defaults
		created, _ := store.GetTenant(c.Context(), t.ID)
		if created == nil {
			created = &t
		}
		// Optionally seed plans from the chosen premise template
		installed := []Plan{}
		if body.InstallPresets {
			if pt := FindPremiseType(created.PremiseType); pt != nil {
				installed, _ = store.InstallPresets(c.Context(), created.ID, pt.Presets)
			}
		}
		return c.JSON(fiber.Map{"tenant": created, "installedPlans": installed})
	})

	hq.Put("/tenants/:id", func(c *fiber.Ctx) error {
		cur, _ := store.GetTenant(c.Context(), c.Params("id"))
		if cur == nil {
			return c.Status(404).JSON(fiber.Map{"error": "tenant not found"})
		}
		var body Tenant
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad body"})
		}
		body.ID = cur.ID
		if body.Slug == "" {
			body.Slug = cur.Slug
		}
		if body.Name == "" {
			body.Name = cur.Name
		}
		if err := store.CreateTenant(c.Context(), body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		updated, _ := store.GetTenant(c.Context(), cur.ID)
		return c.JSON(updated)
	})

	// Premise types library — surfaces the canonical premise patterns + their
	// preset plans so the UI can render an "install template" picker.
	api.Get("/premise-types", func(c *fiber.Ctx) error {
		return c.JSON(PremiseTypes)
	})

	// Install presets onto the active tenant (POST body: { premiseType: "gym" }
	// — or omit to use the tenant's premise_type).
	api.Post("/plans/install-presets", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		var body struct {
			PremiseType string `json:"premiseType"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		key := body.PremiseType
		if key == "" {
			if t, _ := store.GetTenant(c.Context(), tid); t != nil {
				key = t.PremiseType
			}
		}
		pt := FindPremiseType(key)
		if pt == nil {
			return c.Status(400).JSON(fiber.Map{"error": "unknown premise type: " + key})
		}
		installed, err := store.InstallPresets(c.Context(), tid, pt.Presets)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"installed": installed, "premiseType": key})
	})

	hq.Delete("/tenants/:id", func(c *fiber.Ctx) error {
		if err := store.DeleteTenant(c.Context(), c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	hq.Get("/users", func(c *fiber.Ctx) error {
		us, err := store.ListUsers(c.Context(), c.Query("tenantId"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(us)
	})

	hq.Post("/users", func(c *fiber.Ctx) error {
		var body struct {
			TenantID string `json:"tenantId"`
			Email    string `json:"email"`
			Password string `json:"password"`
			Role     string `json:"role"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Email == "" || body.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "email + password required"})
		}
		if body.Role == "" {
			body.Role = RoleTenantAdmin
		}
		hash, err := HashPassword(body.Password)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		u := User{Email: body.Email, Role: body.Role, Name: body.Name, PasswordHash: hash, Active: true}
		if body.TenantID != "" {
			u.TenantID = &body.TenantID
		}
		if err := store.CreateUser(c.Context(), u); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(u)
	})

	hq.Delete("/users/:id", func(c *fiber.Ctx) error {
		if err := store.DeleteUser(c.Context(), c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// ---------- Tenant-scoped: plans / rules / assignments ----------

	api.Get("/plans", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		ps, err := store.ListPlans(c.Context(), tid)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(ps)
	})

	api.Post("/plans", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		var body Plan
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name required"})
		}
		body.TenantID = tid
		if body.Type == "" {
			body.Type = PlanUnlimited
		}
		body.Active = true
		p, err := store.CreatePlan(c.Context(), body)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Persist any nested rules atomically
		for _, r := range body.Rules {
			r.PlanID = p.ID
			_, _ = store.UpsertPlanRule(c.Context(), r)
		}
		full, _ := store.GetPlan(c.Context(), p.ID)
		return c.JSON(full)
	})

	api.Put("/plans/:id", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		var body Plan
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad body"})
		}
		body.ID = c.Params("id")
		body.TenantID = tid
		if _, err := store.CreatePlan(c.Context(), body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Replace rules: remove existing, insert new
		_, _ = store.PG.Exec(c.Context(), `DELETE FROM plan_rules WHERE plan_id=$1`, body.ID)
		for _, r := range body.Rules {
			r.PlanID = body.ID
			r.ID = "" // force new ID per rule
			_, _ = store.UpsertPlanRule(c.Context(), r)
		}
		full, _ := store.GetPlan(c.Context(), body.ID)
		return c.JSON(full)
	})

	api.Delete("/plans/:id", func(c *fiber.Ctx) error {
		if _, err := tenantScope(c); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		if err := store.DeletePlan(c.Context(), c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// Person → plan assignment
	api.Post("/persons/:id/plan", func(c *fiber.Ctx) error {
		var body struct {
			PlanID  string `json:"planId"`
			Credits *int   `json:"credits"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		credits := 0
		if body.Credits != nil {
			credits = *body.Credits
		} else if body.PlanID != "" {
			if p, _ := store.GetPlan(c.Context(), body.PlanID); p != nil {
				credits = p.DefaultCredits
			}
		}
		if err := store.AssignPersonPlan(c.Context(), c.Params("id"), body.PlanID, credits); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		pp, _ := store.GetPersonPlan(c.Context(), c.Params("id"))
		return c.JSON(pp)
	})

	api.Get("/persons/:id/plan", func(c *fiber.Ctx) error {
		pp, _ := store.GetPersonPlan(c.Context(), c.Params("id"))
		if pp == nil {
			return c.Status(404).JSON(fiber.Map{"error": "no plan assigned"})
		}
		return c.JSON(pp)
	})

	// Device → plan(s)
	api.Get("/devices/:id/plans", func(c *fiber.Ctx) error {
		ids, err := store.ListDevicePlans(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(ids)
	})
	api.Post("/devices/:id/plans/:planId", func(c *fiber.Ctx) error {
		if err := store.AssignDeviceToPlan(c.Context(), c.Params("id"), c.Params("planId")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})
	api.Delete("/devices/:id/plans/:planId", func(c *fiber.Ctx) error {
		if err := store.UnassignDeviceFromPlan(c.Context(), c.Params("id"), c.Params("planId")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// Access log (tenant-scoped feed of allow/deny decisions)
	api.Get("/access-log", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "200"))
		out, err := store.ListAccessLog(c.Context(), tid, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})

	// ---------- Devices ----------

	api.Get("/devices", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		devs, err := store.ListDevicesByTenant(c.Context(), tid)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(devs)
	})

	api.Post("/devices", func(c *fiber.Ctx) error {
		var body struct {
			DeviceID      string `json:"deviceId"`
			Name          string `json:"name"`
			Password      string `json:"password"`
			IP            string `json:"ip"`
			Port          int    `json:"port"`
			UseHTTPS      bool   `json:"useHttps"`
			ISAPIUsername string `json:"isapiUsername"`
			ISAPIPassword string `json:"isapiPassword"`
			FDID          string `json:"fdid"`
			FaceLibType   string `json:"faceLibType"`
			AgentID       string `json:"agentId"`
			Reach         string `json:"reach"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.DeviceID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "deviceId required"})
		}
		dev := Device{
			DeviceID:      body.DeviceID,
			Name:          body.Name,
			IP:            body.IP,
			Port:          body.Port,
			UseHTTPS:      body.UseHTTPS,
			ISAPIUsername: body.ISAPIUsername,
			ISAPIPassword: body.ISAPIPassword,
			FDID:          body.FDID,
			FaceLibType:   body.FaceLibType,
			AgentID:       body.AgentID,
			Reach:         body.Reach,
		}
		dev.SetPassword(body.Password)
		if err := store.RegisterDevice(c.Context(), dev); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Tag device with the caller's tenant.
		if tid, err := tenantScope(c); err == nil {
			_ = store.SetDeviceTenant(c.Context(), dev.DeviceID, tid)
		}
		// If ISAPI credentials provided, immediately try to reach the device
		// so the user gets instant feedback in the UI.
		var probe map[string]any
		if body.IP != "" && body.ISAPIUsername != "" {
			d, _ := store.GetDevice(c.Context(), body.DeviceID)
			if d != nil {
				client := NewISAPIClientForDevice(d, hub)
				if info, err := client.GetDeviceInfo(); err == nil {
					_ = store.SetDeviceOnline(c.Context(), d.DeviceID, info.Model, info.FirmwareVersion, true)
					probe = map[string]any{"reachable": true, "info": info}
				} else {
					_ = store.SetDeviceOnline(c.Context(), d.DeviceID, "", "", false)
					probe = map[string]any{"reachable": false, "error": err.Error()}
				}
			}
		}
		return c.JSON(fiber.Map{"ok": true, "probe": probe})
	})

	api.Delete("/devices/:id", func(c *fiber.Ctx) error {
		if err := store.DeleteDevice(c.Context(), c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// Probe a device — checks if ISAPI is reachable, updates online status
	api.Post("/devices/:id/probe", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		if d.IP == "" || d.ISAPIUsername == "" {
			return c.Status(400).JSON(fiber.Map{"error": "device has no ISAPI credentials configured"})
		}
		client := NewISAPIClientForDevice(d, hub)
		info, err := client.GetDeviceInfo()
		if err != nil {
			_ = store.SetDeviceOnline(c.Context(), d.DeviceID, "", "", false)
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		_ = store.SetDeviceOnline(c.Context(), d.DeviceID, info.Model, info.FirmwareVersion, true)
		return c.JSON(fiber.Map{"ok": true, "info": info})
	})

	// Register face_auth as an HTTP alarm host on the device so it pushes events to us
	api.Post("/devices/:id/setup-alarm-host", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		var body struct {
			HostIP   string `json:"hostIp"`
			HostPort int    `json:"hostPort"`
			Slot     int    `json:"slot"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		if body.HostIP == "" {
			body.HostIP = cfg.EventCallbackIP
		}
		if body.HostPort == 0 {
			body.HostPort, _ = strconv.Atoi(cfg.PushPort)
		}
		if body.HostIP == "" {
			return c.Status(400).JSON(fiber.Map{"error": "hostIp not provided and EVENT_CALLBACK_IP env not set"})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetAlarmHost(body.HostIP, body.HostPort, "/hik-event", body.Slot)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- Wi-Fi ----------

	// Read the device's current wireless config (raw device XML).
	api.Get("/devices/:id/wifi", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		client := NewISAPIClientForDevice(d, hub)
		raw, err := client.GetWifi(c.Query("ifId"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "raw": raw})
	})

	// Scan for nearby access points.
	api.Post("/devices/:id/wifi/scan", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		client := NewISAPIClientForDevice(d, hub)
		aps, raw, err := client.ScanWifi(c.Query("ifId"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "accessPoints": aps})
	})

	// Join a Wi-Fi network. Body: { ssid, password, securityMode?, algorithm?, ifId?, enabled? }
	api.Put("/devices/:id/wifi", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		var body struct {
			SSID         string `json:"ssid"`
			Password     string `json:"password"`
			SecurityMode string `json:"securityMode"`
			Algorithm    string `json:"algorithm"`
			IfID         string `json:"ifId"`
			Enabled      *bool  `json:"enabled"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.SSID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "ssid required"})
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetWifi(body.IfID, WifiConfig{
			SSID:         body.SSID,
			Key:          body.Password,
			SecurityMode: body.SecurityMode,
			Algorithm:    body.Algorithm,
			Enabled:      enabled,
		})
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Probe all configured devices (useful for status sweep)
	api.Post("/devices/probe-all", func(c *fiber.Ctx) error {
		devs, _ := store.ListDevices(c.Context())
		results := []fiber.Map{}
		for _, dev := range devs {
			if dev.IP == "" {
				continue
			}
			d, _ := store.GetDevice(c.Context(), dev.DeviceID)
			if d == nil {
				continue
			}
			client := NewISAPIClientForDevice(d, hub)
			info, err := client.GetDeviceInfo()
			if err != nil {
				_ = store.SetDeviceOnline(c.Context(), d.DeviceID, "", "", false)
				results = append(results, fiber.Map{"deviceId": d.DeviceID, "ok": false, "error": err.Error()})
				continue
			}
			_ = store.SetDeviceOnline(c.Context(), d.DeviceID, info.Model, info.FirmwareVersion, true)
			results = append(results, fiber.Map{"deviceId": d.DeviceID, "ok": true, "model": info.Model})
		}
		return c.JSON(results)
	})

	// ---------- Agents ----------

	api.Get("/agents", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		agents, err := store.ListAgentsByTenant(c.Context(), tid)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Attach live online status from hub
		out := make([]fiber.Map, 0, len(agents))
		for _, a := range agents {
			out = append(out, fiber.Map{
				"id":        a.ID,
				"name":      a.Name,
				"online":    hub.IsOnline(a.ID),
				"createdAt": a.CreatedAt,
			})
		}
		return c.JSON(out)
	})

	api.Post("/agents", func(c *fiber.Ctx) error {
		var body struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.ID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "id required"})
		}
		a := Agent{ID: body.ID, Name: body.Name, Token: GenerateAgentToken()}
		if err := store.CreateAgent(c.Context(), a); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if tid, err := tenantScope(c); err == nil {
			_ = store.SetAgentTenant(c.Context(), a.ID, tid)
		}
		return c.JSON(a) // Token is included here ONCE — for the user to copy
	})

	api.Delete("/agents/:id", func(c *fiber.Ctx) error {
		if err := store.DeleteAgent(c.Context(), c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// List available agent binaries
	api.Get("/agents/downloads", func(c *fiber.Ctx) error {
		entries, err := os.ReadDir("/app/agents")
		if err != nil {
			return c.JSON(fiber.Map{"binaries": []any{}})
		}
		out := []fiber.Map{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, _ := e.Info()
			name := e.Name()
			platform, label := describeBinary(name)
			out = append(out, fiber.Map{
				"file":     name,
				"platform": platform,
				"label":    label,
				"size":     info.Size(),
			})
		}
		return c.JSON(out)
	})

	// Companion scripts (Windows QR watcher, etc.)
	api.Get("/agents/scripts/:file", func(c *fiber.Ctx) error {
		name := c.Params("file")
		if strings.Contains(name, "/") || strings.Contains(name, "..") {
			return c.Status(400).SendString("bad filename")
		}
		path := "/app/scripts/" + name
		if _, err := os.Stat(path); err != nil {
			return c.Status(404).SendString("not found")
		}
		c.Set("Content-Disposition", "attachment; filename="+name)
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.SendFile(path)
	})

	api.Get("/agents/downloads/:file", func(c *fiber.Ctx) error {
		name := c.Params("file")
		if strings.Contains(name, "/") || strings.Contains(name, "..") {
			return c.Status(400).SendString("bad filename")
		}
		path := "/app/agents/" + name
		if _, err := os.Stat(path); err != nil {
			return c.Status(404).SendString("not found")
		}
		c.Set("Content-Disposition", "attachment; filename="+name)
		c.Set("Content-Type", "application/octet-stream")
		return c.SendFile(path)
	})

	api.Post("/agents/:id/regen-token", func(c *fiber.Ctx) error {
		t, err := store.RegenerateAgentToken(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"token": t})
	})

	// Agent WebSocket endpoint — agents connect here and stay connected
	app.Use("/agent", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			id := c.Query("id")
			token := c.Query("token")
			if !store.VerifyAgentToken(c.Context(), id, token) {
				return c.Status(401).SendString("invalid agent credentials")
			}
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/agent/ws", websocket.New(hub.Handle))

	// ---------- Persons ----------

	api.Get("/persons", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		out, err := store.ListPersonsByTenant(c.Context(), tid)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})
	api.Post("/persons", func(c *fiber.Ctx) error {
		tid, err := tenantScope(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		var body Person
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name required"})
		}
		if body.ID == "" {
			body.ID = uuid.NewString()
		}
		// Force employeeNo to a Hik-compatible form (alphanumeric only, ≤32
		// chars). If the caller submitted something with hyphens / underscores,
		// strip them silently — otherwise the device would 400 the very first
		// EnrolFace call with badJsonContent.FPID.
		body.EmployeeNo = sanitizeFPID(body.EmployeeNo)
		if body.EmployeeNo == "" {
			body.EmployeeNo = sanitizeFPID(body.ID)
			if len(body.EmployeeNo) > 8 {
				body.EmployeeNo = body.EmployeeNo[:8]
			}
		}
		if err := store.CreatePerson(c.Context(), body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		_ = store.SetPersonTenant(c.Context(), body.ID, tid)
		return c.JSON(body)
	})
	// ---------- QR token + QR auth ----------

	api.Post("/persons/:id/qr/rotate", func(c *fiber.Ctx) error {
		p, _ := store.GetPerson(c.Context(), c.Params("id"))
		if p == nil {
			return c.Status(404).JSON(fiber.Map{"error": "person not found"})
		}
		token := RandomString(24, charset+hexCharset)
		if err := store.SetQRToken(c.Context(), p.ID, token); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "qrToken": token})
	})

	api.Get("/persons/:id/qr.png", func(c *fiber.Ctx) error {
		p, _ := store.GetPerson(c.Context(), c.Params("id"))
		if p == nil || p.QRToken == "" {
			return c.Status(404).SendString("no QR token — call /qr/rotate first")
		}
		size, _ := strconv.Atoi(c.Query("size", "256"))
		if size < 96 {
			size = 96
		}
		if size > 1024 {
			size = 1024
		}
		png, err := qrcode.Encode(p.QRToken, qrcode.Medium, size)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		c.Set("Content-Type", "image/png")
		c.Set("Cache-Control", "no-store")
		return c.Send(png)
	})

	api.Post("/qr-auth/scan", func(c *fiber.Ctx) error {
		var body struct {
			QRToken string `json:"qrToken"`
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.QRToken == "" {
			return c.Status(400).JSON(fiber.Map{"error": "qrToken required"})
		}
		s, err := qrAuth.Scan(c.Context(), body.QRToken, body.AgentID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "session": s})
	})

	api.Get("/qr-auth/sessions", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"active":  qrAuth.ActiveSessions(),
			"history": qrAuth.History(),
		})
	})

	api.Post("/devices/:id/lock-all-users", func(c *fiber.Ctx) error {
		n, err := qrAuth.LockAllUsersOnDevice(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "locked": n})
	})

	api.Delete("/persons/:id", func(c *fiber.Ctx) error {
		ctx := c.Context()
		p, _ := store.GetPerson(ctx, c.Params("id"))

		// If query param ?syncDevice=ID provided, also delete from that device
		var deviceResp string
		if devID := c.Query("syncDevice"); devID != "" && p != nil && p.EmployeeNo != "" {
			d, _ := store.GetDevice(ctx, devID)
			if d != nil && d.IP != "" {
				client := NewISAPIClientForDevice(d, hub)
				if r, err := client.DeleteUserByEmployeeNo(p.EmployeeNo); err != nil {
					return c.Status(502).JSON(fiber.Map{"error": "device delete failed: " + err.Error(), "deviceResponse": r})
				} else {
					deviceResp = r
				}
			}
		}
		if err := store.DeletePerson(ctx, c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "deviceResponse": json.RawMessage(safeJSONFromBytes([]byte(deviceResp)))})
	})

	api.Get("/persons/:id", func(c *fiber.Ctx) error {
		p, err := store.GetPerson(c.Context(), c.Params("id"))
		if err != nil || p == nil {
			return c.Status(404).JSON(fiber.Map{"error": "person not found"})
		}
		faces, _ := store.ListFaces(c.Context(), "", p.ID)
		return c.JSON(fiber.Map{"person": p, "faces": faces})
	})

	// Sync users from a device → local DB (creates / updates persons).
	api.Post("/devices/:id/sync-persons", func(c *fiber.Ctx) error {
		ctx := c.Context()
		d, err := store.GetDevice(ctx, c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not found or not configured"})
		}
		client := NewISAPIClientForDevice(d, hub)
		users, err := client.ListUsers()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": err.Error()})
		}
		faces, _ := client.ListFacesOnDevice(d.FDID, d.FaceLibType)
		cards, _ := client.ListCards()

		// Index cards by employeeNo
		cardByEmp := map[string][]string{}
		for _, ca := range cards {
			cardByEmp[ca.EmployeeNo] = append(cardByEmp[ca.EmployeeNo], ca.CardNo)
		}
		faceByEmp := map[string]DeviceFaceRecord{}
		for _, fa := range faces {
			faceByEmp[fa.FPID] = fa
		}

		synced := 0
		for _, u := range users {
			if u.EmployeeNo == "" {
				continue
			}
			existing, _ := store.GetPersonByEmployeeNo(ctx, u.EmployeeNo)
			personID := ""
			if existing != nil {
				personID = existing.ID
			} else {
				personID = uuid.NewString()
			}

			meta, _ := json.Marshal(map[string]any{
				"cards":            cardByEmp[u.EmployeeNo],
				"deviceUserType":   u.UserType,
				"userVerifyMode":   u.UserVerifyMode,
				"deviceFaceURL":    faceByEmp[u.EmployeeNo].FaceURL,
				"deviceSyncedAt":   time.Now().UTC(),
				"deviceSyncedFrom": d.DeviceID,
			})

			p := Person{
				ID:             personID,
				Name:           u.Name,
				EmployeeNo:     u.EmployeeNo,
				Gender:         u.Gender,
				PersonType:     mapDeviceUserType(u.UserType),
				PersonRole:     ifElse(u.LocalUIRight, "administrator", "basic"),
				AttendanceOnly: u.CheckUser,
				DoorRight:      firstNonEmpty(u.DoorRight, "1"),
				PlanTemplate:   "1",
				Metadata:       meta,
			}
			if u.Valid != nil {
				p.LongTerm = !u.Valid.Enable
				if u.Valid.BeginTime != "" {
					if t, err := time.Parse("2006-01-02T15:04:05", u.Valid.BeginTime); err == nil {
						p.ValidBegin = &t
					}
				}
				if u.Valid.EndTime != "" {
					if t, err := time.Parse("2006-01-02T15:04:05", u.Valid.EndTime); err == nil {
						p.ValidEnd = &t
					}
				}
			}
			if err := store.CreatePerson(ctx, p); err != nil {
				log.Printf("sync createPerson %s: %v", u.EmployeeNo, err)
				continue
			}

			// Pull and cache the face image if not already present
			if face, ok := faceByEmp[u.EmployeeNo]; ok && face.FaceURL != "" {
				existingFaces, _ := store.ListFaces(ctx, d.DeviceID, p.ID)
				if len(existingFaces) == 0 {
					if img, _, err := client.GetFaceImageFromURL(face.FaceURL); err == nil {
						key := fmt.Sprintf("faces/%s/%s.jpg", p.ID, uuid.NewString())
						if err := store.PutObject(ctx, key, "image/jpeg", img); err == nil {
							_ = store.CreateFace(ctx, Face{
								ID:       uuid.NewString(),
								PersonID: p.ID,
								DeviceID: d.DeviceID,
								ImageKey: key,
								Status:   "enrolled",
							})
						}
					}
				}
			}

			synced++
		}
		return c.JSON(fiber.Map{
			"ok":     true,
			"users":  len(users),
			"faces":  len(faces),
			"cards":  len(cards),
			"synced": synced,
		})
	})

	// Open door (or any door number via ?door=N)
	api.Post("/devices/:id/open-door", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		doorNo, _ := strconv.Atoi(c.Query("door", "1"))
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.OpenDoor(doorNo)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- Phase 2: capture-at-device + card / fingerprint enrol ----------
	// reachable returns the device if it can be commanded (has IP, an agent, or
	// OTAP push). OTAP devices have no IP, so we must not require d.IP here.
	reachable := func(c *fiber.Ctx) (*Device, error) {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return nil, fmt.Errorf("device not found")
		}
		if d.IP == "" && d.AgentID == "" && d.Reach != "otap" {
			return nil, fmt.Errorf("device has no reachable transport (set IP, agent, or OTAP)")
		}
		return d, nil
	}

	// Capture a live face from the device camera.
	api.Post("/devices/:id/capture/face", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.CaptureFaceData(c.Query("infrared") == "true")
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Capture a card swipe on the device reader.
	api.Post("/devices/:id/capture/card", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.CaptureCardInfo()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Capture a fingerprint on the device sensor.
	api.Post("/devices/:id/capture/fingerprint", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		finger, _ := strconv.Atoi(c.Query("finger", "1"))
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.CaptureFingerPrint(finger)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Create/modify a card bound to a user.
	api.Post("/devices/:id/cards", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		var body struct {
			EmployeeNo string `json:"employeeNo"`
			CardNo     string `json:"cardNo"`
			CardType   string `json:"cardType"`
			Mode       string `json:"mode"` // "" = create, "modify" = update
		}
		if e := json.Unmarshal(c.Body(), &body); e != nil || body.EmployeeNo == "" || body.CardNo == "" {
			return c.Status(400).JSON(fiber.Map{"error": "employeeNo and cardNo required"})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetCardInfo(body.EmployeeNo, body.CardNo, body.CardType, body.Mode)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Delete a card by number.
	api.Delete("/devices/:id/cards/:cardNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.DeleteCard(c.Params("cardNo"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Upload a fingerprint template for a user.
	api.Post("/devices/:id/fingerprints", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		var body struct {
			EmployeeNo    string `json:"employeeNo"`
			FingerPrintID int    `json:"fingerPrintID"`
			FingerData    string `json:"fingerData"` // base64 template
		}
		if e := json.Unmarshal(c.Body(), &body); e != nil || body.EmployeeNo == "" || body.FingerData == "" {
			return c.Status(400).JSON(fiber.Map{"error": "employeeNo and fingerData required"})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetFingerPrint(body.EmployeeNo, body.FingerPrintID, body.FingerData)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Delete a user's fingerprint(s). ?finger=N for a single print, omit for all.
	api.Delete("/devices/:id/fingerprints/:employeeNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		finger, _ := strconv.Atoi(c.Query("finger", "0"))
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.DeleteFingerPrint(c.Params("employeeNo"), finger)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- Phase 4: intercom (VideoIntercom call signaling) ----------

	// Probe intercom support (VideoIntercom capabilities).
	api.Get("/devices/:id/intercom/capabilities", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		raw, err := NewISAPIClientForDevice(d, hub).GetIntercomCapabilities()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "supported": strings.Contains(raw, "isSupportCallSignal") && strings.Contains(raw, "true"), "raw": raw})
	})

	// Current call status (idle / ring / onCall / …).
	api.Get("/devices/:id/intercom/status", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		raw, err := NewISAPIClientForDevice(d, hub).GetCallStatus()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "raw": raw})
	})

	// Drive the call. ?cmd=request|answer|hangUp|cancel|reject (default request).
	api.Post("/devices/:id/intercom/signal", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		resp, err := NewISAPIClientForDevice(d, hub).SendCallSignal(c.Query("cmd", "request"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- Phase 3: health & access schedules ----------

	// Device health (door / lock / tamper / battery / capacity).
	api.Get("/devices/:id/work-status", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		client := NewISAPIClientForDevice(d, hub)
		raw, err := client.GetAcsWorkStatus()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "raw": raw})
	})

	// Read a week plan.
	api.Get("/devices/:id/week-plan/:planNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		planNo, _ := strconv.Atoi(c.Params("planNo"))
		client := NewISAPIClientForDevice(d, hub)
		raw, err := client.GetWeekPlan(planNo)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "raw": raw})
	})

	// Write a week plan (one allow window per weekday).
	api.Put("/devices/:id/week-plan/:planNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		planNo, _ := strconv.Atoi(c.Params("planNo"))
		var body struct {
			Days []WeekPlanDay `json:"days"`
		}
		if e := json.Unmarshal(c.Body(), &body); e != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetWeekPlan(planNo, body.Days)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Write a plan template referencing a week plan.
	api.Put("/devices/:id/plan-template/:tplNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		tplNo, _ := strconv.Atoi(c.Params("tplNo"))
		var body struct {
			Name       string `json:"name"`
			WeekPlanNo int    `json:"weekPlanNo"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetPlanTemplate(tplNo, body.Name, body.WeekPlanNo)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- QR-via-camera (device-native QR scanning) ----------

	// Probe whether the device camera can read QR codes.
	api.Get("/devices/:id/qr-capability", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		client := NewISAPIClientForDevice(d, hub)
		supported, raw, err := client.SupportsCameraQR()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "supported": supported, "raw": raw})
	})

	// Enable/disable device-native QR scanning. Body: {"enable": true|false}.
	api.Post("/devices/:id/qr-scan", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		var body struct {
			Enable bool `json:"enable"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SetQRScanEnabled(body.Enable)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- Face enrol via ISAPI ----------

	api.Post("/devices/:id/faces", func(c *fiber.Ctx) error {
		deviceID := c.Params("id")
		ctx := c.Context()

		d, err := store.GetDevice(ctx, deviceID)
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		if d.IP == "" || d.ISAPIUsername == "" {
			return c.Status(400).JSON(fiber.Map{"error": "device has no ISAPI credentials — set ip/port/username/password first"})
		}

		fh, err := c.FormFile("file")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "file (jpeg) required"})
		}
		personID := c.FormValue("personId")
		var person *Person
		if personID != "" {
			person, _ = store.GetPerson(ctx, personID)
		}
		if person == nil {
			// Auto-create a minimal person if none provided
			personID = uuid.NewString()
			person = &Person{
				ID:         personID,
				Name:       firstNonEmpty(c.FormValue("name"), "User "+personID[:8]),
				EmployeeNo: firstNonEmpty(c.FormValue("employeeNo"), personID[:8]),
				PersonType: "normal",
				PersonRole: "basic",
			}
			_ = store.CreatePerson(ctx, *person)
		}
		fdid := firstNonEmpty(c.FormValue("FDID"), d.FDID, "1")
		faceLibType := firstNonEmpty(c.FormValue("faceLibType"), d.FaceLibType, "blackFD")

		f, err := fh.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		defer f.Close()
		jpeg, err := io.ReadAll(f)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		imageKey := fmt.Sprintf("faces/%s/%s.jpg", personID, uuid.NewString())
		if err := store.PutObject(ctx, imageKey, "image/jpeg", jpeg); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "store image: " + err.Error()})
		}

		face := Face{
			ID:       uuid.NewString(),
			PersonID: personID,
			DeviceID: deviceID,
			ImageKey: imageKey,
			Status:   "pending",
		}
		_ = store.CreateFace(ctx, face)

		client := NewISAPIClientForDevice(d, hub)

		// Step 1: upsert UserInfo on the device with full role/validity.
		// Verify mode follows the current QR-2FA toggle so the new user lands in
		// the correct state without a separate sync step.
		hikUser := HikUserInfo{
			EmployeeNo:     person.EmployeeNo,
			Name:           person.Name,
			UserType:       person.PersonType,
			Gender:         person.Gender,
			LongTerm:       person.LongTerm,
			DoorRight:      person.DoorRight,
			PlanTemplate:   person.PlanTemplate,
			LocalUIRight:   person.PersonRole == "administrator",
			CheckUser:      person.AttendanceOnly,
			UserVerifyMode: qrAuth.ModeForDevice(ctx, d.DeviceID),
		}
		if person.ValidBegin != nil {
			hikUser.ValidBegin = person.ValidBegin.Format("2006-01-02T15:04:05")
		}
		if person.ValidEnd != nil {
			hikUser.ValidEnd = person.ValidEnd.Format("2006-01-02T15:04:05")
		}
		userResp, userErr := client.UpsertUserOnDevice(hikUser)
		if userErr != nil {
			log.Printf("UpsertUserOnDevice for %s: %v (resp=%s)", person.EmployeeNo, userErr, userResp)
			// We continue anyway — face enrol auto-creates a minimal record on most firmware.
		}

		// Step 2: push the face image. Use employeeNo as the FPID so the face
		// is linked to the user record created in step 1.
		resp, err := client.EnrolFace(fdid, faceLibType, person.EmployeeNo, person.Name, jpeg)
		if err != nil {
			_ = store.UpdateFaceStatus(ctx, face.ID, "failed")
			face.Status = "failed" // keep the returned object in sync with the DB
			return c.Status(502).JSON(fiber.Map{
				"ok":             false,
				"face":           face,
				"userResponse":   json.RawMessage(safeJSONFromBytes([]byte(userResp))),
				"deviceResponse": json.RawMessage(safeJSONFromBytes([]byte(resp))),
				"error":          err.Error(),
			})
		}
		_ = store.UpdateFaceStatus(ctx, face.ID, "enrolled")
		face.Status = "enrolled" // keep the returned object in sync with the DB
		return c.JSON(fiber.Map{
			"ok":             true,
			"face":           face,
			"userResponse":   json.RawMessage(safeJSONFromBytes([]byte(userResp))),
			"deviceResponse": json.RawMessage(safeJSONFromBytes([]byte(resp))),
		})
	})

	api.Get("/devices/:id/faces", func(c *fiber.Ctx) error {
		out, err := store.ListFaces(c.Context(), c.Params("id"), c.Query("personId"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})

	api.Delete("/devices/:id/faces/:personId", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		fdid := firstNonEmpty(c.Query("FDID"), d.FDID, "1")
		faceLibType := firstNonEmpty(c.Query("faceLibType"), d.FaceLibType, "blackFD")
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.DeleteFace(fdid, faceLibType, c.Params("personId"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Live snapshot from the device's camera
	api.Get("/devices/:id/snapshot", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not configured"})
		}
		client := NewISAPIClientForDevice(d, hub)
		jpeg, ctype, err := client.GetSnapshot()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": err.Error()})
		}
		c.Set("Content-Type", ctype)
		c.Set("Cache-Control", "no-store")
		return c.Send(jpeg)
	})

	// Probe device face library (lists faces stored on the device)
	api.Get("/devices/:id/face-lib", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		fdid := firstNonEmpty(c.Query("FDID"), d.FDID, "1")
		faceLibType := firstNonEmpty(c.Query("faceLibType"), d.FaceLibType, "blackFD")
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SearchFaces(fdid, faceLibType, 200)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.Type("json").SendString(resp)
	})

	// Raw ISAPI passthrough (for any other endpoint you want to hit directly)
	api.Post("/devices/:id/isapi", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		var body struct {
			Method      string `json:"method"`
			Path        string `json:"path"`
			ContentType string `json:"contentType"`
			Body        string `json:"body"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Path == "" {
			return c.Status(400).JSON(fiber.Map{"error": "path required"})
		}
		if body.Method == "" {
			body.Method = "GET"
		}
		if body.ContentType == "" && body.Body != "" {
			body.ContentType = "application/json"
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, respBody, err := client.Do(body.Method, body.Path, body.ContentType, []byte(body.Body))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		ct := resp.Header.Get("Content-Type")
		return c.Status(resp.StatusCode).Type(firstNonEmpty(ct, "text/plain")).Send(respBody)
	})

	// ---------- Events ----------

	// ---------- Bridge diagnostics ----------

	api.Get("/bridge/info", func(c *fiber.Ctx) error {
		host := cfg.PublicPushHost
		if host == "" {
			host = c.Hostname() // best-effort fallback
			// Strip any :port that snuck in
			if i := strings.Index(host, ":"); i >= 0 {
				host = host[:i]
			}
		}
		otapURL := cfg.PublicPushURL
		if otapURL == "" {
			otapURL = fmt.Sprintf("http://%s:%s", host, cfg.PushPort)
		}
		out := fiber.Map{
			"otap": fiber.Map{
				"serverHost":  host,
				"serverPort":  cfg.PushPort,
				"fullUrl":     otapURL,
				"protocol":    "HTTP",
				"format":      "JSON",
				"pathPattern": "/iot/{DeviceID}/global/0-global/model/service/operate/PUSH/{Action}",
			},
			"isup": fiber.Map{
				"serverHost": host,
				"serverPort": cfg.PushPort,
				"protocol":   "binary",
				"note":       "ISUP V5 is a binary protocol; this server can observe connections but cannot push commands back without the proprietary spec. Use OTAP if your firmware supports it.",
			},
			"noAuthMode":      cfg.NoAuthMode,
			"defaultPassword": cfg.DefaultPassword != "",
		}
		if cfg.TLSPort != "" {
			out["otapTls"] = fiber.Map{
				"serverHost": host,
				"serverPort": cfg.TLSPort,
				"fullUrl":    fmt.Sprintf("https://%s:%s", host, cfg.TLSPort),
				"protocol":   "HTTPS",
				"note":       "Self-signed certificate. Some Hik firmware refuses self-signed; if your OTAP module fails on TLS too, the firmware requires a CA-signed cert.",
			}
		}
		return c.JSON(out)
	})

	api.Get("/bridge/log", func(c *fiber.Ctx) error {
		return c.JSON(BridgeLogSnapshot())
	})

	api.Post("/bridge/log/clear", func(c *fiber.Ctx) error {
		BridgeLogClear()
		return c.JSON(fiber.Map{"ok": true})
	})

	api.Post("/events/purge-empty", func(c *fiber.Ctx) error {
		n, err := store.PurgeEmptyEvents(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "deleted": n})
	})

	api.Get("/events", func(c *fiber.Ctx) error {
		limit, _ := strconv.Atoi(c.Query("limit", "100"))
		out, err := store.ListEvents(c.Context(), c.Query("deviceId"), limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})

	api.Get("/events/stream", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("Access-Control-Allow-Origin", "*")
		ch := store.Subscribe()
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			defer store.Unsubscribe(ch)
			_, _ = w.WriteString(": ping\n\n")
			_ = w.Flush()
			tick := time.NewTicker(20 * time.Second)
			defer tick.Stop()
			for {
				select {
				case e, ok := <-ch:
					if !ok {
						return
					}
					data, _ := json.Marshal(e)
					if _, err := fmt.Fprintf(w, "event: event\ndata: %s\n\n", data); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				case <-tick.C:
					if _, err := w.WriteString(": keep-alive\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				}
			}
		})
		return nil
	})

	api.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	api.Get("/ws/events", websocket.New(func(c *websocket.Conn) {
		ch := store.Subscribe()
		defer store.Unsubscribe(ch)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}()
		for {
			select {
			case e, ok := <-ch:
				if !ok {
					return
				}
				if err := c.WriteJSON(e); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}))

	// Image proxy
	api.Get("/images/*", func(c *fiber.Ctx) error {
		key := c.Params("*")
		if key == "" {
			return c.Status(400).SendString("missing key")
		}
		obj, ctype, size, err := store.GetObject(c.Context(), key)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		defer obj.Close()
		c.Set("Content-Type", strings.TrimSpace(firstNonEmpty(ctype, "application/octet-stream")))
		c.Set("Cache-Control", "public, max-age=3600")
		c.Response().Header.SetContentLength(int(size))
		_, err = io.Copy(c.Response().BodyWriter(), obj)
		return err
	})

	// ---------- Settings (admin) ----------

	api.Get("/settings", func(c *fiber.Ctx) error {
		return c.JSON(settings.Get())
	})
	api.Put("/settings", func(c *fiber.Ctx) error {
		prev := settings.Get()
		var body Settings
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad body"})
		}
		if err := settings.Save(c.Context(), body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// If the global QR-2FA toggle actually changed, push the new verify mode
		// to every device that follows the global setting. This is what makes
		// the toggle do something at device level — without it the device keeps
		// the previous user verify mode and the toggle has no real effect.
		applied := []map[string]any{}
		if prev.RequireQR2FA != body.RequireQR2FA {
			applied = qrAuth.ApplyAllDeviceModes(c.Context())
		}
		return c.JSON(fiber.Map{"settings": settings.Get(), "applied": applied})
	})

	// Per-device QR override:
	//   PUT /api/devices/:id/require-qr   { "value": true|false|null }
	api.Put("/devices/:id/require-qr", func(c *fiber.Ctx) error {
		var body struct {
			Value *bool `json:"value"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		if err := settings.SetDeviceRequireQR(c.Context(), c.Params("id"), body.Value); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Push the new mode to the device so the change actually takes effect.
		n, mode, err := qrAuth.ApplyDeviceMode(c.Context(), c.Params("id"))
		eff, _ := settings.DeviceRequiresQR(c.Context(), c.Params("id"))
		out := fiber.Map{
			"ok":                 err == nil,
			"effectiveRequireQR": eff,
			"override":           body.Value,
			"appliedToUsers":     n,
			"appliedMode":        mode,
		}
		if err != nil {
			out["applyError"] = err.Error()
		}
		return c.JSON(out)
	})

	// Manual "apply now" — re-pushes the effective mode to one device.
	api.Post("/devices/:id/apply-mode", func(c *fiber.Ctx) error {
		n, mode, err := qrAuth.ApplyDeviceMode(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "users": n, "mode": mode})
	})

	// Manual "apply to all" — re-pushes the effective mode to every device.
	api.Post("/settings/apply-all", func(c *fiber.Ctx) error {
		results := qrAuth.ApplyAllDeviceModes(c.Context())
		return c.JSON(fiber.Map{"ok": true, "results": results})
	})
	api.Get("/devices/:id/require-qr", func(c *fiber.Ctx) error {
		eff, err := settings.DeviceRequiresQR(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"effectiveRequireQR": eff, "global": settings.Get().RequireQR2FA})
	})

	// ---------- API keys (admin) ----------

	api.Get("/api-keys", func(c *fiber.Ctx) error {
		out, err := settings.ListAPIKeys(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})
	api.Post("/api-keys", func(c *fiber.Ctx) error {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		k, err := settings.CreateAPIKey(c.Context(), body.Name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(k) // includes plaintext key — shown once
	})
	api.Delete("/api-keys/:id", func(c *fiber.Ctx) error {
		if err := settings.DeleteAPIKey(c.Context(), c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// ---------- Public v1 API (third-party callers) ----------
	//
	// Auth: every request must carry an API key in one of:
	//   - Header: Authorization: Bearer fa_xxx
	//   - Header: X-API-Key: fa_xxx
	//   - Query:  ?apiKey=fa_xxx
	//
	// All endpoints sit under /api/v1.

	v1 := app.Group("/api/v1", apiKeyAuth(settings))

	// Plugins — each plugin's RegisterX call mounts its own routes under
	// /api/plugins/<name>/* (admin) and /api/v1/plugins/<name>/* (public).
	// Removing a plugin = delete its file and these two lines.
	RegisterFaceApp(api, v1, settings)

	v1.Get("/ping", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true, "service": "face_auth", "time": time.Now().UTC()})
	})

	v1.Get("/devices", func(c *fiber.Ctx) error {
		devs, err := store.ListDevices(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		// Strip internal fields and decorate with effective QR mode.
		out := make([]fiber.Map, 0, len(devs))
		for _, d := range devs {
			eff, _ := settings.DeviceRequiresQR(c.Context(), d.DeviceID)
			out = append(out, fiber.Map{
				"deviceId":     d.DeviceID,
				"name":         d.Name,
				"model":        d.Model,
				"online":       d.Online,
				"requireQR2FA": eff,
				"agentId":      d.AgentID,
			})
		}
		return c.JSON(out)
	})

	v1.Get("/persons", func(c *fiber.Ctx) error {
		ps, err := store.ListPersons(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		out := make([]fiber.Map, 0, len(ps))
		for _, p := range ps {
			out = append(out, fiber.Map{
				"id":         p.ID,
				"name":       p.Name,
				"employeeNo": p.EmployeeNo,
				"hasQR":      p.QRToken != "",
			})
		}
		return c.JSON(out)
	})

	// POST /api/v1/auth/face/start
	//
	// Open a face-auth window on a device. Behavior:
	//
	//   1. If `qrToken` is provided  → person is resolved by QR; QR mode.
	//   2. Else if `personId` / `employeeNo` is provided → person is resolved
	//      directly; face-only mode (works regardless of toggle).
	//   3. Else → face-any mode. Only allowed if the device's effective
	//      requireQR2FA is false (i.e., the admin opted out of QR).
	//
	// Returns the session record. Caller should poll
	//   GET /api/v1/auth/face/{sessionId}
	// until status != "open" — or subscribe to /api/v1/auth/face/stream.
	v1.Post("/auth/face/start", func(c *fiber.Ctx) error {
		if !settings.Get().PublicAPIEnabled {
			return c.Status(503).JSON(fiber.Map{"error": "public api disabled"})
		}
		var body FaceAuthRequest
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad body"})
		}
		body.Source = "api"
		sess, err := qrAuth.StartFaceAuth(c.Context(), body)
		if err != nil {
			code := 400
			if errors.Is(err, ErrQRRequired) {
				return c.Status(409).JSON(fiber.Map{
					"error":  "qr_required",
					"detail": "this device requires QR scan before face — supply qrToken, personId, or employeeNo",
				})
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(sess)
	})

	v1.Get("/auth/face/:id", func(c *fiber.Ctx) error {
		s := qrAuth.GetSession(c.Params("id"))
		if s == nil {
			return c.Status(404).JSON(fiber.Map{"error": "session not found"})
		}
		return c.JSON(s)
	})

	v1.Post("/auth/face/:id/cancel", func(c *fiber.Ctx) error {
		ok := qrAuth.CancelSession(c.Params("id"))
		return c.JSON(fiber.Map{"ok": ok})
	})

	// SSE stream — emits {session} JSON every time a session ends.
	v1.Get("/auth/face/stream", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		// Each face-match event fans out through the existing event bus; we
		// just decorate it with the matching session id (if any).
		ch := store.Subscribe()
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			defer store.Unsubscribe(ch)
			_, _ = w.WriteString(": ping\n\n")
			_ = w.Flush()
			tick := time.NewTicker(20 * time.Second)
			defer tick.Stop()
			for {
				select {
				case e, ok := <-ch:
					if !ok {
						return
					}
					emp, isFace := extractFaceMatchFromEvent(e)
					if !isFace {
						continue
					}
					payload, _ := json.Marshal(fiber.Map{
						"deviceId":   e.DeviceID,
						"employeeNo": emp,
						"receivedAt": e.ReceivedAt,
					})
					if _, err := fmt.Fprintf(w, "event: face_match\ndata: %s\n\n", payload); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				case <-tick.C:
					if _, err := w.WriteString(": keep-alive\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				}
			}
		})
		return nil
	})

	// ---------- v1: Persons (third-party feature parity) ----------

	v1.Post("/persons", func(c *fiber.Ctx) error {
		var body Person
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name required"})
		}
		if body.ID == "" {
			body.ID = uuid.NewString()
		}
		body.EmployeeNo = sanitizeFPID(body.EmployeeNo)
		if body.EmployeeNo == "" {
			body.EmployeeNo = sanitizeFPID(body.ID)
			if len(body.EmployeeNo) > 8 {
				body.EmployeeNo = body.EmployeeNo[:8]
			}
		}
		if err := store.CreatePerson(c.Context(), body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(body)
	})

	v1.Get("/persons/:id", func(c *fiber.Ctx) error {
		p, err := store.GetPerson(c.Context(), c.Params("id"))
		if err != nil || p == nil {
			return c.Status(404).JSON(fiber.Map{"error": "person not found"})
		}
		faces, _ := store.ListFaces(c.Context(), "", p.ID)
		return c.JSON(fiber.Map{"person": p, "faces": faces})
	})

	v1.Delete("/persons/:id", func(c *fiber.Ctx) error {
		ctx := c.Context()
		p, _ := store.GetPerson(ctx, c.Params("id"))
		if devID := c.Query("syncDevice"); devID != "" && p != nil && p.EmployeeNo != "" {
			d, _ := store.GetDevice(ctx, devID)
			if d != nil && d.IP != "" {
				client := NewISAPIClientForDevice(d, hub)
				if _, err := client.DeleteUserByEmployeeNo(p.EmployeeNo); err != nil {
					return c.Status(502).JSON(fiber.Map{"error": "device delete failed: " + err.Error()})
				}
			}
		}
		if err := store.DeletePerson(ctx, c.Params("id")); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	v1.Post("/persons/:id/qr/rotate", func(c *fiber.Ctx) error {
		p, _ := store.GetPerson(c.Context(), c.Params("id"))
		if p == nil {
			return c.Status(404).JSON(fiber.Map{"error": "person not found"})
		}
		token := RandomString(24, charset+hexCharset)
		if err := store.SetQRToken(c.Context(), p.ID, token); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "qrToken": token})
	})

	v1.Get("/persons/:id/qr.png", func(c *fiber.Ctx) error {
		p, _ := store.GetPerson(c.Context(), c.Params("id"))
		if p == nil || p.QRToken == "" {
			return c.Status(404).SendString("no QR token — call /qr/rotate first")
		}
		size, _ := strconv.Atoi(c.Query("size", "256"))
		if size < 96 {
			size = 96
		}
		if size > 1024 {
			size = 1024
		}
		png, err := qrcode.Encode(p.QRToken, qrcode.Medium, size)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		c.Set("Content-Type", "image/png")
		return c.Send(png)
	})

	// ---------- v1: Faces ----------

	v1.Get("/devices/:id/faces", func(c *fiber.Ctx) error {
		out, err := store.ListFaces(c.Context(), c.Params("id"), c.Query("personId"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})

	v1.Post("/devices/:id/faces", func(c *fiber.Ctx) error {
		deviceID := c.Params("id")
		ctx := c.Context()
		d, err := store.GetDevice(ctx, deviceID)
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		if d.IP == "" || d.ISAPIUsername == "" {
			return c.Status(400).JSON(fiber.Map{"error": "device has no ISAPI credentials"})
		}
		fh, err := c.FormFile("file")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "file (jpeg) required"})
		}
		personID := c.FormValue("personId")
		var person *Person
		if personID != "" {
			person, _ = store.GetPerson(ctx, personID)
		}
		if person == nil {
			personID = uuid.NewString()
			person = &Person{
				ID:         personID,
				Name:       firstNonEmpty(c.FormValue("name"), "User "+personID[:8]),
				EmployeeNo: firstNonEmpty(c.FormValue("employeeNo"), personID[:8]),
				PersonType: "normal",
				PersonRole: "basic",
			}
			_ = store.CreatePerson(ctx, *person)
		}
		fdid := firstNonEmpty(c.FormValue("FDID"), d.FDID, "1")
		faceLibType := firstNonEmpty(c.FormValue("faceLibType"), d.FaceLibType, "blackFD")
		f, err := fh.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		defer f.Close()
		jpeg, err := io.ReadAll(f)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		imageKey := fmt.Sprintf("faces/%s/%s.jpg", personID, uuid.NewString())
		if err := store.PutObject(ctx, imageKey, "image/jpeg", jpeg); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "store image: " + err.Error()})
		}
		face := Face{ID: uuid.NewString(), PersonID: personID, DeviceID: deviceID, ImageKey: imageKey, Status: "pending"}
		_ = store.CreateFace(ctx, face)
		client := NewISAPIClientForDevice(d, hub)
		hikUser := HikUserInfo{
			EmployeeNo:     person.EmployeeNo,
			Name:           person.Name,
			UserType:       person.PersonType,
			Gender:         person.Gender,
			LongTerm:       person.LongTerm,
			DoorRight:      person.DoorRight,
			PlanTemplate:   person.PlanTemplate,
			LocalUIRight:   person.PersonRole == "administrator",
			CheckUser:      person.AttendanceOnly,
			UserVerifyMode: qrAuth.ModeForDevice(ctx, d.DeviceID),
		}
		_, _ = client.UpsertUserOnDevice(hikUser)
		resp, err := client.EnrolFace(fdid, faceLibType, person.EmployeeNo, person.Name, jpeg)
		if err != nil {
			_ = store.UpdateFaceStatus(ctx, face.ID, "failed")
			face.Status = "failed" // keep the returned object in sync with the DB
			return c.Status(502).JSON(fiber.Map{"ok": false, "face": face, "error": err.Error(), "deviceResponse": json.RawMessage(safeJSONFromBytes([]byte(resp)))})
		}
		_ = store.UpdateFaceStatus(ctx, face.ID, "enrolled")
		face.Status = "enrolled" // keep the returned object in sync with the DB
		return c.JSON(fiber.Map{"ok": true, "face": face, "deviceResponse": json.RawMessage(safeJSONFromBytes([]byte(resp)))})
	})

	v1.Delete("/devices/:id/faces/:personId", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		fdid := firstNonEmpty(c.Query("FDID"), d.FDID, "1")
		faceLibType := firstNonEmpty(c.Query("faceLibType"), d.FaceLibType, "blackFD")
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.DeleteFace(fdid, faceLibType, c.Params("personId"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- v1: Device ops ----------

	v1.Post("/devices/:id/probe", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		if d.IP == "" || d.ISAPIUsername == "" {
			return c.Status(400).JSON(fiber.Map{"error": "no ISAPI credentials"})
		}
		client := NewISAPIClientForDevice(d, hub)
		info, err := client.GetDeviceInfo()
		if err != nil {
			_ = store.SetDeviceOnline(c.Context(), d.DeviceID, "", "", false)
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		_ = store.SetDeviceOnline(c.Context(), d.DeviceID, info.Model, info.FirmwareVersion, true)
		return c.JSON(fiber.Map{"ok": true, "info": info})
	})

	v1.Post("/devices/:id/sync-persons", func(c *fiber.Ctx) error {
		// Reuse the same logic as the admin endpoint by forwarding through the
		// store path. Calling the admin Fiber handler isn't trivial; instead we
		// just re-implement the minimal flow here.
		ctx := c.Context()
		d, err := store.GetDevice(ctx, c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not found or not configured"})
		}
		client := NewISAPIClientForDevice(d, hub)
		users, err := client.ListUsers()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": err.Error()})
		}
		synced := 0
		for _, u := range users {
			if u.EmployeeNo == "" {
				continue
			}
			existing, _ := store.GetPersonByEmployeeNo(ctx, u.EmployeeNo)
			personID := ""
			if existing != nil {
				personID = existing.ID
			} else {
				personID = uuid.NewString()
			}
			p := Person{
				ID: personID, Name: u.Name, EmployeeNo: u.EmployeeNo,
				Gender: u.Gender, PersonType: mapDeviceUserType(u.UserType),
				PersonRole: ifElse(u.LocalUIRight, "administrator", "basic"),
				DoorRight:  firstNonEmpty(u.DoorRight, "1"), PlanTemplate: "1",
			}
			if err := store.CreatePerson(ctx, p); err == nil {
				synced++
			}
		}
		return c.JSON(fiber.Map{"ok": true, "users": len(users), "synced": synced})
	})

	v1.Get("/devices/:id/face-lib", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		fdid := firstNonEmpty(c.Query("FDID"), d.FDID, "1")
		faceLibType := firstNonEmpty(c.Query("faceLibType"), d.FaceLibType, "blackFD")
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.SearchFaces(fdid, faceLibType, 200)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.Type("json").SendString(resp)
	})

	v1.Post("/devices/:id/isapi", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		var body struct {
			Method      string `json:"method"`
			Path        string `json:"path"`
			ContentType string `json:"contentType"`
			Body        string `json:"body"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Path == "" {
			return c.Status(400).JSON(fiber.Map{"error": "path required"})
		}
		if body.Method == "" {
			body.Method = "GET"
		}
		if body.ContentType == "" && body.Body != "" {
			body.ContentType = "application/json"
		}
		client := NewISAPIClientForDevice(d, hub)
		resp, respBody, err := client.Do(body.Method, body.Path, body.ContentType, []byte(body.Body))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		ct := resp.Header.Get("Content-Type")
		return c.Status(resp.StatusCode).Type(firstNonEmpty(ct, "text/plain")).Send(respBody)
	})

	// ---------- v1: QR scan (third-party agent emulation) ----------

	v1.Post("/qr-auth/scan", func(c *fiber.Ctx) error {
		var body struct {
			QRToken string `json:"qrToken"`
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.QRToken == "" {
			return c.Status(400).JSON(fiber.Map{"error": "qrToken required"})
		}
		s, err := qrAuth.Scan(c.Context(), body.QRToken, body.AgentID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "session": s})
	})

	// ---------- v1: Events ----------

	v1.Get("/events", func(c *fiber.Ctx) error {
		limit, _ := strconv.Atoi(c.Query("limit", "100"))
		out, err := store.ListEvents(c.Context(), c.Query("deviceId"), limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})

	v1.Get("/events/stream", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		ch := store.Subscribe()
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			defer store.Unsubscribe(ch)
			_, _ = w.WriteString(": ping\n\n")
			_ = w.Flush()
			tick := time.NewTicker(20 * time.Second)
			defer tick.Stop()
			for {
				select {
				case e, ok := <-ch:
					if !ok {
						return
					}
					data, _ := json.Marshal(e)
					if _, err := fmt.Fprintf(w, "event: event\ndata: %s\n\n", data); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				case <-tick.C:
					if _, err := w.WriteString(": keep-alive\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				}
			}
		})
		return nil
	})

	// Open door via API (third-party trigger)
	v1.Post("/devices/:id/open-door", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not found"})
		}
		doorNo, _ := strconv.Atoi(c.Query("door", "1"))
		client := NewISAPIClientForDevice(d, hub)
		resp, err := client.OpenDoor(doorNo)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// Snapshot via API (third-party can grab a still frame).
	v1.Get("/devices/:id/snapshot", func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not configured"})
		}
		client := NewISAPIClientForDevice(d, hub)
		jpeg, ctype, err := client.GetSnapshot()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": err.Error()})
		}
		c.Set("Content-Type", ctype)
		c.Set("Cache-Control", "no-store")
		return c.Send(jpeg)
	})

	// ---------- v1: enrolment, schedules, health, intercom, QR (third-party) ----------
	// These mirror the admin endpoints; reachable() allows direct / agent / OTAP.

	v1.Post("/devices/:id/capture/face", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		resp, err := NewISAPIClientForDevice(d, hub).CaptureFaceData(c.Query("infrared") == "true")
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Post("/devices/:id/capture/card", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		resp, err := NewISAPIClientForDevice(d, hub).CaptureCardInfo()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Post("/devices/:id/capture/fingerprint", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		finger, _ := strconv.Atoi(c.Query("finger", "1"))
		resp, err := NewISAPIClientForDevice(d, hub).CaptureFingerPrint(finger)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Post("/devices/:id/cards", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		var b struct{ EmployeeNo, CardNo, CardType, Mode string }
		if e := json.Unmarshal(c.Body(), &b); e != nil || b.EmployeeNo == "" || b.CardNo == "" {
			return c.Status(400).JSON(fiber.Map{"error": "employeeNo and cardNo required"})
		}
		resp, err := NewISAPIClientForDevice(d, hub).SetCardInfo(b.EmployeeNo, b.CardNo, b.CardType, b.Mode)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Delete("/devices/:id/cards/:cardNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		resp, err := NewISAPIClientForDevice(d, hub).DeleteCard(c.Params("cardNo"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Post("/devices/:id/fingerprints", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		var b struct {
			EmployeeNo    string `json:"employeeNo"`
			FingerPrintID int    `json:"fingerPrintID"`
			FingerData    string `json:"fingerData"`
		}
		if e := json.Unmarshal(c.Body(), &b); e != nil || b.EmployeeNo == "" || b.FingerData == "" {
			return c.Status(400).JSON(fiber.Map{"error": "employeeNo and fingerData required"})
		}
		resp, err := NewISAPIClientForDevice(d, hub).SetFingerPrint(b.EmployeeNo, b.FingerPrintID, b.FingerData)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Delete("/devices/:id/fingerprints/:employeeNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		finger, _ := strconv.Atoi(c.Query("finger", "0"))
		resp, err := NewISAPIClientForDevice(d, hub).DeleteFingerPrint(c.Params("employeeNo"), finger)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Get("/devices/:id/work-status", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		raw, err := NewISAPIClientForDevice(d, hub).GetAcsWorkStatus()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "raw": raw})
	})
	v1.Put("/devices/:id/week-plan/:planNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		planNo, _ := strconv.Atoi(c.Params("planNo"))
		var b struct {
			Days []WeekPlanDay `json:"days"`
		}
		if e := json.Unmarshal(c.Body(), &b); e != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		resp, err := NewISAPIClientForDevice(d, hub).SetWeekPlan(planNo, b.Days)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Put("/devices/:id/plan-template/:tplNo", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		tplNo, _ := strconv.Atoi(c.Params("tplNo"))
		var b struct {
			Name       string `json:"name"`
			WeekPlanNo int    `json:"weekPlanNo"`
		}
		_ = json.Unmarshal(c.Body(), &b)
		resp, err := NewISAPIClientForDevice(d, hub).SetPlanTemplate(tplNo, b.Name, b.WeekPlanNo)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Get("/devices/:id/qr-capability", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		supported, raw, err := NewISAPIClientForDevice(d, hub).SupportsCameraQR()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "supported": supported, "raw": raw})
	})
	v1.Post("/devices/:id/qr-scan", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		var b struct {
			Enable bool `json:"enable"`
		}
		_ = json.Unmarshal(c.Body(), &b)
		resp, err := NewISAPIClientForDevice(d, hub).SetQRScanEnabled(b.Enable)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})
	v1.Get("/devices/:id/intercom/capabilities", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		raw, err := NewISAPIClientForDevice(d, hub).GetIntercomCapabilities()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "supported": strings.Contains(raw, "isSupportCallSignal") && strings.Contains(raw, "true"), "raw": raw})
	})
	v1.Get("/devices/:id/intercom/status", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		raw, err := NewISAPIClientForDevice(d, hub).GetCallStatus()
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "raw": raw})
		}
		return c.JSON(fiber.Map{"ok": true, "raw": raw})
	})
	v1.Post("/devices/:id/intercom/signal", func(c *fiber.Ctx) error {
		d, err := reachable(c)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		resp, err := NewISAPIClientForDevice(d, hub).SendCallSignal(c.Query("cmd", "request"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"ok": false, "error": err.Error(), "response": resp})
		}
		return c.JSON(fiber.Map{"ok": true, "response": resp})
	})

	// ---------- Live MJPEG stream (admin + v1) ----------
	//
	// Pulls snapshots in a loop and re-multiplexes them as multipart/x-mixed-replace
	// — works with <img src="..."/> in any browser. Frame rate is tunable via
	// ?fps=1..15 (default 4) and ?seconds=N to auto-close (default 0 = forever).

	mjpeg := func(c *fiber.Ctx) error {
		d, err := store.GetDevice(c.Context(), c.Params("id"))
		if err != nil || d == nil || d.IP == "" {
			return c.Status(404).JSON(fiber.Map{"error": "device not configured"})
		}
		client := NewISAPIClientForDevice(d, hub)
		fps, _ := strconv.Atoi(c.Query("fps", "4"))
		if fps < 1 {
			fps = 1
		}
		if fps > 15 {
			fps = 15
		}
		secs, _ := strconv.Atoi(c.Query("seconds", "0"))

		boundary := "fa-frame-" + RandomString(8, hexCharset)
		c.Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
		c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Set("Pragma", "no-cache")
		c.Set("Connection", "close")

		start := time.Now()
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			defer w.Flush()
			interval := time.Second / time.Duration(fps)
			for {
				if secs > 0 && time.Since(start) > time.Duration(secs)*time.Second {
					return
				}
				jpeg, _, err := client.GetSnapshot()
				if err != nil {
					// Brief back-off — don't tight-loop on a dead device
					time.Sleep(time.Second)
					continue
				}
				if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(jpeg)); err != nil {
					return
				}
				if _, err := w.Write(jpeg); err != nil {
					return
				}
				if _, err := w.Write([]byte("\r\n")); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
				time.Sleep(interval)
			}
		})
		return nil
	}
	api.Get("/devices/:id/stream.mjpg", mjpeg)
	v1.Get("/devices/:id/stream.mjpg", mjpeg)

	api.Get("/status", func(c *fiber.Ctx) error {
		devices, _ := store.ListDevices(c.Context())
		online := 0
		for _, d := range devices {
			if d.Online {
				online++
			}
		}
		return c.JSON(fiber.Map{
			"ok":              true,
			"devices":         len(devices),
			"devicesOnline":   online,
			"time":            time.Now().UTC(),
			"mode":            "isapi",
			"eventCallbackIP": cfg.EventCallbackIP,
			"pushPort":        cfg.PushPort,
		})
	})

	return app
}

func describeBinary(name string) (platform, label string) {
	switch {
	case strings.Contains(name, "windows-amd64"):
		return "windows-amd64", "Windows (x64)"
	case strings.Contains(name, "linux-amd64"):
		return "linux-amd64", "Linux (x64)"
	case strings.Contains(name, "linux-arm64"):
		return "linux-arm64", "Linux ARM64 (Raspberry Pi 4+)"
	case strings.Contains(name, "linux-armv7"):
		return "linux-armv7", "Linux ARMv7 (Pi 3 / older)"
	case strings.Contains(name, "darwin-amd64"):
		return "darwin-amd64", "macOS (Intel)"
	case strings.Contains(name, "darwin-arm64"):
		return "darwin-arm64", "macOS (Apple Silicon)"
	}
	return "unknown", name
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func ifElse[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// apiKeyAuth extracts the API key from header or query and verifies it.
// When NoAuthMode is enabled in config we still require a key here — the
// /api/v1 surface is the third-party entrypoint and should always be
// authenticated, regardless of the admin UI's auth setting.
func apiKeyAuth(settings *SettingsStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := ""
		if h := c.Get("Authorization"); h != "" {
			if strings.HasPrefix(strings.ToLower(h), "bearer ") {
				key = strings.TrimSpace(h[7:])
			}
		}
		if key == "" {
			key = c.Get("X-API-Key")
		}
		if key == "" {
			key = c.Query("apiKey")
		}
		if key == "" {
			return c.Status(401).JSON(fiber.Map{"error": "missing api key"})
		}
		k, err := settings.VerifyAPIKey(c.Context(), key)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": err.Error()})
		}
		c.Locals("apiKey", k)
		return c.Next()
	}
}

func mapDeviceUserType(t string) string {
	switch strings.ToLower(t) {
	case "blacklist", "blocklist":
		return "blackList"
	case "visitor":
		return "visitor"
	default:
		return "normal"
	}
}
