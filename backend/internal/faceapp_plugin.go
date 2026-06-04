package internal

// ----------------------------------------------------------------------------
//  FaceApp plugin
//
//  This file is the entire integration with the standalone `faceapp_main`
//  system. To remove this plugin: delete this file and the two `RegisterFaceApp`
//  calls in api.go. The settings.Plugins["faceapp"] row can stay or be wiped
//  manually — it's a self-contained JSON blob.
//
//  Architecture
//  ------------
//  Third-party callers hit  /api/v1/plugins/faceapp/*  with their face_auth
//  API key. face_auth then forwards to the configured faceapp baseURL using
//  the *stored* faceapp bearer token. Callers never see the faceapp token.
//
//  Admin UI hits  /api/plugins/faceapp/*  with the dashboard's session — same
//  proxy logic, different mount point.
// ----------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const FaceAppPluginKey = "faceapp"

// FaceAppConfig is what the admin saves in Settings → FaceApp.
type FaceAppConfig struct {
	Enabled  bool   `json:"enabled"`
	BaseURL  string `json:"baseUrl"`         // e.g. https://face.qbot.now
	APIToken string `json:"apiToken"`        // FACEAPP_EXTERNAL_API_TOKEN value
	DeviceID int    `json:"deviceId"`        // managed device id (0 = default)
	Timeout  int    `json:"timeoutSeconds"`  // request timeout, 0 = 10
}

// faceAppClient is the thin HTTP client used by the proxy handlers.
type faceAppClient struct {
	cfg FaceAppConfig
	hc  *http.Client
}

func newFaceAppClient(cfg FaceAppConfig) *faceAppClient {
	t := cfg.Timeout
	if t <= 0 {
		t = 10
	}
	return &faceAppClient{
		cfg: cfg,
		hc:  &http.Client{Timeout: time.Duration(t) * time.Second},
	}
}

func (c *faceAppClient) do(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	if base == "" {
		return nil, nil, fmt.Errorf("faceapp baseUrl not configured")
	}
	url := base + path
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out, nil
}

// RegisterFaceApp mounts the plugin's HTTP routes on both the admin and the
// versioned public API groups. Call from NewAPIServer.
func RegisterFaceApp(adminGroup, v1Group fiber.Router, settings *SettingsStore) {
	loadCfg := func() FaceAppConfig {
		var cfg FaceAppConfig
		_, _ = settings.PluginConfig(FaceAppPluginKey, &cfg)
		if cfg.Timeout <= 0 {
			cfg.Timeout = 10
		}
		return cfg
	}

	requireEnabled := func(c *fiber.Ctx) (*faceAppClient, error) {
		cfg := loadCfg()
		if !cfg.Enabled {
			return nil, fiber.NewError(503, "faceapp plugin disabled")
		}
		if cfg.BaseURL == "" || cfg.APIToken == "" {
			return nil, fiber.NewError(503, "faceapp plugin not configured (baseUrl + apiToken required)")
		}
		return newFaceAppClient(cfg), nil
	}

	// -------- Admin-only endpoints: config CRUD + health check --------

	adminGroup.Get("/plugins/faceapp/config", func(c *fiber.Ctx) error {
		cfg := loadCfg()
		// Don't return the token in plaintext to the dashboard — show whether
		// it's set instead. Admin saves a new value to rotate.
		safe := fiber.Map{
			"enabled":        cfg.Enabled,
			"baseUrl":        cfg.BaseURL,
			"deviceId":       cfg.DeviceID,
			"timeoutSeconds": cfg.Timeout,
			"apiTokenSet":    cfg.APIToken != "",
		}
		return c.JSON(safe)
	})

	adminGroup.Put("/plugins/faceapp/config", func(c *fiber.Ctx) error {
		var body struct {
			Enabled        bool   `json:"enabled"`
			BaseURL        string `json:"baseUrl"`
			APIToken       string `json:"apiToken"`  // empty = keep existing
			DeviceID       int    `json:"deviceId"`
			TimeoutSeconds int    `json:"timeoutSeconds"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "bad body"})
		}
		cur := loadCfg()
		next := FaceAppConfig{
			Enabled:  body.Enabled,
			BaseURL:  strings.TrimRight(body.BaseURL, "/"),
			APIToken: body.APIToken,
			DeviceID: body.DeviceID,
			Timeout:  body.TimeoutSeconds,
		}
		if next.APIToken == "" {
			next.APIToken = cur.APIToken // preserve existing if blank
		}
		if err := settings.SavePluginConfig(c.Context(), FaceAppPluginKey, next); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	// -------- Proxy handlers (shared by /api and /api/v1) --------

	proxy := func(method, downstreamPath string, requestBody func(c *fiber.Ctx) (any, error), unwrapWithDeviceID bool) fiber.Handler {
		return func(c *fiber.Ctx) error {
			client, err := requireEnabled(c)
			if err != nil {
				return err
			}
			var body any
			if requestBody != nil {
				b, err := requestBody(c)
				if err != nil {
					return c.Status(400).JSON(fiber.Map{"error": err.Error()})
				}
				body = b
			}
			path := downstreamPath
			if unwrapWithDeviceID {
				devID := c.Query("device_id")
				if devID == "" && client.cfg.DeviceID > 0 {
					devID = fmt.Sprintf("%d", client.cfg.DeviceID)
				}
				if devID != "" {
					sep := "?"
					if strings.Contains(path, "?") {
						sep = "&"
					}
					path = path + sep + "device_id=" + devID
				}
			}
			resp, raw, err := client.do(c.Context(), method, path, body)
			if err != nil {
				return c.Status(502).JSON(fiber.Map{"error": err.Error()})
			}
			ct := resp.Header.Get("Content-Type")
			if ct == "" {
				ct = "application/json"
			}
			return c.Status(resp.StatusCode).Type(strings.SplitN(ct, ";", 2)[0]).Send(raw)
		}
	}

	mount := func(g fiber.Router) {
		// Health probe — proxies to faceapp's /api/external/health
		g.Get("/plugins/faceapp/health", proxy("GET", "/api/external/health", nil, false))

		// Device status (online + person counts)
		g.Get("/plugins/faceapp/device-status", proxy("GET", "/api/device/status", nil, true))

		// Dashboard — returns enrolled users + activity
		g.Get("/plugins/faceapp/people", func(c *fiber.Ctx) error {
			client, err := requireEnabled(c)
			if err != nil {
				return err
			}
			resp, raw, err := client.do(c.Context(), "GET", "/api/app/dashboard", nil)
			if err != nil {
				return c.Status(502).JSON(fiber.Map{"error": err.Error()})
			}
			if resp.StatusCode != 200 {
				return c.Status(resp.StatusCode).Send(raw)
			}
			var parsed struct {
				Users []json.RawMessage `json:"users"`
			}
			_ = json.Unmarshal(raw, &parsed)
			if parsed.Users == nil {
				parsed.Users = []json.RawMessage{}
			}
			return c.JSON(fiber.Map{"users": parsed.Users})
		})

		// Trigger gate open
		g.Post("/plugins/faceapp/open-gate", proxy("POST", "/api/external/open-gate", func(c *fiber.Ctx) (any, error) {
			var b map[string]any
			if len(c.Body()) > 0 {
				if err := json.Unmarshal(c.Body(), &b); err != nil {
					return nil, err
				}
			}
			if b == nil {
				b = map[string]any{}
			}
			return b, nil
		}, false))

		// Single enrolment — forwards as-is to faceapp's /api/enrollments
		g.Post("/plugins/faceapp/enroll", proxy("POST", "/api/enrollments", func(c *fiber.Ctx) (any, error) {
			var b map[string]any
			if err := json.Unmarshal(c.Body(), &b); err != nil {
				return nil, fmt.Errorf("invalid JSON body")
			}
			return b, nil
		}, false))

		// Bulk enrolment — accept { people: [ {...}, {...} ] }, loop and collect.
		// faceapp has no native bulk endpoint, so we just sequence them and
		// report a per-row result. Caller gets one summary response.
		g.Post("/plugins/faceapp/enroll/bulk", func(c *fiber.Ctx) error {
			client, err := requireEnabled(c)
			if err != nil {
				return err
			}
			var body struct {
				People []map[string]any `json:"people"`
			}
			if err := json.Unmarshal(c.Body(), &body); err != nil || len(body.People) == 0 {
				return c.Status(400).JSON(fiber.Map{"error": "people[] required"})
			}
			results := make([]fiber.Map, 0, len(body.People))
			ok := 0
			for i, p := range body.People {
				resp, raw, err := client.do(c.Context(), "POST", "/api/enrollments", p)
				row := fiber.Map{"index": i}
				if name, found := p["name"]; found {
					row["name"] = name
				}
				if emp, found := p["employee_id"]; found {
					row["employeeId"] = emp
				}
				if err != nil {
					row["ok"] = false
					row["error"] = err.Error()
					results = append(results, row)
					continue
				}
				row["status"] = resp.StatusCode
				row["ok"] = resp.StatusCode >= 200 && resp.StatusCode < 300
				if row["ok"] == true {
					ok++
				}
				// Surface a compact summary instead of the full payload
				var parsed struct {
					OK         bool `json:"ok"`
					Enrollment struct {
						PublicID string `json:"public_id"`
						Status   string `json:"status"`
					} `json:"enrollment"`
					Error string `json:"error"`
				}
				if json.Unmarshal(raw, &parsed) == nil {
					if parsed.Enrollment.PublicID != "" {
						row["publicId"] = parsed.Enrollment.PublicID
						row["enrollmentStatus"] = parsed.Enrollment.Status
					}
					if parsed.Error != "" {
						row["error"] = parsed.Error
					}
				}
				results = append(results, row)
			}
			return c.JSON(fiber.Map{
				"ok":      ok == len(body.People),
				"total":   len(body.People),
				"success": ok,
				"failed":  len(body.People) - ok,
				"results": results,
			})
		})

		// Enrolment status lookup
		g.Get("/plugins/faceapp/enrollments/:publicId", func(c *fiber.Ctx) error {
			client, err := requireEnabled(c)
			if err != nil {
				return err
			}
			pid := c.Params("publicId")
			resp, raw, err := client.do(c.Context(), "GET", "/api/enrollments/"+pid, nil)
			if err != nil {
				return c.Status(502).JSON(fiber.Map{"error": err.Error()})
			}
			ct := resp.Header.Get("Content-Type")
			if ct == "" {
				ct = "application/json"
			}
			return c.Status(resp.StatusCode).Type(strings.SplitN(ct, ";", 2)[0]).Send(raw)
		})
	}

	mount(adminGroup)
	mount(v1Group)
}
