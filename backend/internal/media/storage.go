package media

import (
	"context"
	"time"
)

// PublicObjectDelivery issues a public-safe URL for a processed object.
// Implementations may use a short-lived signed GET or a configured public base URL.
// Original upload keys are never accepted.
type PublicObjectDelivery interface {
	IssueGetTarget(ctx context.Context, processedObjectKey string) (GetTarget, error)
}

// ObjectStorage is the infrastructure port for object bytes. No provider types belong here.
type ObjectStorage interface {
	PublicObjectDelivery
	IssueUploadTarget(ctx context.Context, objectKey string) (UploadTarget, error)
	Stat(ctx context.Context, objectKey string) (ObjectStat, error)
	GetObject(ctx context.Context, objectKey string) ([]byte, ObjectStat, error)
	PutObject(ctx context.Context, objectKey string, data []byte, contentType string) error
	DeleteObject(ctx context.Context, objectKey string) error
}

// UploadTarget is a server-owned upload slot. UploadURL is a short-lived signed PUT
// target from infrastructure; Media does not treat it as a public CDN URL.
type UploadTarget struct {
	ObjectKey       string
	UploadURL       string
	ExpiresAt       time.Time
	RequiredHeaders map[string]string
	MaxBytes        int64
}

// GetTarget is a public-safe read URL for a processed object. It is not an object key.
type GetTarget struct {
	URL       string
	ExpiresAt time.Time
}

// ObjectStat is raw object metadata from storage. It is untrusted until processing.
type ObjectStat struct {
	Exists      bool
	SizeBytes   int64
	ContentType string
}
