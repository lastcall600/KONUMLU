package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func parsePushEndpoints() (PushEndpoints, error) {
	encRaw := strings.TrimSpace(os.Getenv(envPushEndpointEncryptionKey))
	hashRaw := strings.TrimSpace(os.Getenv(envPushEndpointHashKey))
	if encRaw == "" && hashRaw == "" {
		return PushEndpoints{}, nil
	}
	if encRaw == "" {
		return PushEndpoints{}, fmt.Errorf("%s must not be empty when %s is set", envPushEndpointEncryptionKey, envPushEndpointHashKey)
	}
	if hashRaw == "" {
		return PushEndpoints{}, fmt.Errorf("%s must not be empty when %s is set", envPushEndpointHashKey, envPushEndpointEncryptionKey)
	}
	enc, err := decodePushKey(envPushEndpointEncryptionKey, encRaw)
	if err != nil {
		return PushEndpoints{}, err
	}
	hash, err := decodePushKey(envPushEndpointHashKey, hashRaw)
	if err != nil {
		return PushEndpoints{}, err
	}
	return PushEndpoints{Enabled: true, EncryptionKey: enc, HashKey: hash}, nil
}

func decodePushKey(envName, enc string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("%s is invalid", envName)
		}
	}
	if len(b) != materialAESKeySize {
		return nil, fmt.Errorf("%s key length is invalid", envName)
	}
	return b, nil
}
