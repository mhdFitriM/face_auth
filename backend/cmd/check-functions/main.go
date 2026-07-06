// check-functions — CLI tool that connects to a Hikvision Face Recognition
// Terminal and reports which ISAPI features are available on the device.
//
// Usage:
//   go run ./cmd/check-functions
//   # or after build:
//   ./check-functions
//
// Environment variables (loaded from .env in the repo root if present):
//   HIKVISION_HOST             device IP or hostname
//   HIKVISION_PORT             port (default 443 for HTTPS, 80 for HTTP)
//   HIKVISION_PROTOCOL         http or https (default https)
//   HIKVISION_USERNAME         ISAPI username (default admin)
//   HIKVISION_PASSWORD         ISAPI password
//   HIKVISION_REJECT_UNAUTHORIZED  false to skip TLS cert check (default false)
//
// Output:
//   - Feature table to stdout
//   - Raw capability JSON files to ./debug/

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"face_auth/internal"
)

func main() {
	loadDotEnv()

	host := envOr("HIKVISION_HOST", "192.168.1.64")
	portStr := envOr("HIKVISION_PORT", "")
	protocol := strings.ToLower(envOr("HIKVISION_PROTOCOL", "https"))
	username := envOr("HIKVISION_USERNAME", "admin")
	password := envOr("HIKVISION_PASSWORD", "")
	rejectUnauth := strings.ToLower(envOr("HIKVISION_REJECT_UNAUTHORIZED", "false")) == "true"

	if password == "" {
		fatalf("HIKVISION_PASSWORD is not set. Set it in the environment or .env file.")
	}

	useHTTPS := protocol == "https"
	port := 0
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	_ = rejectUnauth // ISAPIClient always allows self-signed certs by default

	fmt.Printf("\n=== Hikvision Device Function Checker ===\n")
	fmt.Printf("Target: %s://%s\n", protocol, host)
	fmt.Printf("Time:   %s\n\n", time.Now().Format(time.RFC3339))

	client := internal.NewISAPIClient(host, port, useHTTPS, username, password)

	fmt.Println("Probing device capabilities — this may take 15–30 seconds...")
	start := time.Now()
	report, err := client.CheckDeviceFunctionSupport()
	elapsed := time.Since(start)

	if err != nil {
		if report != nil && !report.Device.Online {
			fatalf("Device unreachable at %s://%s — %v", protocol, host, err)
		}
	}

	fmt.Printf("Done in %s\n\n", elapsed.Round(time.Millisecond))

	// ── Device summary ────────────────────────────────────────────────────────
	fmt.Println("Device")
	fmt.Println("------")
	fmt.Printf("  Online:      %s\n", yesNo(report.Device.Online))
	fmt.Printf("  Activated:   %s\n", yesNo(report.Device.Activated))
	if report.Device.Model != "" {
		fmt.Printf("  Model:       %s\n", report.Device.Model)
	}
	if report.Device.FirmwareVersion != "" {
		fmt.Printf("  Firmware:    %s\n", report.Device.FirmwareVersion)
	}
	if report.Device.SerialNumber != "" {
		fmt.Printf("  Serial:      %s\n", report.Device.SerialNumber)
	}
	if report.Device.DeviceName != "" {
		fmt.Printf("  Name:        %s\n", report.Device.DeviceName)
	}
	fmt.Println()

	if !report.Device.Activated {
		fmt.Println("⚠  Device is not activated. Activate via PUT /ISAPI/System/activate before using any features.")
		os.Exit(1)
	}

	// ── Feature table ─────────────────────────────────────────────────────────
	type row struct {
		name     string
		key      string
		endpoint string
	}
	rows := []row{
		{"Device Info", "deviceInfo", "GET /ISAPI/System/deviceInfo"},
		{"Activation Status", "activationStatus", "GET /SDK/activateStatus"},
		{"AccessControl Capabilities", "accessControlCapabilities", "GET /ISAPI/AccessControl/capabilities"},
		{"Person Search", "personSearch", "POST /ISAPI/AccessControl/UserInfo/Search"},
		{"Person Create", "personCreate", "POST /ISAPI/AccessControl/UserInfo/Record"},
		{"Person Modify", "personModify", "PUT /ISAPI/AccessControl/UserInfo/Modify"},
		{"Person SetUp (upsert)", "personSetUp", "PUT /ISAPI/AccessControl/UserInfo/SetUp"},
		{"Person Delete", "personDelete", "PUT /ISAPI/AccessControl/UserInfoDetail/Delete"},
		{"Person Count", "personCount", "GET /ISAPI/AccessControl/UserInfo/Count"},
		{"Face Library Get", "faceLibraryGet", "GET /ISAPI/Intelligent/FDLib"},
		{"Face Library Create", "faceLibraryCreate", "POST /ISAPI/Intelligent/FDLib"},
		{"Face Library Delete", "faceLibraryDelete", "DELETE /ISAPI/Intelligent/FDLib"},
		{"Face Upload", "faceUpload", "POST /ISAPI/Intelligent/FDLib/FaceDataRecord"},
		{"Face Modify", "faceModify", "PUT /ISAPI/Intelligent/FDLib/FDModify"},
		{"Face Search", "faceSearch", "POST /ISAPI/Intelligent/FDLib/FDSearch"},
		{"Face Delete", "faceDelete", "PUT /ISAPI/Intelligent/FDLib/FDSearch/Delete"},
		{"Face SetUp (upsert)", "faceSetUp", "PUT /ISAPI/Intelligent/FDLib/FDSetUp"},
		{"Face Recognize Mode Get", "faceRecognizeModeGet", "GET /ISAPI/AccessControl/FaceRecognizeMode"},
		{"Face Recognize Mode Set", "faceRecognizeModeSet", "PUT /ISAPI/AccessControl/FaceRecognizeMode"},
		{"Permission Schedule", "permissionSchedule", "UserRightPlanTemplate / UserRightWeekPlanCfg"},
		{"Week Plan", "userRightWeekPlan", "UserRightWeekPlanCfg"},
		{"Holiday Group", "userRightHolidayGroup", "UserRightHolidayGroupCfg"},
		{"Holiday Plan", "userRightHolidayPlan", "UserRightHolidayPlanCfg"},
		{"Access Event Search", "accessEventSearch", "POST /ISAPI/AccessControl/AcsEvent"},
		{"Access Event Total Num", "accessEventTotalNum", "POST /ISAPI/AccessControl/AcsEventTotalNum"},
		{"Event Storage Config", "accessEventStorageConfig", "GET /ISAPI/AccessControl/AcsEvent/StorageCfg"},
		{"Real-time Alert Stream", "realTimeAlertStream", "GET /ISAPI/Event/notification/alertStream"},
		{"Event Subscription", "eventSubscription", "POST /ISAPI/Event/notification/subscribeEvent"},
		{"HTTP Listening Host", "httpListeningHost", "/ISAPI/Event/notification/httpHosts"},
		{"Remote Check", "remoteCheck", "PUT /ISAPI/AccessControl/remoteCheck"},
	}

	colW := 30
	epW := 48
	fmt.Printf("%-*s  %-10s  %s\n", colW, "Feature", "Supported", "Endpoint")
	fmt.Printf("%s  %s  %s\n", strings.Repeat("-", colW), "----------", strings.Repeat("-", epW))

	yesCount, noCount := 0, 0
	for _, r := range rows {
		fs, ok := report.Features[r.key]
		supported := ok && fs.Supported
		sup := "NO "
		if supported {
			sup = "YES"
			yesCount++
		} else {
			noCount++
		}
		endpoint := r.endpoint
		if ok && fs.Endpoint != "" {
			endpoint = fs.Endpoint
		}
		if len(endpoint) > epW {
			endpoint = endpoint[:epW-3] + "..."
		}
		fmt.Printf("%-*s  %-10s  %s\n", colW, r.name, sup, endpoint)
		if !supported && ok && fs.Reason != "" {
			fmt.Printf("  %*s  ↳ %s\n", colW, "", fs.Reason)
		}
	}
	fmt.Println()
	fmt.Printf("Summary: %d supported, %d not supported\n", yesCount, noCount)

	// ── Warnings ──────────────────────────────────────────────────────────────
	if len(report.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range report.Warnings {
			fmt.Printf("  ⚠  %s\n", w)
		}
	}

	// ── Write debug files ─────────────────────────────────────────────────────
	debugDir := findDebugDir()
	if debugDir != "" {
		writeDebug(debugDir, "device-info.json", report.RawCaps.DeviceInfo)
		writeDebug(debugDir, "access-control-capabilities.json", report.RawCaps.AccessControlCaps)
		writeDebug(debugDir, "user-info-capabilities.json", report.RawCaps.UserInfoCaps)
		writeDebug(debugDir, "fdlib-capabilities.json", report.RawCaps.FDLibCaps)
		writeDebug(debugDir, "face-recognize-mode-capabilities.json", report.RawCaps.FaceRecognizeModeCaps)
		writeDebug(debugDir, "acs-event-capabilities.json", report.RawCaps.AcsEventCaps)
		writeDebug(debugDir, "system-capabilities.json", report.RawCaps.SystemCaps)
		writeDebug(debugDir, "http-host-capabilities.json", report.RawCaps.HttpHostCaps)

		// Write full report JSON
		reportJSON, _ := json.MarshalIndent(report, "", "  ")
		writeDebug(debugDir, "function-report.json", string(reportJSON))
		fmt.Printf("\nDebug files written to: %s\n", debugDir)
	}

	fmt.Println()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func yesNo(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// findDebugDir returns the debug output directory, creating it if needed.
// Searches up from the executable's location to find the repo root.
func findDebugDir() string {
	// Try a few common locations
	candidates := []string{
		"debug",
		"../../debug",
		"../../../debug",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		parent := filepath.Dir(abs)
		if _, err := os.Stat(parent); err == nil {
			_ = os.MkdirAll(abs, 0755)
			return abs
		}
	}
	// Fallback: create in current directory
	_ = os.MkdirAll("debug", 0755)
	return "debug"
}

func writeDebug(dir, filename, content string) {
	if content == "" {
		return
	}
	path := filepath.Join(dir, filename)
	// Try to pretty-print JSON
	var raw interface{}
	if json.Unmarshal([]byte(content), &raw) == nil {
		if pretty, err := json.MarshalIndent(raw, "", "  "); err == nil {
			content = string(pretty)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: could not write %s: %v\n", path, err)
	}
}

// loadDotEnv reads KEY=VALUE pairs from a .env file in the current or parent
// directories. Does not override already-set environment variables.
func loadDotEnv() {
	candidates := []string{".env", "../.env", "../../.env"}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			// Strip inline comments
			if idx := strings.Index(v, " #"); idx > 0 {
				v = strings.TrimSpace(v[:idx])
			}
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
		break
	}
}
