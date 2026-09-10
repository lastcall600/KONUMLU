package storage

import (
	"fmt"
	"strings"
	"unicode"

	"backend/internal/media"
)

// ListingImageObjectKey builds a server-owned listing-image object key.
// ownerUserID, assetID, and random must already be safe tokens (no slashes or filenames).
func ListingImageObjectKey(ownerUserID, assetID, random string) (string, error) {
	if err := requireKeyToken(ownerUserID); err != nil {
		return "", media.ErrInvalidObjectKey
	}
	if err := requireKeyToken(assetID); err != nil {
		return "", media.ErrInvalidObjectKey
	}
	if err := requireKeyToken(random); err != nil {
		return "", media.ErrInvalidObjectKey
	}
	key := "media/listing-images/" + ownerUserID + "/" + assetID + "/" + random
	if !media.IsServerObjectKey(key) {
		return "", media.ErrInvalidObjectKey
	}
	return key, nil
}

func requireKeyToken(v string) error {
	if v == "" || strings.Contains(v, "..") || strings.ContainsAny(v, "/\\") {
		return fmt.Errorf("invalid token")
	}
	if strings.Contains(v, ".") {
		return fmt.Errorf("invalid token")
	}
	for _, r := range v {
		if unicode.IsSpace(r) {
			return fmt.Errorf("invalid token")
		}
	}
	return nil
}
