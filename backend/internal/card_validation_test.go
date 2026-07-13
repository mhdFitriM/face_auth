package internal

import "testing"

func TestValidateHikvisionCardNo(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "numeric card", input: "1234567890", want: "1234567890"},
		{name: "capability maximum", input: "12345678901234567890", want: "12345678901234567890"},
		{name: "trim surrounding space", input: " 9999001 ", want: "9999001"},
		{name: "empty", input: "", wantErr: true},
		{name: "too long", input: "123456789012345678901", wantErr: true},
		{name: "control byte", input: "123\n456", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateHikvisionCardNo(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
