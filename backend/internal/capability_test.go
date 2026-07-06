package internal

import (
	"testing"
)

// ── capBool ───────────────────────────────────────────────────────────────────

func TestCapBool_JSONTrue(t *testing.T) {
	raw := `{"isSupportUserRightPlanTemplate":true,"isSupportFDLib":false}`
	if !capBool(raw, "isSupportUserRightPlanTemplate") {
		t.Fatal("expected true for isSupportUserRightPlanTemplate")
	}
	if capBool(raw, "isSupportFDLib") {
		t.Fatal("expected false for isSupportFDLib")
	}
}

func TestCapBool_JSONSpacedTrue(t *testing.T) {
	raw := `{"isSupportSubscribeEvent": true}`
	if !capBool(raw, "isSupportSubscribeEvent") {
		t.Fatal("expected true with space after colon")
	}
}

func TestCapBool_XMLTrue(t *testing.T) {
	raw := `<AccessControlCap><isSupportRemoteCheck>true</isSupportRemoteCheck></AccessControlCap>`
	if !capBool(raw, "isSupportRemoteCheck") {
		t.Fatal("expected true from XML")
	}
}

func TestCapBool_XMLSupport(t *testing.T) {
	raw := `<cap><get>support</get></cap>`
	if !capBool(raw, "get") {
		t.Fatal("expected true for <get>support</get>")
	}
}

func TestCapBool_CaseInsensitive(t *testing.T) {
	raw := `{"ISSUPPORTREMOTECHECK":true}`
	if !capBool(raw, "isSupportRemoteCheck") {
		t.Fatal("expected case-insensitive match")
	}
}

func TestCapBool_Missing(t *testing.T) {
	if capBool(`{}`, "isSupportAnything") {
		t.Fatal("should be false for missing key")
	}
	if capBool("", "anything") {
		t.Fatal("should be false for empty raw")
	}
}

// ── capFalse ──────────────────────────────────────────────────────────────────

func TestCapFalse_JSONFalse(t *testing.T) {
	raw := `{"isSupportAlertStream":false}`
	if !capFalse(raw, "isSupportAlertStream") {
		t.Fatal("expected false value detected")
	}
}

func TestCapFalse_XMLNotSupport(t *testing.T) {
	raw := `<cap><subscribe>notSupport</subscribe></cap>`
	if !capFalse(raw, "subscribe") {
		t.Fatal("expected false for notSupport XML")
	}
}

// ── isNotSupportResponse ──────────────────────────────────────────────────────

func TestIsNotSupportResponse(t *testing.T) {
	resp := `{"statusCode":3,"subStatusCode":"notSupport","statusString":"Device Error"}`
	if !isNotSupportResponse(resp) {
		t.Fatal("expected notSupport to be detected")
	}
}

func TestIsNotSupportResponse_Normal(t *testing.T) {
	resp := `{"isSupportUserInfoDetailDelete":true}`
	if isNotSupportResponse(resp) {
		t.Fatal("isSupportXxx should not trigger notSupport detection")
	}
}

// ── CapabilityReport.AssertFeature ───────────────────────────────────────────

func TestAssertFeature_Supported(t *testing.T) {
	r := &CapabilityReport{
		Features: map[string]FeatureStatus{
			"personCreate": {Supported: true, Endpoint: "POST /ISAPI/..."},
		},
	}
	if err := r.AssertFeature("personCreate"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestAssertFeature_Unsupported(t *testing.T) {
	r := &CapabilityReport{
		Features: map[string]FeatureStatus{
			"remoteCheck": {Supported: false, Endpoint: "PUT /ISAPI/AccessControl/remoteCheck"},
		},
	}
	err := r.AssertFeature("remoteCheck")
	if err == nil {
		t.Fatal("expected FeatureUnsupportedError, got nil")
	}
	fErr, ok := err.(*FeatureUnsupportedError)
	if !ok {
		t.Fatalf("expected *FeatureUnsupportedError, got %T", err)
	}
	if fErr.Feature != "remoteCheck" {
		t.Errorf("wrong feature name: %q", fErr.Feature)
	}
}

func TestAssertFeature_NotInReport(t *testing.T) {
	r := &CapabilityReport{Features: make(map[string]FeatureStatus)}
	err := r.AssertFeature("unknownFeature")
	if err == nil {
		t.Fatal("expected error for unknown feature")
	}
}
