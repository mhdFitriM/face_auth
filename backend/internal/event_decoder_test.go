package internal

import (
	"testing"
)

// ── DecodeEvent ───────────────────────────────────────────────────────────────

func TestDecodeEvent_FaceAuthCompleted(t *testing.T) {
	d := DecodeEvent(5, 75)
	if d.Message != "Face Authentication Completed" {
		t.Errorf("unexpected message: %q", d.Message)
	}
	if d.MajorHex != "0x5" {
		t.Errorf("unexpected majorHex: %q", d.MajorHex)
	}
	if d.MinorHex != "0x4b" {
		t.Errorf("unexpected minorHex: %q (expected 0x4b)", d.MinorHex)
	}
	if d.MajorName != "Other Events" {
		t.Errorf("unexpected majorName: %q", d.MajorName)
	}
}

func TestDecodeEvent_FaceAuthFailed(t *testing.T) {
	d := DecodeEvent(5, 76)
	if d.Message != "Face Authentication Failed" {
		t.Errorf("unexpected message: %q", d.Message)
	}
}

func TestDecodeEvent_DoorUnlocked(t *testing.T) {
	d := DecodeEvent(5, 21)
	if d.Message != "Door Unlocked" {
		t.Errorf("expected Door Unlocked, got %q", d.Message)
	}
}

func TestDecodeEvent_WearingMask(t *testing.T) {
	d := DecodeEvent(5, 221) // 0xdd
	if d.Message != "Wearing Mask" {
		t.Errorf("expected Wearing Mask, got %q", d.Message)
	}
}

func TestDecodeEvent_InvalidAngle(t *testing.T) {
	d := DecodeEvent(5, 2063) // 0x80f
	if d.Message != "Invalid Face Recognition Angle" {
		t.Errorf("expected Invalid Face Recognition Angle, got %q", d.Message)
	}
}

func TestDecodeEvent_AlarmEvent(t *testing.T) {
	d := DecodeEvent(1, 1043)
	if d.MajorName != "Alarm Events" {
		t.Errorf("expected Alarm Events, got %q", d.MajorName)
	}
	if d.Message != "Low Face Picture Quality" {
		t.Errorf("unexpected message: %q", d.Message)
	}
}

func TestDecodeEvent_OperationEvent(t *testing.T) {
	d := DecodeEvent(3, 1074)
	if d.MajorName != "Operation Events" {
		t.Errorf("expected Operation Events")
	}
	if d.Message != "Add Person Information" {
		t.Errorf("unexpected message: %q", d.Message)
	}
}

func TestDecodeEvent_UnknownMinor(t *testing.T) {
	d := DecodeEvent(5, 9999)
	if d.Message == "" {
		t.Error("expected a fallback message for unknown minor")
	}
	// Should not panic, should contain "Unknown"
}

func TestDecodeEvent_UnknownMajor(t *testing.T) {
	d := DecodeEvent(99, 1)
	if d.MajorName == "" {
		t.Error("expected non-empty MajorName for unknown major")
	}
}

// hex representation
func TestDecodeEvent_HexFormat(t *testing.T) {
	d := DecodeEvent(5, 75)
	if d.MajorHex != "0x5" {
		t.Errorf("major hex wrong: %q", d.MajorHex)
	}
	if d.MinorHex != "0x4b" {
		t.Errorf("minor hex wrong: %q (75 decimal = 0x4b hex)", d.MinorHex)
	}
}

// ── DecodeISAPIError ──────────────────────────────────────────────────────────

func TestDecodeISAPIError_OK(t *testing.T) {
	d := DecodeISAPIError("ok", 0)
	if d.Category != "success" {
		t.Errorf("expected success, got %q", d.Category)
	}
}

func TestDecodeISAPIError_EmployeeNoAlreadyExist(t *testing.T) {
	d := DecodeISAPIError("employeeNoAlreadyExist", 0)
	if d.Category != "person" {
		t.Errorf("expected person category, got %q", d.Category)
	}
	if d.Message != "Employee No. already exists" {
		t.Errorf("unexpected message: %q", d.Message)
	}
	if d.Suggestion == "" {
		t.Error("expected a suggestion")
	}
}

func TestDecodeISAPIError_NotSupport(t *testing.T) {
	d := DecodeISAPIError("notSupport", 0)
	if d.Category != "device" {
		t.Errorf("expected device category, got %q", d.Category)
	}
}

func TestDecodeISAPIError_BadAuthorization(t *testing.T) {
	d := DecodeISAPIError("badAuthorization", 0)
	if d.Category != "auth" {
		t.Errorf("expected auth category, got %q", d.Category)
	}
}

func TestDecodeISAPIError_LibraryNotExist(t *testing.T) {
	d := DecodeISAPIError("libraryNotExist", 0)
	if d.Category != "face" {
		t.Errorf("expected face category, got %q", d.Category)
	}
	if d.Suggestion == "" {
		t.Error("expected a suggestion for libraryNotExist")
	}
}

func TestDecodeISAPIError_IllegalEmployeeNo(t *testing.T) {
	d := DecodeISAPIError("illegalEmployeeNo", 0)
	if d.Suggestion == "" {
		t.Error("expected a suggestion for illegalEmployeeNo")
	}
}

func TestDecodeISAPIError_FallbackByHexCode(t *testing.T) {
	// 0x60007002 = deviceUserAlreadyExist
	d := DecodeISAPIError("", 0x60007002)
	if d.Category != "person" {
		t.Errorf("expected person category via hex lookup, got %q", d.Category)
	}
}

func TestDecodeISAPIError_Unknown(t *testing.T) {
	d := DecodeISAPIError("somethingUnknown", 0)
	if d.Category != "unknown" {
		t.Errorf("expected unknown category, got %q", d.Category)
	}
}

// ── ISAPIResponseStatus ───────────────────────────────────────────────────────

func TestISAPIResponseStatus_IsOK(t *testing.T) {
	s := ISAPIResponseStatus{StatusCode: 1, StatusString: "OK", SubStatusCode: "ok"}
	if !s.IsOK() {
		t.Fatal("expected IsOK=true for statusCode=1")
	}
}

func TestISAPIResponseStatus_NotOK(t *testing.T) {
	s := ISAPIResponseStatus{StatusCode: 4, SubStatusCode: "deviceUserAlreadyExist"}
	if s.IsOK() {
		t.Fatal("expected IsOK=false for error status")
	}
	d := s.Decode()
	if d.Category != "person" {
		t.Errorf("expected person category, got %q", d.Category)
	}
}

// ── AcsEvent normalization (no device needed) ─────────────────────────────────

func TestNormalizeAcsEvent_Basic(t *testing.T) {
	ev := AcsEventInfo{
		Major:             5,
		Minor:             75,
		Time:              "2026-01-01T08:00:00+08:00",
		EmployeeNoString:  "EMP001",
		Name:              "John Doe",
		DoorNo:            1,
		CurrentVerifyMode: "face",
	}
	n := normalizeAcsEvent(ev)
	if n.Message != "Face Authentication Completed" {
		t.Errorf("unexpected message: %q", n.Message)
	}
	if n.EmployeeNo != "EMP001" {
		t.Errorf("wrong employee no: %q", n.EmployeeNo)
	}
	if n.AuthType != "face" {
		t.Errorf("wrong auth type: %q", n.AuthType)
	}
	if n.Raw == nil {
		t.Error("expected non-nil raw")
	}
}

// ── encode helper ─────────────────────────────────────────────────────────────

func TestEncode(t *testing.T) {
	if encode(5, 75) != (5<<16)|75 {
		t.Error("encode() produced wrong key")
	}
}
