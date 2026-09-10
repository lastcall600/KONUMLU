package staffidp

import (
	"context"
	"errors"
	"testing"

	"backend/internal/platform/config"
	"backend/internal/staffauth/contracts"
)

func TestBindEmptyHasNoProvider(t *testing.T) {
	p, err := Bind(config.StaffIDP{}, nil)
	if err != nil || p != nil {
		t.Fatalf("p=%v err=%v", p, err)
	}
}

func TestBindPartialFailsClosed(t *testing.T) {
	_, err := Bind(config.StaffIDP{Issuer: "https://idp.example.test"}, nil)
	if !errors.Is(err, ErrMisconfigured) {
		t.Fatalf("err=%v", err)
	}
}

func TestBindCompleteWithoutAdapterFailsClosed(t *testing.T) {
	_, err := Bind(config.StaffIDP{
		Issuer:   "https://idp.example.test",
		Audience: "konumlu-staff",
		JWKSURL:  "https://idp.example.test/jwks",
	}, nil)
	if !errors.Is(err, ErrAdapterRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestBindCompleteWithAdapter(t *testing.T) {
	adapter := stubProvider{}
	p, err := Bind(config.StaffIDP{
		Issuer:   "https://idp.example.test",
		Audience: "konumlu-staff",
		JWKSURL:  "https://idp.example.test/jwks",
	}, adapter)
	if err != nil || p != adapter {
		t.Fatalf("p=%v err=%v", p, err)
	}
}

type stubProvider struct{}

func (stubProvider) Verify(context.Context, contracts.Credential) (contracts.Principal, error) {
	return contracts.Principal{}, contracts.ErrUnavailable
}
