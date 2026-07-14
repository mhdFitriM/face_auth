package main

import "testing"

func TestScannerDeviceKeyGroupsCompositeInterfaces(t *testing.T) {
	pos := `\\?\hid#vid_1eab&pid_1d22&mi_01#7&38940cf4&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	raw := `\\?\HID#VID_1EAB&PID_1D22&MI_00&Col01#7&38940cf4&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	otherPort := `\\?\HID#VID_1EAB&PID_1D22&MI_00#8&12345678&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	if scannerDeviceKey(pos) != scannerDeviceKey(raw) {
		t.Fatalf("composite interfaces were not grouped: %q != %q", scannerDeviceKey(pos), scannerDeviceKey(raw))
	}
	if scannerDeviceKey(pos) == scannerDeviceKey(otherPort) {
		t.Fatal("different USB instances must remain isolated")
	}
}

func TestScannerModelKey(t *testing.T) {
	pos := `\\?\hid#vid_1eab&pid_1d22&mi_01#7&38940cf4&0&0000#{guid}`
	rawDifferentInstance := `\\?\HID#VID_1EAB&PID_1D22&MI_00#8&12345678&0&0000#{guid}`
	if got := scannerModelKey(pos); got != "1EAB:1D22" {
		t.Fatalf("scannerModelKey() = %q", got)
	}
	if scannerModelKey(pos) != scannerModelKey(rawDifferentInstance) {
		t.Fatal("sibling interfaces should share a model key")
	}
	if scannerModelKey(`\\?\HID#SYSTEM_KEYBOARD`) != "" {
		t.Fatal("path without VID/PID must not have a model key")
	}
}

func TestDecodePOSReportPart(t *testing.T) {
	tests := []struct {
		name      string
		report    []byte
		want      string
		continued bool
	}{
		{"vendor fallback", []byte{1, 'X', 0, 0, 'Q', 'R', '-', '1', '2', '3', 0}, "QR-123", false},
		{"reject short noise", []byte{1, 'O', 'K', 0}, "", false},
	}
	standard := make([]byte, 55)
	standard[0], standard[1], standard[2], standard[3] = 2, ']', 'Q', '3'
	copy(standard[4:], "in#ABC-123")
	tests = append(tests, struct {
		name      string
		report    []byte
		want      string
		continued bool
	}{"standard scanned data", standard, "in#ABC-123", false})
	continued := append([]byte(nil), standard...)
	continued[54] = 1
	tests = append(tests, struct {
		name      string
		report    []byte
		want      string
		continued bool
	}{"standard continued", continued, "in#ABC-123", true})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, more, ok := decodePOSReportPart(tt.report)
			if got != tt.want || more != tt.continued || ok != (tt.want != "") {
				t.Fatalf("got (%q,%v,%v) want (%q,%v,%v)", got, more, ok, tt.want, tt.continued, tt.want != "")
			}
		})
	}
}
