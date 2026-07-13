package internal

import (
	"encoding/json"
	"testing"
)

func TestDeviceUserRecordKeepsUnknownHikvisionFields(t *testing.T) {
	input := []byte(`{"employeeNo":"42","name":"Member","userVerifyMode":"face","doorRight":"3","RightPlan":[{"doorNo":2,"planTemplateNo":"7"}],"Valid":{"enable":true,"beginTime":"2026-01-01T00:00:00","endTime":"2027-01-01T00:00:00","timeType":"local"},"customFirmwarePermission":{"level":9}}`)
	var user DeviceUserRecord
	if err := json.Unmarshal(input, &user); err != nil {
		t.Fatal(err)
	}
	if user.EmployeeNo != "42" || user.UserVerifyMode != "face" {
		t.Fatalf("typed fields not decoded: %#v", user)
	}
	if user.Raw["customFirmwarePermission"] == nil {
		t.Fatal("unknown firmware field was not retained")
	}
	copy := cloneUserRecord(user.Raw)
	copy["userVerifyMode"] = nativeVerifyMode
	if user.Raw["userVerifyMode"] != "face" {
		t.Fatal("clone mutation changed original snapshot")
	}
	rightPlan := copy["RightPlan"].([]any)
	plan := rightPlan[0].(map[string]any)
	if plan["planTemplateNo"] != "7" {
		t.Fatalf("door plan changed: %#v", plan)
	}
}
