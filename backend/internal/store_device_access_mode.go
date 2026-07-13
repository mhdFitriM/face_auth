package internal

import (
	"context"
	"errors"
	"strings"
)

const (
	DeviceAccessModeNormalQR        = "normal_qr"
	DeviceAccessModeHikvisionNative = "hikvision_native"
)

func normalizeDeviceAccessMode(mode string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DeviceAccessModeNormalQR:
		return DeviceAccessModeNormalQR, nil
	case DeviceAccessModeHikvisionNative:
		return DeviceAccessModeHikvisionNative, nil
	default:
		return "", errors.New("accessMode must be normal_qr or hikvision_native")
	}
}

func accessPointAuthModeForDevice(mode string) string {
	if mode == DeviceAccessModeHikvisionNative {
		return "hikvision"
	}
	return "faceauth_qr"
}

// SetDeviceAccessMode changes the authoritative credential path and updates
// every scanner station targeting the device in the same transaction. It does
// not enable/disable stations or open a door.
func (s *Store) SetDeviceAccessMode(ctx context.Context, tenantID, deviceID, mode string) (int64, error) {
	mode, err := normalizeDeviceAccessMode(mode)
	if err != nil {
		return 0, err
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `UPDATE devices SET access_mode=$1 WHERE device_id=$2 AND tenant_id=$3`, mode, deviceID, tenantID)
	if err != nil {
		return 0, err
	}
	if result.RowsAffected() == 0 {
		return 0, errors.New("device not found in merchant")
	}

	authMode := accessPointAuthModeForDevice(mode)
	result, err = tx.Exec(ctx, `
		UPDATE access_points ap
		SET auth_mode=$1,
		    accepted_qr=CASE WHEN $1='faceauth_qr'
		        THEN COALESCE(NULLIF(b.qr_mode,''),'static') ELSE 'both' END,
		    updated_at=NOW()
		FROM branches b
		WHERE ap.device_id=$2 AND ap.tenant_id=$3 AND b.id=ap.outlet_id
	`, authMode, deviceID, tenantID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
