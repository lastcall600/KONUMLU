package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
)

const HMACKeyMinSize = 32

var ErrInvalidHMACKey = errors.New("crypto hmac key invalid")

type HMACKey []byte

func (HMACKey) String() string   { return "crypto.HMACKey" }
func (HMACKey) GoString() string { return "crypto.HMACKey{}" }

func NewHMACKey(key []byte) (HMACKey, error) {
	if len(key) < HMACKeyMinSize {
		return nil, ErrInvalidHMACKey
	}
	return HMACKey(cloneBytes(key)), nil
}

func (k HMACKey) Sum(msg []byte) ([]byte, error) {
	if len(k) < HMACKeyMinSize {
		return nil, ErrInvalidHMACKey
	}
	mac := hmac.New(sha256.New, k)
	_, _ = mac.Write(msg)
	return mac.Sum(nil), nil
}
