package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	envTRIngressEnabled    = "TR_COMPLIANCE_INGRESS_ENABLED"
	envTRIngressToken      = "TR_COMPLIANCE_INGRESS_TOKEN"
	envTRTrustedPublicKeys = "TR_COMPLIANCE_TRUSTED_PUBLIC_KEYS"
	envTRAudience          = "TR_COMPLIANCE_AUDIENCE"
	envTRMaxClockSkew      = "TR_COMPLIANCE_MAX_CLOCK_SKEW"
	envTRMaxIngressAge     = "TR_COMPLIANCE_MAX_INGRESS_AGE"
	envTRSigningKeyID      = "TR_COMPLIANCE_SIGNING_KEY_ID"
	envTRSigningPrivateKey = "TR_COMPLIANCE_SIGNING_PRIVATE_KEY"
	envTRGermanyIngressURL = "TR_COMPLIANCE_GERMANY_INGRESS_URL"
	envTRSignerEnabled     = "TR_COMPLIANCE_SIGNER_ENABLED"
	DefaultTRAudience      = "konumlu-germany-eids-v1"
	defaultTRClockSkew     = 2 * time.Minute
	defaultTRIngressAge    = 10 * time.Minute
)

// TRCompliance is Germany-side TR decision ingress. The TR signing private
// key must never be present on this struct.
type TRCompliance struct {
	IngressEnabled   bool
	IngressToken     string
	Audience         string
	MaxClockSkew     time.Duration
	MaxIngressAge    time.Duration
	TrustedKeyIDs    []string
	trustedKeysRaw   string
}

func (t TRCompliance) String() string {
	return fmt.Sprintf("config.TRCompliance{ingress:%t keys:%d audience:%s}", t.IngressEnabled, len(t.TrustedKeyIDs), t.Audience)
}

func (t TRCompliance) GoString() string { return t.String() }

func (t TRCompliance) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("ingress_enabled", t.IngressEnabled),
		slog.Int("trusted_keys", len(t.TrustedKeyIDs)),
		slog.Bool("ingress_token_configured", t.IngressToken != ""),
		slog.String("audience", t.Audience),
	)
}

func (t TRCompliance) TrustedPublicKeysRaw() string {
	return t.trustedKeysRaw
}

func parseTRCompliance() (TRCompliance, error) {
	if strings.TrimSpace(os.Getenv(envTRSigningPrivateKey)) != "" {
		return TRCompliance{}, fmt.Errorf("%s must not be set on the Germany runtime", envTRSigningPrivateKey)
	}
	enabled, err := parseOptionalBool(envTRIngressEnabled)
	if err != nil {
		return TRCompliance{}, err
	}
	cfg := TRCompliance{
		IngressEnabled: enabled,
		IngressToken:   strings.TrimSpace(os.Getenv(envTRIngressToken)),
		Audience:       strings.TrimSpace(os.Getenv(envTRAudience)),
		trustedKeysRaw: strings.TrimSpace(os.Getenv(envTRTrustedPublicKeys)),
	}
	if cfg.Audience == "" {
		cfg.Audience = DefaultTRAudience
	}
	skew := defaultTRClockSkew
	if raw := strings.TrimSpace(os.Getenv(envTRMaxClockSkew)); raw != "" {
		d, err := parsePositiveDuration(envTRMaxClockSkew, raw)
		if err != nil {
			return TRCompliance{}, err
		}
		skew = d
	}
	age := defaultTRIngressAge
	if raw := strings.TrimSpace(os.Getenv(envTRMaxIngressAge)); raw != "" {
		d, err := parsePositiveDuration(envTRMaxIngressAge, raw)
		if err != nil {
			return TRCompliance{}, err
		}
		age = d
	}
	cfg.MaxClockSkew = skew
	cfg.MaxIngressAge = age
	ids, err := parseTrustedKeyIDs(cfg.trustedKeysRaw)
	if err != nil {
		return TRCompliance{}, err
	}
	cfg.TrustedKeyIDs = ids
	if cfg.IngressEnabled {
		if cfg.IngressToken == "" {
			return TRCompliance{}, fmt.Errorf("%s is required when TR compliance ingress is enabled", envTRIngressToken)
		}
		if len(cfg.TrustedKeyIDs) == 0 {
			return TRCompliance{}, fmt.Errorf("%s is required when TR compliance ingress is enabled", envTRTrustedPublicKeys)
		}
		if cfg.Audience != DefaultTRAudience {
			return TRCompliance{}, fmt.Errorf("%s must be %s", envTRAudience, DefaultTRAudience)
		}
		if cfg.MaxClockSkew <= 0 || cfg.MaxIngressAge <= 0 {
			return TRCompliance{}, fmt.Errorf("TR compliance time policy is invalid")
		}
	}
	return cfg, nil
}

func parseTrustedKeyIDs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := splitCommaList(raw)
	ids := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		kid, enc, ok := strings.Cut(part, ":")
		kid = strings.TrimSpace(kid)
		enc = strings.TrimSpace(enc)
		if !ok || kid == "" || enc == "" {
			return nil, fmt.Errorf("%s is malformed", envTRTrustedPublicKeys)
		}
		if _, dup := seen[kid]; dup {
			return nil, fmt.Errorf("%s has duplicate key_id", envTRTrustedPublicKeys)
		}
		seen[kid] = struct{}{}
		ids = append(ids, kid)
	}
	return ids, nil
}

// TRSigner is Türkiye-gateway signing/delivery config. Used by cmd/trgateway only.
type TRSigner struct {
	Enabled         bool
	KeyID           string
	PrivateKeyRaw   string
	GermanyURL      string
	IngressToken    string
	DeliveryTimeout time.Duration
}

func (t TRSigner) String() string {
	return fmt.Sprintf("config.TRSigner{enabled:%t key_id:%s url_configured:%t token_configured:%t}",
		t.Enabled, t.KeyID, t.GermanyURL != "", t.IngressToken != "")
}

func (t TRSigner) GoString() string { return t.String() }

func (t TRSigner) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enabled", t.Enabled),
		slog.String("key_id", t.KeyID),
		slog.Bool("private_key_configured", t.PrivateKeyRaw != ""),
		slog.Bool("germany_url_configured", t.GermanyURL != ""),
		slog.Bool("token_configured", t.IngressToken != ""),
	)
}

func LoadTRSigner() (TRSigner, error) {
	enabled, err := parseOptionalBool(envTRSignerEnabled)
	if err != nil {
		return TRSigner{}, err
	}
	s := TRSigner{
		Enabled:         enabled,
		KeyID:           strings.TrimSpace(os.Getenv(envTRSigningKeyID)),
		PrivateKeyRaw:   strings.TrimSpace(os.Getenv(envTRSigningPrivateKey)),
		GermanyURL:      strings.TrimSpace(os.Getenv(envTRGermanyIngressURL)),
		IngressToken:    strings.TrimSpace(os.Getenv(envTRIngressToken)),
		DeliveryTimeout: 5 * time.Second,
	}
	if s.Enabled {
		if s.KeyID == "" || s.PrivateKeyRaw == "" {
			return TRSigner{}, fmt.Errorf("%s and %s are required when the TR signer is enabled", envTRSigningKeyID, envTRSigningPrivateKey)
		}
		if s.GermanyURL == "" || s.IngressToken == "" {
			return TRSigner{}, fmt.Errorf("%s and %s are required when the TR signer is enabled", envTRGermanyIngressURL, envTRIngressToken)
		}
		if !strings.HasPrefix(s.GermanyURL, "https://") && !strings.HasPrefix(s.GermanyURL, "http://127.0.0.1") && !strings.HasPrefix(s.GermanyURL, "http://localhost") {
			return TRSigner{}, fmt.Errorf("%s must be an https URL", envTRGermanyIngressURL)
		}
		if strings.Contains(s.String(), s.PrivateKeyRaw) || strings.Contains(fmt.Sprintf("%#v", s.LogValue()), s.PrivateKeyRaw) {
			return TRSigner{}, fmt.Errorf("%s must not appear in config stringers", envTRSigningPrivateKey)
		}
	}
	return s, nil
}
