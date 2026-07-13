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
