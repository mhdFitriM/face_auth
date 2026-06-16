package internal

// Tenant-scoped query helpers — wrappers that add a tenant_id WHERE clause to
// the existing single-tenant queries. The original `List*` methods are kept
// for internal use (event fanout, plug-ins) but new HTTP handlers go through
// these instead.

import "context"

func (s *Store) ListDevicesByTenant(ctx context.Context, tenantID string) ([]Device, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT device_id, name, username, digest_type, is_auth,
		       ip, port, use_https, isapi_username, fdid, face_lib_type,
		       online, last_seen, model, firmware, COALESCE(agent_id,''), created_at
		FROM devices
		WHERE tenant_id=$1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.DeviceID, &d.Name, &d.Username, &d.DigestType, &d.IsAuth,
			&d.IP, &d.Port, &d.UseHTTPS, &d.ISAPIUsername, &d.FDID, &d.FaceLibType,
			&d.Online, &d.LastSeen, &d.Model, &d.Firmware, &d.AgentID, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if out == nil {
		out = []Device{}
	}
	return out, rows.Err()
}

func (s *Store) ListPersonsByTenant(ctx context.Context, tenantID string) ([]Person, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT id, name, employee_no, gender, person_type, person_role,
		       long_term, attendance_only, door_right, plan_template,
		       valid_begin, valid_end, metadata, COALESCE(qr_token,''), created_at
		FROM persons
		WHERE tenant_id=$1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name, &p.EmployeeNo, &p.Gender, &p.PersonType, &p.PersonRole,
			&p.LongTerm, &p.AttendanceOnly, &p.DoorRight, &p.PlanTemplate,
			&p.ValidBegin, &p.ValidEnd, &p.Metadata, &p.QRToken, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []Person{}
	}
	return out, rows.Err()
}

func (s *Store) ListAgentsByTenant(ctx context.Context, tenantID string) ([]Agent, error) {
	rows, err := s.PG.Query(ctx, `SELECT id, name, created_at FROM agents WHERE tenant_id=$1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetDeviceTenant tags a device with a tenant id. Called after RegisterDevice.
func (s *Store) SetDeviceTenant(ctx context.Context, deviceID, tenantID string) error {
	_, err := s.PG.Exec(ctx, `UPDATE devices SET tenant_id=$1 WHERE device_id=$2`, tenantID, deviceID)
	return err
}

// SetPersonTenant tags a person with a tenant id.
func (s *Store) SetPersonTenant(ctx context.Context, personID, tenantID string) error {
	_, err := s.PG.Exec(ctx, `UPDATE persons SET tenant_id=$1 WHERE id=$2`, tenantID, personID)
	return err
}

func (s *Store) SetAgentTenant(ctx context.Context, agentID, tenantID string) error {
	_, err := s.PG.Exec(ctx, `UPDATE agents SET tenant_id=$1 WHERE id=$2`, tenantID, agentID)
	return err
}

// GetDeviceTenant returns the tenant_id for a device, or "" if not set.
func (s *Store) GetDeviceTenant(ctx context.Context, deviceID string) (string, error) {
	var tid *string
	err := s.PG.QueryRow(ctx, `SELECT tenant_id FROM devices WHERE device_id=$1`, deviceID).Scan(&tid)
	if err != nil || tid == nil {
		return "", err
	}
	return *tid, nil
}

// GetPersonTenant returns the tenant_id for a person.
func (s *Store) GetPersonTenant(ctx context.Context, personID string) (string, error) {
	var tid *string
	err := s.PG.QueryRow(ctx, `SELECT tenant_id FROM persons WHERE id=$1`, personID).Scan(&tid)
	if err != nil || tid == nil {
		return "", err
	}
	return *tid, nil
}

// FindPersonByEmployeeNoInTenant finds a person within a tenant.
func (s *Store) FindPersonByEmployeeNoInTenant(ctx context.Context, tenantID, employeeNo string) (*Person, error) {
	row := s.PG.QueryRow(ctx, `
		SELECT id, name, employee_no, gender, person_type, person_role,
		       long_term, attendance_only, door_right, plan_template,
		       valid_begin, valid_end, metadata, COALESCE(qr_token,''), created_at
		FROM persons WHERE tenant_id=$1 AND employee_no=$2 LIMIT 1
	`, tenantID, employeeNo)
	p := &Person{}
	err := row.Scan(&p.ID, &p.Name, &p.EmployeeNo, &p.Gender, &p.PersonType, &p.PersonRole,
		&p.LongTerm, &p.AttendanceOnly, &p.DoorRight, &p.PlanTemplate,
		&p.ValidBegin, &p.ValidEnd, &p.Metadata, &p.QRToken, &p.CreatedAt)
	if err != nil {
		return nil, nil //nolint
	}
	return p, nil
}
