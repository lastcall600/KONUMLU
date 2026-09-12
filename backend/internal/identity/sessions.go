package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"
)

var (
	errInvalidPolicy   = errors.New("invalid session policy")
	errUnauthenticated = errors.New("unauthenticated")
	errUnavailable     = errors.New("session store unavailable")
	errNotFound        = errors.New("not found")
	errStoreRequired   = errors.New("session store required")
)

// IssuedSession is returned once at creation. RawToken must not be stored.
type IssuedSession struct {
	Session  Session
	RawToken string
}

// Sessions is the durable browser-session lifecycle (PostgreSQL SoT).
// Valkey is an optional disposable hot cache keyed by token hash.
type Sessions struct {
	store       sessionStore
	hot         sessionHotCache
	policy      SessionPolicy
	cachePolicy SessionCachePolicy
	now         func() time.Time
}

func NewSessions(store sessionStore, policy SessionPolicy, hot sessionHotCache, cachePolicy SessionCachePolicy, now func() time.Time) (*Sessions, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if err := cachePolicy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Sessions{store: store, hot: hot, policy: policy, cachePolicy: cachePolicy, now: now}, nil
}

func (s *Sessions) Create(ctx context.Context, userID ID, deviceID *ID) (IssuedSession, error) {
	now := s.now()
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return IssuedSession{}, mapLookupErr(err, errAccountIneligible)
	}
	if !user.EligibleForSession() || user.ID != userID {
		return IssuedSession{}, errAccountIneligible
	}

	var device *Device
	if deviceID != nil {
		d, derr := s.store.GetDevice(ctx, *deviceID)
		if derr != nil {
			return IssuedSession{}, mapLookupErr(derr, errDeviceRevoked)
		}
		if !d.Active() || d.UserID != userID {
			return IssuedSession{}, errDeviceRevoked
		}
		device = &d
	}

	raw, hash, err := GenerateSessionToken()
	if err != nil {
		return IssuedSession{}, errUnavailable
	}

	id, err := NewID()
	if err != nil {
		return IssuedSession{}, errUnavailable
	}

	absolute := now.Add(s.policy.Absolute)
	session := Session{
		ID:                id,
		UserID:            userID,
		DeviceID:          deviceID,
		TokenHash:         hash,
		CreatedAt:         now,
		LastSeenAt:        now,
		IdleExpiresAt:     s.policy.idleExpiresAt(now, absolute),
		AbsoluteExpiresAt: absolute,
	}
	if err := AuthorizeSession(now, user, device, session); err != nil {
		return IssuedSession{}, err
	}
	if err := s.store.InsertSession(ctx, session); err != nil {
		return IssuedSession{}, mapStoreErr(err)
	}
	return IssuedSession{Session: session, RawToken: raw}, nil
}

func (s *Sessions) Resolve(ctx context.Context, rawToken string) (Session, error) {
	secret, err := decodeSessionToken(rawToken)
	if err != nil {
		return Session{}, errUnauthenticated
	}
	hash, err := HashSessionSecret(secret)
	if err != nil {
		return Session{}, errUnauthenticated
	}

	if cached, hit, err := s.resolveHot(ctx, hash); hit {
		if err == nil {
			if terr := s.maybeTouch(ctx, cached); terr != nil {
				return Session{}, terr
			}
		}
		return cached, err
	}

	session, err := s.store.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		return Session{}, mapLookupErr(err, errUnauthenticated)
	}
	if subtle.ConstantTimeCompare(session.TokenHash, hash) != 1 {
		return Session{}, errUnauthenticated
	}

	user, device, err := s.loadPrincipal(ctx, session)
	if err != nil {
		return Session{}, err
	}
	if err := AuthorizeSession(s.now(), user, device, session); err != nil {
		return Session{}, err
	}
	s.populateHot(ctx, session, user, device)
	if err := s.maybeTouch(ctx, session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Sessions) maybeTouch(ctx context.Context, session Session) error {
	now := s.now()
	if !session.LastSeenAt.IsZero() && now.Sub(session.LastSeenAt) < s.policy.TouchQuantum() {
		return nil
	}
	err := s.Touch(ctx, session.ID)
	if err == nil {
		return nil
	}
	if errors.Is(err, errSessionRevoked) || errors.Is(err, errSessionIdleExpired) ||
		errors.Is(err, errSessionAbsExpired) || errors.Is(err, errAccountIneligible) ||
		errors.Is(err, errDeviceRevoked) || errors.Is(err, errUnauthenticated) {
		return err
	}
	return nil
}

func (s *Sessions) Touch(ctx context.Context, sessionID ID) error {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return mapLookupErr(err, errUnauthenticated)
	}
	if err := s.authorizeStored(ctx, session); err != nil {
		return err
	}
	now := s.now()
	if err := mapStoreErr(s.store.UpdateSessionActivity(ctx, sessionID, now, s.policy.idleExpiresAt(now, session.AbsoluteExpiresAt))); err != nil {
		return err
	}
	s.invalidateHot(ctx, session.TokenHash)
	return nil
}

func (s *Sessions) ListActiveForUser(ctx context.Context, userID ID) ([]Session, error) {
	if userID.IsZero() {
		return nil, errZeroID
	}
	list, err := s.store.ListSessionsForUser(ctx, userID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	now := s.now()
	out := make([]Session, 0, len(list))
	for _, session := range list {
		if session.UserID != userID {
			continue
		}
		if err := session.DurableValid(now); err != nil {
			continue
		}
		session.TokenHash = nil
		out = append(out, session)
	}
	return out, nil
}

func (s *Sessions) RevokeOthers(ctx context.Context, userID, keepSessionID ID) error {
	if userID.IsZero() || keepSessionID.IsZero() {
		return errZeroID
	}
	keep, err := s.store.GetSession(ctx, keepSessionID)
	if err != nil {
		return mapLookupErr(err, errNotFound)
	}
	if keep.UserID != userID {
		return errNotFound
	}
	if err := s.authorizeStored(ctx, keep); err != nil {
		return err
	}
	hashes, err := s.store.RevokeOtherSessionsForUser(ctx, userID, keepSessionID, s.now())
	if err != nil {
		return mapStoreErr(err)
	}
	for _, hash := range hashes {
		s.invalidateHot(ctx, hash)
	}
	return nil
}

func (s *Sessions) GetOwned(ctx context.Context, actorUserID, sessionID ID) (Session, error) {
	if actorUserID.IsZero() || sessionID.IsZero() {
		return Session{}, errZeroID
	}
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, mapLookupErr(err, errNotFound)
	}
	if session.UserID != actorUserID {
		return Session{}, errNotFound
	}
	session.TokenHash = nil
	return session, nil
}

func (s *Sessions) Revoke(ctx context.Context, sessionID ID) error {
	if sessionID.IsZero() {
		return errZeroID
	}
	if err := mapStoreErr(s.store.RevokeSession(ctx, sessionID, s.now())); err != nil {
		return err
	}
	if session, err := s.store.GetSession(ctx, sessionID); err == nil {
		s.invalidateHot(ctx, session.TokenHash)
	}
	return nil
}

func (s *Sessions) RevokeAllForUser(ctx context.Context, userID ID) error {
	if userID.IsZero() {
		return errZeroID
	}
	epoch, err := s.store.RevokeSessionsForUser(ctx, userID, s.now())
	if err != nil {
		return mapStoreErr(err)
	}
	s.ApplyUserEpoch(ctx, userID, epoch)
	return nil
}

// ApplyUserEpoch is best-effort Valkey epoch publish after durable revocation.
func (s *Sessions) ApplyUserEpoch(ctx context.Context, userID ID, epoch int64) {
	if s == nil {
		return
	}
	ttl, ok := s.cacheTTL(s.now(), s.now().Add(s.policy.Absolute))
	if ok {
		s.writeCachedEpoch(ctx, userID, epoch, ttl)
	}
}

func (s *Sessions) authorizeStored(ctx context.Context, session Session) error {
	user, device, err := s.loadPrincipal(ctx, session)
	if err != nil {
		return err
	}
	return AuthorizeSession(s.now(), user, device, session)
}

func (s *Sessions) loadPrincipal(ctx context.Context, session Session) (User, *Device, error) {
	user, err := s.store.GetUser(ctx, session.UserID)
	if err != nil {
		return User{}, nil, mapLookupErr(err, errAccountIneligible)
	}
	if session.DeviceID == nil {
		return user, nil, nil
	}
	d, derr := s.store.GetDevice(ctx, *session.DeviceID)
	if derr != nil {
		return User{}, nil, mapLookupErr(derr, errDeviceRevoked)
	}
	return user, &d, nil
}

func mapLookupErr(err, missing error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) {
		return missing
	}
	return mapStoreErr(err)
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errNotFound) || errors.Is(err, errZeroID) ||
		errors.Is(err, errSessionRevoked) || errors.Is(err, errUnauthenticated) ||
		errors.Is(err, errSessionIdleExpired) || errors.Is(err, errSessionAbsExpired) {
		return err
	}
	return errUnavailable
}
