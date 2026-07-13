package internal

import "testing"

func TestNormalizeDeviceAccessMode(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"normal_qr", DeviceAccessModeNormalQR, true},
		{" HIKVISION_NATIVE ", DeviceAccessModeHikvisionNative, true},
		{"faceauth_qr", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, err := normalizeDeviceAccessMode(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Fatalf("normalizeDeviceAccessMode(%q) = %q, %v; want %q, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestAccessPointAuthModeForDevice(t *testing.T) {
	if got := accessPointAuthModeForDevice(DeviceAccessModeNormalQR); got != "faceauth_qr" {
		t.Fatalf("normal QR mapped to %q", got)
	}
	if got := accessPointAuthModeForDevice(DeviceAccessModeHikvisionNative); got != "hikvision" {
		t.Fatalf("native mapped to %q", got)
	}
}

func TestNormalQRRejectsNativeCredentials(t *testing.T) {
	if err := requireHikvisionNative(&Device{AccessMode: DeviceAccessModeNormalQR}); err == nil {
		t.Fatal("Normal QR accepted native credentials")
	}
	if err := requireHikvisionNative(&Device{AccessMode: DeviceAccessModeHikvisionNative}); err != nil {
		t.Fatalf("native mode rejected: %v", err)
	}
	blocked := []string{
		"/ISAPI/AccessControl/UserInfo/Modify?format=json",
		"/ISAPI/AccessControl/CardInfo/Record?format=json",
		"/ISAPI/AccessControl/FingerPrint/SetUp?format=json",
		"/ISAPI/Intelligent/FDLib/FaceDataRecord?format=json",
		"/ISAPI/AccessControl/QRCodeInfo/Record?format=json",
	}
	for _, path := range blocked {
		if !isNativeCredentialISAPIPath(path) {
			t.Errorf("credential path was not blocked: %s", path)
		}
	}
	if isNativeCredentialISAPIPath("/ISAPI/AccessControl/RemoteControl/door/1") {
		t.Fatal("OpenDoor was incorrectly classified as a credential write")
	}
}
