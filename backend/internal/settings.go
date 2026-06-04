package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Settings is the global, mutable system configuration that admins can change
// from the UI (or via API). It lives in the `settings` table as a single
// JSON row. We keep a cached snapshot in memory and refresh on Save().
type Settings struct {
	// RequireQR2FA is the global default. When true, every face authentication
	// MUST be preceded by a QR scan that identifies the user. When false, the
	// device's face library is always armed and a face match alone unlocks the
	// door.
	//
	// Per-device overrides live in Device.RequireQR2FA (when not nil).
	RequireQR2FA bool `json:"requireQR2FA"`

	// FaceAuthWindowSec is how long a face-auth session stays "open" (i.e. the
	// device is in unlocked face-verify mode) before we time out and re-lock.
	FaceAuthWindowSec int `json:"faceAuthWindowSec"`

	// PublicAPIEnabled gates the /api/v1/* surface. When false the public
	// endpoints return 503 — useful when bringing the system up without exposing
	// it yet.
	PublicAPIEnabled bool `json:"publicApiEnabled"`

	// Plugins is a free-form bag of plugin-specific config blobs. Each plugin
	// owns its key and unmarshals into its own struct. Deletable by simply
	// dropping the plugin file + key here.
	Plugins map[string]json.RawMessage `json:"plugins,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// PluginConfig fetches a typed config blob from the plugins map. Returns
// (config, found, err). When `found` is false the caller should fall back
// to defaults.
func (s *SettingsStore) PluginConfig(key string, into any) (bool, error) {
	s.mu.RLock()
	raw, ok := s.cache.Plugins[key]
	s.mu.RUnlock()
	if !ok || len(raw) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, err
	}
	return true, nil
}

// SavePluginConfig persists a plugin's config under its key.
func (s *SettingsStore) SavePluginConfig(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	cur := s.Get()
	if cur.Plugins == nil {
		cur.Plugins = map[string]json.RawMessage{}
	}
	cur.Plugins[key] = raw
	return s.Save(ctx, cur)
}

func defaultSettings() Settings {
	return Settings{
		RequireQR2FA:      true, // safer default — require both factors
		FaceAuthWindowSec: 10,
		PublicAPIEnabled:  true,
	}
}

// APIKey is an opaque token a third-party caller presents on /api/v1/* requests.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Key       string    `json:"key,omitempty"` // only returned once on creation
	LastUsed  *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// SettingsStore wraps the cached settings + thread-safe access.
type SettingsStore struct {
	pg *Store

	mu       sync.RWMutex
	cache    Settings
	devCache map[string]*bool // per-device RequireQR2FA overrides (nil = use global)
}

func NewSettingsStore(store *Store) *SettingsStore {
	return &SettingsStore{
		pg:       store,
		devCache: map[string]*bool{},
	}
}

func (s *SettingsStore) Load(ctx context.Context) error {
	row := s.pg.PG.QueryRow(ctx, `SELECT value FROM settings WHERE key='global' LIMIT 1`)
	var raw json.RawMessage
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.mu.Lock()
			s.cache = defaultSettings()
			s.mu.Unlock()
			return s.Save(ctx, s.Get())
		}
		return err
	}
	var loaded Settings
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	if loaded.FaceAuthWindowSec <= 0 {
		loaded.FaceAuthWindowSec = 10
	}
	s.mu.Lock()
	s.cache = loaded
	s.mu.Unlock()
	return nil
}

func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache
}

func (s *SettingsStore) Save(ctx context.Context, next Settings) error {
	next.UpdatedAt = time.Now().UTC()
	if next.FaceAuthWindowSec <= 0 {
		next.FaceAuthWindowSec = 10
	}
	if next.FaceAuthWindowSec > 120 {
		next.FaceAuthWindowSec = 120
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = s.pg.PG.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ('global', $1, NOW())
		ON CONFLICT (key) DO UPDATE SET value=$1, updated_at=NOW()
	`, raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = next
	s.mu.Unlock()
	return nil
}

// DeviceRequiresQR returns true if QR scan is mandatory before face for the
// given device. Per-device override wins; otherwise global default.
func (s *SettingsStore) DeviceRequiresQR(ctx context.Context, deviceID string) (bool, error) {
	s.mu.RLock()
	if v, ok := s.devCache[deviceID]; ok && v != nil {
		out := *v
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	var b *bool
	err := s.pg.PG.QueryRow(ctx, `SELECT require_qr_2fa FROM devices WHERE device_id=$1`, deviceID).Scan(&b)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.Get().RequireQR2FA, nil
		}
		return s.Get().RequireQR2FA, err
	}
	s.mu.Lock()
	s.devCache[deviceID] = b
	s.mu.Unlock()
	if b == nil {
		return s.Get().RequireQR2FA, nil
	}
	return *b, nil
}

// SetDeviceRequireQR sets / clears the per-device override.
//   value == nil  → clear override (fall back to global)
//   value == true → require QR on this device
//   value == false → device is face-only
func (s *SettingsStore) SetDeviceRequireQR(ctx context.Context, deviceID string, value *bool) error {
	_, err := s.pg.PG.Exec(ctx, `UPDATE devices SET require_qr_2fa=$1 WHERE device_id=$2`, value, deviceID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.devCache[deviceID] = value
	s.mu.Unlock()
	return nil
}

// ---- API keys ----

func (s *SettingsStore) CreateAPIKey(ctx context.Context, name string) (APIKey, error) {
	k := APIKey{
		ID:   "ak_" + RandomString(10, hexCharset),
		Name: name,
		Key:  "fa_" + RandomString(40, charset+hexCharset),
	}
	_, err := s.pg.PG.Exec(ctx, `INSERT INTO api_keys (id, name, key) VALUES ($1, $2, $3)`, k.ID, k.Name, k.Key)
	if err != nil {
		return APIKey{}, err
	}
	k.CreatedAt = time.Now().UTC()
	return k, nil
}

func (s *SettingsStore) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pg.PG.Query(ctx, `SELECT id, name, last_used_at, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.LastUsed, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *SettingsStore) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := s.pg.PG.Exec(ctx, `DELETE FROM api_keys WHERE id=$1`, id)
	return err
}

// VerifyAPIKey returns the key record if the presented string matches.
func (s *SettingsStore) VerifyAPIKey(ctx context.Context, presented string) (*APIKey, error) {
	presented = strings.TrimSpace(presented)
	if presented == "" {
		return nil, errors.New("missing api key")
	}
	row := s.pg.PG.QueryRow(ctx, `SELECT id, name, created_at FROM api_keys WHERE key=$1`, presented)
	var k APIKey
	if err := row.Scan(&k.ID, &k.Name, &k.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("invalid api key")
		}
		return nil, err
	}
	_, _ = s.pg.PG.Exec(ctx, `UPDATE api_keys SET last_used_at=NOW() WHERE id=$1`, k.ID)
	return &k, nil
}
