package internal

import (
	"context"
	"fmt"
	"time"
)

type AccessLogFilter struct {
	TenantID, OutletID, AccessPointID, AgentID, DeviceID, PersonID, Decision, Direction, CredentialMode string
	From, To                                                                                            time.Time
	Limit                                                                                               int
}

func (s *Store) QueryAccessLog(ctx context.Context, f AccessLogFilter) ([]AccessLog, error) {
	if f.Limit <= 0 || f.Limit > 10000 {
		f.Limit = 500
	}
	q := `SELECT id,COALESCE(tenant_id,''),COALESCE(person_id,''),COALESCE(employee_no,''),COALESCE(device_id,''),decision,COALESCE(reason,''),COALESCE(direction,''),COALESCE(open_method,''),COALESCE(outlet_id,''),COALESCE(access_point_id,''),COALESCE(access_point_name,''),COALESCE(agent_id,''),COALESCE(scanner_id,''),COALESCE(door_no,0),COALESCE(member_name,''),COALESCE(credential_mode,''),COALESCE(door_result,''),COALESCE(correlation_id,''),created_at FROM access_log WHERE tenant_id=$1`
	args := []any{f.TenantID}
	add := func(column, value string) {
		if value != "" {
			args = append(args, value)
			q += fmt.Sprintf(" AND %s=$%d", column, len(args))
		}
	}
	add("outlet_id", f.OutletID)
	add("access_point_id", f.AccessPointID)
	add("agent_id", f.AgentID)
	add("device_id", f.DeviceID)
	add("person_id", f.PersonID)
	add("decision", f.Decision)
	add("direction", f.Direction)
	add("credential_mode", f.CredentialMode)
	if !f.From.IsZero() {
		args = append(args, f.From)
		q += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}
	if !f.To.IsZero() {
		args = append(args, f.To)
		q += fmt.Sprintf(" AND created_at < $%d", len(args))
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", f.Limit)
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessLog{}
	for rows.Next() {
		var a AccessLog
		if err := rows.Scan(&a.ID, &a.TenantID, &a.PersonID, &a.EmployeeNo, &a.DeviceID, &a.Decision, &a.Reason, &a.Direction, &a.OpenMethod, &a.OutletID, &a.AccessPointID, &a.AccessPointName, &a.AgentID, &a.ScannerID, &a.DoorNo, &a.MemberName, &a.CredentialMode, &a.DoorResult, &a.CorrelationID, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AccessSummary(ctx context.Context, tenantID, outletID string, from, to time.Time) (map[string]any, error) {
	q := `SELECT COUNT(*),COUNT(*) FILTER(WHERE decision='allow'),COUNT(*) FILTER(WHERE decision='deny'),COUNT(*) FILTER(WHERE direction='in' AND decision='allow'),COUNT(*) FILTER(WHERE direction='out' AND decision='allow'),COUNT(DISTINCT person_id) FILTER(WHERE person_id IS NOT NULL) FROM access_log WHERE tenant_id=$1`
	args := []any{tenantID}
	if outletID != "" {
		args = append(args, outletID)
		q += " AND outlet_id=$2"
	}
	if !from.IsZero() {
		args = append(args, from)
		q += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}
	if !to.IsZero() {
		args = append(args, to)
		q += fmt.Sprintf(" AND created_at < $%d", len(args))
	}
	var total, allowed, denied, entries, exits, members int64
	if err := s.PG.QueryRow(ctx, q, args...).Scan(&total, &allowed, &denied, &entries, &exits, &members); err != nil {
		return nil, err
	}
	return map[string]any{"total": total, "allowed": allowed, "denied": denied, "entries": entries, "exits": exits, "uniqueMembers": members, "estimatedOccupancy": entries - exits}, nil
}

func (s *Store) DenialReport(ctx context.Context, tenantID, outletID string) ([]map[string]any, error) {
	q := `SELECT COALESCE(reason,'unknown'),COUNT(*) FROM access_log WHERE tenant_id=$1 AND decision='deny'`
	args := []any{tenantID}
	if outletID != "" {
		args = append(args, outletID)
		q += " AND outlet_id=$2"
	}
	q += " GROUP BY reason ORDER BY COUNT(*) DESC"
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var reason string
		var count int64
		if err := rows.Scan(&reason, &count); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"reason": reason, "count": count})
	}
	return out, rows.Err()
}

func (s *Store) AttendanceReport(ctx context.Context, tenantID, outletID string, from, to time.Time) ([]map[string]any, error) {
	q := `SELECT COALESCE(person_id,''),COALESCE(MAX(member_name),''),COALESCE(MAX(employee_no),''),DATE(created_at),MIN(created_at),MAX(created_at),COUNT(*) FILTER(WHERE direction='in'),COUNT(*) FILTER(WHERE direction='out') FROM access_log WHERE tenant_id=$1 AND decision='allow' AND person_id IS NOT NULL`
	args := []any{tenantID}
	if outletID != "" {
		args = append(args, outletID)
		q += fmt.Sprintf(" AND outlet_id=$%d", len(args))
	}
	if !from.IsZero() {
		args = append(args, from)
		q += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}
	if !to.IsZero() {
		args = append(args, to)
		q += fmt.Sprintf(" AND created_at < $%d", len(args))
	}
	q += " GROUP BY person_id,DATE(created_at) ORDER BY DATE(created_at) DESC,MAX(member_name)"
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, employee string
		var day time.Time
		var first, last time.Time
		var entries, exits int64
		if err := rows.Scan(&id, &name, &employee, &day, &first, &last, &entries, &exits); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"personId": id, "memberName": name, "employeeNo": employee, "date": day.Format("2006-01-02"), "firstAccess": first, "lastAccess": last, "entries": entries, "exits": exits})
	}
	return out, rows.Err()
}

func (s *Store) OccupancyReport(ctx context.Context, tenantID string) ([]map[string]any, error) {
	rows, err := s.PG.Query(ctx, `SELECT p.id,p.name,p.employee_no,COALESCE(pp.inside,false),pp.last_event_at FROM persons p JOIN person_plans pp ON pp.person_id=p.id WHERE p.tenant_id=$1 AND pp.inside=true ORDER BY pp.last_event_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, employee string
		var inside bool
		var last *time.Time
		if err := rows.Scan(&id, &name, &employee, &inside, &last); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"personId": id, "memberName": name, "employeeNo": employee, "inside": inside, "lastEventAt": last})
	}
	return out, rows.Err()
}
