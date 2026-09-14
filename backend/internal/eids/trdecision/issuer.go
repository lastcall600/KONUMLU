package trdecision

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrIssuerRequired = errors.New("tr decision issuer required")
	ErrProviderResult = errors.New("tr provider result invalid")
)

// ProviderResult is the trusted INTERNAL TR adapter outcome. It is never an
// HTTP client body. Official evidence stays on the TR side.
type ProviderResult struct {
	SubjectRef       string
	VerificationType VerificationType
	Status           Status
	ValidUntil       time.Time
}

func (r ProviderResult) Validate() error {
	if err := validateSubjectRef(r.SubjectRef); err != nil {
		return ErrProviderResult
	}
	if !r.VerificationType.Valid() || !r.Status.Valid() {
		return ErrProviderResult
	}
	if r.ValidUntil.IsZero() {
		return ErrProviderResult
	}
	return nil
}

type Issuer struct {
	key PrivateKey
	now func() time.Time
}

func NewIssuer(key PrivateKey, now func() time.Time) (*Issuer, error) {
	if len(key.key) != ed25519.PrivateKeySize || key.keyID == "" {
		return nil, ErrInvalidKey
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Issuer{key: key, now: now}, nil
}

func (i *Issuer) Issue(result ProviderResult) (Envelope, error) {
	if i == nil {
		return Envelope{}, ErrIssuerRequired
	}
	if err := result.Validate(); err != nil {
		return Envelope{}, err
	}
	now := i.now().UTC().Truncate(time.Second)
	until := result.ValidUntil.UTC().Truncate(time.Second)
	if !until.After(now) {
		return Envelope{}, ErrInvalidTime
	}
	id, err := newDecisionID()
	if err != nil {
		return Envelope{}, err
	}
	claims := ClaimsV1{
		SchemaVersion:    SchemaV1,
		VerificationType: result.VerificationType,
		Status:           result.Status,
		DecisionID:       id,
		SubjectRef:       result.SubjectRef,
		IssuedAt:         now,
		ValidUntil:       until,
		Audience:         AudienceGermanyV1,
	}
	return i.key.Sign(claims)
}

func newDecisionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrInvalidClaims
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:]), nil
}
