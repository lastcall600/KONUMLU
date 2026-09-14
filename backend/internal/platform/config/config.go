package config

import (
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr                 = ":8080"
	defaultShutdownTimeout          = 10 * time.Second
	defaultDBConnectTimeout         = 5 * time.Second
	defaultOutboxWorkerConcurrency  = 1
	envAppEnv                       = "APP_ENV"
	envHTTPAddr                     = "HTTP_ADDR"
	envShutdownTimeout              = "SHUTDOWN_TIMEOUT"
	envDatabaseURL                  = "DATABASE_URL"
	envDBConnectTimeout             = "DB_CONNECT_TIMEOUT"
	envDBMaxConns                   = "DB_MAX_CONNS"
	envDBMinConns                   = "DB_MIN_CONNS"
	envDBMaxConnLifetime            = "DB_MAX_CONN_LIFETIME"
	envDBMaxConnIdleTime            = "DB_MAX_CONN_IDLE_TIME"
	envValkeyURL                    = "VALKEY_URL"
	envTrustedProxies               = "TRUSTED_PROXIES"
	envAllowInsecureCookies         = "ALLOW_INSECURE_COOKIES"
	envLogLevel                     = "LOG_LEVEL"
	envObjectStorageEnabled         = "OBJECT_STORAGE_ENABLED"
	envOutboxWorkerConcurrency      = "OUTBOX_WORKER_CONCURRENCY"
	envWebAuthnRPDisplayName        = "IDENTITY_WEBAUTHN_RP_DISPLAY_NAME"
	envWebAuthnRPID                 = "IDENTITY_WEBAUTHN_RP_ID"
	envWebAuthnRPOrigins            = "IDENTITY_WEBAUTHN_RP_ORIGINS"
	envWebAuthnCeremonyTTL          = "IDENTITY_WEBAUTHN_CEREMONY_TTL"
	envSessionIdle                  = "IDENTITY_SESSION_IDLE"
	envSessionAbsolute              = "IDENTITY_SESSION_ABSOLUTE"
	envStepUpTTL                    = "IDENTITY_STEP_UP_TTL"
	envAuthIPMaxAttempts            = "IDENTITY_AUTH_IP_MAX_ATTEMPTS"
	envAuthIPWindow                 = "IDENTITY_AUTH_IP_WINDOW"
	envAuthPasswordUserMaxAttempts  = "IDENTITY_AUTH_PASSWORD_USER_MAX_ATTEMPTS"
	envAuthPasswordUserWindow       = "IDENTITY_AUTH_PASSWORD_USER_WINDOW"
	envAuthTargetMaxAttempts        = "IDENTITY_AUTH_TARGET_MAX_ATTEMPTS"
	envAuthTargetWindow             = "IDENTITY_AUTH_TARGET_WINDOW"
	envAuthCompleteMaxAttempts      = "IDENTITY_AUTH_COMPLETE_MAX_ATTEMPTS"
	envAuthCompleteWindow           = "IDENTITY_AUTH_COMPLETE_WINDOW"
	envAuthSensitiveMaxAttempts     = "IDENTITY_AUTH_SENSITIVE_MAX_ATTEMPTS"
	envAuthSensitiveWindow          = "IDENTITY_AUTH_SENSITIVE_WINDOW"
	envHumanChallengeProvider       = "IDENTITY_HUMAN_CHALLENGE_PROVIDER"
	envHumanChallengeOperations     = "IDENTITY_HUMAN_CHALLENGE_OPERATIONS"
	envHumanChallengeHostname       = "IDENTITY_HUMAN_CHALLENGE_HOSTNAME"
	envHumanChallengeReplayTTL      = "IDENTITY_HUMAN_CHALLENGE_REPLAY_TTL"
	envHumanChallengeTimeout        = "IDENTITY_HUMAN_CHALLENGE_TIMEOUT"
	envTurnstileSecret              = "IDENTITY_HUMAN_CHALLENGE_TURNSTILE_SECRET"
	envTurnstileSiteKey             = "IDENTITY_HUMAN_CHALLENGE_TURNSTILE_SITEKEY"
	defaultHumanChallengeTimeout    = 3 * time.Second
	envChallengeTTL                 = "IDENTITY_VERIFICATION_CHALLENGE_TTL"
	envChallengeMaxAttempts         = "IDENTITY_VERIFICATION_CHALLENGE_MAX_ATTEMPTS"
	envChallengePhoneOTPDigits      = "IDENTITY_VERIFICATION_PHONE_OTP_DIGITS"
	envIssueDestMax                 = "IDENTITY_VERIFICATION_ISSUE_DEST_MAX"
	envIssueDestWindow              = "IDENTITY_VERIFICATION_ISSUE_DEST_WINDOW"
	envIssueIPMax                   = "IDENTITY_VERIFICATION_ISSUE_IP_MAX"
	envIssueIPWindow                = "IDENTITY_VERIFICATION_ISSUE_IP_WINDOW"
	envSignupProofTTL               = "IDENTITY_SIGNUP_PROOF_TTL"
	envOutboxBatchSize              = "OUTBOX_BATCH_SIZE"
	envOutboxLease                  = "OUTBOX_LEASE"
	envOutboxPollInterval           = "OUTBOX_POLL_INTERVAL"
	envOutboxRetryBase              = "OUTBOX_RETRY_BASE"
	envOutboxRetryMultiplier        = "OUTBOX_RETRY_MULTIPLIER"
	envOutboxRetryCap               = "OUTBOX_RETRY_CAP"
	envOutboxRetryJitter            = "OUTBOX_RETRY_JITTER"
	envMaterialActiveKeyID          = "IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID"
	envMaterialKeys                 = "IDENTITY_VERIFICATION_MATERIAL_KEYS"
	envNotificationsEmailMode       = "NOTIFICATIONS_EMAIL_MODE"
	envNotificationsSMSMode         = "NOTIFICATIONS_SMS_MODE"
	envEmailProvider                = "EMAIL_PROVIDER"
	envEmailSESRegion               = "EMAIL_SES_REGION"
	envEmailSESFrom                 = "EMAIL_SES_FROM"
	envEmailSESTimeout              = "EMAIL_SES_TIMEOUT"
	defaultEmailSESTimeout          = 5 * time.Second
	envSMSProvider                  = "SMS_PROVIDER"
	envNetgsmUsername               = "NETGSM_USERNAME"
	envNetgsmPassword               = "NETGSM_PASSWORD"
	envNetgsmMsgHeader              = "NETGSM_MSGHEADER"
	envNetgsmTimeout                = "NETGSM_TIMEOUT"
	defaultNetgsmTimeout            = 5 * time.Second
	envPushEndpointEncryptionKey    = "PUSH_ENDPOINT_ENCRYPTION_KEY"
	envPushEndpointHashKey          = "PUSH_ENDPOINT_HASH_KEY"
	envWebPushVAPIDPublicKey        = "WEBPUSH_VAPID_PUBLIC_KEY"
	envWebPushVAPIDPrivateKey       = "WEBPUSH_VAPID_PRIVATE_KEY"
	envWebPushVAPIDSubject          = "WEBPUSH_VAPID_SUBJECT"
	envWebPushTimeout               = "WEBPUSH_TIMEOUT"
	defaultWebPushTimeout           = 5 * time.Second
	envFCMProjectID                 = "FCM_PROJECT_ID"
	envFCMCredentialsFile           = "FCM_CREDENTIALS_FILE"
	envFCMTimeout                   = "FCM_TIMEOUT"
	defaultFCMTimeout               = 5 * time.Second
	envAPNSTeamID                   = "APNS_TEAM_ID"
	envAPNSKeyID                    = "APNS_KEY_ID"
	envAPNSTopic                    = "APNS_TOPIC"
	envAPNSPrivateKey               = "APNS_PRIVATE_KEY"
	envAPNSPrivateKeyFile           = "APNS_PRIVATE_KEY_FILE"
	envAPNSEnvironment              = "APNS_ENVIRONMENT"
	envAPNSTimeout                  = "APNS_TIMEOUT"
	defaultAPNSTimeout              = 5 * time.Second
	envMediaMalwareScanRequired     = "MEDIA_MALWARE_SCAN_REQUIRED"
	envMediaImageModerationRequired = "MEDIA_IMAGE_MODERATION_REQUIRED"
	envStaffIDPIssuer               = "STAFF_IDP_ISSUER"
	envStaffIDPAudience             = "STAFF_IDP_AUDIENCE"
	envStaffIDPJWKSURL              = "STAFF_IDP_JWKS_URL"
	envStaffDevIDPEnabled           = "STAFF_DEV_IDP_ENABLED"
	envStaffDevIDPToken             = "STAFF_DEV_IDP_TOKEN"
	envStaffDevIDPStaffID           = "STAFF_DEV_IDP_STAFF_ID"
	envStaffDevIDPRoles             = "STAFF_DEV_IDP_ROLES"
	materialAESKeySize              = 32

	NotificationChannelDisabled = "disabled"
	NotificationChannelExternal = "external"
	EmailProviderSES            = "ses"
	SMSProviderNetgsm           = "netgsm"
)

// Config holds process configuration loaded from the environment.
// Secret fields must never appear in String, GoString, slog, or error text.
type Config struct {
	Environment      Environment
	HTTPAddr         string
	ShutdownTimeout  time.Duration
	DatabaseURL      string
	DBConnectTimeout time.Duration
	DBPool           Pool
	ValkeyURL        string
	// TrustedProxies are CIDRs allowed to set X-Forwarded-For. Empty means never trust XFF.
	TrustedProxies []net.IPNet
	// TrustedProxiesSet is true when TRUSTED_PROXIES was present (even if empty).
	TrustedProxiesSet    bool
	LogLevel             string
	AllowInsecureCookies bool
	// WebAuthn RP settings are optional at process load in development/test.
	// Staging and production require display name, RP ID, and at least one origin.
	WebAuthnRPDisplayName       string
	WebAuthnRPID                string
	WebAuthnRPOrigins           []string
	WebAuthnCeremonyTTL         time.Duration
	SessionIdle                 time.Duration
	SessionAbsolute             time.Duration
	StepUpTTL                   time.Duration
	AuthIPMaxAttempts           int
	AuthIPWindow                time.Duration
	AuthPasswordUserMaxAttempts int
	AuthPasswordUserWindow      time.Duration
	AuthTargetMaxAttempts       int
	AuthTargetWindow            time.Duration
	AuthCompleteMaxAttempts     int
	AuthCompleteWindow          time.Duration
	AuthSensitiveMaxAttempts    int
	AuthSensitiveWindow         time.Duration
	HumanChallenge              HumanChallenge
	ChallengeTTL                time.Duration
	ChallengeMaxAttempts        int
	ChallengePhoneOTPDigits     int
	IssueDestMax                int
	IssueDestWindow             time.Duration
	IssueIPMax                  int
	IssueIPWindow               time.Duration
	SignupProofTTL              time.Duration
	OutboxBatchSize             int
	OutboxLease                 time.Duration
	OutboxPollInterval          time.Duration
	OutboxRetryBase             time.Duration
	OutboxRetryMultiplier       float64
	OutboxRetryCap              time.Duration
	OutboxRetryJitter           time.Duration
	OutboxWorkerConcurrency     int
	MaterialKeys                MaterialKeys
	// Notification channel modes are disabled|external. External requires a
	// registered vendor adapter at process wiring; credentials are not defined here.
	NotificationsEmailMode string
	NotificationsSMSMode   string
	// Email is transactional Amazon SES settings. Secrets are not stored here;
	// the AWS default credential chain is used at adapter construction.
	Email Email
	// SMS is transactional/OTP Netgsm settings. Password is server-only and
	// must never appear in String, GoString, slog, or error text.
	SMS SMS
	// PushEndpoints holds AES-256-GCM + HMAC keys for durable push tokens.
	// Keys are never logged. Registration HTTP is enabled only when keys load.
	PushEndpoints PushEndpoints
	// WebPush is VAPID Web Push. Private key is never logged.
	WebPush WebPush
	// FCM is Firebase Cloud Messaging HTTP v1. Credential JSON is never stored here.
	FCM FCM
	// APNs is Apple Push Notification service token authentication.
	APNs APNs
	// Media scanner/moderation requirements. Vendors are not selected; required+missing is retryable.
	MediaMalwareScanRequired     bool
	MediaImageModerationRequired bool
	// StaffIDP is provider-neutral OIDC settings. Empty means staff APIs stay
	// unregistered. A vendor adapter is still required at wiring; there is no
	// local fake-admin login and no consumer-session fallback.
	StaffIDP StaffIDP
	// StaffDevIDP is an explicit development/test Staff IAM fixture. It is never
	// a production fallback and must stay empty in staging/production.
	StaffDevIDP StaffDevIDP
}

// Email is the transactional email provider selection. Credentials are never
// stored on this struct; SES uses the AWS default credential chain.
type Email struct {
	Provider string
	Region   string
	From     string
	Timeout  time.Duration
}

func (e Email) String() string {
	return fmt.Sprintf("config.Email{provider:%s region:%s from_configured:%t}", e.Provider, e.Region, e.From != "")
}

func (e Email) GoString() string { return e.String() }

func (e Email) SESWired() bool {
	return e.Provider == EmailProviderSES && e.Region != "" && e.From != "" && e.Timeout > 0
}

// SMS is the transactional/OTP SMS provider selection. Password is never logged.
type SMS struct {
	Provider  string
	Username  string
	Password  string
	MsgHeader string
	Timeout   time.Duration
}

func (s SMS) String() string {
	return fmt.Sprintf("config.SMS{provider:%s username_configured:%t password_configured:%t msgheader_configured:%t}",
		s.Provider, s.Username != "", s.Password != "", s.MsgHeader != "")
}

func (s SMS) GoString() string { return s.String() }

func (s SMS) NetgsmWired() bool {
	return s.Provider == SMSProviderNetgsm && s.Username != "" && s.Password != "" && s.MsgHeader != "" && s.Timeout > 0
}

// PushEndpoints is application-level encryption for push provider material.
// V1 loads one AES-256 encryption key and one HMAC key. Ciphertext is sealed
// with key id "v1". A previous-key ring and automatic rotation are not
// implemented: replacing PUSH_ENDPOINT_ENCRYPTION_KEY makes existing
// ciphertext unreadable until clients re-register.
type PushEndpoints struct {
	Enabled       bool
	EncryptionKey []byte
	HashKey       []byte
}

func (p PushEndpoints) String() string {
	return fmt.Sprintf("config.PushEndpoints{enabled:%t encryption_key_configured:%t hash_key_configured:%t}",
		p.Enabled, len(p.EncryptionKey) == materialAESKeySize, len(p.HashKey) >= materialAESKeySize)
}

func (p PushEndpoints) GoString() string { return p.String() }

// WebPush is VAPID configuration. The private key is never logged.
type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string
	Timeout    time.Duration
}

func (w WebPush) String() string {
	return fmt.Sprintf("config.WebPush{public_key_configured:%t private_key_configured:%t subject_configured:%t}",
		w.PublicKey != "", w.PrivateKey != "", w.Subject != "")
}

func (w WebPush) GoString() string { return w.String() }

func (w WebPush) Wired() bool {
	return w.PublicKey != "" && w.PrivateKey != "" && w.Subject != "" && w.Timeout > 0
}

// FCM is FCM HTTP v1 project settings. Access tokens and JSON credentials
// are never stored on this struct.
type FCM struct {
	ProjectID       string
	CredentialsFile string
	Timeout         time.Duration
}

func (f FCM) String() string {
	return fmt.Sprintf("config.FCM{project_id_configured:%t credentials_file_configured:%t}",
		f.ProjectID != "", f.CredentialsFile != "")
}

func (f FCM) GoString() string { return f.String() }

func (f FCM) Wired() bool {
	return f.ProjectID != "" && f.Timeout > 0
}

// APNs is token-based APNs settings. The signing key is never logged.
type APNs struct {
	TeamID      string
	KeyID       string
	Topic       string
	PrivateKey  string
	Environment string
	Timeout     time.Duration
}

func (a APNs) String() string {
	return fmt.Sprintf("config.APNs{team_id_configured:%t key_id_configured:%t topic_configured:%t private_key_configured:%t environment:%s}",
		a.TeamID != "", a.KeyID != "", a.Topic != "", a.PrivateKey != "", a.Environment)
}

func (a APNs) GoString() string { return a.String() }

func (a APNs) Wired() bool {
	return a.TeamID != "" && a.KeyID != "" && a.Topic != "" && a.PrivateKey != "" &&
		(a.Environment == "sandbox" || a.Environment == "production") && a.Timeout > 0
}

// StaffIDP holds issuer/audience/JWKS for a future staff identity adapter.
type StaffIDP struct {
	Issuer   string
	Audience string
	JWKSURL  string
}

func (s StaffIDP) Empty() bool {
	return s.Issuer == "" && s.Audience == "" && s.JWKSURL == ""
}

func (s StaffIDP) Complete() bool {
	return s.Issuer != "" && s.Audience != "" && s.JWKSURL != ""
}

// StaffDevIDP is a runtime-only development Staff Identity Provider fixture.
// Token must never be logged. Roles and staff ID are server-authoritative.
type StaffDevIDP struct {
	Enabled bool
	Token   string
	StaffID string
	Roles   []string
}

func (s StaffDevIDP) Empty() bool {
	return !s.Enabled && s.Token == "" && s.StaffID == "" && len(s.Roles) == 0
}

func (s StaffDevIDP) Complete() bool {
	return s.Enabled && s.Token != "" && s.StaffID != "" && len(s.Roles) > 0
}

// HumanChallenge is the AUTH-A provider-neutral challenge configuration.
// Production must not use the fake provider. Turnstile secret is never logged.
type HumanChallenge struct {
	Provider         string
	Operations       []string
	Hostname         string
	AllowedHostnames []string
	ReplayTTL        time.Duration
	Timeout          time.Duration
	TurnstileSecret  string
	TurnstileSiteKey string
}

func (h HumanChallenge) String() string {
	return fmt.Sprintf("config.HumanChallenge{provider:%s operations:%d hostnames:%d}", h.Provider, len(h.Operations), len(h.AllowedHostnames))
}

func (h HumanChallenge) GoString() string {
	return h.String()
}

func (h HumanChallenge) ProductionWired() bool {
	return strings.EqualFold(strings.TrimSpace(h.Provider), "turnstile") &&
		strings.TrimSpace(h.TurnstileSecret) != "" &&
		len(h.AllowedHostnames) > 0
}

// Load reads configuration from environment variables.
// HTTP bind and timeouts have local defaults. DATABASE_URL is required.
func Load() (Config, error) {
	env, err := parseEnvironment(os.Getenv(envAppEnv))
	if err != nil {
		return Config{}, err
	}
	logLevel, err := parseLogLevel(os.Getenv(envLogLevel))
	if err != nil {
		return Config{}, err
	}
	insecureCookies, err := parseOptionalBool(envAllowInsecureCookies)
	if err != nil {
		return Config{}, err
	}
	proxies, proxiesSet, err := parseTrustedProxies()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment:           env,
		HTTPAddr:              getenv(envHTTPAddr, defaultHTTPAddr),
		ShutdownTimeout:       defaultShutdownTimeout,
		DatabaseURL:           os.Getenv(envDatabaseURL),
		DBConnectTimeout:      defaultDBConnectTimeout,
		ValkeyURL:             os.Getenv(envValkeyURL),
		TrustedProxies:        proxies,
		TrustedProxiesSet:     proxiesSet,
		LogLevel:              logLevel,
		AllowInsecureCookies:  insecureCookies,
		WebAuthnRPDisplayName: os.Getenv(envWebAuthnRPDisplayName),
		WebAuthnRPID:          os.Getenv(envWebAuthnRPID),
		WebAuthnRPOrigins:     splitCommaList(os.Getenv(envWebAuthnRPOrigins)),
	}

	if raw := os.Getenv(envShutdownTimeout); raw != "" {
		d, err := parsePositiveDuration(envShutdownTimeout, raw)
		if err != nil {
			return Config{}, err
		}
		cfg.ShutdownTimeout = d
	}

	if raw := os.Getenv(envDBConnectTimeout); raw != "" {
		d, err := parsePositiveDuration(envDBConnectTimeout, raw)
		if err != nil {
			return Config{}, err
		}
		cfg.DBConnectTimeout = d
	}

	pool, err := parsePool(cfg.DBConnectTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.DBPool = pool

	if cfg.HTTPAddr == "" {
		return Config{}, fmt.Errorf("%s must not be empty", envHTTPAddr)
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("%s must not be empty", envDatabaseURL)
	}

	idle, err := requiredPositiveDuration(envSessionIdle)
	if err != nil {
		return Config{}, err
	}
	absolute, err := requiredPositiveDuration(envSessionAbsolute)
	if err != nil {
		return Config{}, err
	}
	if absolute < idle {
		return Config{}, fmt.Errorf("%s must be greater than or equal to %s", envSessionAbsolute, envSessionIdle)
	}
	cfg.SessionIdle = idle
	cfg.SessionAbsolute = absolute

	stepUpTTL, err := requiredPositiveDuration(envStepUpTTL)
	if err != nil {
		return Config{}, err
	}
	if stepUpTTL > 15*time.Minute {
		return Config{}, fmt.Errorf("%s must not exceed 15m", envStepUpTTL)
	}
	if stepUpTTL > idle {
		return Config{}, fmt.Errorf("%s must be less than or equal to %s", envStepUpTTL, envSessionIdle)
	}
	cfg.StepUpTTL = stepUpTTL

	ceremonyTTL, err := requiredPositiveDuration(envWebAuthnCeremonyTTL)
	if err != nil {
		return Config{}, err
	}
	cfg.WebAuthnCeremonyTTL = ceremonyTTL

	ipMax, err := requiredPositiveInt(envAuthIPMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	ipWindow, err := requiredPositiveDuration(envAuthIPWindow)
	if err != nil {
		return Config{}, err
	}
	userMax, err := requiredPositiveInt(envAuthPasswordUserMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	userWindow, err := requiredPositiveDuration(envAuthPasswordUserWindow)
	if err != nil {
		return Config{}, err
	}
	cfg.AuthIPMaxAttempts = ipMax
	cfg.AuthIPWindow = ipWindow
	cfg.AuthPasswordUserMaxAttempts = userMax
	cfg.AuthPasswordUserWindow = userWindow

	targetMax, err := optionalPositiveInt(envAuthTargetMaxAttempts, userMax)
	if err != nil {
		return Config{}, err
	}
	targetWindow, err := optionalPositiveDuration(envAuthTargetWindow, userWindow)
	if err != nil {
		return Config{}, err
	}
	completeMax, err := optionalPositiveInt(envAuthCompleteMaxAttempts, ipMax)
	if err != nil {
		return Config{}, err
	}
	completeWindow, err := optionalPositiveDuration(envAuthCompleteWindow, ipWindow)
	if err != nil {
		return Config{}, err
	}
	sensitiveMax, err := optionalPositiveInt(envAuthSensitiveMaxAttempts, userMax)
	if err != nil {
		return Config{}, err
	}
	sensitiveWindow, err := optionalPositiveDuration(envAuthSensitiveWindow, userWindow)
	if err != nil {
		return Config{}, err
	}
	cfg.AuthTargetMaxAttempts = targetMax
	cfg.AuthTargetWindow = targetWindow
	cfg.AuthCompleteMaxAttempts = completeMax
	cfg.AuthCompleteWindow = completeWindow
	cfg.AuthSensitiveMaxAttempts = sensitiveMax
	cfg.AuthSensitiveWindow = sensitiveWindow

	human, err := parseHumanChallenge(userWindow)
	if err != nil {
		return Config{}, err
	}
	cfg.HumanChallenge = human

	challengeTTL, err := requiredPositiveDuration(envChallengeTTL)
	if err != nil {
		return Config{}, err
	}
	challengeMax, err := requiredPositiveInt(envChallengeMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	otpDigits, err := requiredPositiveInt(envChallengePhoneOTPDigits)
	if err != nil {
		return Config{}, err
	}
	destMax, err := requiredPositiveInt(envIssueDestMax)
	if err != nil {
		return Config{}, err
	}
	destWindow, err := requiredPositiveDuration(envIssueDestWindow)
	if err != nil {
		return Config{}, err
	}
	issueIPMax, err := requiredPositiveInt(envIssueIPMax)
	if err != nil {
		return Config{}, err
	}
	issueIPWindow, err := requiredPositiveDuration(envIssueIPWindow)
	if err != nil {
		return Config{}, err
	}
	proofTTL, err := requiredPositiveDuration(envSignupProofTTL)
	if err != nil {
		return Config{}, err
	}
	cfg.ChallengeTTL = challengeTTL
	cfg.ChallengeMaxAttempts = challengeMax
	cfg.ChallengePhoneOTPDigits = otpDigits
	cfg.IssueDestMax = destMax
	cfg.IssueDestWindow = destWindow
	cfg.IssueIPMax = issueIPMax
	cfg.IssueIPWindow = issueIPWindow
	cfg.SignupProofTTL = proofTTL

	batch, err := requiredPositiveInt(envOutboxBatchSize)
	if err != nil {
		return Config{}, err
	}
	lease, err := requiredPositiveDuration(envOutboxLease)
	if err != nil {
		return Config{}, err
	}
	poll, err := requiredPositiveDuration(envOutboxPollInterval)
	if err != nil {
		return Config{}, err
	}
	retryBase, err := requiredPositiveDuration(envOutboxRetryBase)
	if err != nil {
		return Config{}, err
	}
	retryMult, err := requiredFloatAtLeast(envOutboxRetryMultiplier, 1)
	if err != nil {
		return Config{}, err
	}
	retryCap, err := requiredPositiveDuration(envOutboxRetryCap)
	if err != nil {
		return Config{}, err
	}
	if retryCap < retryBase {
		return Config{}, fmt.Errorf("%s must be greater than or equal to %s", envOutboxRetryCap, envOutboxRetryBase)
	}
	retryJitter, err := requiredNonNegativeDuration(envOutboxRetryJitter)
	if err != nil {
		return Config{}, err
	}
	cfg.OutboxBatchSize = batch
	cfg.OutboxLease = lease
	cfg.OutboxPollInterval = poll
	cfg.OutboxRetryBase = retryBase
	cfg.OutboxRetryMultiplier = retryMult
	cfg.OutboxRetryCap = retryCap
	cfg.OutboxRetryJitter = retryJitter
	workers, err := parseOutboxWorkerConcurrency()
	if err != nil {
		return Config{}, err
	}
	cfg.OutboxWorkerConcurrency = workers

	keys, err := parseMaterialKeys()
	if err != nil {
		return Config{}, err
	}
	cfg.MaterialKeys = keys

	emailMode, err := parseNotificationChannelMode(envNotificationsEmailMode)
	if err != nil {
		return Config{}, err
	}
	smsMode, err := parseNotificationChannelMode(envNotificationsSMSMode)
	if err != nil {
		return Config{}, err
	}
	cfg.NotificationsEmailMode = emailMode
	cfg.NotificationsSMSMode = smsMode
	email, err := parseEmail(emailMode)
	if err != nil {
		return Config{}, err
	}
	cfg.Email = email
	sms, err := parseSMS(smsMode)
	if err != nil {
		return Config{}, err
	}
	cfg.SMS = sms

	push, err := parsePushEndpoints()
	if err != nil {
		return Config{}, err
	}
	cfg.PushEndpoints = push
	webPush, err := parseWebPush()
	if err != nil {
		return Config{}, err
	}
	cfg.WebPush = webPush
	fcm, err := parseFCM()
	if err != nil {
		return Config{}, err
	}
	cfg.FCM = fcm
	apns, err := parseAPNs()
	if err != nil {
		return Config{}, err
	}
	cfg.APNs = apns

	malwareReq, err := parseOptionalBool(envMediaMalwareScanRequired)
	if err != nil {
		return Config{}, err
	}
	moderationReq, err := parseOptionalBool(envMediaImageModerationRequired)
	if err != nil {
		return Config{}, err
	}
	cfg.MediaMalwareScanRequired = malwareReq
	cfg.MediaImageModerationRequired = moderationReq

	staffIDP := StaffIDP{
		Issuer:   strings.TrimSpace(os.Getenv(envStaffIDPIssuer)),
		Audience: strings.TrimSpace(os.Getenv(envStaffIDPAudience)),
		JWKSURL:  strings.TrimSpace(os.Getenv(envStaffIDPJWKSURL)),
	}
	if !staffIDP.Empty() && !staffIDP.Complete() {
		return Config{}, fmt.Errorf("STAFF_IDP_ISSUER, STAFF_IDP_AUDIENCE, and STAFF_IDP_JWKS_URL must all be set together")
	}
	cfg.StaffIDP = staffIDP

	staffDevIDP, err := parseStaffDevIDP()
	if err != nil {
		return Config{}, err
	}
	if !staffDevIDP.Empty() && !staffIDP.Empty() {
		return Config{}, fmt.Errorf("%s cannot be combined with STAFF_IDP_*", envStaffDevIDPEnabled)
	}
	cfg.StaffDevIDP = staffDevIDP

	if err := applyRuntimeGates(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func parseStaffDevIDP() (StaffDevIDP, error) {
	enabled, err := parseOptionalBool(envStaffDevIDPEnabled)
	if err != nil {
		return StaffDevIDP{}, err
	}
	dev := StaffDevIDP{
		Enabled: enabled,
		Token:   strings.TrimSpace(os.Getenv(envStaffDevIDPToken)),
		StaffID: strings.TrimSpace(os.Getenv(envStaffDevIDPStaffID)),
		Roles:   splitCommaList(os.Getenv(envStaffDevIDPRoles)),
	}
	if dev.Empty() {
		return StaffDevIDP{}, nil
	}
	if !dev.Complete() {
		return StaffDevIDP{}, fmt.Errorf("%s, %s, %s, and %s must all be set together", envStaffDevIDPEnabled, envStaffDevIDPToken, envStaffDevIDPStaffID, envStaffDevIDPRoles)
	}
	return dev, nil
}

func parseHumanChallenge(fallbackWindow time.Duration) (HumanChallenge, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(envHumanChallengeProvider)))
	if provider == "" {
		provider = "none"
	}
	switch provider {
	case "none", "fake", "unconfigured", "turnstile":
	default:
		return HumanChallenge{}, fmt.Errorf("%s must be none, fake, unconfigured, or turnstile", envHumanChallengeProvider)
	}
	ops := splitCommaList(os.Getenv(envHumanChallengeOperations))
	known := map[string]struct{}{
		"password_login": {}, "passkey_login_begin": {}, "passkey_login_finish": {},
		"signup_start": {}, "signup_finish": {}, "signup_complete": {},
		"reset_start": {}, "reset_verify": {}, "reset_complete": {},
		"passkey_register_begin": {}, "passkey_register_finish": {},
	}
	for _, op := range ops {
		if _, ok := known[strings.ToLower(op)]; !ok {
			return HumanChallenge{}, fmt.Errorf("%s contains unknown operation", envHumanChallengeOperations)
		}
	}
	ttl := time.Duration(0)
	if raw := strings.TrimSpace(os.Getenv(envHumanChallengeReplayTTL)); raw != "" {
		d, err := parsePositiveDuration(envHumanChallengeReplayTTL, raw)
		if err != nil {
			return HumanChallenge{}, err
		}
		ttl = d
	} else if len(ops) > 0 {
		ttl = fallbackWindow
		if ttl <= 0 {
			ttl = 10 * time.Minute
		}
	}
	hosts := splitCommaList(os.Getenv(envHumanChallengeHostname))
	hostname := ""
	if len(hosts) > 0 {
		hostname = hosts[0]
	}
	timeout := time.Duration(0)
	if raw := strings.TrimSpace(os.Getenv(envHumanChallengeTimeout)); raw != "" {
		d, err := parsePositiveDuration(envHumanChallengeTimeout, raw)
		if err != nil {
			return HumanChallenge{}, err
		}
		timeout = d
	} else if provider == "turnstile" {
		timeout = defaultHumanChallengeTimeout
	}
	return HumanChallenge{
		Provider:         provider,
		Operations:       ops,
		Hostname:         hostname,
		AllowedHostnames: hosts,
		ReplayTTL:        ttl,
		Timeout:          timeout,
		TurnstileSecret:  strings.TrimSpace(os.Getenv(envTurnstileSecret)),
		TurnstileSiteKey: strings.TrimSpace(os.Getenv(envTurnstileSiteKey)),
	}, nil
}

func optionalPositiveInt(name string, fallback int) (int, error) {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		return fallback, nil
	}
	return requiredPositiveInt(name)
}

func optionalPositiveDuration(name string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		return fallback, nil
	}
	return requiredPositiveDuration(name)
}

func parseOptionalBool(name string) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return false, nil
	}
	switch raw {
	case "1", "true", "yes", "required":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func parseEmail(emailMode string) (Email, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(envEmailProvider)))
	if provider == "" && emailMode != NotificationChannelExternal {
		return Email{}, nil
	}
	if provider == "" {
		return Email{}, fmt.Errorf("%s must be %s when %s=%s", envEmailProvider, EmailProviderSES, envNotificationsEmailMode, NotificationChannelExternal)
	}
	if provider != EmailProviderSES {
		return Email{}, fmt.Errorf("%s must be %s", envEmailProvider, EmailProviderSES)
	}
	out := Email{
		Provider: EmailProviderSES,
		Region:   strings.TrimSpace(os.Getenv(envEmailSESRegion)),
		From:     strings.TrimSpace(os.Getenv(envEmailSESFrom)),
		Timeout:  defaultEmailSESTimeout,
	}
	if raw := strings.TrimSpace(os.Getenv(envEmailSESTimeout)); raw != "" {
		d, err := parsePositiveDuration(envEmailSESTimeout, raw)
		if err != nil {
			return Email{}, err
		}
		out.Timeout = d
	}
	if out.Timeout > 30*time.Second {
		return Email{}, fmt.Errorf("%s must be 30s or less", envEmailSESTimeout)
	}
	if out.Region == "" {
		return Email{}, fmt.Errorf("%s must not be empty when %s=%s", envEmailSESRegion, envEmailProvider, EmailProviderSES)
	}
	if strings.ContainsAny(out.Region, " \t\r\n") {
		return Email{}, fmt.Errorf("%s is malformed", envEmailSESRegion)
	}
	if out.From == "" {
		return Email{}, fmt.Errorf("%s must not be empty when %s=%s", envEmailSESFrom, envEmailProvider, EmailProviderSES)
	}
	if strings.ContainsAny(out.From, "\r\n") || !strings.Contains(out.From, "@") {
		return Email{}, fmt.Errorf("%s is malformed", envEmailSESFrom)
	}
	return out, nil
}

func parseSMS(smsMode string) (SMS, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(envSMSProvider)))
	if provider == "" && smsMode != NotificationChannelExternal {
		return SMS{}, nil
	}
	if provider == "" {
		return SMS{}, fmt.Errorf("%s must be %s when %s=%s", envSMSProvider, SMSProviderNetgsm, envNotificationsSMSMode, NotificationChannelExternal)
	}
	if provider != SMSProviderNetgsm {
		return SMS{}, fmt.Errorf("%s must be %s", envSMSProvider, SMSProviderNetgsm)
	}
	out := SMS{
		Provider:  SMSProviderNetgsm,
		Username:  strings.TrimSpace(os.Getenv(envNetgsmUsername)),
		Password:  strings.TrimSpace(os.Getenv(envNetgsmPassword)),
		MsgHeader: strings.TrimSpace(os.Getenv(envNetgsmMsgHeader)),
		Timeout:   defaultNetgsmTimeout,
	}
	if raw := strings.TrimSpace(os.Getenv(envNetgsmTimeout)); raw != "" {
		d, err := parsePositiveDuration(envNetgsmTimeout, raw)
		if err != nil {
			return SMS{}, err
		}
		out.Timeout = d
	}
	if out.Timeout > 30*time.Second {
		return SMS{}, fmt.Errorf("%s must be 30s or less", envNetgsmTimeout)
	}
	if out.Username == "" {
		return SMS{}, fmt.Errorf("%s must not be empty when %s=%s", envNetgsmUsername, envSMSProvider, SMSProviderNetgsm)
	}
	if strings.ContainsAny(out.Username, " \t\r\n") {
		return SMS{}, fmt.Errorf("%s is malformed", envNetgsmUsername)
	}
	if out.Password == "" {
		return SMS{}, fmt.Errorf("%s must not be empty when %s=%s", envNetgsmPassword, envSMSProvider, SMSProviderNetgsm)
	}
	if strings.ContainsAny(out.Password, "\r\n") {
		return SMS{}, fmt.Errorf("%s is malformed", envNetgsmPassword)
	}
	if out.MsgHeader == "" {
		return SMS{}, fmt.Errorf("%s must not be empty when %s=%s", envNetgsmMsgHeader, envSMSProvider, SMSProviderNetgsm)
	}
	if strings.ContainsAny(out.MsgHeader, "\r\n") {
		return SMS{}, fmt.Errorf("%s is malformed", envNetgsmMsgHeader)
	}
	return out, nil
}

func parseNotificationChannelMode(name string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return NotificationChannelDisabled, nil
	}
	switch raw {
	case NotificationChannelDisabled, NotificationChannelExternal:
		return raw, nil
	default:
		return "", fmt.Errorf("%s must be %s or %s", name, NotificationChannelDisabled, NotificationChannelExternal)
	}
}

func requiredPositiveInt(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s must not be empty", name)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return n, nil
}

func requiredPositiveDuration(name string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s must not be empty", name)
	}
	return parsePositiveDuration(name, raw)
}

func requiredNonNegativeDuration(name string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s must not be empty", name)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return d, nil
}

func requiredFloatAtLeast(name string, min float64) (float64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s must not be empty", name)
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < min {
		return 0, fmt.Errorf("%s must be greater than or equal to %g", name, min)
	}
	return n, nil
}

func parsePositiveDuration(name, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return d, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCommaList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
