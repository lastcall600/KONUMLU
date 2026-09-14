package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
)

const (
	AES256KeySize = 32
	GCMNonceSize  = 12
	minCiphertext = 16
)

var (
	ErrUnavailable = errors.New("crypto unavailable")
	ErrInvalidKey  = errors.New("crypto key invalid")
	ErrOpen        = errors.New("crypto open failed")
)

// Envelope is AES-256-GCM sealed bytes. Plaintext is never retained.
type Envelope struct {
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
}

func (Envelope) String() string  { return "crypto.Envelope" }
func (Envelope) GoString() string { return "crypto.Envelope{}" }

func (e Envelope) LogValue() slog.Value {
	return slog.StringValue("crypto.Envelope")
}

type Keyring struct {
	activeID string
	keys     map[string][]byte
}

func (Keyring) String() string  { return "crypto.Keyring" }
func (Keyring) GoString() string { return "crypto.Keyring{}" }

func NewKeyring(activeID string, keys map[string][]byte) (*Keyring, error) {
	if activeID == "" || len(keys) == 0 {
		return nil, ErrInvalidKey
	}
	cloned := make(map[string][]byte, len(keys))
	for id, key := range keys {
		if id == "" || len(key) != AES256KeySize {
			return nil, ErrInvalidKey
		}
		if _, dup := cloned[id]; dup {
			return nil, ErrInvalidKey
		}
		cloned[id] = cloneBytes(key)
	}
	if _, ok := cloned[activeID]; !ok {
		return nil, ErrInvalidKey
	}
	return &Keyring{activeID: activeID, keys: cloned}, nil
}

// NewSingleKey builds a one-entry keyring with stable id "v1".
// Push endpoints V1 use this constructor: there is one active encryption key
// and no previous-key ring / automatic rotation.
func NewSingleKey(key []byte) (*Keyring, error) {
	return NewKeyring("v1", map[string][]byte{"v1": key})
}

func (k *Keyring) ActiveID() string {
	if k == nil {
		return ""
	}
	return k.activeID
}

type AEAD struct {
	keys *Keyring
}

func (AEAD) String() string  { return "crypto.AEAD" }
func (AEAD) GoString() string { return "crypto.AEAD{}" }

func NewAEAD(keys *Keyring) (*AEAD, error) {
	if keys == nil {
		return nil, ErrInvalidKey
	}
	key, ok := keys.keys[keys.activeID]
	if !ok || len(key) != AES256KeySize {
		return nil, ErrInvalidKey
	}
	return &AEAD{keys: keys}, nil
}

func (a *AEAD) Seal(plaintext, aad []byte) (Envelope, error) {
	if a == nil || a.keys == nil {
		return Envelope{}, ErrUnavailable
	}
	if len(plaintext) == 0 {
		return Envelope{}, ErrUnavailable
	}
	key := a.keys.keys[a.keys.activeID]
	gcm, err := aes256GCM(key)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, GCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, ErrUnavailable
	}
	ct := gcm.Seal(nil, nonce, plaintext, aad)
	return Envelope{KeyID: a.keys.activeID, Nonce: nonce, Ciphertext: ct}, nil
}

func (a *AEAD) Open(env Envelope, aad []byte) ([]byte, error) {
	if a == nil || a.keys == nil {
		return nil, ErrUnavailable
	}
	if env.KeyID == "" || len(env.Nonce) != GCMNonceSize || len(env.Ciphertext) < minCiphertext {
		return nil, ErrOpen
	}
	key, ok := a.keys.keys[env.KeyID]
	if !ok || len(key) != AES256KeySize {
		return nil, ErrOpen
	}
	gcm, err := aes256GCM(key)
	if err != nil {
		return nil, ErrOpen
	}
	plain, err := gcm.Open(nil, env.Nonce, env.Ciphertext, aad)
	if err != nil {
		return nil, ErrOpen
	}
	if len(plain) == 0 {
		return nil, ErrOpen
	}
	return plain, nil
}

func aes256GCM(key []byte) (cipher.AEAD, error) {
	if len(key) != AES256KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	if aead.NonceSize() != GCMNonceSize {
		return nil, ErrUnavailable
	}
	return aead, nil
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func RedactError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w", ErrUnavailable)
}
