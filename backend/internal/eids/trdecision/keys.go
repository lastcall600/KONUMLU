package trdecision

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
)

var (
	ErrInvalidKey     = errors.New("tr decision key invalid")
	ErrUnknownKey     = errors.New("tr decision key unknown")
	ErrInvalidSignature = errors.New("tr decision signature invalid")
	ErrMalformedKey   = errors.New("tr decision key malformed")
)

// PrivateKey is a TR-only Ed25519 signing key. It must never appear in
// Germany config, logs, or database rows.
type PrivateKey struct {
	keyID string
	key   ed25519.PrivateKey
}

func (PrivateKey) String() string   { return "trdecision.PrivateKey" }
func (PrivateKey) GoString() string { return "trdecision.PrivateKey{}" }
func (PrivateKey) LogValue() slog.Value {
	return slog.StringValue("trdecision.PrivateKey")
}

func (k PrivateKey) KeyID() string { return k.keyID }

func ParsePrivateKey(keyID, raw string) (PrivateKey, error) {
	if err := validateKeyID(keyID); err != nil {
		return PrivateKey{}, err
	}
	b, err := decodeKeyBytes(raw)
	if err != nil {
		return PrivateKey{}, ErrMalformedKey
	}
	var priv ed25519.PrivateKey
	switch len(b) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(b)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(append(ed25519.PrivateKey(nil), b...))
		if !ed25519.NewKeyFromSeed(priv.Seed()).Equal(priv) {
			return PrivateKey{}, ErrMalformedKey
		}
	default:
		return PrivateKey{}, ErrMalformedKey
	}
	return PrivateKey{keyID: strings.TrimSpace(keyID), key: priv}, nil
}

func GeneratePrivateKey(keyID string) (PrivateKey, error) {
	if err := validateKeyID(keyID); err != nil {
		return PrivateKey{}, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKey{}, ErrInvalidKey
	}
	return PrivateKey{keyID: strings.TrimSpace(keyID), key: priv}, nil
}

func (k PrivateKey) Public() (PublicKey, error) {
	if len(k.key) != ed25519.PrivateKeySize {
		return PublicKey{}, ErrInvalidKey
	}
	pub, ok := k.key.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return PublicKey{}, ErrInvalidKey
	}
	return PublicKey{keyID: k.keyID, key: pub}, nil
}

func (k PrivateKey) Sign(claims ClaimsV1) (Envelope, error) {
	if len(k.key) != ed25519.PrivateKeySize {
		return Envelope{}, ErrInvalidKey
	}
	msg, err := claims.SigningBytes(k.keyID)
	if err != nil {
		return Envelope{}, err
	}
	sig := ed25519.Sign(k.key, msg)
	return Envelope{
		KeyID:      k.keyID,
		Claims:     claims,
		Signature:  base64.RawURLEncoding.EncodeToString(sig),
		signatureB: sig,
	}, nil
}

// PublicKey is a Germany-trusted verification key.
type PublicKey struct {
	keyID string
	key   ed25519.PublicKey
}

func (PublicKey) String() string   { return "trdecision.PublicKey" }
func (PublicKey) GoString() string { return "trdecision.PublicKey{}" }
func (p PublicKey) KeyID() string  { return p.keyID }

func ParsePublicKey(keyID, raw string) (PublicKey, error) {
	if err := validateKeyID(keyID); err != nil {
		return PublicKey{}, err
	}
	b, err := decodeKeyBytes(raw)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return PublicKey{}, ErrMalformedKey
	}
	return PublicKey{keyID: strings.TrimSpace(keyID), key: ed25519.PublicKey(b)}, nil
}

func (p PublicKey) Encoded() string {
	if len(p.key) != ed25519.PublicKeySize {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(p.key)
}

type PublicRing struct {
	keys map[string]ed25519.PublicKey
}

func (PublicRing) String() string   { return "trdecision.PublicRing" }
func (PublicRing) GoString() string { return "trdecision.PublicRing{}" }
func (PublicRing) LogValue() slog.Value {
	return slog.StringValue("trdecision.PublicRing")
}

func NewPublicRing(keys []PublicKey) (PublicRing, error) {
	if len(keys) == 0 {
		return PublicRing{}, ErrInvalidKey
	}
	out := make(map[string]ed25519.PublicKey, len(keys))
	for _, k := range keys {
		if err := validateKeyID(k.keyID); err != nil {
			return PublicRing{}, err
		}
		if len(k.key) != ed25519.PublicKeySize {
			return PublicRing{}, ErrMalformedKey
		}
		if _, dup := out[k.keyID]; dup {
			return PublicRing{}, ErrInvalidKey
		}
		cloned := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(cloned, k.key)
		out[k.keyID] = cloned
	}
	return PublicRing{keys: out}, nil
}

func (r PublicRing) Len() int { return len(r.keys) }

func (r PublicRing) KeyIDs() []string {
	ids := make([]string, 0, len(r.keys))
	for id := range r.keys {
		ids = append(ids, id)
	}
	return ids
}

func decodeKeyBytes(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrMalformedKey
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil && (len(b) == ed25519.PublicKeySize || len(b) == ed25519.PrivateKeySize || len(b) == ed25519.SeedSize) {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && (len(b) == ed25519.PublicKeySize || len(b) == ed25519.PrivateKeySize || len(b) == ed25519.SeedSize) {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && (len(b) == ed25519.PublicKeySize || len(b) == ed25519.PrivateKeySize || len(b) == ed25519.SeedSize) {
		return b, nil
	}
	return nil, ErrMalformedKey
}

func ParseTrustedRing(raw string) (PublicRing, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return PublicRing{}, ErrInvalidKey
	}
	parts := strings.Split(raw, ",")
	keys := make([]PublicKey, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kid, enc, ok := strings.Cut(part, ":")
		if !ok {
			return PublicRing{}, ErrMalformedKey
		}
		pk, err := ParsePublicKey(strings.TrimSpace(kid), strings.TrimSpace(enc))
		if err != nil {
			return PublicRing{}, err
		}
		keys = append(keys, pk)
	}
	return NewPublicRing(keys)
}

func EncodePrivateSeed(k PrivateKey) (string, error) {
	if len(k.key) != ed25519.PrivateKeySize {
		return "", ErrInvalidKey
	}
	return base64.RawURLEncoding.EncodeToString(k.key.Seed()), nil
}
