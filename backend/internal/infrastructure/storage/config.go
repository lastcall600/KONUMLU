package storage

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envEnabled          = "OBJECT_STORAGE_ENABLED"
	envEndpoint         = "OBJECT_STORAGE_ENDPOINT"
	envRegion           = "OBJECT_STORAGE_REGION"
	envBucket           = "OBJECT_STORAGE_BUCKET"
	envAccessKey        = "OBJECT_STORAGE_ACCESS_KEY"
	envSecretKey        = "OBJECT_STORAGE_SECRET_KEY"
	envPathStyle        = "OBJECT_STORAGE_PATH_STYLE"
	envUploadTTL        = "OBJECT_STORAGE_UPLOAD_TTL"
	envGetTTL           = "OBJECT_STORAGE_GET_TTL"
	envPublicBaseURL    = "OBJECT_STORAGE_PUBLIC_BASE_URL"
	envMaxUploadBytes   = "OBJECT_STORAGE_MAX_UPLOAD_BYTES"
	envCredentialSource = "OBJECT_STORAGE_CREDENTIAL_SOURCE"

	defaultMaxUploadBytes int64 = 10 << 20
	defaultGetTTL               = 5 * time.Minute

	CredentialStatic   = "static"
	CredentialWorkload = "workload"
)

// Config is provider-neutral S3-compatible object storage settings.
// Credentials are never logged; String/error paths must not include secrets.
type Config struct {
	Enabled          bool
	Endpoint         string
	Region           string
	Bucket           string
	AccessKey        string
	SecretKey        string
	UsePathStyle     bool
	UploadTTL        time.Duration
	GetTTL           time.Duration
	PublicBaseURL    string
	MaxUploadBytes   int64
	CredentialSource string
}

// Load reads object-storage configuration from the environment.
// When storage is disabled (default), other fields are ignored.
// When enabled, missing or malformed required fields fail clearly.
func Load() (Config, error) {
	enabled, err := parseEnabled(os.Getenv(envEnabled))
	if err != nil {
		return Config{}, err
	}
	if !enabled {
		return Config{Enabled: false}, nil
	}

	cfg := Config{
		Enabled:          true,
		Endpoint:         strings.TrimSpace(os.Getenv(envEndpoint)),
		Region:           strings.TrimSpace(os.Getenv(envRegion)),
		Bucket:           strings.TrimSpace(os.Getenv(envBucket)),
		AccessKey:        strings.TrimSpace(os.Getenv(envAccessKey)),
		SecretKey:        strings.TrimSpace(os.Getenv(envSecretKey)),
		UsePathStyle:     true,
		CredentialSource: CredentialStatic,
	}

	if raw := strings.TrimSpace(os.Getenv(envPathStyle)); raw != "" {
		v, err := parseBool(envPathStyle, raw)
		if err != nil {
			return Config{}, err
		}
		cfg.UsePathStyle = v
	} else if cfg.Endpoint == "" {
		cfg.UsePathStyle = false
	}

	ttl, err := requiredPositiveDuration(envUploadTTL)
	if err != nil {
		return Config{}, err
	}
	cfg.UploadTTL = ttl

	if raw := strings.TrimSpace(os.Getenv(envGetTTL)); raw == "" {
		cfg.GetTTL = defaultGetTTL
	} else {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s is malformed", envGetTTL)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("%s must be greater than zero", envGetTTL)
		}
		cfg.GetTTL = d
	}

	if raw := strings.TrimSpace(os.Getenv(envPublicBaseURL)); raw != "" {
		if err := validatePublicBaseURL(raw); err != nil {
			return Config{}, err
		}
		cfg.PublicBaseURL = strings.TrimRight(raw, "/")
	}

	if raw := strings.TrimSpace(os.Getenv(envMaxUploadBytes)); raw == "" {
		cfg.MaxUploadBytes = defaultMaxUploadBytes
	} else {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("%s is malformed", envMaxUploadBytes)
		}
		if n <= 0 {
			return Config{}, fmt.Errorf("%s must be greater than zero", envMaxUploadBytes)
		}
		cfg.MaxUploadBytes = n
	}

	source, err := parseCredentialSource(os.Getenv(envCredentialSource))
	if err != nil {
		return Config{}, err
	}
	cfg.CredentialSource = source

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Region == "" {
		return fmt.Errorf("%s must not be empty when object storage is enabled", envRegion)
	}
	if c.Bucket == "" {
		return fmt.Errorf("%s must not be empty when object storage is enabled", envBucket)
	}
	if strings.ContainsAny(c.Bucket, "/\\") || strings.Contains(c.Bucket, "..") {
		return fmt.Errorf("%s is malformed", envBucket)
	}
	if c.CredentialSource == "" {
		c.CredentialSource = CredentialStatic
	}
	switch c.CredentialSource {
	case CredentialStatic:
		if c.AccessKey == "" {
			return fmt.Errorf("%s must not be empty when object storage is enabled", envAccessKey)
		}
		if c.SecretKey == "" {
			return fmt.Errorf("%s must not be empty when object storage is enabled", envSecretKey)
		}
	case CredentialWorkload:
		if c.AccessKey != "" || c.SecretKey != "" {
			return fmt.Errorf("%s must be empty when %s=%s", envAccessKey, envCredentialSource, CredentialWorkload)
		}
	default:
		return fmt.Errorf("%s must be %s or %s", envCredentialSource, CredentialStatic, CredentialWorkload)
	}
	if c.UploadTTL <= 0 {
		return fmt.Errorf("%s must be greater than zero when object storage is enabled", envUploadTTL)
	}
	if c.UploadTTL > 15*time.Minute {
		return fmt.Errorf("%s must be 15m or less when object storage is enabled", envUploadTTL)
	}
	if c.GetTTL <= 0 {
		return fmt.Errorf("%s must be greater than zero when object storage is enabled", envGetTTL)
	}
	if c.PublicBaseURL != "" {
		if err := validatePublicBaseURL(c.PublicBaseURL); err != nil {
			return err
		}
	}
	if c.MaxUploadBytes <= 0 {
		return fmt.Errorf("%s must be greater than zero when object storage is enabled", envMaxUploadBytes)
	}
	if c.Endpoint != "" {
		if strings.Contains(c.Endpoint, "://") {
			if !strings.HasPrefix(c.Endpoint, "http://") && !strings.HasPrefix(c.Endpoint, "https://") {
				return fmt.Errorf("%s is malformed", envEndpoint)
			}
		}
	}
	return nil
}

func parseCredentialSource(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return CredentialStatic, nil
	}
	if raw == CredentialStatic || raw == CredentialWorkload {
		return raw, nil
	}
	return "", fmt.Errorf("%s must be %s or %s", envCredentialSource, CredentialStatic, CredentialWorkload)
}

func (c Config) String() string {
	return fmt.Sprintf("storage.Config{enabled:%v bucket:%s region:%s endpoint:%s credentials:%s}", c.Enabled, c.Bucket, c.Region, c.Endpoint, c.CredentialSource)
}

func (c Config) GoString() string {
	return c.String()
}

func parseEnabled(raw string) (bool, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "0" || raw == "false" || raw == "no" || raw == "disabled" {
		return false, nil
	}
	if raw == "1" || raw == "true" || raw == "yes" || raw == "enabled" {
		return true, nil
	}
	return false, fmt.Errorf("%s must be true or false", envEnabled)
}

func parseBool(name, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func requiredPositiveDuration(name string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, fmt.Errorf("%s must not be empty when object storage is enabled", name)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is malformed", name)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return d, nil
}

func validatePublicBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return fmt.Errorf("%s is malformed", envPublicBaseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s is malformed", envPublicBaseURL)
	}
	if strings.Contains(raw, "..") {
		return fmt.Errorf("%s is malformed", envPublicBaseURL)
	}
	return nil
}
