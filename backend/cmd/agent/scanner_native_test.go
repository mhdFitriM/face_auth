package main

import "testing"

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
