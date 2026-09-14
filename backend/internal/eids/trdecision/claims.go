package trdecision

import (
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	SchemaV1            = "tr-decision-v1"
	AudienceGermanyV1   = "konumlu-germany-eids-v1"
	signingContextV1    = "KONUMLU:TR-COMPLIANCE:DECISION:V1"
	timeLayout          = "2006-01-02T15:04:05.000000000Z"
	maxKeyIDLen         = 64
	subjectRefHexLen    = 64
	minDecisionIDLen    = 16
	maxDecisionIDLen    = 128
)

var (
	ErrInvalidClaims     = errors.New("tr decision claims invalid")
	ErrUnsupportedSchema = errors.New("tr decision schema unsupported")
	ErrUnsupportedType   = errors.New("tr decision verification_type unsupported")
	ErrUnsupportedStatus = errors.New("tr decision status unsupported")
	ErrInvalidAudience   = errors.New("tr decision audience invalid")
	ErrInvalidKeyID      = errors.New("tr decision key_id invalid")
	ErrInvalidTime       = errors.New("tr decision time invalid")
)

// Status is the coarse TR→DE decision class. Not an EİDS product status.
type Status string

const (
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

func (s Status) Valid() bool {
	switch s {
	case StatusApproved, StatusRejected:
		return true
	default:
		return false
	}
}

type VerificationType string

const (
	TypeProperty VerificationType = "property"
	TypeVehicle  VerificationType = "vehicle"
)

func (t VerificationType) Valid() bool {
	switch t {
	case TypeProperty, TypeVehicle:
		return true
	default:
		return false
	}
}

// ClaimsV1 is the typed V1 decision document. It must not carry TCKN,
// address, birth date, provider tokens, or raw official payloads.
type ClaimsV1 struct {
	SchemaVersion    string
	VerificationType VerificationType
	Status           Status
	DecisionID       string
	SubjectRef       string
	IssuedAt         time.Time
	ValidUntil       time.Time
	Audience         string
}

func (c ClaimsV1) Validate() error {
	if c.SchemaVersion != SchemaV1 {
		return ErrUnsupportedSchema
	}
	if !c.VerificationType.Valid() {
		return ErrUnsupportedType
	}
	if !c.Status.Valid() {
		return ErrUnsupportedStatus
	}
	if err := validateDecisionID(c.DecisionID); err != nil {
		return err
	}
	if err := validateSubjectRef(c.SubjectRef); err != nil {
		return err
	}
	if strings.TrimSpace(c.Audience) != AudienceGermanyV1 {
		return ErrInvalidAudience
	}
	if c.IssuedAt.IsZero() || c.ValidUntil.IsZero() {
		return ErrInvalidTime
	}
	issued := c.IssuedAt.UTC()
	until := c.ValidUntil.UTC()
	if !until.After(issued) {
		return ErrInvalidTime
	}
	return nil
}

// SigningBytes is the repository-owned V1 canonical encoding. Field order is
// fixed. key_id is bound into the signed context. Map iteration is never used.
func (c ClaimsV1) SigningBytes(keyID string) ([]byte, error) {
	if err := validateKeyID(keyID); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.Grow(512)
	b.WriteString(signingContextV1)
	b.WriteByte(0)
	writeField(&b, "key_id", keyID)
	writeField(&b, "schema_version", c.SchemaVersion)
	writeField(&b, "verification_type", string(c.VerificationType))
	writeField(&b, "status", string(c.Status))
	writeField(&b, "decision_id", c.DecisionID)
	writeField(&b, "subject_ref", c.SubjectRef)
	writeField(&b, "issued_at", formatTime(c.IssuedAt))
	writeField(&b, "valid_until", formatTime(c.ValidUntil))
	writeField(&b, "audience", c.Audience)
	return []byte(b.String()), nil
}

func (c ClaimsV1) CanonicalHash(keyID string) ([32]byte, error) {
	raw, err := c.SigningBytes(keyID)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func writeField(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteByte('=')
	b.WriteString(value)
	b.WriteByte(0)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func validateKeyID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxKeyIDLen {
		return ErrInvalidKeyID
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ErrInvalidKeyID
	}
	return nil
}

func validateDecisionID(id string) error {
	id = strings.TrimSpace(id)
	if len(id) < minDecisionIDLen || len(id) > maxDecisionIDLen {
		return ErrInvalidClaims
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			continue
		}
		return ErrInvalidClaims
	}
	return nil
}

func validateSubjectRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if len(ref) != subjectRefHexLen {
		return ErrInvalidClaims
	}
	for _, r := range ref {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return ErrInvalidClaims
	}
	return nil
}

func parseVerificationType(raw string) (VerificationType, error) {
	t := VerificationType(strings.TrimSpace(raw))
	if !t.Valid() {
		return "", ErrUnsupportedType
	}
	return t, nil
}

func parseStatus(raw string) (Status, error) {
	s := Status(strings.TrimSpace(raw))
	if !s.Valid() {
		return "", ErrUnsupportedStatus
	}
	return s, nil
}

