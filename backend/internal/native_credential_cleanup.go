package internal

import (
	"context"
	"errors"
	"fmt"
)

type NativeCredentialUser struct {
	EmployeeNo string `json:"employeeNo"`
	Name       string `json:"name"`
}

type NativeCredentialCleanupPreview struct {
	DeviceID string                 `json:"deviceId"`
	Mode     string                 `json:"accessMode"`
	Users    []NativeCredentialUser `json:"users"`
	Faces    int                    `json:"faces"`
	Cards    int                    `json:"cards"`
}

func previewNativeCredentialCleanup(ctx context.Context, store *Store, hub *AgentHub, tenantID, deviceID string) (*NativeCredentialCleanupPreview, *Device, error) {
	deviceTenant, err := store.GetDeviceTenant(ctx, deviceID)
	if err != nil || deviceTenant != tenantID {
		return nil, nil, errors.New("device not found in merchant")
	}
	d, err := store.GetDevice(ctx, deviceID)
	if err != nil || d == nil {
		return nil, nil, errors.New("device not found")
	}
	if d.AccessMode != DeviceAccessModeNormalQR {
		return nil, nil, errors.New("cleanup is only available for Normal QR relay-only devices")
	}
	client := NewISAPIClientForDevice(d, hub)
	users, err := client.ListUsers()
	if err != nil {
		return nil, nil, err
	}
	faces, err := client.ListFacesOnDevice(d.FDID, d.FaceLibType)
	if err != nil {
		return nil, nil, err
	}
	cards, err := client.ListCards()
	if err != nil {
		return nil, nil, err
	}
	out := &NativeCredentialCleanupPreview{DeviceID: deviceID, Mode: d.AccessMode, Users: []NativeCredentialUser{}, Faces: len(faces), Cards: len(cards)}
	for _, u := range users {
		out.Users = append(out.Users, NativeCredentialUser{EmployeeNo: u.EmployeeNo, Name: u.Name})
	}
	return out, d, nil
}

func cleanupNativeCredentials(ctx context.Context, store *Store, hub *AgentHub, tenantID, deviceID string) (map[string]any, error) {
	preview, d, err := previewNativeCredentialCleanup(ctx, store, hub, tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	client := NewISAPIClientForDevice(d, hub)
	users, err := client.ListUsers()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if len(u.Raw) == 0 || employeeNoOf(u.Raw) == "" {
			return nil, fmt.Errorf("incomplete user record for %s", u.EmployeeNo)
		}
		if err := store.SaveDeviceUserModeSnapshot(ctx, deviceID, u.EmployeeNo, u.Raw); err != nil {
			return nil, err
		}
	}
	faces, _ := client.ListFacesOnDevice(d.FDID, d.FaceLibType)
	cards, _ := client.ListCards()
	errs := []string{}
	for _, card := range cards {
		if _, e := client.DeleteCard(card.CardNo); e != nil {
			errs = append(errs, "card "+card.CardNo+": "+e.Error())
		}
	}
	for _, face := range faces {
		if _, e := client.DeleteFace(d.FDID, d.FaceLibType, face.FPID); e != nil {
			errs = append(errs, "face "+face.FPID+": "+e.Error())
		}
	}
	for _, u := range users {
		if _, e := client.DeleteUserOnDevice(u.EmployeeNo); e != nil {
			errs = append(errs, "user "+u.EmployeeNo+": "+e.Error())
		}
	}
	remainingUsers, usersErr := client.ListUsers()
	remainingFaces, facesErr := client.ListFacesOnDevice(d.FDID, d.FaceLibType)
	remainingCards, cardsErr := client.ListCards()
	if usersErr != nil || facesErr != nil || cardsErr != nil {
		return nil, fmt.Errorf("cleanup read-back failed: users=%v faces=%v cards=%v", usersErr, facesErr, cardsErr)
	}
	if len(errs) > 0 || len(remainingUsers) > 0 || len(remainingFaces) > 0 || len(remainingCards) > 0 {
		return map[string]any{"ok": false, "deviceId": deviceID, "errors": errs, "remainingUsers": len(remainingUsers), "remainingFaces": len(remainingFaces), "remainingCards": len(remainingCards)}, errors.New("terminal credential cleanup incomplete")
	}
	// Keep cloud images and card numbers as master data, but remove the claim
	// that they are currently enrolled on this relay-only terminal.
	_, _ = store.PG.Exec(ctx, `UPDATE faces SET status='cloud_only' WHERE device_id=$1`, deviceID)
	_, _ = store.PG.Exec(ctx, `UPDATE cards SET device_id='' WHERE device_id=$1`, deviceID)
	return map[string]any{"ok": true, "deviceId": deviceID, "usersDeleted": len(preview.Users), "facesDeleted": preview.Faces, "cardsDeleted": preview.Cards, "cloudDataPreserved": true}, nil
}
