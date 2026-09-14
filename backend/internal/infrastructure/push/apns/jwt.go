package apns

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	domain "backend/internal/notifications"
)

type jwtCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
	now     func() time.Time
	key     *ecdsa.PrivateKey
	keyID   string
	teamID  string
}

func parseAPNsKey(pemBytes string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemBytes))
	if block == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, domain.ErrProviderUnconfigured
		}
		return ec, nil
	}
	ec, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return ec, nil
}

func (c *jwtCache) bearer() (string, error) {
	if c == nil || c.key == nil {
		return "", domain.ErrProviderUnconfigured
	}
	now := time.Now().UTC()
	if c.now != nil {
		now = c.now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && now.Add(30*time.Second).Before(c.expires) {
		return c.token, nil
	}
	exp := now.Add(jwtTTL)
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.teamID,
		"iat": now.Unix(),
	})
	tok.Header["kid"] = c.keyID
	signed, err := tok.SignedString(c.key)
	if err != nil {
		return "", domain.ErrProviderPermanent
	}
	c.token = signed
	c.expires = exp
	return signed, nil
}

func (c *jwtCache) String() string {
	return fmt.Sprintf("apns.jwtCache{key_id_configured:%t team_id_configured:%t}", c != nil && c.keyID != "", c != nil && c.teamID != "")
}

func (c *jwtCache) GoString() string { return c.String() }
