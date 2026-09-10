package publicprofile

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"backend/internal/identity"
)

const MaxDisplayNameRunes = 80

type ModerationState string

const (
	ModerationNone       ModerationState = "none"
	ModerationRestricted ModerationState = "restricted"
	ModerationRemoved    ModerationState = "removed"
)

var (
	ErrUnavailable            = errors.New("public profile unavailable")
	ErrNotFound               = errors.New("public profile not found")
	ErrInvalidPublicID        = errors.New("invalid public profile id")
	ErrInvalidDisplayName     = errors.New("invalid display name")
	ErrStoreRequired          = errors.New("public profile store required")
	ErrInvalidModerationState = errors.New("invalid public profile moderation state")
	ErrModerationNotEnforced  = errors.New("public profile moderation state is not an enforceable restriction")
	errZeroID                 = errors.New("identity id must not be zero")
	errConflict               = errors.New("public profile conflict")
)

// Profile is Identity-owned public identity. It is not a login or legal identity record.
type Profile struct {
	UserID          identity.ID
	PublicProfileID identity.ID
	DisplayName     *string
	ModerationState ModerationState
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (p Profile) Validate() error {
	if p.UserID.IsZero() || p.PublicProfileID.IsZero() {
		return errZeroID
	}
	if p.UserID == p.PublicProfileID {
		return ErrUnavailable
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return ErrUnavailable
	}
	if p.DisplayName != nil {
		if _, err := NormalizeDisplayName(*p.DisplayName); err != nil {
			return err
		}
	}
	if _, err := ParseModerationState(string(p.ModerationState)); err != nil {
		return err
	}
	return nil
}

func ParseModerationState(raw string) (ModerationState, error) {
	switch ModerationState(strings.TrimSpace(raw)) {
	case "", ModerationNone:
		return ModerationNone, nil
	case ModerationRestricted, ModerationRemoved:
		return ModerationState(strings.TrimSpace(raw)), nil
	default:
		return "", ErrInvalidModerationState
	}
}

func (s ModerationState) Normalized() ModerationState {
	if s == "" {
		return ModerationNone
	}
	return s
}

func (s ModerationState) hidesPublic() bool {
	n := s.Normalized()
	return n == ModerationRestricted || n == ModerationRemoved
}

// ApplyModeration sets staff hide/remove without changing account/auth state.
// none is not applied here; restoration uses ClearModeration. Same-state apply is allowed.
func (p Profile) ApplyModeration(state ModerationState, now time.Time) (Profile, error) {
	parsed, err := ParseModerationState(string(state))
	if err != nil {
		return Profile{}, err
	}
	if parsed == ModerationNone {
		return Profile{}, ErrModerationNotEnforced
	}
	if now.Before(p.CreatedAt) {
		return Profile{}, ErrUnavailable
	}
	p.ModerationState = parsed
	p.UpdatedAt = now
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// ClearModeration resets staff visibility to none without changing account state.
// Already-none is idempotent and still bumps UpdatedAt.
func (p Profile) ClearModeration(now time.Time) (Profile, error) {
	if now.Before(p.CreatedAt) {
		return Profile{}, ErrUnavailable
	}
	p.ModerationState = ModerationNone
	p.UpdatedAt = now
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// PublicView is the only public-safe projection. Internal user id is omitted.
type PublicView struct {
	PublicProfileID identity.ID
	DisplayName     *string
	MemberSince     time.Time
}

func ParsePublicID(s string) (identity.ID, error) {
	id, err := identity.ParseID(s)
	if err != nil || id.IsZero() {
		return identity.ID{}, ErrInvalidPublicID
	}
	return id, nil
}

func NormalizeDisplayName(raw string) (*string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(trimmed) > MaxDisplayNameRunes {
		return nil, ErrInvalidDisplayName
	}
	for _, r := range trimmed {
		if r == '<' || r == '>' || r == 0x7f || unicode.IsControl(r) {
			return nil, ErrInvalidDisplayName
		}
	}
	out := trimmed
	return &out, nil
}
