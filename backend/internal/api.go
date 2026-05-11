package internal

import (
	"bufio"
	"encoding/json"
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
)

func NewAPIServer(store *Store, cfg Config, hub *AgentHub) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             50 * 1024 * 1024,
		AppName:               "face_auth-api",
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:  "*",
		AllowMethods:  "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:  "Content-Type, Authorization",
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

	// ---------- Devices ----------

	api.Get("/devices", func(c *fiber.Ctx) error {
		devs, err := store.ListDevices(c.Context())
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
		}
		dev.SetPassword(body.Password)
		if err := store.RegisterDevice(c.Context(), dev); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
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
		agents, err := store.ListAgents(c.Context())
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
		out, err := store.ListPersons(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(out)
	})
	api.Post("/persons", func(c *fiber.Ctx) error {
		var body Person
		if err := json.Unmarshal(c.Body(), &body); err != nil || body.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name required"})
		}
		if body.ID == "" {
			body.ID = uuid.NewString()
		}
		if body.EmployeeNo == "" {
			// Hik devices require employeeNo to be set; use short version of ID
			body.EmployeeNo = body.ID[:8]
		}
		if err := store.CreatePerson(c.Context(), body); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(body)
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
				"cards":           cardByEmp[u.EmployeeNo],
				"deviceUserType":  u.UserType,
				"userVerifyMode":  u.UserVerifyMode,
				"deviceFaceURL":   faceByEmp[u.EmployeeNo].FaceURL,
				"deviceSyncedAt":  time.Now().UTC(),
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
			"ok":      true,
			"users":   len(users),
			"faces":   len(faces),
			"cards":   len(cards),
			"synced":  synced,
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

		// Step 1: upsert UserInfo on the device with full role/validity
		hikUser := HikUserInfo{
			EmployeeNo:   person.EmployeeNo,
			Name:         person.Name,
			UserType:     person.PersonType,
			Gender:       person.Gender,
			LongTerm:     person.LongTerm,
			DoorRight:    person.DoorRight,
			PlanTemplate: person.PlanTemplate,
			LocalUIRight: person.PersonRole == "administrator",
			CheckUser:    person.AttendanceOnly,
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
			return c.Status(502).JSON(fiber.Map{
				"ok":             false,
				"face":           face,
				"userResponse":   json.RawMessage(safeJSONFromBytes([]byte(userResp))),
				"deviceResponse": json.RawMessage(safeJSONFromBytes([]byte(resp))),
				"error":          err.Error(),
			})
		}
		_ = store.UpdateFaceStatus(ctx, face.ID, "enrolled")
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

	api.Get("/status", func(c *fiber.Ctx) error {
		devices, _ := store.ListDevices(c.Context())
		online := 0
		for _, d := range devices {
			if d.Online {
				online++
			}
		}
		return c.JSON(fiber.Map{
			"ok":            true,
			"devices":       len(devices),
			"devicesOnline": online,
			"time":          time.Now().UTC(),
			"mode":          "isapi",
			"eventCallbackIP": cfg.EventCallbackIP,
			"pushPort":      cfg.PushPort,
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
