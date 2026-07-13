package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type DeviceReadiness struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Online   bool   `json:"online"`
	AgentID  string `json:"agentId,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Model    string `json:"model,omitempty"`
}

type GroupFaceResult struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	FaceID   string `json:"faceId,omitempty"`
}

func checkFaceDeviceReadiness(ctx context.Context, d Device, hub *AgentHub) DeviceReadiness {
	r := DeviceReadiness{DeviceID: d.DeviceID, Name: d.Name, AgentID: d.AgentID}
	if err := requireHikvisionNative(&d); err != nil {
		r.Reason = err.Error()
		return r
	}
	if d.IP == "" || d.ISAPIUsername == "" {
		r.Reason = "missing device IP or ISAPI credentials"
		return r
	}
	if d.AgentID != "" && !hub.IsOnline(d.AgentID) {
		r.Reason = "assigned LAN agent is offline"
		return r
	}
	client := NewISAPIClientForDevice(&d, hub)
	info, err := client.GetDeviceInfo()
	if err != nil {
		r.Reason = "device probe failed: " + err.Error()
		return r
	}
	r.Online = true
	if info != nil {
		r.Model = info.Model
	}
	resp, _, err := client.Do("GET", "/ISAPI/Intelligent/FDLib/capabilities?format=json", "", nil)
	if err != nil {
		r.Reason = "face library capability check failed: " + err.Error()
		return r
	}
	if resp.StatusCode != 200 {
		r.Reason = fmt.Sprintf("face enrolment capability unavailable (status %d)", resp.StatusCode)
		return r
	}
	r.Ready = true
	return r
}

func enrollPersonFaceOnDevice(ctx context.Context, store *Store, hub *AgentHub, qrAuth *QRAuth, d Device, person *Person, jpeg []byte, imageKey string) GroupFaceResult {
	r := GroupFaceResult{DeviceID: d.DeviceID, Name: d.Name, Status: "failed"}
	if err := requireHikvisionNative(&d); err != nil {
		r.Error = err.Error()
		return r
	}
	client := NewISAPIClientForDevice(&d, hub)
	hikUser := HikUserInfo{
		EmployeeNo:     person.EmployeeNo,
		Name:           person.Name,
		UserType:       person.PersonType,
		Gender:         person.Gender,
		LongTerm:       person.LongTerm,
		DoorRight:      person.DoorRight,
		PlanTemplate:   person.PlanTemplate,
		LocalUIRight:   person.PersonRole == "administrator",
		CheckUser:      person.AttendanceOnly,
		UserVerifyMode: qrAuth.ModeForDevice(ctx, d.DeviceID),
	}
	if person.ValidBegin != nil {
		hikUser.ValidBegin = person.ValidBegin.Format("2006-01-02T15:04:05")
	}
	if person.ValidEnd != nil {
		hikUser.ValidEnd = person.ValidEnd.Format("2006-01-02T15:04:05")
	}
	if _, err := client.UpsertUserOnDevice(hikUser); err != nil {
		r.Error = "person upsert failed: " + err.Error()
		return r
	}
	face := Face{ID: uuid.NewString(), PersonID: person.ID, DeviceID: d.DeviceID, ImageKey: imageKey, Status: "pending", CreatedAt: time.Now()}
	if err := store.CreateFace(ctx, face); err != nil {
		r.Error = "save enrolment record: " + err.Error()
		return r
	}
	r.FaceID = face.ID
	fdid := firstNonEmpty(d.FDID, "1")
	faceLibType := firstNonEmpty(d.FaceLibType, "blackFD")
	if _, err := client.EnrolFace(fdid, faceLibType, person.EmployeeNo, person.Name, jpeg); err != nil {
		_ = store.UpdateFaceStatus(ctx, face.ID, "failed")
		r.Error = err.Error()
		return r
	}
	_ = store.UpdateFaceStatus(ctx, face.ID, "enrolled")
	r.OK = true
	r.Status = "enrolled"
	return r
}
