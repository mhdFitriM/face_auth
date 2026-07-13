package internal

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) SaveDeviceGroup(ctx context.Context, tenantID string, g DeviceGroup, deviceIDs []string) (*DeviceGroup, error) {
	if tenantID == "" || strings.TrimSpace(g.OutletID) == "" || strings.TrimSpace(g.Name) == "" {
		return nil, errors.New("outlet and group name are required")
	}
	var outletTenant string
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,'') FROM branches WHERE id=$1`, g.OutletID).Scan(&outletTenant); err != nil || outletTenant != tenantID {
		return nil, errors.New("outlet not found in merchant")
	}
	if len(deviceIDs) == 0 {
		return nil, errors.New("select at least one device")
	}
	seen := map[string]bool{}
	cleanIDs := make([]string, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var deviceTenant, outletID string
		if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,''),COALESCE(branch_id,'') FROM devices WHERE device_id=$1`, id).Scan(&deviceTenant, &outletID); err != nil || deviceTenant != tenantID || outletID != g.OutletID {
			return nil, errors.New("every grouped device must belong to the selected merchant outlet")
		}
		cleanIDs = append(cleanIDs, id)
	}
	if len(cleanIDs) == 0 {
		return nil, errors.New("select at least one valid device")
	}
	if g.ID == "" {
		g.ID = "dg-" + RandomString(12, charset+hexCharset)
	} else {
		var existingTenant, existingOutlet string
		if err := s.PG.QueryRow(ctx, `SELECT tenant_id,outlet_id FROM device_groups WHERE id=$1`, g.ID).Scan(&existingTenant, &existingOutlet); err != nil || existingTenant != tenantID || existingOutlet != g.OutletID {
			return nil, errors.New("device group not found in merchant outlet")
		}
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO device_groups(id,tenant_id,outlet_id,name)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(id) DO UPDATE SET name=$4,updated_at=NOW()
	`, g.ID, tenantID, g.OutletID, strings.TrimSpace(g.Name))
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM device_group_members WHERE group_id=$1`, g.ID); err != nil {
		return nil, err
	}
	for _, id := range cleanIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO device_group_members(group_id,device_id) VALUES($1,$2)`, g.ID, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetDeviceGroup(ctx, tenantID, g.ID)
}

func (s *Store) GetDeviceGroup(ctx context.Context, tenantID, id string) (*DeviceGroup, error) {
	g := &DeviceGroup{}
	err := s.PG.QueryRow(ctx, `
		SELECT id,tenant_id,outlet_id,name,created_at,updated_at,
		       (SELECT COUNT(*) FROM device_group_members WHERE group_id=device_groups.id)
		FROM device_groups WHERE id=$1 AND tenant_id=$2
	`, id, tenantID).Scan(&g.ID, &g.TenantID, &g.OutletID, &g.Name, &g.CreatedAt, &g.UpdatedAt, &g.DeviceCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.Devices, err = s.ListDeviceGroupDevices(ctx, tenantID, id)
	return g, err
}

func (s *Store) ListDeviceGroups(ctx context.Context, tenantID, outletID string) ([]DeviceGroup, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT id FROM device_groups
		WHERE tenant_id=$1 AND ($2='' OR outlet_id=$2)
		ORDER BY name,id
	`, tenantID, outletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]DeviceGroup, 0, len(ids))
	for _, id := range ids {
		g, err := s.GetDeviceGroup(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		if g != nil {
			out = append(out, *g)
		}
	}
	return out, rows.Err()
}

func (s *Store) ListDeviceGroupDevices(ctx context.Context, tenantID, groupID string) ([]Device, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT gm.device_id FROM device_group_members gm
		JOIN device_groups g ON g.id=gm.group_id
		WHERE gm.group_id=$1 AND g.tenant_id=$2 ORDER BY gm.created_at,gm.device_id
	`, groupID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]Device, 0, len(ids))
	for _, id := range ids {
		d, err := s.GetDevice(ctx, id)
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, *d)
		}
	}
	return out, rows.Err()
}

func (s *Store) DeleteDeviceGroup(ctx context.Context, tenantID, id string) error {
	result, err := s.PG.Exec(ctx, `DELETE FROM device_groups WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("device group not found")
	}
	return nil
}
