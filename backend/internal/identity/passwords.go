package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Maximum accepted password size. Composition rules are not applied yet (ADR-004 OI-004-06).
const MaxPasswordBytes = 4096

const (
	phcArgon2idID = "argon2id"
	phcVersion19  = 19
)

// PasswordVerifyResult is returned only after a successful Argon2id verify.
type PasswordVerifyResult struct {
	NeedsRehash bool
}

// Passwords is the Argon2id fallback credential lifecycle (no HTTP, no session issuance).
type Passwords struct {
	store    passwordStore
	policy   PasswordPolicy
	now      func() time.Time
	dummyPHC string
}

func NewPasswords(store passwordStore, policy PasswordPolicy, now func() time.Time) (*Passwords, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	dummySecret := make([]byte, policy.KeyLen)
	if _, err := rand.Read(dummySecret); err != nil {
		return nil, errUnavailable
	}
	dummyPHC, err := hashPassword(dummySecret, policy)
	if err != nil {
		return nil, err
	}
	return &Passwords{store: store, policy: policy, now: now, dummyPHC: dummyPHC}, nil
}

func (p *Passwords) Set(ctx context.Context, userID ID, password []byte) error {
	if userID.IsZero() {
		return errZeroID
	}
	if err := checkPasswordInput(password); err != nil {
		return err
	}
	user, err := p.store.GetUser(ctx, userID)
	if err != nil {
		return mapLookupErr(err, errAccountIneligible)
	}
	if !user.EligibleForSession() || user.ID != userID {
		return errAccountIneligible
	}

	encoded, err := hashPassword(password, p.policy)
	if err != nil {
		return err
	}
	now := p.now()
	existing, err := p.store.GetPasswordCredential(ctx, userID)
	if err != nil && !errors.Is(err, errNotFound) {
		return mapPasswordStoreErr(err)
	}
	cred := PasswordCredential{
		UserID:       userID,
		PasswordHash: encoded,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err == nil {
		cred.CreatedAt = existing.CreatedAt
	}
	return mapPasswordStoreErr(p.store.UpsertPasswordCredential(ctx, cred))
}

func (p *Passwords) hashForCreate(password []byte) (string, error) {
	if p == nil {
		return "", errStoreRequired
	}
	if err := checkPasswordInput(password); err != nil {
		return "", err
	}
	return hashPassword(password, p.policy)
}

func (p *Passwords) Verify(ctx context.Context, userID ID, password []byte) (PasswordVerifyResult, error) {
	if userID.IsZero() {
		return PasswordVerifyResult{}, errZeroID
	}
	if len(password) > MaxPasswordBytes {
		return PasswordVerifyResult{}, errUnauthenticated
	}

	user, err := p.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return PasswordVerifyResult{}, errUnauthenticated
		}
		return PasswordVerifyResult{}, mapPasswordStoreErr(err)
	}
	if !user.EligibleForSession() || user.ID != userID {
		return PasswordVerifyResult{}, errAccountIneligible
	}

	cred, err := p.store.GetPasswordCredential(ctx, userID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return PasswordVerifyResult{}, errUnauthenticated
		}
		return PasswordVerifyResult{}, mapPasswordStoreErr(err)
	}
	if !cred.Active() || cred.UserID != userID {
		return PasswordVerifyResult{}, errUnauthenticated
	}

	decoded, err := decodePHC(cred.PasswordHash)
	if err != nil {
		return PasswordVerifyResult{}, errMalformedPasswordHash
	}
	if !verifyPHC(password, decoded) {
		return PasswordVerifyResult{}, errUnauthenticated
	}
	return PasswordVerifyResult{NeedsRehash: decoded.needsRehash(p.policy)}, nil
}

// DummyVerify runs an Argon2id verification-equivalent against a process-local PHC.
// It never reads or writes stored credentials.
func (p *Passwords) DummyVerify(password []byte) {
	if p == nil || p.dummyPHC == "" {
		return
	}
	if len(password) > MaxPasswordBytes {
		return
	}
	decoded, err := decodePHC(p.dummyPHC)
	if err != nil {
		return
	}
	_ = verifyPHC(password, decoded)
}

func (p *Passwords) Disable(ctx context.Context, userID ID) error {
	if userID.IsZero() {
		return errZeroID
	}
	return mapPasswordStoreErr(p.store.DisablePasswordCredential(ctx, userID, p.now()))
}

func checkPasswordInput(password []byte) error {
	if len(password) == 0 {
		return errInvalidPassword
	}
	if len(password) > MaxPasswordBytes {
		return errPasswordTooLong
	}
	return nil
}

type phcArgon2id struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

func verifyPHC(password []byte, decoded phcArgon2id) bool {
	computed := argon2.IDKey(password, decoded.salt, decoded.iterations, decoded.memoryKiB, decoded.parallelism, uint32(len(decoded.key)))
	return subtle.ConstantTimeCompare(computed, decoded.key) == 1
}

func hashPassword(password []byte, policy PasswordPolicy) (string, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	salt := make([]byte, policy.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", errUnavailable
	}
	key := argon2.IDKey(password, salt, policy.Iterations, policy.MemoryKiB, policy.Parallelism, policy.KeyLen)
	return encodePHC(phcArgon2id{
		memoryKiB:   policy.MemoryKiB,
		iterations:  policy.Iterations,
		parallelism: policy.Parallelism,
		salt:        salt,
		key:         key,
	}), nil
}

func encodePHC(h phcArgon2id) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		phcArgon2idID, phcVersion19, h.memoryKiB, h.iterations, h.parallelism,
		base64.RawStdEncoding.EncodeToString(h.salt),
		base64.RawStdEncoding.EncodeToString(h.key),
	)
}

func decodePHC(encoded string) (phcArgon2id, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	if parts[1] != phcArgon2idID {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	if parts[2] != "v="+strconv.Itoa(phcVersion19) {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	memory, err := parsePHCU32(params[0], "m")
	if err != nil {
		return phcArgon2id{}, err
	}
	iterations, err := parsePHCU32(params[1], "t")
	if err != nil {
		return phcArgon2id{}, err
	}
	parallelism64, err := parsePHCU32(params[2], "p")
	if err != nil {
		return phcArgon2id{}, err
	}
	if parallelism64 == 0 || parallelism64 > 255 {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	if memory == 0 || iterations == 0 {
		return phcArgon2id{}, errMalformedPasswordHash
	}
	return phcArgon2id{
		memoryKiB:   memory,
		iterations:  iterations,
		parallelism: uint8(parallelism64),
		salt:        salt,
		key:         key,
	}, nil
}

func parsePHCU32(part, key string) (uint32, error) {
	prefix := key + "="
	if !strings.HasPrefix(part, prefix) {
		return 0, errMalformedPasswordHash
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(part, prefix), 10, 32)
	if err != nil {
		return 0, errMalformedPasswordHash
	}
	return uint32(n), nil
}

func (h phcArgon2id) needsRehash(policy PasswordPolicy) bool {
	return h.memoryKiB != policy.MemoryKiB ||
		h.iterations != policy.Iterations ||
		h.parallelism != policy.Parallelism ||
		uint32(len(h.salt)) != policy.SaltLen ||
		uint32(len(h.key)) != policy.KeyLen
}

func mapPasswordStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errAccountIneligible) || errors.Is(err, errUnauthenticated) ||
		errors.Is(err, errMalformedPasswordHash) || errors.Is(err, errInvalidPassword) ||
		errors.Is(err, errPasswordTooLong) || errors.Is(err, errInvalidPasswordPolicy) {
		return err
	}
	return errUnavailable
}
