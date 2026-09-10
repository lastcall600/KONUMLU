package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"strings"
	"time"
)

const (
	aes256KeySize         = 32
	materialNonceSize     = 12
	materialMinCiphertext = 16
	materialAADPrefix     = "konumlu.identity.verification_material.v1"
)

// VerificationMaterial is AES-256-GCM sealed delivery secret. It never holds plaintext.
type VerificationMaterial struct {
	ChallengeID ID
	KeyID       string
	Nonce       []byte
	Ciphertext  []byte
	CreatedAt   time.Time
	DestroyedAt *time.Time
}

func (m VerificationMaterial) Validate() error {
	if m.ChallengeID.IsZero() {
		return errZeroID
	}
	if strings.TrimSpace(m.KeyID) == "" || m.KeyID != strings.TrimSpace(m.KeyID) {
		return errInvalidChallenge
	}
	if m.CreatedAt.IsZero() {
		return errInvalidChallenge
	}
	if m.DestroyedAt != nil {
		if m.DestroyedAt.Before(m.CreatedAt) {
			return errInvalidChallenge
		}
		if len(m.Nonce) != 0 || len(m.Ciphertext) != 0 {
			return errInvalidChallenge
		}
		return nil
	}
	if len(m.Nonce) != materialNonceSize || len(m.Ciphertext) < materialMinCiphertext {
		return errInvalidChallenge
	}
	return nil
}

func (m VerificationMaterial) Active() bool {
	return m.Validate() == nil && m.DestroyedAt == nil
}

// MaterialKeyring supplies AES-256 keys by id. Implementations must not hardcode production keys.
type MaterialKeyring interface {
	Active() (keyID string, key []byte, err error)
	Lookup(keyID string) ([]byte, error)
}

// Keyring is an injected in-memory key set. Active KeyID selects the sealing key.
// Old named keys stay in the map so ciphertext sealed before rotation can still
// be opened. Unknown key_id lookups fail closed. Callers must not generate keys.
type Keyring struct {
	activeID string
	keys     map[string][]byte
}

func NewKeyring(activeID string, keys map[string][]byte) (*Keyring, error) {
	activeID = strings.TrimSpace(activeID)
	if activeID == "" || len(keys) == 0 {
		return nil, errInvalidChallenge
	}
	cloned := make(map[string][]byte, len(keys))
	for id, key := range keys {
		id = strings.TrimSpace(id)
		if id == "" || len(key) != aes256KeySize {
			return nil, errInvalidChallenge
		}
		if _, dup := cloned[id]; dup {
			return nil, errInvalidChallenge
		}
		cloned[id] = cloneBytes(key)
	}
	if _, ok := cloned[activeID]; !ok {
		return nil, errInvalidChallenge
	}
	return &Keyring{activeID: activeID, keys: cloned}, nil
}

// NewMaterialProtectorFromDecodedKeys builds the runtime protector from already-decoded
// config. Parsing of environment encoding lives in platform/config so server and
// worker share one parse path. Keys are never generated here.
func NewMaterialProtectorFromDecodedKeys(activeID string, keys map[string][]byte) (*MaterialProtector, error) {
	kr, err := NewKeyring(activeID, keys)
	if err != nil {
		return nil, err
	}
	return NewMaterialProtector(kr)
}

func (k *Keyring) Active() (string, []byte, error) {
	if k == nil {
		return "", nil, errStoreRequired
	}
	key, err := k.Lookup(k.activeID)
	if err != nil {
		return "", nil, err
	}
	return k.activeID, key, nil
}

func (k *Keyring) Lookup(keyID string) ([]byte, error) {
	if k == nil {
		return nil, errStoreRequired
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return nil, errUnavailable
	}
	key, ok := k.keys[keyID]
	if !ok || len(key) != aes256KeySize {
		return nil, errUnavailable
	}
	return cloneBytes(key), nil
}

// MaterialProtector seals and opens verification secrets with AES-256-GCM.
type MaterialProtector struct {
	keys MaterialKeyring
}

func NewMaterialProtector(keys MaterialKeyring) (*MaterialProtector, error) {
	if keys == nil {
		return nil, errStoreRequired
	}
	if _, key, err := keys.Active(); err != nil {
		return nil, err
	} else if len(key) != aes256KeySize {
		return nil, errUnavailable
	}
	return &MaterialProtector{keys: keys}, nil
}

func (p *MaterialProtector) Seal(challenge VerificationChallenge, plaintext []byte) (VerificationMaterial, error) {
	if p == nil || p.keys == nil {
		return VerificationMaterial{}, errStoreRequired
	}
	if err := challenge.Validate(); err != nil {
		return VerificationMaterial{}, err
	}
	if len(plaintext) == 0 {
		return VerificationMaterial{}, errInvalidChallenge
	}
	keyID, key, err := p.keys.Active()
	if err != nil {
		return VerificationMaterial{}, mapMaterialErr(err)
	}
	aead, err := aes256GCM(key)
	if err != nil {
		return VerificationMaterial{}, err
	}
	nonce := make([]byte, materialNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return VerificationMaterial{}, errUnavailable
	}
	ct := aead.Seal(nil, nonce, plaintext, materialAAD(challenge))
	m := VerificationMaterial{
		ChallengeID: challenge.ID,
		KeyID:       keyID,
		Nonce:       nonce,
		Ciphertext:  ct,
		CreatedAt:   challenge.CreatedAt,
	}
	if err := m.Validate(); err != nil {
		return VerificationMaterial{}, errUnavailable
	}
	return m, nil
}

func (p *MaterialProtector) Open(challenge VerificationChallenge, material VerificationMaterial) ([]byte, error) {
	if p == nil || p.keys == nil {
		return nil, errStoreRequired
	}
	if err := challenge.Validate(); err != nil {
		return nil, err
	}
	if !material.Active() {
		return nil, errUnavailable
	}
	if material.ChallengeID != challenge.ID {
		return nil, errUnavailable
	}
	key, err := p.keys.Lookup(material.KeyID)
	if err != nil {
		return nil, mapMaterialErr(err)
	}
	aead, err := aes256GCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, material.Nonce, material.Ciphertext, materialAAD(challenge))
	if err != nil {
		return nil, errUnavailable
	}
	if len(plain) == 0 {
		return nil, errUnavailable
	}
	return plain, nil
}

func aes256GCM(key []byte) (cipher.AEAD, error) {
	if len(key) != aes256KeySize {
		return nil, errUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errUnavailable
	}
	if aead.NonceSize() != materialNonceSize {
		return nil, errUnavailable
	}
	return aead, nil
}

func materialAAD(ch VerificationChallenge) []byte {
	var b []byte
	b = append(b, materialAADPrefix...)
	b = append(b, 0)
	b = append(b, ch.ID[:]...)
	b = append(b, 0)
	b = append(b, string(ch.Kind)...)
	b = append(b, 0)
	b = append(b, string(ch.Purpose)...)
	b = append(b, 0)
	b = append(b, ch.DestinationCanonical...)
	return b
}

func mapMaterialErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errUnavailable) ||
		errors.Is(err, errInvalidChallenge) || errors.Is(err, errZeroID) {
		return err
	}
	return errUnavailable
}
