package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
)

type Store struct {
	PG     *pgxpool.Pool
	Redis  *redis.Client
	MinIO  *minio.Client
	Bucket string

	mu       sync.RWMutex
	eventSub map[chan Event]struct{}
}

func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	pg, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("pg: %w", err)
	}

	// Wait for Postgres
	for i := 0; i < 60; i++ {
		if err := pg.Ping(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err := pg.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pg ping: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisURL})
	for i := 0; i < 30; i++ {
		if _, err := rdb.Ping(ctx).Result(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	mc, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: %w", err)
	}

	// Wait for MinIO
	for i := 0; i < 30; i++ {
		_, err := mc.BucketExists(ctx, cfg.MinIOBucket)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}

	exists, err := mc.BucketExists(ctx, cfg.MinIOBucket)
	if err != nil {
		return nil, fmt.Errorf("minio bucket check: %w", err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.MinIOBucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("minio make bucket: %w", err)
		}
	}

	s := &Store{
		PG:       pg,
		Redis:    rdb,
		MinIO:    mc,
		Bucket:   cfg.MinIOBucket,
		eventSub: map[chan Event]struct{}{},
	}

	if err := s.migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	go s.runRedisEventFanout(context.Background())

	return s, nil
}

func (s *Store) Close() {
	s.PG.Close()
	_ = s.Redis.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.PG.Exec(ctx, migrationSQL)
	return err
}

const migrationSQL = `
CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    -- Push SDK / ISUP fields (kept for future devices that support HTTP push)
    password TEXT DEFAULT '',
    salt TEXT DEFAULT '',
    challenge TEXT DEFAULT '',
    iterations INTEGER DEFAULT 0,
    username TEXT DEFAULT '',
    digest_type TEXT DEFAULT '',
    is_auth BOOLEAN DEFAULT FALSE,
    -- ISAPI pull-mode fields (the device's web admin credentials + LAN address)
    ip TEXT DEFAULT '',
    port INTEGER DEFAULT 80,
    use_https BOOLEAN DEFAULT FALSE,
    isapi_username TEXT DEFAULT '',
    isapi_password TEXT DEFAULT '',
    fdid TEXT DEFAULT '1',
    face_lib_type TEXT DEFAULT 'blackFD',
    -- Status
    online BOOLEAN DEFAULT FALSE,
    last_seen TIMESTAMPTZ,
    model TEXT DEFAULT '',
    firmware TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS persons (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    employee_no TEXT DEFAULT '',
    gender TEXT DEFAULT 'unknown',         -- male | female | unknown
    person_type TEXT DEFAULT 'normal',      -- normal | visitor | blackList
    person_role TEXT DEFAULT 'basic',       -- basic | administrator | operator
    long_term BOOLEAN DEFAULT FALSE,
    attendance_only BOOLEAN DEFAULT FALSE,
    door_right TEXT DEFAULT '1',
    plan_template TEXT DEFAULT '1',
    valid_begin TIMESTAMPTZ,
    valid_end TIMESTAMPTZ,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
-- Compatibility ALTERs (no-op on fresh schema; safe on existing deployments)
ALTER TABLE persons ADD COLUMN IF NOT EXISTS gender TEXT DEFAULT 'unknown';
ALTER TABLE persons ADD COLUMN IF NOT EXISTS person_type TEXT DEFAULT 'normal';
ALTER TABLE persons ADD COLUMN IF NOT EXISTS person_role TEXT DEFAULT 'basic';
ALTER TABLE persons ADD COLUMN IF NOT EXISTS long_term BOOLEAN DEFAULT FALSE;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS attendance_only BOOLEAN DEFAULT FALSE;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS door_right TEXT DEFAULT '1';
ALTER TABLE persons ADD COLUMN IF NOT EXISTS plan_template TEXT DEFAULT '1';
ALTER TABLE persons ADD COLUMN IF NOT EXISTS valid_begin TIMESTAMPTZ;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS valid_end TIMESTAMPTZ;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS qr_token TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_persons_qr_token ON persons(qr_token) WHERE qr_token IS NOT NULL AND qr_token <> '';

CREATE TABLE IF NOT EXISTS faces (
    id TEXT PRIMARY KEY,
    person_id TEXT REFERENCES persons(id) ON DELETE CASCADE,
    device_id TEXT,
    image_key TEXT NOT NULL,
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_faces_person ON faces(person_id);
CREATE INDEX IF NOT EXISTS idx_faces_device ON faces(device_id);

CREATE TABLE IF NOT EXISTS commands (
    id TEXT PRIMARY KEY,
    device_id TEXT NOT NULL,
    method TEXT NOT NULL,
    url TEXT NOT NULL,
    data_format TEXT NOT NULL DEFAULT 'json',
    body_base64 TEXT,
    response_body TEXT,
    response_status INT,
    sent_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_commands_device_status ON commands(device_id, status);

CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    device_id TEXT NOT NULL,
    event_type TEXT DEFAULT '',
    raw JSONB NOT NULL,
    image_key TEXT DEFAULT '',
    received_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_device_time ON events(device_id, received_at DESC);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    token TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

ALTER TABLE devices ADD COLUMN IF NOT EXISTS agent_id TEXT;
CREATE INDEX IF NOT EXISTS idx_devices_agent ON devices(agent_id);

-- Per-device override for the "require QR before face" toggle.
-- NULL = follow the global setting; TRUE / FALSE = explicit override.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS require_qr_2fa BOOLEAN;

-- Global key/value config blob. Single row with key='global'.
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- API keys for third-party callers of /api/v1/*.
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    key TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
`

// ---------- Device ops ----------

func (s *Store) UpsertDeviceAuth(ctx context.Context, deviceID, salt, challenge string, iterations int, isAuth bool) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO devices (device_id, salt, challenge, iterations, is_auth, last_seen, online)
		VALUES ($1, $2, $3, $4, $5, NOW(), TRUE)
		ON CONFLICT (device_id) DO UPDATE
		SET salt=$2, challenge=$3, iterations=$4, is_auth=$5, last_seen=NOW(), online=TRUE
	`, deviceID, salt, challenge, iterations, isAuth)
	return err
}

func (s *Store) UpsertDeviceNoAuth(ctx context.Context, deviceID string) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO devices (device_id, is_auth, last_seen, online)
		VALUES ($1, FALSE, NOW(), TRUE)
		ON CONFLICT (device_id) DO UPDATE
		SET last_seen=NOW(), online=TRUE
	`, deviceID)
	return err
}

func (s *Store) UpdateDeviceChallenge(ctx context.Context, deviceID, challenge string) error {
	_, err := s.PG.Exec(ctx, `UPDATE devices SET challenge=$1, last_seen=NOW(), online=TRUE WHERE device_id=$2`, challenge, deviceID)
	return err
}

func (s *Store) SetDeviceLogin(ctx context.Context, deviceID, username, digestType string) error {
	_, err := s.PG.Exec(ctx, `UPDATE devices SET username=$1, digest_type=$2 WHERE device_id=$3`, username, digestType, deviceID)
	return err
}

func (s *Store) SetDeviceOffline(ctx context.Context, deviceID string) error {
	_, err := s.PG.Exec(ctx, `UPDATE devices SET online=FALSE WHERE device_id=$1`, deviceID)
	return err
}

func (s *Store) TouchDevice(ctx context.Context, deviceID string) {
	_, _ = s.PG.Exec(ctx, `UPDATE devices SET last_seen=NOW(), online=TRUE WHERE device_id=$1`, deviceID)
}

func (s *Store) GetDevice(ctx context.Context, deviceID string) (*Device, error) {
	row := s.PG.QueryRow(ctx, `
		SELECT device_id, name, salt, challenge, iterations, username, digest_type, is_auth,
		       ip, port, use_https, isapi_username, isapi_password, fdid, face_lib_type,
		       online, last_seen, model, firmware, COALESCE(agent_id,''), created_at
		FROM devices WHERE device_id=$1`, deviceID)
	d := &Device{}
	err := row.Scan(&d.DeviceID, &d.Name, &d.Salt, &d.Challenge, &d.Iterations, &d.Username, &d.DigestType,
		&d.IsAuth, &d.IP, &d.Port, &d.UseHTTPS, &d.ISAPIUsername, &d.ISAPIPassword, &d.FDID, &d.FaceLibType,
		&d.Online, &d.LastSeen, &d.Model, &d.Firmware, &d.AgentID, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *Store) GetDevicePassword(ctx context.Context, deviceID string) (string, error) {
	var pwd string
	err := s.PG.QueryRow(ctx, `SELECT password FROM devices WHERE device_id=$1`, deviceID).Scan(&pwd)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return pwd, err
}

func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT device_id, name, username, digest_type, is_auth,
		       ip, port, use_https, isapi_username, fdid, face_lib_type,
		       online, last_seen, model, firmware, COALESCE(agent_id,''), created_at
		FROM devices ORDER BY created_at DESC`)
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

func (s *Store) RegisterDevice(ctx context.Context, d Device) error {
	if d.Port == 0 {
		d.Port = 80
	}
	if d.FDID == "" {
		d.FDID = "1"
	}
	if d.FaceLibType == "" {
		d.FaceLibType = "blackFD"
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO devices (device_id, name, password, ip, port, use_https, isapi_username, isapi_password, fdid, face_lib_type, agent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11,''))
		ON CONFLICT (device_id) DO UPDATE
		SET name=$2, password=COALESCE(NULLIF($3,''), devices.password),
		    ip=$4, port=$5, use_https=$6,
		    isapi_username=$7, isapi_password=COALESCE(NULLIF($8,''), devices.isapi_password),
		    fdid=$9, face_lib_type=$10,
		    agent_id=NULLIF($11,'')
	`, d.DeviceID, d.Name, d.Password(), d.IP, d.Port, d.UseHTTPS, d.ISAPIUsername, d.ISAPIPassword, d.FDID, d.FaceLibType, d.AgentID)
	return err
}

func (s *Store) SetDeviceOnline(ctx context.Context, deviceID, model, firmware string, online bool) error {
	_, err := s.PG.Exec(ctx, `
		UPDATE devices SET online=$1, model=COALESCE(NULLIF($2,''), model), firmware=COALESCE(NULLIF($3,''), firmware), last_seen=NOW() WHERE device_id=$4
	`, online, model, firmware, deviceID)
	return err
}

// FindDeviceByIP returns the first device whose stored IP matches the given
// address. Used to attribute alarm-host pushes when the device omits a serial
// in the event body.
func (s *Store) FindDeviceByIP(ctx context.Context, ip string) (*Device, error) {
	if ip == "" {
		return nil, nil
	}
	row := s.PG.QueryRow(ctx, `SELECT device_id FROM devices WHERE ip=$1 LIMIT 1`, ip)
	d := &Device{}
	if err := row.Scan(&d.DeviceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

// PurgeEmptyEvents removes rows with no usable content (heartbeats / pings
// that arrived before the noise filter was in place).
func (s *Store) PurgeEmptyEvents(ctx context.Context) (int64, error) {
	tag, err := s.PG.Exec(ctx, `
		DELETE FROM events
		WHERE raw IS NULL
		   OR raw = '{}'::jsonb
		   OR raw = 'null'::jsonb
		   OR (raw ? 'raw' AND raw->>'raw' = '')
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) DeleteDevice(ctx context.Context, deviceID string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM devices WHERE device_id=$1`, deviceID)
	return err
}

// ---------- Agent ops ----------

func (s *Store) CreateAgent(ctx context.Context, a Agent) error {
	if a.Token == "" {
		a.Token = GenerateAgentToken()
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO agents (id, name, token) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET name=$2, token=$3
	`, a.ID, a.Name, a.Token)
	return err
}

func (s *Store) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.PG.QueryRow(ctx, `SELECT id, name, token, created_at FROM agents WHERE id=$1`, id)
	a := &Agent{}
	err := row.Scan(&a.ID, &a.Name, &a.Token, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.PG.Query(ctx, `SELECT id, name, created_at FROM agents ORDER BY created_at DESC`)
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

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM agents WHERE id=$1`, id)
	return err
}

func (s *Store) RegenerateAgentToken(ctx context.Context, id string) (string, error) {
	t := GenerateAgentToken()
	_, err := s.PG.Exec(ctx, `UPDATE agents SET token=$1 WHERE id=$2`, t, id)
	if err != nil {
		return "", err
	}
	return t, nil
}

// VerifyAgentToken returns true if the given (id, token) matches.
func (s *Store) VerifyAgentToken(ctx context.Context, id, token string) bool {
	if id == "" || token == "" {
		return false
	}
	var stored string
	if err := s.PG.QueryRow(ctx, `SELECT token FROM agents WHERE id=$1`, id).Scan(&stored); err != nil {
		return false
	}
	return stored == token
}

func GenerateAgentToken() string {
	return RandomString(40, charset+hexCharset)
}

// ---------- Person ops ----------

func (s *Store) CreatePerson(ctx context.Context, p Person) error {
	meta := p.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	if p.Gender == "" {
		p.Gender = "unknown"
	}
	if p.PersonType == "" {
		p.PersonType = "normal"
	}
	if p.PersonRole == "" {
		p.PersonRole = "basic"
	}
	if p.DoorRight == "" {
		p.DoorRight = "1"
	}
	if p.PlanTemplate == "" {
		p.PlanTemplate = "1"
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO persons (id, name, employee_no, gender, person_type, person_role,
		                    long_term, attendance_only, door_right, plan_template,
		                    valid_begin, valid_end, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE
		SET name=$2, employee_no=$3, gender=$4, person_type=$5, person_role=$6,
		    long_term=$7, attendance_only=$8, door_right=$9, plan_template=$10,
		    valid_begin=$11, valid_end=$12, metadata=$13
	`, p.ID, p.Name, p.EmployeeNo, p.Gender, p.PersonType, p.PersonRole,
		p.LongTerm, p.AttendanceOnly, p.DoorRight, p.PlanTemplate,
		p.ValidBegin, p.ValidEnd, meta)
	return err
}

// GetPersonByQRToken finds the person owning a given QR token. Used at
// scan time to map "the person scanned the QR" back to a user record.
func (s *Store) GetPersonByQRToken(ctx context.Context, token string) (*Person, error) {
	if token == "" {
		return nil, nil
	}
	row := s.PG.QueryRow(ctx, `
		SELECT id, name, employee_no, gender, person_type, person_role,
		       long_term, attendance_only, door_right, plan_template,
		       valid_begin, valid_end, metadata, COALESCE(qr_token,''), created_at
		FROM persons WHERE qr_token=$1 LIMIT 1`, token)
	p := &Person{}
	err := row.Scan(&p.ID, &p.Name, &p.EmployeeNo, &p.Gender, &p.PersonType, &p.PersonRole,
		&p.LongTerm, &p.AttendanceOnly, &p.DoorRight, &p.PlanTemplate,
		&p.ValidBegin, &p.ValidEnd, &p.Metadata, &p.QRToken, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// SetQRToken assigns or rotates a person's QR token.
func (s *Store) SetQRToken(ctx context.Context, personID, token string) error {
	_, err := s.PG.Exec(ctx, `UPDATE persons SET qr_token=NULLIF($1,'') WHERE id=$2`, token, personID)
	return err
}

func (s *Store) GetPersonByEmployeeNo(ctx context.Context, employeeNo string) (*Person, error) {
	row := s.PG.QueryRow(ctx, `
		SELECT id, name, employee_no, gender, person_type, person_role,
		       long_term, attendance_only, door_right, plan_template,
		       valid_begin, valid_end, metadata, COALESCE(qr_token,''), created_at
		FROM persons WHERE employee_no=$1 LIMIT 1`, employeeNo)
	p := &Person{}
	err := row.Scan(&p.ID, &p.Name, &p.EmployeeNo, &p.Gender, &p.PersonType, &p.PersonRole,
		&p.LongTerm, &p.AttendanceOnly, &p.DoorRight, &p.PlanTemplate,
		&p.ValidBegin, &p.ValidEnd, &p.Metadata, &p.QRToken, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) GetPerson(ctx context.Context, id string) (*Person, error) {
	row := s.PG.QueryRow(ctx, `
		SELECT id, name, employee_no, gender, person_type, person_role,
		       long_term, attendance_only, door_right, plan_template,
		       valid_begin, valid_end, metadata, COALESCE(qr_token,''), created_at
		FROM persons WHERE id=$1`, id)
	p := &Person{}
	err := row.Scan(&p.ID, &p.Name, &p.EmployeeNo, &p.Gender, &p.PersonType, &p.PersonRole,
		&p.LongTerm, &p.AttendanceOnly, &p.DoorRight, &p.PlanTemplate,
		&p.ValidBegin, &p.ValidEnd, &p.Metadata, &p.QRToken, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) ListPersons(ctx context.Context) ([]Person, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT id, name, employee_no, gender, person_type, person_role,
		       long_term, attendance_only, door_right, plan_template,
		       valid_begin, valid_end, metadata, COALESCE(qr_token,''), created_at
		FROM persons ORDER BY created_at DESC`)
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

func (s *Store) DeletePerson(ctx context.Context, id string) error {
	_, err := s.PG.Exec(ctx, `DELETE FROM persons WHERE id=$1`, id)
	return err
}

// ---------- Face ops ----------

func (s *Store) CreateFace(ctx context.Context, f Face) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO faces (id, person_id, device_id, image_key, status)
		VALUES ($1, $2, $3, $4, $5)
	`, f.ID, f.PersonID, f.DeviceID, f.ImageKey, f.Status)
	return err
}

func (s *Store) UpdateFaceStatus(ctx context.Context, id, status string) error {
	_, err := s.PG.Exec(ctx, `UPDATE faces SET status=$1 WHERE id=$2`, status, id)
	return err
}

func (s *Store) ListFaces(ctx context.Context, deviceID, personID string) ([]Face, error) {
	q := `SELECT id, person_id, device_id, image_key, status, created_at FROM faces WHERE 1=1`
	args := []any{}
	i := 1
	if deviceID != "" {
		q += fmt.Sprintf(" AND device_id=$%d", i)
		args = append(args, deviceID)
		i++
	}
	if personID != "" {
		q += fmt.Sprintf(" AND person_id=$%d", i)
		args = append(args, personID)
	}
	q += " ORDER BY created_at DESC LIMIT 500"
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Face
	for rows.Next() {
		var f Face
		if err := rows.Scan(&f.ID, &f.PersonID, &f.DeviceID, &f.ImageKey, &f.Status, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if out == nil {
		out = []Face{}
	}
	return out, rows.Err()
}

// ---------- Command queue ----------

func (s *Store) EnqueueCommand(ctx context.Context, c Command) error {
	_, err := s.PG.Exec(ctx, `
		INSERT INTO commands (id, device_id, method, url, data_format, body_base64, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending')
	`, c.ID, c.DeviceID, c.Method, c.URL, c.DataFormat, c.BodyBase64)
	if err != nil {
		return err
	}
	// Push UUID into Redis list for fast pop
	return s.Redis.LPush(ctx, "cmdq:"+c.DeviceID, c.ID).Err()
}

func (s *Store) PopPendingCommands(ctx context.Context, deviceID string, max int) ([]Command, error) {
	out := []Command{}
	for i := 0; i < max; i++ {
		id, err := s.Redis.RPop(ctx, "cmdq:"+deviceID).Result()
		if errors.Is(err, redis.Nil) {
			break
		}
		if err != nil {
			return out, err
		}
		var c Command
		err = s.PG.QueryRow(ctx, `
			SELECT id, device_id, method, url, data_format, COALESCE(body_base64,'') FROM commands WHERE id=$1 AND status='pending'
		`, id).Scan(&c.ID, &c.DeviceID, &c.Method, &c.URL, &c.DataFormat, &c.BodyBase64)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, err
		}
		_, _ = s.PG.Exec(ctx, `UPDATE commands SET status='sent', sent_at=NOW() WHERE id=$1`, c.ID)
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) PendingCommandCount(ctx context.Context, deviceID string) (int, error) {
	n, err := s.Redis.LLen(ctx, "cmdq:"+deviceID).Result()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *Store) CompleteCommand(ctx context.Context, uuid, responseBody string, status int) error {
	_, err := s.PG.Exec(ctx, `
		UPDATE commands SET response_body=$1, response_status=$2, completed_at=NOW(), status='done' WHERE id=$3
	`, responseBody, status, uuid)
	if err != nil {
		return err
	}
	// Notify waiters via pubsub
	payload, _ := json.Marshal(map[string]any{"uuid": uuid, "response": responseBody, "status": status})
	return s.Redis.Publish(ctx, "cmd:result:"+uuid, payload).Err()
}

func (s *Store) AwaitCommandResult(ctx context.Context, uuid string, timeout time.Duration) (string, int, error) {
	// Check if already done
	var rb string
	var status int
	err := s.PG.QueryRow(ctx, `SELECT COALESCE(response_body,''), COALESCE(response_status,0) FROM commands WHERE id=$1 AND status='done'`, uuid).Scan(&rb, &status)
	if err == nil {
		return rb, status, nil
	}

	sub := s.Redis.Subscribe(ctx, "cmd:result:"+uuid)
	defer sub.Close()

	ch := sub.Channel()
	select {
	case msg := <-ch:
		var payload struct {
			UUID     string `json:"uuid"`
			Response string `json:"response"`
			Status   int    `json:"status"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
			return "", 0, err
		}
		return payload.Response, payload.Status, nil
	case <-time.After(timeout):
		return "", 0, errors.New("timeout waiting for command result")
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
}

func (s *Store) ListCommands(ctx context.Context, deviceID string, limit int) ([]Command, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, device_id, method, url, data_format, COALESCE(response_body,''), COALESCE(response_status,0), status, sent_at, completed_at, created_at FROM commands`
	args := []any{}
	if deviceID != "" {
		q += ` WHERE device_id=$1`
		args = append(args, deviceID)
	}
	q += ` ORDER BY created_at DESC LIMIT ` + fmt.Sprint(limit)
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Command
	for rows.Next() {
		var c Command
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.Method, &c.URL, &c.DataFormat, &c.ResponseBody, &c.ResponseStatus, &c.Status, &c.SentAt, &c.CompletedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Command{}
	}
	return out, rows.Err()
}

// ---------- Events ----------

func (s *Store) InsertEvent(ctx context.Context, e Event) (int64, error) {
	if len(e.Raw) == 0 {
		e.Raw = json.RawMessage(`{}`)
	}
	var id int64
	err := s.PG.QueryRow(ctx, `
		INSERT INTO events (device_id, event_type, raw, image_key)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, e.DeviceID, e.EventType, e.Raw, e.ImageKey).Scan(&id)
	if err != nil {
		return 0, err
	}
	e.ID = id
	e.ReceivedAt = time.Now()
	s.publishEvent(e)
	return id, nil
}

func (s *Store) ListEvents(ctx context.Context, deviceID string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, device_id, event_type, raw, COALESCE(image_key,''), received_at FROM events`
	args := []any{}
	if deviceID != "" {
		q += ` WHERE device_id=$1`
		args = append(args, deviceID)
	}
	q += fmt.Sprintf(` ORDER BY received_at DESC LIMIT %d`, limit)
	rows, err := s.PG.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.EventType, &e.Raw, &e.ImageKey, &e.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []Event{}
	}
	return out, rows.Err()
}

// ---------- Event pub/sub ----------

func (s *Store) Subscribe() chan Event {
	ch := make(chan Event, 16)
	s.mu.Lock()
	s.eventSub[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Store) Unsubscribe(ch chan Event) {
	s.mu.Lock()
	delete(s.eventSub, ch)
	s.mu.Unlock()
	close(ch)
}

func (s *Store) publishEvent(e Event) {
	payload, err := json.Marshal(e)
	if err == nil {
		_ = s.Redis.Publish(context.Background(), "events", payload).Err()
	}
}

func (s *Store) runRedisEventFanout(ctx context.Context) {
	sub := s.Redis.Subscribe(ctx, "events")
	defer sub.Close()
	for msg := range sub.Channel() {
		var e Event
		if err := json.Unmarshal([]byte(msg.Payload), &e); err != nil {
			continue
		}
		s.mu.RLock()
		for ch := range s.eventSub {
			select {
			case ch <- e:
			default:
			}
		}
		s.mu.RUnlock()
	}
}

// ---------- MinIO ----------

func (s *Store) PutObject(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.MinIO.PutObject(ctx, s.Bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *Store) GetObject(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	obj, err := s.MinIO.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", 0, err
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, "", 0, err
	}
	return obj, info.ContentType, info.Size, nil
}

func init() {
	// Silence pgx default logger noise — we use stdlib log.
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}
