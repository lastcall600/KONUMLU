package trdecision

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const maxEnvelopeBytes = 16 << 10

var (
	ErrMalformedEnvelope = errors.New("tr decision envelope malformed")
	ErrForbiddenField    = errors.New("tr decision forbidden field")
)

// Envelope is the signed transport document. Algorithm is fixed to Ed25519
// by schema_version. Clients cannot select alg.
type Envelope struct {
	KeyID      string   `json:"key_id"`
	Claims     ClaimsV1 `json:"claims"`
	Signature  string   `json:"signature"`
	signatureB []byte
}

type wireEnvelope struct {
	KeyID     string          `json:"key_id"`
	Claims    json.RawMessage `json:"claims"`
	Signature string          `json:"signature"`
}

type wireClaims struct {
	SchemaVersion    string `json:"schema_version"`
	VerificationType string `json:"verification_type"`
	Status           string `json:"status"`
	DecisionID       string `json:"decision_id"`
	SubjectRef       string `json:"subject_ref"`
	IssuedAt         string `json:"issued_at"`
	ValidUntil       string `json:"valid_until"`
	Audience         string `json:"audience"`
}

func DecodeEnvelope(r io.Reader) (Envelope, error) {
	if r == nil {
		return Envelope{}, ErrMalformedEnvelope
	}
	limited := io.LimitReader(r, maxEnvelopeBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return Envelope{}, ErrMalformedEnvelope
	}
	if len(raw) == 0 || len(raw) > maxEnvelopeBytes {
		return Envelope{}, ErrMalformedEnvelope
	}
	if err := rejectForbiddenJSON(raw); err != nil {
		return Envelope{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var wire wireEnvelope
	if err := dec.Decode(&wire); err != nil {
		return Envelope{}, ErrMalformedEnvelope
	}
	if dec.More() {
		return Envelope{}, ErrMalformedEnvelope
	}
	if strings.TrimSpace(wire.KeyID) == "" || strings.TrimSpace(wire.Signature) == "" || len(wire.Claims) == 0 {
		return Envelope{}, ErrMalformedEnvelope
	}
	claims, err := decodeClaims(wire.Claims)
	if err != nil {
		return Envelope{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(wire.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Envelope{}, ErrMalformedEnvelope
	}
	return Envelope{
		KeyID:      strings.TrimSpace(wire.KeyID),
		Claims:     claims,
		Signature:  strings.TrimSpace(wire.Signature),
		signatureB: sig,
	}, nil
}

func decodeClaims(raw json.RawMessage) (ClaimsV1, error) {
	if err := rejectForbiddenJSON(raw); err != nil {
		return ClaimsV1{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w wireClaims
	if err := dec.Decode(&w); err != nil {
		return ClaimsV1{}, ErrMalformedEnvelope
	}
	kind, err := parseVerificationType(w.VerificationType)
	if err != nil {
		return ClaimsV1{}, err
	}
	st, err := parseStatus(w.Status)
	if err != nil {
		return ClaimsV1{}, err
	}
	issued, err := parseClaimTime(w.IssuedAt)
	if err != nil {
		return ClaimsV1{}, err
	}
	until, err := parseClaimTime(w.ValidUntil)
	if err != nil {
		return ClaimsV1{}, err
	}
	c := ClaimsV1{
		SchemaVersion:    strings.TrimSpace(w.SchemaVersion),
		VerificationType: kind,
		Status:           st,
		DecisionID:       strings.TrimSpace(w.DecisionID),
		SubjectRef:       strings.TrimSpace(w.SubjectRef),
		IssuedAt:         issued,
		ValidUntil:       until,
		Audience:         strings.TrimSpace(w.Audience),
	}
	if err := c.Validate(); err != nil {
		return ClaimsV1{}, err
	}
	return c, nil
}

func parseClaimTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, ErrInvalidTime
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, ErrInvalidTime
}

func rejectForbiddenJSON(raw []byte) error {
	lower := strings.ToLower(string(raw))
	for _, needle := range []string{
		`"tckn"`, `"birth_date"`, `"birthdate"`, `"address"`, `"full_name"`,
		`"provider_token"`, `"access_token"`, `"raw_response"`, `"provider_payload"`,
		`"national_id"`, `"tc_kimlik"`, `"alg"`, `"approved":`,
	} {
		if strings.Contains(lower, needle) {
			return ErrForbiddenField
		}
	}
	return nil
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	type out struct {
		KeyID     string          `json:"key_id"`
		Claims    jsonClaimsWire  `json:"claims"`
		Signature string          `json:"signature"`
	}
	return json.Marshal(out{
		KeyID: e.KeyID,
		Claims: jsonClaimsWire{
			SchemaVersion:    e.Claims.SchemaVersion,
			VerificationType: string(e.Claims.VerificationType),
			Status:           string(e.Claims.Status),
			DecisionID:       e.Claims.DecisionID,
			SubjectRef:       e.Claims.SubjectRef,
			IssuedAt:         formatTime(e.Claims.IssuedAt),
			ValidUntil:       formatTime(e.Claims.ValidUntil),
			Audience:         e.Claims.Audience,
		},
		Signature: e.Signature,
	})
}

type jsonClaimsWire struct {
	SchemaVersion    string `json:"schema_version"`
	VerificationType string `json:"verification_type"`
	Status           string `json:"status"`
	DecisionID       string `json:"decision_id"`
	SubjectRef       string `json:"subject_ref"`
	IssuedAt         string `json:"issued_at"`
	ValidUntil       string `json:"valid_until"`
	Audience         string `json:"audience"`
}
