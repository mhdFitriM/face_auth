package internal

// capability.go — Hikvision ISAPI device function availability checker.
//
// Call ISAPIClient.CheckDeviceFunctionSupport() to get a CapabilityReport
// describing which features the physical device supports on its current
// firmware. Use CapabilityReport.AssertFeature(name) before calling any
// mutation endpoint.
//
// All probes use only safe (non-mutating) capability/GET endpoints. No
// create/update/delete calls are made.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FeatureStatus describes one feature's availability on the device.
type FeatureStatus struct {
	Supported bool   `json:"supported"`
	Endpoint  string `json:"endpoint"`
	Source    string `json:"source,omitempty"`
	Method    string `json:"method,omitempty"` // e.g. "multipart/form-data"
	Reason    string `json:"reason,omitempty"` // why unsupported
	Status    string `json:"status,omitempty"` // "ok", "error", etc.
}

// DeviceCapStatus is the device identity/health section of the report.
type DeviceCapStatus struct {
	Online          bool   `json:"online"`
	Activated       bool   `json:"activated"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmwareVersion"`
	SerialNumber    string `json:"serialNumber"`
	DeviceName      string `json:"deviceName"`
}

// CapabilityReport is the full output from CheckDeviceFunctionSupport.
type CapabilityReport struct {
	Device   DeviceCapStatus          `json:"device"`
	Features map[string]FeatureStatus `json:"features"`
	Warnings []string                 `json:"warnings"`
	// RawCaps holds raw response bodies for debug file dumps. Not serialized.
	RawCaps RawCapabilities `json:"-"`
}

// RawCapabilities holds raw capability response bodies for debug dumps.
type RawCapabilities struct {
	DeviceInfo            string
	AccessControlCaps     string
	UserInfoCaps          string
	FDLibCaps             string
	FaceRecognizeModeCaps string
	AcsEventCaps          string
	SystemCaps            string
	HttpHostCaps          string
}

// FeatureUnsupportedError is returned by AssertFeature when the named
// feature is not available on the current device/firmware.
type FeatureUnsupportedError struct {
	Feature    string `json:"feature"`
	Endpoint   string `json:"endpoint"`
	Suggestion string `json:"suggestion"`
}

func (e *FeatureUnsupportedError) Error() string {
	return fmt.Sprintf("FEATURE_UNSUPPORTED: %q is not supported on this device/firmware — endpoint: %s",
		e.Feature, e.Endpoint)
}

// AssertFeature returns a FeatureUnsupportedError if the named feature is
// not marked as supported. Call before every device mutation API.
func (r *CapabilityReport) AssertFeature(feature string) error {
	f, ok := r.Features[feature]
	if !ok || !f.Supported {
		ep := ""
		if ok {
			ep = f.Endpoint
		}
		return &FeatureUnsupportedError{
			Feature:    feature,
			Endpoint:   ep,
			Suggestion: "Check firmware version or use device web UI / iVMS-4200 to confirm support.",
		}
	}
	return nil
}

// GetActivationStatus checks /SDK/activateStatus — no authentication required.
// Returns (activated, rawResponseBody, error).
func (c *ISAPIClient) GetActivationStatus() (bool, string, error) {
	resp, body, err := c.Do("GET", "/SDK/activateStatus", "", nil)
	raw := string(body)
	if err != nil {
		return false, raw, err
	}
	if resp.StatusCode != 200 {
		return false, raw, fmt.Errorf("activateStatus: HTTP %d", resp.StatusCode)
	}

	// Try JSON first
	var j map[string]interface{}
	if json.Unmarshal(body, &j) == nil {
		for _, key := range []string{"activated", "isActivated", "Activated"} {
			if v, ok := j[key]; ok {
				switch vt := v.(type) {
				case bool:
					return vt, raw, nil
				case string:
					return strings.EqualFold(vt, "true") || vt == "1", raw, nil
				}
			}
		}
	}

	// XML / plain-string fallback
	low := strings.ToLower(raw)
	if strings.Contains(low, "<activated>true</activated>") ||
		strings.Contains(low, `"activated":true`) ||
		strings.Contains(low, `"activated": true`) {
		return true, raw, nil
	}
	if strings.Contains(low, "notactivated") ||
		strings.Contains(low, "not_activated") ||
		strings.Contains(low, `"activated":false`) ||
		strings.Contains(low, `"activated": false`) {
		return false, raw, nil
	}
	// 200 with no explicit indicator — assume activated
	return true, raw, nil
}

// CheckDeviceFunctionSupport connects to the device and probes every major
// feature using only safe read/capability endpoints. No mutation endpoints
// are called. Returns a CapabilityReport describing which features are
// available on the device's current firmware.
func (c *ISAPIClient) CheckDeviceFunctionSupport() (*CapabilityReport, error) {
	report := &CapabilityReport{
		Features: make(map[string]FeatureStatus),
	}

	// ── Step 1: Activation check (no auth) ──────────────────────────────────
	activated, activationRaw, activationErr := c.GetActivationStatus()
	report.RawCaps.DeviceInfo = activationRaw
	if activationErr != nil {
		report.Device.Online = false
		report.Features["activationStatus"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /SDK/activateStatus",
			Reason:    activationErr.Error(),
		}
		return report, fmt.Errorf("device unreachable: %w", activationErr)
	}
	report.Device.Online = true
	report.Device.Activated = activated
	report.Features["activationStatus"] = FeatureStatus{
		Supported: true,
		Endpoint:  "GET /SDK/activateStatus",
		Status:    "ok",
	}
	if !activated {
		report.Warnings = append(report.Warnings,
			"Device is not activated. Activate via PUT /ISAPI/System/activate before using any features.")
		return report, nil
	}

	// ── Step 2: Device info (requires auth) ─────────────────────────────────
	info, err := c.GetDeviceInfo()
	if err == nil && info != nil {
		report.Device.Model = info.Model
		report.Device.FirmwareVersion = info.FirmwareVersion
		report.Device.SerialNumber = info.SerialNumber
		report.Device.DeviceName = info.DeviceName
		report.Features["deviceInfo"] = FeatureStatus{
			Supported: true,
			Endpoint:  "GET /ISAPI/System/deviceInfo",
			Status:    "ok",
		}
	} else {
		report.Features["deviceInfo"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /ISAPI/System/deviceInfo",
			Reason:    safeErrStr(err),
		}
	}

	// ── Steps 3–10: Capability probes ────────────────────────────────────────
	acCapsRaw := c.probeAccessControlCaps(report)
	report.RawCaps.AccessControlCaps = acCapsRaw
	c.probeUserInfoCaps(report)
	c.probeFDLibCaps(report)
	c.probeFaceRecognizeCaps(report)
	c.probeAcsEventCaps(report)
	c.probeSystemCaps(report)
	c.probeHttpHostCaps(report)
	c.probeRemoteCheckCaps(report, acCapsRaw)

	// ── Cross-feature warnings ────────────────────────────────────────────────
	if fs := report.Features["remoteCheck"]; !fs.Supported {
		report.Warnings = append(report.Warnings,
			"Do not enable remoteCheck unless the device confirms isSupportRemoteCheck=true")
	}
	if fs := report.Features["faceUpload"]; !fs.Supported {
		report.Warnings = append(report.Warnings,
			"Do not upload face images until FDLib capability confirms face record POST support")
	}
	if fs := report.Features["permissionSchedule"]; !fs.Supported {
		report.Warnings = append(report.Warnings,
			"Do not assign permissions until access control right plan capability is confirmed")
	}

	return report, nil
}

// ── Private probe helpers ─────────────────────────────────────────────────────

func (c *ISAPIClient) probeAccessControlCaps(report *CapabilityReport) string {
	resp, body, err := c.Do("GET", "/ISAPI/AccessControl/capabilities", "", nil)
	raw := string(body)
	if err != nil || resp.StatusCode != 200 {
		reason := httpErrReason(resp, err)
		report.Features["accessControlCapabilities"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /ISAPI/AccessControl/capabilities",
			Reason:    reason,
		}
		for _, f := range []string{"permissionSchedule", "userRightPlanTemplate",
			"userRightWeekPlan", "userRightHolidayGroup", "userRightHolidayPlan"} {
			report.Features[f] = FeatureStatus{
				Supported: false,
				Endpoint:  "AccessControl capabilities unavailable",
				Reason:    reason,
			}
		}
		return raw
	}
	report.Features["accessControlCapabilities"] = FeatureStatus{
		Supported: true,
		Endpoint:  "GET /ISAPI/AccessControl/capabilities",
		Status:    "ok",
	}

	hasWeekPlan := capBool(raw, "isSupportUserRightPlanTemplate") || capBool(raw, "isSupportCardRightWeekPlanCfg")
	if hasWeekPlan {
		report.Features["permissionSchedule"] = FeatureStatus{
			Supported: true,
			Endpoint:  "UserRightPlanTemplate / UserRightWeekPlanCfg",
			Source:    "AccessControl capabilities isSupportUserRightPlanTemplate",
		}
	} else {
		report.Features["permissionSchedule"] = FeatureStatus{
			Supported: false,
			Endpoint:  "UserRightPlanTemplate / UserRightWeekPlanCfg",
			Reason:    "isSupportUserRightPlanTemplate not found or false in AccessControl capabilities",
		}
	}
	report.Features["userRightPlanTemplate"] = FeatureStatus{
		Supported: capBool(raw, "isSupportUserRightPlanTemplate"),
		Endpoint:  "GET /ISAPI/AccessControl/UserRightPlanTemplate/capabilities?format=json",
		Source:    "AccessControl capabilities",
	}
	report.Features["userRightWeekPlan"] = FeatureStatus{
		Supported: capBool(raw, "isSupportCardRightWeekPlanCfg"),
		Endpoint:  "GET /ISAPI/AccessControl/UserRightWeekPlanCfg/capabilities?format=json",
		Source:    "AccessControl capabilities",
	}
	report.Features["userRightHolidayGroup"] = FeatureStatus{
		Supported: capBool(raw, "isSupportUserRightHolidayGroupCfg"),
		Endpoint:  "GET /ISAPI/AccessControl/UserRightHolidayGroupCfg/capabilities?format=json",
		Source:    "AccessControl capabilities",
	}
	report.Features["userRightHolidayPlan"] = FeatureStatus{
		Supported: capBool(raw, "isSupportCardRightHolidayPlanCfg"),
		Endpoint:  "GET /ISAPI/AccessControl/UserRightHolidayPlanCfg/capabilities?format=json",
		Source:    "AccessControl capabilities",
	}
	return raw
}

func (c *ISAPIClient) probeUserInfoCaps(report *CapabilityReport) {
	resp, body, err := c.Do("GET", "/ISAPI/AccessControl/UserInfo/capabilities?format=json", "", nil)
	raw := string(body)
	report.RawCaps.UserInfoCaps = raw
	if err != nil || resp.StatusCode != 200 {
		reason := "UserInfo capabilities unavailable: " + httpErrReason(resp, err)
		for _, f := range []string{"personSearch", "personCreate", "personModify",
			"personSetUp", "personDelete", "personCount"} {
			report.Features[f] = FeatureStatus{
				Supported: false,
				Endpoint:  userInfoEndpoint(f),
				Reason:    reason,
			}
		}
		return
	}
	// Any mention of supportFunction in a 200 response is sufficient evidence
	// that the endpoint family is available. Hikvision firmware varies in how
	// it describes individual method support.
	hasSupportFunc := strings.Contains(raw, "supportFunction") || strings.Contains(raw, "SupportFunction")

	report.Features["personSearch"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "get"),
		Endpoint:  "POST /ISAPI/AccessControl/UserInfo/Search?format=json",
		Source:    "UserInfo capabilities supportFunction contains get",
	}
	report.Features["personCreate"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "post") || capBool(raw, "add"),
		Endpoint:  "POST /ISAPI/AccessControl/UserInfo/Record?format=json",
		Source:    "UserInfo capabilities supportFunction contains post",
	}
	report.Features["personModify"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "put") || capBool(raw, "modify"),
		Endpoint:  "PUT /ISAPI/AccessControl/UserInfo/Modify?format=json",
		Source:    "UserInfo capabilities supportFunction contains put",
	}
	report.Features["personSetUp"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "setUp") || capBool(raw, "setup"),
		Endpoint:  "PUT /ISAPI/AccessControl/UserInfo/SetUp?format=json",
		Source:    "UserInfo capabilities supportFunction contains setUp",
	}
	report.Features["personDelete"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "delete") || capBool(raw, "isSupportUserInfoDetailDelete"),
		Endpoint:  "PUT /ISAPI/AccessControl/UserInfoDetail/Delete?format=json",
		Source:    "UserInfo capabilities",
	}
	report.Features["personCount"] = FeatureStatus{
		Supported: true, // 200 from capabilities implies count is supported
		Endpoint:  "GET /ISAPI/AccessControl/UserInfo/Count?format=json",
		Source:    "UserInfo capabilities 200 response",
	}
}

func (c *ISAPIClient) probeFDLibCaps(report *CapabilityReport) {
	resp, body, err := c.Do("GET", "/ISAPI/Intelligent/FDLib/capabilities?format=json", "", nil)
	raw := string(body)
	report.RawCaps.FDLibCaps = raw
	if err != nil || resp.StatusCode != 200 {
		reason := "FDLib capabilities unavailable: " + httpErrReason(resp, err)
		for _, f := range []string{"faceLibraryGet", "faceLibraryCreate", "faceLibraryDelete",
			"faceUpload", "faceModify", "faceSearch", "faceDelete", "faceSetUp"} {
			report.Features[f] = FeatureStatus{
				Supported: false,
				Endpoint:  fdlibEndpoint(f),
				Reason:    reason,
			}
		}
		return
	}
	hasSupportFunc := strings.Contains(raw, "supportFunction") ||
		strings.Contains(raw, "supportFDFunction") ||
		strings.Contains(raw, "SupportFunction") ||
		strings.Contains(raw, "SupportFDFunction")

	rawLow := strings.ToLower(raw)
	fmtStr := "jpg"
	if strings.Contains(rawLow, "\"jpg\"") || strings.Contains(rawLow, "\"jpeg\"") {
		fmtStr = "jpg, jpeg"
	}

	report.Features["faceLibraryGet"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "get"),
		Endpoint:  "GET /ISAPI/Intelligent/FDLib?format=json",
		Source:    "FDLib capabilities",
	}
	report.Features["faceLibraryCreate"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "post") || capBool(raw, "add"),
		Endpoint:  "POST /ISAPI/Intelligent/FDLib?format=json",
		Source:    "FDLib capabilities",
	}
	report.Features["faceLibraryDelete"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "delete"),
		Endpoint:  "DELETE /ISAPI/Intelligent/FDLib?format=json",
		Source:    "FDLib capabilities",
	}
	report.Features["faceUpload"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "post"),
		Endpoint:  "POST /ISAPI/Intelligent/FDLib/FaceDataRecord?format=json",
		Method:    "multipart/form-data",
		Source:    fmt.Sprintf("FDLib capabilities; face formats: %s", fmtStr),
	}
	report.Features["faceModify"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "put") || capBool(raw, "modify"),
		Endpoint:  "PUT /ISAPI/Intelligent/FDLib/FDModify?format=json",
		Source:    "FDLib capabilities",
	}
	report.Features["faceSearch"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "search"),
		Endpoint:  "POST /ISAPI/Intelligent/FDLib/FDSearch?format=json",
		Source:    "FDLib capabilities",
	}
	report.Features["faceDelete"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "delete"),
		Endpoint:  "PUT /ISAPI/Intelligent/FDLib/FDSearch/Delete?format=json",
		Source:    "FDLib capabilities",
	}
	report.Features["faceSetUp"] = FeatureStatus{
		Supported: hasSupportFunc || capBool(raw, "setUp") || capBool(raw, "setup"),
		Endpoint:  "PUT /ISAPI/Intelligent/FDLib/FDSetUp?format=json",
		Source:    "FDLib capabilities",
	}
}

func (c *ISAPIClient) probeFaceRecognizeCaps(report *CapabilityReport) {
	resp, body, err := c.Do("GET", "/ISAPI/AccessControl/FaceRecognizeMode/capabilities?format=json", "", nil)
	raw := string(body)
	report.RawCaps.FaceRecognizeModeCaps = raw
	ok := err == nil && resp.StatusCode == 200
	if ok {
		report.Features["faceRecognizeModeGet"] = FeatureStatus{
			Supported: true,
			Endpoint:  "GET /ISAPI/AccessControl/FaceRecognizeMode?format=json",
			Source:    "FaceRecognizeMode capabilities",
		}
		hasPut := capBool(raw, "put") ||
			strings.Contains(raw, "supportFunction") || strings.Contains(raw, "SupportFunction")
		report.Features["faceRecognizeModeSet"] = FeatureStatus{
			Supported: hasPut,
			Endpoint:  "PUT /ISAPI/AccessControl/FaceRecognizeMode?format=json",
			Source:    "FaceRecognizeMode capabilities",
		}
	} else {
		reason := httpErrReason(resp, err)
		report.Features["faceRecognizeModeGet"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /ISAPI/AccessControl/FaceRecognizeMode?format=json",
			Reason:    reason,
		}
		report.Features["faceRecognizeModeSet"] = FeatureStatus{
			Supported: false,
			Endpoint:  "PUT /ISAPI/AccessControl/FaceRecognizeMode?format=json",
			Reason:    reason,
		}
	}
}

func (c *ISAPIClient) probeAcsEventCaps(report *CapabilityReport) {
	resp, body, err := c.Do("GET", "/ISAPI/AccessControl/AcsEvent/capabilities?format=json", "", nil)
	raw := string(body)
	report.RawCaps.AcsEventCaps = raw
	if err != nil || resp.StatusCode != 200 {
		report.Features["accessEventSearch"] = FeatureStatus{
			Supported: false,
			Endpoint:  "POST /ISAPI/AccessControl/AcsEvent?format=json",
			Reason:    httpErrReason(resp, err),
		}
	} else {
		report.Features["accessEventSearch"] = FeatureStatus{
			Supported: true,
			Endpoint:  "POST /ISAPI/AccessControl/AcsEvent?format=json",
			Source:    "AcsEvent capabilities",
		}
	}

	resp2, _, err2 := c.Do("GET", "/ISAPI/AccessControl/AcsEventTotalNum/capabilities?format=json", "", nil)
	if err2 == nil && resp2.StatusCode == 200 {
		report.Features["accessEventTotalNum"] = FeatureStatus{
			Supported: true,
			Endpoint:  "POST /ISAPI/AccessControl/AcsEventTotalNum?format=json",
			Source:    "AcsEventTotalNum capabilities",
		}
	} else {
		report.Features["accessEventTotalNum"] = FeatureStatus{
			Supported: false,
			Endpoint:  "POST /ISAPI/AccessControl/AcsEventTotalNum?format=json",
			Reason:    httpErrReason(resp2, err2),
		}
	}

	resp3, _, err3 := c.Do("GET", "/ISAPI/AccessControl/AcsEvent/StorageCfg/capabilities?format=json", "", nil)
	if err3 == nil && resp3.StatusCode == 200 {
		report.Features["accessEventStorageConfig"] = FeatureStatus{
			Supported: true,
			Endpoint:  "GET /ISAPI/AccessControl/AcsEvent/StorageCfg?format=json",
			Source:    "AcsEvent StorageCfg capabilities",
		}
	} else {
		report.Features["accessEventStorageConfig"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /ISAPI/AccessControl/AcsEvent/StorageCfg?format=json",
			Reason:    httpErrReason(resp3, err3),
		}
	}
}

func (c *ISAPIClient) probeSystemCaps(report *CapabilityReport) {
	resp, body, err := c.Do("GET", "/ISAPI/System/capabilities", "", nil)
	raw := string(body)
	report.RawCaps.SystemCaps = raw
	ok := err == nil && resp.StatusCode == 200

	if !ok {
		reason := httpErrReason(resp, err)
		report.Features["realTimeAlertStream"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /ISAPI/Event/notification/alertStream",
			Reason:    "System capabilities unavailable: " + reason,
		}
		report.Features["eventSubscription"] = FeatureStatus{
			Supported: false,
			Endpoint:  "POST /ISAPI/Event/notification/subscribeEvent",
			Reason:    reason,
		}
		return
	}

	subscribeSupported := capBool(raw, "isSupportSubscribeEvent")
	// Alert stream: mark unsupported only when explicitly false
	alertExplicitlyFalse := capFalse(raw, "isSupportAlertStream")
	report.Features["realTimeAlertStream"] = FeatureStatus{
		Supported: !alertExplicitlyFalse,
		Endpoint:  "GET /ISAPI/Event/notification/alertStream",
		Source:    "System capabilities",
	}
	if alertExplicitlyFalse {
		report.Features["realTimeAlertStream"] = FeatureStatus{
			Supported: false,
			Endpoint:  "GET /ISAPI/Event/notification/alertStream",
			Reason:    "Device capability did not confirm subscribe/alert stream support",
		}
	}
	if subscribeSupported {
		report.Features["eventSubscription"] = FeatureStatus{
			Supported: true,
			Endpoint:  "POST /ISAPI/Event/notification/subscribeEvent",
			Source:    "System capabilities isSupportSubscribeEvent=true",
		}
	} else {
		report.Features["eventSubscription"] = FeatureStatus{
			Supported: false,
			Endpoint:  "POST /ISAPI/Event/notification/subscribeEvent",
			Reason:    "isSupportSubscribeEvent is false or not present in System capabilities",
		}
	}
}

func (c *ISAPIClient) probeHttpHostCaps(report *CapabilityReport) {
	resp, body, err := c.Do("GET", "/ISAPI/Event/notification/httpHosts/capabilities", "", nil)
	raw := string(body)
	report.RawCaps.HttpHostCaps = raw
	ok := err == nil && resp.StatusCode == 200 && !isNotSupportResponse(raw)
	if ok {
		report.Features["httpListeningHost"] = FeatureStatus{
			Supported: true,
			Endpoint:  "/ISAPI/Event/notification/httpHosts",
			Source:    "httpHosts capabilities",
		}
	} else {
		report.Features["httpListeningHost"] = FeatureStatus{
			Supported: false,
			Endpoint:  "/ISAPI/Event/notification/httpHosts",
			Reason:    "Capability endpoint unavailable or returned notSupport",
		}
	}
}

func (c *ISAPIClient) probeRemoteCheckCaps(report *CapabilityReport, acCapsRaw string) {
	supported := capBool(acCapsRaw, "isSupportRemoteCheck")
	if !supported {
		resp2, _, err2 := c.Do("GET", "/ISAPI/AccessControl/remoteCheck/capabilities?format=json", "", nil)
		if err2 == nil && resp2.StatusCode == 200 {
			supported = true
		}
	}
	if supported {
		report.Features["remoteCheck"] = FeatureStatus{
			Supported: true,
			Endpoint:  "PUT /ISAPI/AccessControl/remoteCheck?format=json",
			Source:    "isSupportRemoteCheck=true in AccessControl capabilities",
		}
	} else {
		report.Features["remoteCheck"] = FeatureStatus{
			Supported: false,
			Endpoint:  "PUT /ISAPI/AccessControl/remoteCheck?format=json",
			Reason:    "isSupportRemoteCheck is false or missing",
		}
	}
}

// ── Utility helpers ───────────────────────────────────────────────────────────

// capBool returns true if raw contains `"key":true` or XML `<key>true</key>`.
// Case-insensitive on both key and value.
func capBool(raw, key string) bool {
	if raw == "" {
		return false
	}
	lkey := strings.ToLower(key)
	low := strings.ToLower(raw)
	return strings.Contains(low, `"`+lkey+`":true`) ||
		strings.Contains(low, `"`+lkey+`": true`) ||
		strings.Contains(low, "<"+lkey+">true</"+lkey+">") ||
		strings.Contains(low, "<"+lkey+">support</"+lkey+">")
}

// capFalse returns true if raw explicitly sets key to false/notSupport.
func capFalse(raw, key string) bool {
	if raw == "" {
		return false
	}
	lkey := strings.ToLower(key)
	low := strings.ToLower(raw)
	return strings.Contains(low, `"`+lkey+`":false`) ||
		strings.Contains(low, `"`+lkey+`": false`) ||
		strings.Contains(low, "<"+lkey+">false</"+lkey+">") ||
		strings.Contains(low, "<"+lkey+">notsupport</"+lkey+">")
}

// isNotSupportResponse returns true if raw looks like a "not supported" body.
func isNotSupportResponse(raw string) bool {
	low := strings.ToLower(raw)
	return strings.Contains(low, `"substatuscode":"notsupport"`) ||
		(strings.Contains(low, "notsupport") && !strings.Contains(low, "issupport"))
}

// httpErrReason returns a short reason string from an HTTP response/error pair.
func httpErrReason(resp *http.Response, err error) string {
	if err != nil {
		return err.Error()
	}
	if resp != nil && resp.StatusCode != 200 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return ""
}

func safeErrStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func userInfoEndpoint(feature string) string {
	switch feature {
	case "personSearch":
		return "POST /ISAPI/AccessControl/UserInfo/Search?format=json"
	case "personCreate":
		return "POST /ISAPI/AccessControl/UserInfo/Record?format=json"
	case "personModify":
		return "PUT /ISAPI/AccessControl/UserInfo/Modify?format=json"
	case "personSetUp":
		return "PUT /ISAPI/AccessControl/UserInfo/SetUp?format=json"
	case "personDelete":
		return "PUT /ISAPI/AccessControl/UserInfoDetail/Delete?format=json"
	case "personCount":
		return "GET /ISAPI/AccessControl/UserInfo/Count?format=json"
	}
	return "GET /ISAPI/AccessControl/UserInfo/capabilities?format=json"
}

func fdlibEndpoint(feature string) string {
	switch feature {
	case "faceLibraryGet":
		return "GET /ISAPI/Intelligent/FDLib?format=json"
	case "faceLibraryCreate":
		return "POST /ISAPI/Intelligent/FDLib?format=json"
	case "faceLibraryDelete":
		return "DELETE /ISAPI/Intelligent/FDLib?format=json"
	case "faceUpload":
		return "POST /ISAPI/Intelligent/FDLib/FaceDataRecord?format=json"
	case "faceModify":
		return "PUT /ISAPI/Intelligent/FDLib/FDModify?format=json"
	case "faceSearch":
		return "POST /ISAPI/Intelligent/FDLib/FDSearch?format=json"
	case "faceDelete":
		return "PUT /ISAPI/Intelligent/FDLib/FDSearch/Delete?format=json"
	case "faceSetUp":
		return "PUT /ISAPI/Intelligent/FDLib/FDSetUp?format=json"
	}
	return "GET /ISAPI/Intelligent/FDLib/capabilities?format=json"
}
