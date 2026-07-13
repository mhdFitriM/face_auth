package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func validateAccessPointValues(a *AccessPoint) error {
	if a == nil || strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Name) == "" {
		return errors.New("access point id and name required")
	}
	if a.ScannerPort < 1 || a.ScannerPort > 65535 {
		return errors.New("scannerPort must be 1..65535")
	}
	if a.DoorNo < 1 || a.DoorNo > 32 {
		return errors.New("doorNo must be 1..32")
	}
	if a.Direction != "in" && a.Direction != "out" && a.Direction != "bidirectional" {
		return errors.New("direction must be in, out, or bidirectional")
	}
	if a.AuthMode != "faceauth_qr" && a.AuthMode != "hikvision" && a.AuthMode != "disabled" {
		return errors.New("authMode must be faceauth_qr, hikvision, or disabled")
	}
	if a.AcceptedQR != "static" && a.AcceptedQR != "dynamic" && a.AcceptedQR != "both" {
		return errors.New("acceptedQr must be static, dynamic, or both")
	}
	return nil
}

func (s *Store) SaveAccessPoint(ctx context.Context, a AccessPoint) error {
	var outletTenant, outletQRMode, agentTenant, agentOutlet, deviceTenant, deviceOutlet, deviceAccessMode string
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,''), COALESCE(qr_mode,'both') FROM branches WHERE id=$1`, a.OutletID).Scan(&outletTenant, &outletQRMode); err != nil {
		return errors.New("outlet not found")
	}
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,''), COALESCE(branch_id,'') FROM agents WHERE id=$1`, a.AgentID).Scan(&agentTenant, &agentOutlet); err != nil {
		return errors.New("agent not found")
	}
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,''), COALESCE(branch_id,''), COALESCE(access_mode,'normal_qr') FROM devices WHERE device_id=$1`, a.DeviceID).Scan(&deviceTenant, &deviceOutlet, &deviceAccessMode); err != nil {
		return errors.New("device not found")
	}
	a.AuthMode = accessPointAuthModeForDevice(deviceAccessMode)
	if a.AuthMode == "faceauth_qr" {
		a.AcceptedQR = normalizeOutletQRMode(outletQRMode, "both")
	} else {
		a.AcceptedQR = "both"
	}
	if err := validateAccessPointValues(&a); err != nil {
		return err
	}
	if outletTenant == "" || a.TenantID != outletTenant || agentTenant != outletTenant || deviceTenant != outletTenant {
		return errors.New("access point, outlet, agent, and device must belong to the same merchant")
	}
	if agentOutlet != a.OutletID || deviceOutlet != a.OutletID {
		return errors.New("agent and device must belong to the selected outlet")
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO access_points (id,tenant_id,outlet_id,name,agent_id,scanner_id,scanner_port,device_id,door_no,direction,auth_mode,accepted_qr,rejection_sound,enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET name=$4,agent_id=$5,scanner_id=$6,scanner_port=$7,device_id=$8,door_no=$9,direction=$10,auth_mode=$11,accepted_qr=$12,rejection_sound=$13,enabled=$14,updated_at=NOW()
	`, a.ID, a.TenantID, a.OutletID, a.Name, a.AgentID, a.ScannerID, a.ScannerPort, a.DeviceID, a.DoorNo, a.Direction, a.AuthMode, a.AcceptedQR, a.RejectionSound, a.Enabled)
	if err != nil && strings.Contains(err.Error(), "access_points_agent_id_scanner_port") {
		return fmt.Errorf("scanner port %d is already assigned on agent %s", a.ScannerPort, a.AgentID)
	}
	return err
}

const accessPointSelect = `SELECT ap.id,ap.tenant_id,ap.outlet_id,ap.name,ap.agent_id,ap.scanner_id,ap.scanner_port,ap.device_id,ap.door_no,ap.direction,ap.auth_mode,ap.accepted_qr,ap.rejection_sound,ap.enabled,ap.created_at,ap.updated_at,COALESCE(d.online,false) FROM access_points ap LEFT JOIN devices d ON d.device_id=ap.device_id`

func scanAccessPoint(row pgx.Row) (*AccessPoint, error) {
	a := &AccessPoint{}
	err := row.Scan(&a.ID, &a.TenantID, &a.OutletID, &a.Name, &a.AgentID, &a.ScannerID, &a.ScannerPort, &a.DeviceID, &a.DoorNo, &a.Direction, &a.AuthMode, &a.AcceptedQR, &a.RejectionSound, &a.Enabled, &a.CreatedAt, &a.UpdatedAt, &a.DeviceOnline)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (s *Store) GetAccessPoint(ctx context.Context, tenantID, id string) (*AccessPoint, error) {
	return scanAccessPoint(s.PG.QueryRow(ctx, accessPointSelect+` WHERE ap.id=$1 AND ap.tenant_id=$2`, id, tenantID))
}

func (s *Store) ListAccessPoints(ctx context.Context, tenantID, outletID, agentID string) ([]AccessPoint, error) {
	q := accessPointSelect + ` WHERE ap.tenant_id=$1`
	args := []any{tenantID}
	if outletID != "" {
		args = append(args, outletID)
		q += fmt.Sprintf(" AND ap.outlet_id=$%d", len(args))
	}
	if agentID != "" {
		args = append(args, agentID)
		q += fmt.Sprintf(" AND ap.agent_id=$%d", len(args))
	}
	q += ` ORDER BY ap.outlet_id,ap.name`
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessPoint{}
	for rows.Next() {
		a := AccessPoint{}
		if err := rows.Scan(&a.ID, &a.TenantID, &a.OutletID, &a.Name, &a.AgentID, &a.ScannerID, &a.ScannerPort, &a.DeviceID, &a.DoorNo, &a.Direction, &a.AuthMode, &a.AcceptedQR, &a.RejectionSound, &a.Enabled, &a.CreatedAt, &a.UpdatedAt, &a.DeviceOnline); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAccessPoint(ctx context.Context, tenantID, id string) error {
	cmd, err := s.PG.Exec(ctx, `DELETE FROM access_points WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	if err == nil && cmd.RowsAffected() == 0 {
		return errors.New("access point not found")
	}
	return err
}

func (s *Store) GetAgentScope(ctx context.Context, agentID string) (tenantID, outletID string, err error) {
	err = s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,''),COALESCE(branch_id,'') FROM agents WHERE id=$1`, agentID).Scan(&tenantID, &outletID)
	return
}

func (s *Store) SetPersonAccessPoints(ctx context.Context, tenantID, personID string, ids []string) error {
	var personTenant string
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,'') FROM persons WHERE id=$1`, personID).Scan(&personTenant); err != nil || personTenant != tenantID {
		return errors.New("person not found in merchant")
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM person_access_points WHERE person_id=$1`, personID); err != nil {
		return err
	}
	for _, id := range ids {
		var apTenant string
		if err = tx.QueryRow(ctx, `SELECT tenant_id FROM access_points WHERE id=$1`, id).Scan(&apTenant); err != nil || apTenant != tenantID {
			return fmt.Errorf("access point %s not found in merchant", id)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO person_access_points(person_id,access_point_id) VALUES($1,$2)`, personID, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListPersonAccessPoints(ctx context.Context, tenantID, personID string) ([]string, error) {
	rows, err := s.PG.Query(ctx, `SELECT pa.access_point_id FROM person_access_points pa JOIN access_points ap ON ap.id=pa.access_point_id WHERE pa.person_id=$1 AND ap.tenant_id=$2 ORDER BY pa.access_point_id`, personID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) PersonAllowedAtAccessPoint(ctx context.Context, personID, accessPointID string) (bool, error) {
	var total int
	if err := s.PG.QueryRow(ctx, `SELECT COUNT(*) FROM person_access_points WHERE person_id=$1`, personID).Scan(&total); err != nil {
		return false, err
	}
	if total == 0 {
		return true, nil
	}
	var exists bool
	err := s.PG.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM person_access_points WHERE person_id=$1 AND access_point_id=$2)`, personID, accessPointID).Scan(&exists)
	return exists, err
}
