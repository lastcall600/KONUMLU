package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	errZeroID               = errors.New("media id must not be zero")
	errInvalidAsset         = errors.New("invalid media asset")
	errInvalidKind          = errors.New("invalid media kind")
	errInvalidStatus        = errors.New("invalid media status")
	errInvalidTransition    = errors.New("invalid media status transition")
	errInvalidMetadata      = errors.New("invalid media metadata")
	errInvalidObjectKey     = errors.New("invalid media object key")
	errInvalidFilename      = errors.New("invalid original filename")
	errNotUsable            = errors.New("media asset is not usable")
	errAlreadyAttached      = errors.New("media asset already attached")
	errNotAttached          = errors.New("media asset is not attached")
	errForbidden            = errors.New("media access denied")
	errStoreRequired        = errors.New("media store required")
	errStorageRequired      = errors.New("media object storage required")
	errUnavailable          = errors.New("media unavailable")
	errNotFound             = errors.New("media asset not found")
	errConflict             = errors.New("media conflict")
	errObjectMissing        = errors.New("media object not uploaded")
	errObjectTooLarge       = errors.New("media object exceeds upload limit")
	errInvalidOrder         = errors.New("invalid listing image order")
	errUnsupportedImage     = errors.New("media image format is unsupported")
	errInvalidImage         = errors.New("media image is invalid")
	errScannerUnavailable   = errors.New("media scanner unavailable")
	errModeratorUnavailable = errors.New("media moderator unavailable")
)

// Exported sentinels for tests and later HTTP adapters.
var (
	ErrZeroID               = errZeroID
	ErrInvalidAsset         = errInvalidAsset
	ErrInvalidKind          = errInvalidKind
	ErrInvalidStatus        = errInvalidStatus
	ErrInvalidTransition    = errInvalidTransition
	ErrInvalidMetadata      = errInvalidMetadata
	ErrInvalidObjectKey     = errInvalidObjectKey
	ErrInvalidFilename      = errInvalidFilename
	ErrNotUsable            = errNotUsable
	ErrAlreadyAttached      = errAlreadyAttached
	ErrNotAttached          = errNotAttached
	ErrForbidden            = errForbidden
	ErrStoreRequired        = errStoreRequired
	ErrStorageRequired      = errStorageRequired
	ErrUnavailable          = errUnavailable
	ErrNotFound             = errNotFound
	ErrConflict             = errConflict
	ErrObjectMissing        = errObjectMissing
	ErrObjectTooLarge       = errObjectTooLarge
	ErrInvalidOrder         = errInvalidOrder
	ErrUnsupportedImage     = errUnsupportedImage
	ErrInvalidImage         = errInvalidImage
	ErrScannerUnavailable   = errScannerUnavailable
	ErrModeratorUnavailable = errModeratorUnavailable
)

// Kind is the media type. V1 foundation supports listing images only.
type Kind string

const KindListingImage Kind = "listing_image"

func (k Kind) valid() bool {
	return k == KindListingImage
}

// Status is the media upload/processing lifecycle. Bytes remain untrusted until ready.
type Status string

const (
	StatusPendingUpload Status = "pending_upload"
	StatusUploaded      Status = "uploaded"
	StatusProcessing    Status = "processing"
	StatusReady         Status = "ready"
	StatusRejected      Status = "rejected"
	StatusDeleted       Status = "deleted"
)

func (s Status) valid() bool {
	switch s {
	case StatusPendingUpload, StatusUploaded, StatusProcessing, StatusReady, StatusRejected, StatusDeleted:
		return true
	default:
		return false
	}
}

// ID is an application-generated UUID. The database does not mint IDs.
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
		return ID{}, errInvalidAsset
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidAsset
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

const (
	listingImageKeyRoot     = "media/listing-images/"
	processedKeyMarker      = "p"
	objectKeyRandomByteSize = 16
	maxImageWidth           = 8192
	maxImageHeight          = 8192
	maxImagePixels          = 32_000_000
	defaultProcessMaxBytes  = 10 << 20
)

// ObjectKeyPrefix is the server-owned key prefix for a listing-image asset.
func ObjectKeyPrefix(ownerUserID, assetID ID) string {
	return listingImageKeyRoot + ownerUserID.String() + "/" + assetID.String() + "/"
}

// ObjectKeyFor is the only allowed original object key shape. Clients cannot choose keys.
func ObjectKeyFor(ownerUserID, assetID ID, random string) string {
	return ObjectKeyPrefix(ownerUserID, assetID) + random
}

// ProcessedObjectKeyFor is the trusted processed-object key. It never overwrites the original.
func ProcessedObjectKeyFor(ownerUserID, assetID ID, random string) string {
	return ObjectKeyPrefix(ownerUserID, assetID) + processedKeyMarker + "/" + random
}

func objectKeyRandom(ownerUserID, assetID ID, key string) (string, bool) {
	prefix := ObjectKeyPrefix(ownerUserID, assetID)
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	return strings.TrimPrefix(key, prefix), true
}

// ProcessedObjectKeyFromOriginal derives the processed key from the untrusted original key.
func ProcessedObjectKeyFromOriginal(ownerUserID, assetID ID, original string) (string, error) {
	random, ok := objectKeyRandom(ownerUserID, assetID, original)
	if !ok || !validObjectKeyRandom(random) || !IsOriginalObjectKey(original) {
		return "", errInvalidObjectKey
	}
	return ProcessedObjectKeyFor(ownerUserID, assetID, random), nil
}

func newObjectKeyRandom() (string, error) {
	var b [objectKeyRandomByteSize]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errUnavailable
	}
	return hex.EncodeToString(b[:]), nil
}

func validObjectKeyRandom(random string) bool {
	if len(random) != objectKeyRandomByteSize*2 {
		return false
	}
	for i := 0; i < len(random); i++ {
		c := random[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func listingImageKeyParts(key string) ([]string, bool) {
	if key == "" || strings.Contains(key, "..") || strings.ContainsAny(key, "\\ \t\r\n") || strings.ContainsRune(key, 0) {
		return nil, false
	}
	if strings.Contains(key, ".") {
		return nil, false
	}
	if !strings.HasPrefix(key, listingImageKeyRoot) {
		return nil, false
	}
	rest := strings.TrimPrefix(key, listingImageKeyRoot)
	parts := strings.Split(rest, "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
		return nil, false
	}
	if _, err := ParseID(parts[0]); err != nil {
		return nil, false
	}
	if _, err := ParseID(parts[1]); err != nil {
		return nil, false
	}
	return parts, true
}

// IsOriginalObjectKey reports whether key is the untrusted client-upload object key.
func IsOriginalObjectKey(key string) bool {
	parts, ok := listingImageKeyParts(key)
	if !ok || len(parts) != 3 {
		return false
	}
	return validObjectKeyRandom(parts[2])
}

// IsProcessedObjectKey reports whether key is the trusted processed-object key.
func IsProcessedObjectKey(key string) bool {
	parts, ok := listingImageKeyParts(key)
	if !ok || len(parts) != 4 {
		return false
	}
	return parts[2] == processedKeyMarker && validObjectKeyRandom(parts[3])
}

// IsServerObjectKey reports whether key is a server-generated listing-image object key.
func IsServerObjectKey(key string) bool {
	return IsOriginalObjectKey(key) || IsProcessedObjectKey(key)
}

func objectKeyMatchesAsset(ownerUserID, assetID ID, key string) bool {
	random, ok := objectKeyRandom(ownerUserID, assetID, key)
	return ok && validObjectKeyRandom(random) && IsOriginalObjectKey(key)
}

func processedObjectKeyMatchesAsset(ownerUserID, assetID ID, original, processed string) bool {
	want, err := ProcessedObjectKeyFromOriginal(ownerUserID, assetID, original)
	return err == nil && processed == want && IsProcessedObjectKey(processed)
}

// ValidatedMetadata is processor-attested image metadata. Client claims are not this type.
type ValidatedMetadata struct {
	ContentType string
	SizeBytes   int64
	Width       int
	Height      int
}

func (m ValidatedMetadata) Validate() error {
	ct := strings.ToLower(strings.TrimSpace(m.ContentType))
	if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
		return errInvalidMetadata
	}
	if m.SizeBytes <= 0 || m.Width <= 0 || m.Height <= 0 {
		return errInvalidMetadata
	}
	return nil
}

// Asset is durable media metadata. Object bytes are not stored in PostgreSQL.
type Asset struct {
	ID                 ID
	OwnerUserID        ID
	ListingID          *ID
	Kind               Kind
	Status             Status
	ObjectKey          string
	ProcessedObjectKey string
	OriginalFilename   *string
	ContentType        *string
	SizeBytes          *int64
	Width              *int
	Height             *int
	SortOrder          *int
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ReadyAt            *time.Time
	RejectedAt         *time.Time
	DeletedAt          *time.Time
}

func (a Asset) UsableAsListingMedia() bool {
	return a.Status == StatusReady && a.ListingID != nil && a.DeletedAt == nil && a.RejectedAt == nil
}

func (a Asset) Validate() error {
	if a.ID.IsZero() || a.OwnerUserID.IsZero() {
		return errZeroID
	}
	if a.ListingID != nil && a.ListingID.IsZero() {
		return errZeroID
	}
	if !a.Kind.valid() {
		return errInvalidKind
	}
	if !a.Status.valid() {
		return errInvalidStatus
	}
	if !objectKeyMatchesAsset(a.OwnerUserID, a.ID, a.ObjectKey) {
		return errInvalidObjectKey
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return errInvalidAsset
	}
	if a.SortOrder != nil && *a.SortOrder < 0 {
		return errInvalidAsset
	}
	switch a.Status {
	case StatusPendingUpload, StatusUploaded, StatusProcessing:
		if a.ContentType != nil || a.SizeBytes != nil || a.Width != nil || a.Height != nil {
			return errInvalidAsset
		}
		if a.ProcessedObjectKey != "" {
			return errInvalidAsset
		}
		if a.ReadyAt != nil || a.RejectedAt != nil || a.DeletedAt != nil {
			return errInvalidAsset
		}
	case StatusReady:
		if a.ReadyAt == nil || a.RejectedAt != nil || a.DeletedAt != nil {
			return errInvalidAsset
		}
		if !processedObjectKeyMatchesAsset(a.OwnerUserID, a.ID, a.ObjectKey, a.ProcessedObjectKey) {
			return errInvalidObjectKey
		}
		if err := validatedFromAsset(a).Validate(); err != nil {
			return err
		}
	case StatusRejected:
		if a.RejectedAt == nil || a.DeletedAt != nil {
			return errInvalidAsset
		}
	case StatusDeleted:
		if a.DeletedAt == nil {
			return errInvalidAsset
		}
	}
	return nil
}

func validatedFromAsset(a Asset) ValidatedMetadata {
	m := ValidatedMetadata{}
	if a.ContentType != nil {
		m.ContentType = *a.ContentType
	}
	if a.SizeBytes != nil {
		m.SizeBytes = *a.SizeBytes
	}
	if a.Width != nil {
		m.Width = *a.Width
	}
	if a.Height != nil {
		m.Height = *a.Height
	}
	return m
}

func NewPendingAsset(ownerUserID ID, originalFilename *string, now time.Time) (Asset, error) {
	if ownerUserID.IsZero() {
		return Asset{}, errZeroID
	}
	if now.IsZero() {
		return Asset{}, errInvalidAsset
	}
	filename, err := normalizeFilename(originalFilename)
	if err != nil {
		return Asset{}, err
	}
	id, err := NewID()
	if err != nil {
		return Asset{}, errUnavailable
	}
	random, err := newObjectKeyRandom()
	if err != nil {
		return Asset{}, err
	}
	a := Asset{
		ID:               id,
		OwnerUserID:      ownerUserID,
		Kind:             KindListingImage,
		Status:           StatusPendingUpload,
		ObjectKey:        ObjectKeyFor(ownerUserID, id, random),
		OriginalFilename: filename,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := a.Validate(); err != nil {
		return Asset{}, err
	}
	return a, nil
}

func (a Asset) MarkUploaded(now time.Time) (Asset, error) {
	if a.Status != StatusPendingUpload {
		return Asset{}, errInvalidTransition
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	a.Status = StatusUploaded
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) MarkProcessing(now time.Time) (Asset, error) {
	if a.Status != StatusUploaded {
		return Asset{}, errInvalidTransition
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	a.Status = StatusProcessing
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) MarkReady(meta ValidatedMetadata, processedKey string, now time.Time) (Asset, error) {
	if a.Status != StatusProcessing {
		return Asset{}, errInvalidTransition
	}
	if err := meta.Validate(); err != nil {
		return Asset{}, err
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	if !processedObjectKeyMatchesAsset(a.OwnerUserID, a.ID, a.ObjectKey, processedKey) {
		return Asset{}, errInvalidObjectKey
	}
	ct := strings.ToLower(strings.TrimSpace(meta.ContentType))
	size := meta.SizeBytes
	w := meta.Width
	h := meta.Height
	at := now
	a.Status = StatusReady
	a.ProcessedObjectKey = processedKey
	a.ContentType = &ct
	a.SizeBytes = &size
	a.Width = &w
	a.Height = &h
	a.ReadyAt = &at
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) Reject(now time.Time) (Asset, error) {
	switch a.Status {
	case StatusPendingUpload, StatusUploaded, StatusProcessing:
	default:
		return Asset{}, errInvalidTransition
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	at := now
	a.Status = StatusRejected
	a.RejectedAt = &at
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) Delete(now time.Time) (Asset, error) {
	if a.Status == StatusDeleted {
		return Asset{}, errInvalidTransition
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	at := now
	a.Status = StatusDeleted
	a.DeletedAt = &at
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) AttachListing(listingID ID, now time.Time) (Asset, error) {
	if listingID.IsZero() {
		return Asset{}, errZeroID
	}
	if a.Status == StatusRejected || a.Status == StatusDeleted {
		return Asset{}, errNotUsable
	}
	if a.ListingID != nil {
		return Asset{}, errAlreadyAttached
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	id := listingID
	a.ListingID = &id
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) DetachListing(now time.Time) (Asset, error) {
	if a.Status == StatusRejected || a.Status == StatusDeleted {
		return Asset{}, errNotUsable
	}
	if a.ListingID == nil {
		return Asset{}, errNotAttached
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	a.ListingID = nil
	a.SortOrder = nil
	a.UpdatedAt = now
	return a, a.Validate()
}

func (a Asset) WithSortOrder(order int, now time.Time) (Asset, error) {
	if a.Status != StatusReady || a.ListingID == nil {
		return Asset{}, errNotUsable
	}
	if order < 0 {
		return Asset{}, errInvalidOrder
	}
	if err := requireTime(a, now); err != nil {
		return Asset{}, err
	}
	a.SortOrder = &order
	a.UpdatedAt = now
	return a, a.Validate()
}

func requireTime(a Asset, now time.Time) error {
	if now.IsZero() || now.Before(a.CreatedAt) {
		return errInvalidAsset
	}
	return nil
}

func normalizeFilename(in *string) (*string, error) {
	if in == nil {
		return nil, nil
	}
	name := strings.TrimSpace(*in)
	if name == "" {
		return nil, nil
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return nil, errInvalidFilename
	}
	if utf8.RuneCountInString(name) > 255 {
		return nil, errInvalidFilename
	}
	return &name, nil
}

func cloneString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func cloneInt(n *int) *int {
	if n == nil {
		return nil
	}
	v := *n
	return &v
}

func cloneInt64(n *int64) *int64 {
	if n == nil {
		return nil
	}
	v := *n
	return &v
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func cloneID(id *ID) *ID {
	if id == nil {
		return nil
	}
	v := *id
	return &v
}

func cloneAsset(a Asset) Asset {
	a.ListingID = cloneID(a.ListingID)
	a.OriginalFilename = cloneString(a.OriginalFilename)
	a.ContentType = cloneString(a.ContentType)
	a.SizeBytes = cloneInt64(a.SizeBytes)
	a.Width = cloneInt(a.Width)
	a.Height = cloneInt(a.Height)
	a.SortOrder = cloneInt(a.SortOrder)
	a.ReadyAt = cloneTime(a.ReadyAt)
	a.RejectedAt = cloneTime(a.RejectedAt)
	a.DeletedAt = cloneTime(a.DeletedAt)
	return a
}
