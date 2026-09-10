package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"backend/internal/platform/cache"
)

const (
	sessionHotKeyPrefix   = "identity:session:"
	sessionEpochKeyPrefix = "identity:session:epoch:"
)

// sessionHotCache is the disposable Valkey surface for session resolution.
// PostgreSQL remains the durable authority.
type sessionHotCache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// SessionCachePolicy caps hot-cache TTL. Zero MaxTTL means remaining absolute expiry only.
// Values are injected; they are not product defaults.
type SessionCachePolicy struct {
	MaxTTL time.Duration
}

func (p SessionCachePolicy) Validate() error {
	if p.MaxTTL < 0 {
		return errInvalidPolicy
	}
	return nil
}

type hotSessionRecord struct {
	SessionID         string  `json:"session_id"`
	UserID            string  `json:"user_id"`
	DeviceID          *string `json:"device_id,omitempty"`
	LastSeenAt        string  `json:"last_seen_at"`
	IdleExpiresAt     string  `json:"idle_expires_at"`
	AbsoluteExpiresAt string  `json:"absolute_expires_at"`
	RevokedAt         *string `json:"revoked_at,omitempty"`
	UserDisabled      bool    `json:"user_disabled"`
	UserDeleted       bool    `json:"user_deleted"`
	DeviceRevoked     bool    `json:"device_revoked"`
	UserEpoch         int64   `json:"user_epoch"`
}

func sessionHotKey(tokenHash []byte) string {
	if len(tokenHash) != TokenHashSize {
		return ""
	}
	return sessionHotKeyPrefix + hex.EncodeToString(tokenHash)
}

func sessionEpochKey(userID ID) string {
	if userID.IsZero() {
		return ""
	}
	return sessionEpochKeyPrefix + userID.String()
}

func (s *Sessions) cacheTTL(now, absoluteExpiresAt time.Time) (time.Duration, bool) {
	remaining := absoluteExpiresAt.Sub(now)
	if remaining < time.Millisecond {
		return 0, false
	}
	ttl := remaining
	if s.cachePolicy.MaxTTL > 0 && s.cachePolicy.MaxTTL < ttl {
		ttl = s.cachePolicy.MaxTTL
	}
	return ttl, true
}

func encodeHotSession(session Session, user User, device *Device, epoch int64) (string, error) {
	rec := hotSessionRecord{
		SessionID:         session.ID.String(),
		UserID:            session.UserID.String(),
		LastSeenAt:        session.LastSeenAt.UTC().Format(time.RFC3339Nano),
		IdleExpiresAt:     session.IdleExpiresAt.UTC().Format(time.RFC3339Nano),
		AbsoluteExpiresAt: session.AbsoluteExpiresAt.UTC().Format(time.RFC3339Nano),
		UserDisabled:      user.DisabledAt != nil,
		UserDeleted:       user.DeletedAt != nil,
		UserEpoch:         epoch,
	}
	if session.DeviceID != nil {
		did := session.DeviceID.String()
		rec.DeviceID = &did
		if device != nil && device.RevokedAt != nil {
			rec.DeviceRevoked = true
		}
	}
	if session.RevokedAt != nil {
		v := session.RevokedAt.UTC().Format(time.RFC3339Nano)
		rec.RevokedAt = &v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeHotSession(raw string, tokenHash []byte) (Session, User, *Device, int64, error) {
	if raw == "" || len(tokenHash) != TokenHashSize {
		return Session{}, User{}, nil, 0, errInvalidTokenHash
	}
	var rec hotSessionRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return Session{}, User{}, nil, 0, err
	}
	sid, err := ParseID(rec.SessionID)
	if err != nil {
		return Session{}, User{}, nil, 0, err
	}
	uid, err := ParseID(rec.UserID)
	if err != nil {
		return Session{}, User{}, nil, 0, err
	}
	lastSeen, err := parseHotTime(rec.LastSeenAt)
	if err != nil {
		return Session{}, User{}, nil, 0, err
	}
	idle, err := parseHotTime(rec.IdleExpiresAt)
	if err != nil {
		return Session{}, User{}, nil, 0, err
	}
	absolute, err := parseHotTime(rec.AbsoluteExpiresAt)
	if err != nil {
		return Session{}, User{}, nil, 0, err
	}
	session := Session{
		ID:                sid,
		UserID:            uid,
		TokenHash:         cloneBytes(tokenHash),
		LastSeenAt:        lastSeen,
		IdleExpiresAt:     idle,
		AbsoluteExpiresAt: absolute,
	}
	if rec.DeviceID != nil && *rec.DeviceID != "" {
		did, derr := ParseID(*rec.DeviceID)
		if derr != nil {
			return Session{}, User{}, nil, 0, derr
		}
		session.DeviceID = &did
	}
	if rec.RevokedAt != nil && *rec.RevokedAt != "" {
		revoked, rerr := parseHotTime(*rec.RevokedAt)
		if rerr != nil {
			return Session{}, User{}, nil, 0, rerr
		}
		session.RevokedAt = &revoked
	}
	now := time.Unix(0, 0).UTC()
	user := User{ID: uid, CreatedAt: now, UpdatedAt: now}
	if rec.UserDisabled {
		t := now
		user.DisabledAt = &t
	}
	if rec.UserDeleted {
		t := now
		user.DeletedAt = &t
	}
	var device *Device
	if session.DeviceID != nil {
		d := Device{ID: *session.DeviceID, UserID: uid, CreatedAt: now, LastSeenAt: lastSeen}
		if rec.DeviceRevoked {
			t := now
			d.RevokedAt = &t
		}
		device = &d
	}
	return session, user, device, rec.UserEpoch, nil
}

func parseHotTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, errInvalidPolicy
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func (s *Sessions) resolveHot(ctx context.Context, tokenHash []byte) (Session, bool, error) {
	if s.hot == nil {
		return Session{}, false, nil
	}
	key := sessionHotKey(tokenHash)
	if key == "" {
		return Session{}, false, nil
	}
	raw, err := s.hot.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrMiss) || isCacheUnavailable(err) {
			return Session{}, false, nil
		}
		return Session{}, false, nil
	}
	session, _, device, epoch, err := decodeHotSession(raw, tokenHash)
	if err != nil {
		_ = s.hot.Delete(ctx, key)
		return Session{}, false, nil
	}
	if epoch < 0 {
		_ = s.hot.Delete(ctx, key)
		return Session{}, false, nil
	}
	if cached, ok := s.readCachedEpoch(ctx, session.UserID); ok && cached != epoch {
		_ = s.hot.Delete(ctx, key)
		return Session{}, false, nil
	}
	user, uerr := s.store.GetUser(ctx, session.UserID)
	if uerr != nil {
		return Session{}, false, nil
	}
	if user.SessionEpoch != epoch {
		_ = s.hot.Delete(ctx, key)
		return Session{}, false, nil
	}
	if err := AuthorizeSession(s.now(), user, device, session); err != nil {
		return Session{}, true, err
	}
	return session, true, nil
}

func (s *Sessions) populateHot(ctx context.Context, session Session, user User, device *Device) {
	if s.hot == nil {
		return
	}
	ttl, ok := s.cacheTTL(s.now(), session.AbsoluteExpiresAt)
	if !ok {
		return
	}
	if user.SessionEpoch < 0 {
		return
	}
	raw, err := encodeHotSession(session, user, device, user.SessionEpoch)
	if err != nil {
		return
	}
	key := sessionHotKey(session.TokenHash)
	if key == "" {
		return
	}
	_ = s.hot.Set(ctx, key, raw, ttl)
	s.writeCachedEpoch(ctx, session.UserID, user.SessionEpoch, ttl)
}

func (s *Sessions) invalidateHot(ctx context.Context, tokenHash []byte) {
	if s.hot == nil {
		return
	}
	key := sessionHotKey(tokenHash)
	if key == "" {
		return
	}
	_ = s.hot.Delete(ctx, key)
}

func (s *Sessions) writeCachedEpoch(ctx context.Context, userID ID, epoch int64, ttl time.Duration) {
	if s.hot == nil || userID.IsZero() || epoch < 0 || ttl < time.Millisecond {
		return
	}
	_ = s.hot.Set(ctx, sessionEpochKey(userID), strconv.FormatInt(epoch, 10), ttl)
}

func (s *Sessions) readCachedEpoch(ctx context.Context, userID ID) (int64, bool) {
	if s.hot == nil || userID.IsZero() {
		return 0, false
	}
	raw, err := s.hot.Get(ctx, sessionEpochKey(userID))
	if err != nil {
		return 0, false
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func isCacheUnavailable(err error) bool {
	return err != nil && (errors.Is(err, cache.ErrUnavailable) || errors.Is(err, errUnavailable))
}
