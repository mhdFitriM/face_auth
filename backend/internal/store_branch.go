package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ---------- Branch ops ----------
//
// A branch is one physical site inside a tenant. Invariants:
//   - a branch may own multiple agents
//   - devices attached to a branch route ISAPI through that branch's agent
//     (resolved in the device SELECTs via effectiveAgentSQL)

// effectiveAgentSQL resolves the agent a device's commands route through.
// A device in a branch routes ONLY via that branch's agent — never another
// branch's, even if a stale direct agent_id points there. If the branch has
// no agent (yet), the device simply has no agent route. Devices outside any
// branch keep the legacy direct agent_id link.
const effectiveAgentSQL = `CASE
    WHEN devices.branch_id IS NOT NULL AND devices.branch_id <> '' THEN
        COALESCE(NULLIF(devices.agent_id,''),(SELECT ba.id FROM agents ba WHERE ba.branch_id = devices.branch_id ORDER BY ba.created_at LIMIT 1), '')
    ELSE COALESCE(NULLIF(devices.agent_id,''), '')
    END`

func (s *Store) CreateBranch(ctx context.Context, b Branch) error {
	if strings.TrimSpace(b.ID) == "" {
		return errors.New("branch id required")
	}
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("branch name required")
	}
	b.QRMode = normalizeOutletQRMode(b.QRMode, "static")
	_, err := s.PG.Exec(ctx, `
		INSERT INTO branches (id, tenant_id, name, address, qr_mode)
		VALUES ($1, NULLIF($2,''), $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET name=$3, address=$4, qr_mode=$5
	`, b.ID, b.TenantID, b.Name, b.Address, b.QRMode)
	return err
}

func normalizeOutletQRMode(mode, fallback string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "static" || mode == "dynamic" || mode == "both" {
		return mode
	}
	return fallback
}

func (s *Store) UpdateBranch(ctx context.Context, tenantID, id, name, address, qrMode string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("branch name required")
	}
	qrMode = normalizeOutletQRMode(qrMode, "static")
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE branches SET name=$1, address=$2, qr_mode=$3 WHERE id=$4 AND tenant_id=$5`, name, address, qrMode, id, tenantID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("outlet not found in merchant")
	}
	// Outlet policy is authoritative for every FaceAuth access point. Hikvision
	// native/card points remain untouched because they do not consume FaceAuth QR.
	if _, err = tx.Exec(ctx, `UPDATE access_points SET accepted_qr=$1, updated_at=NOW() WHERE outlet_id=$2 AND tenant_id=$3 AND auth_mode='faceauth_qr'`, qrMode, id, tenantID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetBranch(ctx context.Context, id string) (*Branch, error) {
	row := s.PG.QueryRow(ctx, `
		SELECT b.id, COALESCE(b.tenant_id,''), b.name, COALESCE(b.address,''), COALESCE(b.qr_mode,'both'), b.created_at,
		       COALESCE((SELECT a.id FROM agents a WHERE a.branch_id=b.id ORDER BY a.created_at LIMIT 1),''),
		       COALESCE((SELECT a.name FROM agents a WHERE a.branch_id=b.id ORDER BY a.created_at LIMIT 1),''),
		       (SELECT COUNT(*) FROM agents a WHERE a.branch_id=b.id),
		       (SELECT COUNT(*) FROM devices d WHERE d.branch_id = b.id)
		FROM branches b
		WHERE b.id=$1`, id)
	b := &Branch{}
	err := row.Scan(&b.ID, &b.TenantID, &b.Name, &b.Address, &b.QRMode, &b.CreatedAt, &b.AgentID, &b.AgentName, &b.AgentCount, &b.DeviceCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

func (s *Store) ListBranchesByTenant(ctx context.Context, tenantID string) ([]Branch, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT b.id, COALESCE(b.tenant_id,''), b.name, COALESCE(b.address,''), COALESCE(b.qr_mode,'both'), b.created_at,
		       COALESCE((SELECT a.id FROM agents a WHERE a.branch_id=b.id ORDER BY a.created_at LIMIT 1),''),
		       COALESCE((SELECT a.name FROM agents a WHERE a.branch_id=b.id ORDER BY a.created_at LIMIT 1),''),
		       (SELECT COUNT(*) FROM agents a WHERE a.branch_id=b.id),
		       (SELECT COUNT(*) FROM devices d WHERE d.branch_id = b.id)
		FROM branches b
		WHERE b.tenant_id=$1
		ORDER BY b.created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Branch{}
	for rows.Next() {
		var b Branch
		if err := rows.Scan(&b.ID, &b.TenantID, &b.Name, &b.Address, &b.QRMode, &b.CreatedAt, &b.AgentID, &b.AgentName, &b.AgentCount, &b.DeviceCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBranch removes the branch and detaches (not deletes) its agent and
// devices — they keep working via their legacy direct agent_id link.
func (s *Store) DeleteBranch(ctx context.Context, id string) error {
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE agents SET branch_id=NULL WHERE branch_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET branch_id=NULL WHERE branch_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM branches WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AssignAgentToBranch(ctx context.Context, branchID, agentID string) error {
	var branchTenant, agentTenant string
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,'') FROM branches WHERE id=$1`, branchID).Scan(&branchTenant); err != nil {
		return errors.New("outlet not found")
	}
	if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,'') FROM agents WHERE id=$1`, agentID).Scan(&agentTenant); err != nil {
		return errors.New("agent not found")
	}
	if branchTenant == "" || agentTenant != branchTenant {
		return errors.New("agent and outlet must belong to the same merchant")
	}
	var current *string
	if err := s.PG.QueryRow(ctx, `SELECT branch_id FROM agents WHERE id=$1`, agentID).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("agent %q not found", agentID)
		}
		return err
	}
	if current != nil && *current != "" && *current != branchID {
		return fmt.Errorf("agent is already assigned to branch %q — unassign it first", *current)
	}
	_, err := s.PG.Exec(ctx, `UPDATE agents SET branch_id=$1 WHERE id=$2`, branchID, agentID)
	return err
}

func (s *Store) UnassignAgent(ctx context.Context, branchID, agentID string) error {
	_, err := s.PG.Exec(ctx, `UPDATE agents SET branch_id=NULL WHERE branch_id=$1 AND id=$2`, branchID, agentID)
	return err
}

func (s *Store) UnassignAgentFromBranch(ctx context.Context, branchID string) error {
	_, err := s.PG.Exec(ctx, `UPDATE agents SET branch_id=NULL WHERE branch_id=$1`, branchID)
	return err
}

// SetDeviceBranch attaches a device to a branch ("" detaches it).
func (s *Store) SetDeviceBranch(ctx context.Context, deviceID, branchID string) error {
	if branchID != "" {
		var branchTenant, deviceTenant string
		if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,'') FROM branches WHERE id=$1`, branchID).Scan(&branchTenant); err != nil {
			return errors.New("outlet not found")
		}
		if err := s.PG.QueryRow(ctx, `SELECT COALESCE(tenant_id,'') FROM devices WHERE device_id=$1`, deviceID).Scan(&deviceTenant); err != nil {
			return errors.New("device not found")
		}
		if branchTenant == "" || deviceTenant != branchTenant {
			return errors.New("device and outlet must belong to the same merchant")
		}
	}
	_, err := s.PG.Exec(ctx, `UPDATE devices SET branch_id=NULLIF($1,'') WHERE device_id=$2`, branchID, deviceID)
	return err
}

// GetBranchTenant returns the tenant_id for a branch, or "" if not set.
func (s *Store) GetBranchTenant(ctx context.Context, branchID string) (string, error) {
	var tid *string
	err := s.PG.QueryRow(ctx, `SELECT tenant_id FROM branches WHERE id=$1`, branchID).Scan(&tid)
	if err != nil || tid == nil {
		return "", err
	}
	return *tid, nil
}
