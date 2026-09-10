package verified

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	verifiedcontracts "backend/internal/verified/contracts"
)

const (
	StatusRequested = "requested"
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
	StatusCancelled = "cancelled"
	StatusCompleted = "completed"
	StatusNoShow    = "no_show"

	MethodOTP = "otp"
	MethodQR  = "qr"

	InteractionListingInspection = verifiedcontracts.InteractionTypeListingInspection
	InteractionTransaction       = verifiedcontracts.InteractionTypeTransaction
	InteractionDelivery          = verifiedcontracts.InteractionTypeDelivery

	FlowOpen      = "open"
	FlowCompleted = "completed"

	EventTypeInteractionCompleted = verifiedcontracts.EventTypeInteractionCompleted
	EventVersion                  = verifiedcontracts.EventVersion

	TokenSecretBytes     = 32
	TokenHashSize        = sha256.Size
	MaxAppointments      = 50
	MaxVerificationFlows = 50
	DefaultChallengeTTL  = 5 * time.Minute
	DefaultOTPDigits     = 6
	MaxOTPDigits         = 12
)

var (
	errZeroID             = errors.New("verified id must not be zero")
	errStoreRequired      = errors.New("verified store required")
	errUnavailable        = errors.New("verified unavailable")
	errNotFound           = errors.New("appointment not found")
	errForbidden          = errors.New("verified access denied")
	errListingsReq        = errors.New("listings source required")
	errOutboxRequired     = errors.New("verified outbox required")
	errSelfAppointment    = errors.New("self appointment")
	errInvalidStatus      = errors.New("invalid appointment status")
	errInvalidTransition  = errors.New("invalid appointment transition")
	errInvalidMethod      = errors.New("invalid verification method")
	errInvalidToken       = errors.New("invalid verification token")
	errExpiredChallenge   = errors.New("verification challenge expired")
	errChallengeConsumed  = errors.New("verification challenge consumed")
	errInvalidAppointment = errors.New("invalid appointment")
	errInvalidChallenge   = errors.New("invalid verification challenge")
	errInvalidInteraction = errors.New("invalid verified interaction")
	errConflict           = errors.New("verified conflict")
	errInvalidPolicy      = errors.New("invalid verified policy")
)

var (
	ErrZeroID             = errZeroID
	ErrStoreRequired      = errStoreRequired
	ErrUnavailable        = errUnavailable
	ErrNotFound           = errNotFound
	ErrForbidden          = errForbidden
	ErrListingsReq        = errListingsReq
	ErrOutboxRequired     = errOutboxRequired
	ErrSelfAppointment    = errSelfAppointment
	ErrInvalidStatus      = errInvalidStatus
	ErrInvalidTransition  = errInvalidTransition
	ErrInvalidMethod      = errInvalidMethod
	ErrInvalidToken       = errInvalidToken
	ErrExpiredChallenge   = errExpiredChallenge
	ErrChallengeConsumed  = errChallengeConsumed
	ErrInvalidAppointment = errInvalidAppointment
	ErrInvalidInteraction = errInvalidInteraction
	ErrConflict           = errConflict
	ErrInvalidPolicy      = errInvalidPolicy
)

// Challenge secrets are method-specific: method=qr issues a high-entropy opaque
// token (HTTP start also returns it as qrPayload). method=otp issues a short
// numeric code for manual entry. Both are stored hash-only; the raw secret is
// returned once at start and must never be persisted or logged.

// ID is an appointment, challenge, interaction, user, or listing UUID.
// Verified does not own identity or listings tables.
type ID [16]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func ParseID(s string) (ID, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ID{}, errZeroID
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errZeroID
	}
	var id ID
	copy(id[:], b)
	if id.IsZero() {
		return ID{}, errZeroID
	}
	return id, nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

type Policy struct {
	ChallengeTTL time.Duration
	OTPDigits    int
}

func (p Policy) Validate() error {
	if p.ChallengeTTL <= 0 {
		return errInvalidPolicy
	}
	if p.OTPDigits <= 0 || p.OTPDigits > MaxOTPDigits {
		return errInvalidPolicy
	}
	return nil
}

func DefaultPolicy() Policy {
	return Policy{ChallengeTTL: DefaultChallengeTTL, OTPDigits: DefaultOTPDigits}
}

type Appointment struct {
	ID              ID
	ListingID       ID
	RequesterUserID ID
	ProviderUserID  ID
	Status          string
	RequestedAt     time.Time
	ScheduledAt     *time.Time
	UpdatedAt       time.Time
	// VerifiedInteractionID is a read-side join from verified_interactions. It is not a persisted appointments column.
	VerifiedInteractionID *ID
}

func (a Appointment) Validate() error {
	if a.ID.IsZero() || a.ListingID.IsZero() || a.RequesterUserID.IsZero() || a.ProviderUserID.IsZero() {
		return errZeroID
	}
	if a.RequesterUserID == a.ProviderUserID {
		return errSelfAppointment
	}
	if !validStatus(a.Status) {
		return errInvalidStatus
	}
	if a.RequestedAt.IsZero() || a.UpdatedAt.IsZero() {
		return errInvalidAppointment
	}
	return nil
}

func (a Appointment) HasParticipant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return a.RequesterUserID == userID || a.ProviderUserID == userID
}

func validStatus(status string) bool {
	switch status {
	case StatusRequested, StatusAccepted, StatusRejected, StatusCancelled, StatusCompleted, StatusNoShow:
		return true
	default:
		return false
	}
}

func canCancel(status string) bool {
	return status == StatusRequested || status == StatusAccepted
}

func NormalizeMethod(raw string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case MethodOTP:
		return MethodOTP, nil
	case MethodQR:
		return MethodQR, nil
	default:
		return "", errInvalidMethod
	}
}

func NormalizeInteractionType(raw string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case InteractionListingInspection:
		return InteractionListingInspection, nil
	case InteractionTransaction:
		return InteractionTransaction, nil
	case InteractionDelivery:
		return InteractionDelivery, nil
	default:
		return "", errInvalidInteraction
	}
}

func NormalizeFlowInteractionType(raw string) (string, error) {
	typ, err := NormalizeInteractionType(raw)
	if err != nil {
		return "", err
	}
	if typ == InteractionListingInspection {
		return "", errInvalidInteraction
	}
	return typ, nil
}

// Challenge is stored hash-only. RawToken must never be persisted or logged.
type Challenge struct {
	ID            ID
	AppointmentID ID
	FlowID        ID
	Method        string
	TokenHash     []byte
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	CreatedAt     time.Time
}

func (c Challenge) Validate() error {
	if c.ID.IsZero() {
		return errZeroID
	}
	appt := !c.AppointmentID.IsZero()
	flow := !c.FlowID.IsZero()
	if appt == flow {
		return errInvalidChallenge
	}
	if _, err := NormalizeMethod(c.Method); err != nil {
		return err
	}
	if len(c.TokenHash) != TokenHashSize {
		return errInvalidChallenge
	}
	if c.CreatedAt.IsZero() || c.ExpiresAt.IsZero() || !c.ExpiresAt.After(c.CreatedAt) {
		return errInvalidChallenge
	}
	return nil
}

func (c Challenge) Consumed() bool {
	return c.ConsumedAt != nil && !c.ConsumedAt.IsZero()
}

func (c Challenge) Expired(now time.Time) bool {
	return !now.Before(c.ExpiresAt)
}

type IssuedChallenge struct {
	Challenge       Challenge
	RawToken        string
	InteractionType string
}

func (IssuedChallenge) String() string {
	return "verified.IssuedChallenge"
}

func (IssuedChallenge) GoString() string {
	return "verified.IssuedChallenge{}"
}

type VerificationFlow struct {
	ID              ID
	ListingID       ID
	RequesterUserID ID
	ProviderUserID  ID
	InteractionType string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// CompletedInteractionID is a read-side join from verified_interactions. It is not a persisted flows column.
	CompletedInteractionID *ID
}

func (f VerificationFlow) Validate() error {
	if f.ID.IsZero() || f.ListingID.IsZero() || f.RequesterUserID.IsZero() || f.ProviderUserID.IsZero() {
		return errZeroID
	}
	if f.RequesterUserID == f.ProviderUserID {
		return errSelfAppointment
	}
	if _, err := NormalizeFlowInteractionType(f.InteractionType); err != nil {
		return err
	}
	if f.Status != FlowOpen && f.Status != FlowCompleted {
		return errInvalidStatus
	}
	if f.CreatedAt.IsZero() || f.UpdatedAt.IsZero() {
		return errInvalidInteraction
	}
	return nil
}

func (f VerificationFlow) HasParticipant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return f.RequesterUserID == userID || f.ProviderUserID == userID
}

type VerifiedInteraction struct {
	ID                 ID
	AppointmentID      ID
	FlowID             ID
	ListingID          ID
	RequesterUserID    ID
	ProviderUserID     ID
	InteractionType    string
	VerificationMethod string
	VerifiedAt         time.Time
}

func (v VerifiedInteraction) Validate() error {
	if v.ID.IsZero() || v.ListingID.IsZero() || v.RequesterUserID.IsZero() || v.ProviderUserID.IsZero() {
		return errZeroID
	}
	if v.RequesterUserID == v.ProviderUserID {
		return errSelfAppointment
	}
	typ, err := NormalizeInteractionType(v.InteractionType)
	if err != nil {
		return err
	}
	appt := !v.AppointmentID.IsZero()
	flow := !v.FlowID.IsZero()
	switch typ {
	case InteractionListingInspection:
		if !appt || flow {
			return errInvalidInteraction
		}
	case InteractionTransaction, InteractionDelivery:
		if appt || !flow {
			return errInvalidInteraction
		}
	default:
		return errInvalidInteraction
	}
	if _, err := NormalizeMethod(v.VerificationMethod); err != nil {
		return err
	}
	if v.VerifiedAt.IsZero() {
		return errInvalidInteraction
	}
	return nil
}

func (v VerifiedInteraction) HasParticipant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return v.RequesterUserID == userID || v.ProviderUserID == userID
}

func GenerateChallengeToken() (raw string, hash []byte, err error) {
	secret := make([]byte, TokenSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(secret)
	out := make([]byte, TokenHashSize)
	copy(out, sum[:])
	return base64.RawURLEncoding.EncodeToString(secret), out, nil
}

func HashChallengeToken(raw string) ([]byte, error) {
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(secret) < TokenSecretBytes {
		return nil, errInvalidToken
	}
	sum := sha256.Sum256(secret)
	out := make([]byte, TokenHashSize)
	copy(out, sum[:])
	return out, nil
}

// GenerateNumericOTP returns a cryptographically random decimal code of the given width
// (leading zeroes preserved) and the SHA-256 of that digit string. The raw OTP is transient.
func GenerateNumericOTP(digits int) (raw string, hash []byte, err error) {
	raw, err = generateNumericOTP(digits)
	if err != nil {
		return "", nil, err
	}
	hash, err = HashNumericOTP(raw)
	if err != nil {
		return "", nil, err
	}
	return raw, hash, nil
}

func HashNumericOTP(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errInvalidToken
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return nil, errInvalidToken
		}
	}
	sum := sha256.Sum256([]byte(raw))
	out := make([]byte, TokenHashSize)
	copy(out, sum[:])
	return out, nil
}

// HashSubmittedChallengeSecret hashes a finish presentation. Six-digit (policy width)
// numeric strings use the OTP hash; all other values use the QR opaque-token hash.
func HashSubmittedChallengeSecret(raw string, otpDigits int) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if looksLikeNumericOTP(raw, otpDigits) {
		return HashNumericOTP(raw)
	}
	return HashChallengeToken(raw)
}

func looksLikeNumericOTP(raw string, digits int) bool {
	if digits <= 0 || len(raw) != digits {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func generateNumericOTP(digits int) (string, error) {
	if digits <= 0 || digits > MaxOTPDigits {
		return "", errInvalidPolicy
	}
	modulus := uint64(1)
	for i := 0; i < digits; i++ {
		if modulus > math.MaxUint64/10 {
			return "", errInvalidPolicy
		}
		modulus *= 10
	}
	limit := (math.MaxUint64 / modulus) * modulus
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		v := binary.BigEndian.Uint64(buf[:])
		if v >= limit {
			continue
		}
		return fmt.Sprintf("%0*d", digits, v%modulus), nil
	}
}

type InteractionCompletedPayload = verifiedcontracts.InteractionCompletedPayload
