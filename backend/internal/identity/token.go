package identity

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
)

// GenerateSessionToken returns a URL-safe opaque secret and its SHA-256 hash.
// The raw token is transient: callers must not persist or log it.
func GenerateSessionToken() (raw string, hash []byte, err error) {
	secret := make([]byte, MinSessionSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, err
	}
	hash, err = HashSessionSecret(secret)
	if err != nil {
		return "", nil, err
	}
	return base64.RawURLEncoding.EncodeToString(secret), hash, nil
}

// GenerateCeremonyToken returns a URL-safe opaque ceremony secret and its SHA-256 hash.
// The raw token is transient: callers must not persist or log it.
func GenerateCeremonyToken() (raw string, hash []byte, err error) {
	return GenerateSessionToken()
}

func decodeSessionToken(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errUnauthenticated
	}
	secret, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(secret) < MinSessionSecretBytes {
		return nil, errUnauthenticated
	}
	return secret, nil
}

func decodeCeremonyToken(raw string) ([]byte, error) {
	secret, err := decodeSessionToken(raw)
	if err != nil {
		return nil, errInvalidCeremony
	}
	return secret, nil
}

// GenerateEmailVerificationToken returns a high-entropy URL-safe secret and its SHA-256 hash.
// The raw token is transient: callers must not persist or log it.
func GenerateEmailVerificationToken() (raw string, hash []byte, err error) {
	raw, _, err = GenerateSessionToken()
	if err != nil {
		return "", nil, err
	}
	secret, err := decodeSessionToken(raw)
	if err != nil {
		return "", nil, err
	}
	hash, err = HashVerificationSecret(secret)
	if err != nil {
		return "", nil, err
	}
	return raw, hash, nil
}

// GeneratePhoneOTP returns a cryptographically random numeric OTP of the given width and its SHA-256 hash.
// The raw OTP is transient: callers must not persist or log it.
func GeneratePhoneOTP(digits int) (raw string, hash []byte, err error) {
	raw, err = generateNumericOTP(digits)
	if err != nil {
		return "", nil, err
	}
	hash, err = HashVerificationSecret([]byte(raw))
	if err != nil {
		return "", nil, err
	}
	return raw, hash, nil
}

func generateNumericOTP(digits int) (string, error) {
	if digits <= 0 || digits > 12 {
		return "", errInvalidChallengePolicy
	}
	modulus := uint64(1)
	for i := 0; i < digits; i++ {
		if modulus > math.MaxUint64/10 {
			return "", errInvalidChallengePolicy
		}
		modulus *= 10
	}
	limit := (math.MaxUint64 / modulus) * modulus
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		v := binary.BigEndian.Uint64(buf[:])
		if v >= limit {
			continue
		}
		return fmt.Sprintf("%0*d", digits, v%modulus), nil
	}
}

func hashSubmittedChallengeSecret(kind IdentifierKind, raw string) ([]byte, error) {
	switch kind {
	case IdentifierEmail:
		secret, err := decodeSessionToken(raw)
		if err != nil {
			return nil, errInvalidChallenge
		}
		return HashVerificationSecret(secret)
	case IdentifierPhone:
		return HashVerificationSecret([]byte(raw))
	default:
		return nil, errInvalidChallenge
	}
}
