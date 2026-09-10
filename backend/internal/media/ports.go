package media

import "context"

// ScanVerdict is the outcome of a malware scan or image-moderation check.
type ScanVerdict string

const (
	ScanClean       ScanVerdict = "clean"
	ScanRejected    ScanVerdict = "rejected"
	ScanUnavailable ScanVerdict = "unavailable"
)

// MalwareScanner inspects untrusted object bytes. Vendor adapters live in infrastructure.
type MalwareScanner interface {
	Scan(ctx context.Context, objectKey string, data []byte) (ScanVerdict, error)
}

// ImageModerator inspects decoded/normalized image bytes. Vendor adapters live in infrastructure.
type ImageModerator interface {
	Moderate(ctx context.Context, objectKey string, data []byte, contentType string) (ScanVerdict, error)
}

// ProcessingPolicy is explicit scanner/moderation configuration. Missing required
// adapters fail retryably; there is no production no-op that silently approves.
type ProcessingPolicy struct {
	RequireMalwareScan bool
	RequireModeration  bool
	MaxBytes           int64
	MaxWidth           int
	MaxHeight          int
}
