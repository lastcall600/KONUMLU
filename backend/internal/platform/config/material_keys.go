package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// MaterialKeys is the process verification-material AES-256 keyring.
// Keys are injected from the environment (secret-manager compatible). This
// process never generates keys, never writes them to PostgreSQL or disk, and
// never logs encoded or decoded key material.
//
// Manual rotation (not automated):
//  1. Add a new named key to IDENTITY_VERIFICATION_MATERIAL_KEYS.
//  2. Switch IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID to the new key (sealing).
//  3. Keep the old key while any live ciphertext still references its key_id.
//  4. Remove the old key only after no live material depends on it.
type MaterialKeys struct {
	ActiveID string
	Keys     map[string][]byte
}

func (m MaterialKeys) String() string {
	return "config.MaterialKeys"
}

func (m MaterialKeys) GoString() string {
	return "config.MaterialKeys{}"
}

func parseMaterialKeys() (MaterialKeys, error) {
	active := strings.TrimSpace(os.Getenv(envMaterialActiveKeyID))
	if active == "" {
		return MaterialKeys{}, fmt.Errorf("%s must not be empty", envMaterialActiveKeyID)
	}
	if !validMaterialKeyID(active) {
		return MaterialKeys{}, fmt.Errorf("%s is invalid", envMaterialActiveKeyID)
	}
	raw := strings.TrimSpace(os.Getenv(envMaterialKeys))
	if raw == "" {
		return MaterialKeys{}, fmt.Errorf("%s must not be empty", envMaterialKeys)
	}

	keys := make(map[string][]byte)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, enc, ok := strings.Cut(part, ":")
		id = strings.TrimSpace(id)
		enc = strings.TrimSpace(enc)
		if !ok || !validMaterialKeyID(id) || enc == "" {
			return MaterialKeys{}, fmt.Errorf("%s is invalid", envMaterialKeys)
		}
		if _, dup := keys[id]; dup {
			return MaterialKeys{}, fmt.Errorf("%s has duplicate key id", envMaterialKeys)
		}
		decoded, err := decodeAES256Key(enc)
		if err != nil {
			return MaterialKeys{}, err
		}
		keys[id] = decoded
	}
	if len(keys) == 0 {
		return MaterialKeys{}, fmt.Errorf("%s must not be empty", envMaterialKeys)
	}
	if _, ok := keys[active]; !ok {
		return MaterialKeys{}, fmt.Errorf("%s must refer to a configured key", envMaterialActiveKeyID)
	}
	return MaterialKeys{ActiveID: active, Keys: keys}, nil
}

func decodeAES256Key(enc string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("%s is invalid", envMaterialKeys)
		}
	}
	if len(b) != materialAESKeySize {
		return nil, fmt.Errorf("%s key length is invalid", envMaterialKeys)
	}
	return b, nil
}

func validMaterialKeyID(id string) bool {
	if id == "" || id != strings.TrimSpace(id) {
		return false
	}
	for _, r := range id {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
