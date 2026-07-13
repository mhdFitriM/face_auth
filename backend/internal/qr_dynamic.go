package internal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const defaultQRRotationSec = 180

func normalizeQRRotation(sec int) int {
	if sec < 30 {
		return defaultQRRotationSec
	}
	if sec > 86400 {
		return 86400
	}
	return sec
}

func (s *Store) ConfigurePersonQR(ctx context.Context, personID, mode string, rotationSec int) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "dynamic" && mode != "hikvision" {
		mode = "static"
	}
	rotationSec = normalizeQRRotation(rotationSec)
	secret := ""
	if mode == "dynamic" {
		secret = RandomString(48, charset+hexCharset)
		if err := s.SetQRToken(ctx, personID, ""); err != nil {
			return err
		}
	} else if mode == "hikvision" {
		if err := s.SetQRToken(ctx, personID, ""); err != nil {
			return err
		}
	} else {
		p, err := s.GetPerson(ctx, personID)
		if err != nil || p == nil {
			return errors.New("person not found")
		}
		if p.QRToken == "" {
			if err := s.SetQRToken(ctx, personID, RandomString(24, charset+hexCharset)); err != nil {
				return err
			}
		}
	}
	_, err := s.PG.Exec(ctx, `
		INSERT INTO person_qr_settings (person_id, mode, rotation_seconds, secret, updated_at)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (person_id) DO UPDATE SET mode=$2, rotation_seconds=$3,
		secret=CASE WHEN $4<>'' THEN $4 ELSE person_qr_settings.secret END, updated_at=NOW()
	`, personID, mode, rotationSec, secret)
	return err
}

func (s *Store) PopulatePersonQRSettings(ctx context.Context, p *Person) {
	if p == nil {
		return
	}
	p.QRMode = "static"
	p.QRRotationSec = defaultQRRotationSec
	_ = s.PG.QueryRow(ctx, `SELECT mode, rotation_seconds FROM person_qr_settings WHERE person_id=$1`, p.ID).Scan(&p.QRMode, &p.QRRotationSec)
}

func (s *Store) DynamicQRToken(ctx context.Context, personID string, now time.Time) (string, time.Time, error) {
	var mode, secret string
	var rotation int
	if err := s.PG.QueryRow(ctx, `SELECT mode, rotation_seconds, secret FROM person_qr_settings WHERE person_id=$1`, personID).Scan(&mode, &rotation, &secret); err != nil {
		return "", time.Time{}, err
	}
	if mode != "dynamic" || secret == "" {
		return "", time.Time{}, errors.New("dynamic QR is not enabled")
	}
	rotation = normalizeQRRotation(rotation)
	step := now.Unix() / int64(rotation)
	encodedID := base64.RawURLEncoding.EncodeToString([]byte(personID))
	message := encodedID + "." + strconv.FormatInt(step, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	expires := time.Unix((step+1)*int64(rotation), 0)
	return "faqr1." + message + "." + sig, expires, nil
}

func (s *Store) ResolveDynamicQR(ctx context.Context, token string, now time.Time) (*Person, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "faqr1" {
		return nil, nil
	}
	idBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil
	}
	step, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, nil
	}
	personID := string(idBytes)
	var mode, secret string
	var rotation int
	if err := s.PG.QueryRow(ctx, `SELECT mode, rotation_seconds, secret FROM person_qr_settings WHERE person_id=$1`, personID).Scan(&mode, &rotation, &secret); err != nil {
		return nil, nil
	}
	rotation = normalizeQRRotation(rotation)
	if mode != "dynamic" || secret == "" || step != now.Unix()/int64(rotation) {
		return nil, nil
	}
	message := parts[1] + "." + parts[2]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !hmac.Equal(got, want) {
		return nil, nil
	}
	p, err := s.GetPerson(ctx, personID)
	if err != nil {
		return nil, fmt.Errorf("dynamic QR person lookup: %w", err)
	}
	return p, nil
}
