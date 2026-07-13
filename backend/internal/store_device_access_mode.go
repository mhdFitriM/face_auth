package internal

import (
	"context"
	"encoding/json"
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

func requireHikvisionNative(d *Device) error {
	if d == nil || d.AccessMode != DeviceAccessModeHikvisionNative {
		return errors.New("device is Normal QR relay-only; member credentials stay in FaceAuth cloud")
	}
	return nil
}

func isNativeCredentialISAPIPath(path string) bool {
	p := strings.ToLower(path)
	return strings.Contains(p, "/accesscontrol/userinfo") ||
		strings.Contains(p, "/accesscontrol/cardinfo") ||
		strings.Contains(p, "/accesscontrol/fingerprint") ||
		strings.Contains(p, "/intelligent/fdlib/facedatarecord") ||
		strings.Contains(p, "/intelligent/fdlib/fdsearch/delete") ||
		strings.Contains(p, "/accesscontrol/qrcodeinfo")
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

	result, err := tx.Exec(ctx, `UPDATE devices SET access_mode=$1, mode_apply_status='applied', mode_apply_error='', mode_applied_at=NOW() WHERE device_id=$2 AND tenant_id=$3`, mode, deviceID, tenantID)
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

func (s *Store) SetDeviceModeApplyState(ctx context.Context, tenantID, deviceID, status, message string) error {
	result, err := s.PG.Exec(ctx, `UPDATE devices SET mode_apply_status=$1, mode_apply_error=$2 WHERE device_id=$3 AND tenant_id=$4`, status, message, deviceID, tenantID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("device not found in merchant")
	}
	return nil
}

// SaveDeviceUserModeSnapshot preserves the first complete record seen for a
// user. ON CONFLICT intentionally does nothing: later mode changes must never
// replace the original schedules and permissions with an already-locked copy.
func (s *Store) SaveDeviceUserModeSnapshot(ctx context.Context, deviceID, employeeNo string, snapshot map[string]any) error {
	b, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = s.PG.Exec(ctx, `INSERT INTO device_user_mode_snapshots(device_id,employee_no,snapshot) VALUES($1,$2,$3::jsonb) ON CONFLICT(device_id,employee_no) DO NOTHING`, deviceID, employeeNo, b)
	return err
}

func (s *Store) GetDeviceUserModeSnapshots(ctx context.Context, deviceID string) (map[string]map[string]any, error) {
	rows, err := s.PG.Query(ctx, `SELECT employee_no,snapshot FROM device_user_mode_snapshots WHERE device_id=$1`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var employeeNo string
		var b []byte
		if err := rows.Scan(&employeeNo, &b); err != nil {
			return nil, err
		}
		var snapshot map[string]any
		if err := json.Unmarshal(b, &snapshot); err != nil {
			return nil, err
		}
		out[employeeNo] = snapshot
	}
	return out, rows.Err()
}
