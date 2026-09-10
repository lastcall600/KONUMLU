package storage

import (
	"context"
	"errors"
	"strings"

	"backend/internal/media"
)

var errStorageDisabled = errors.New("object storage is disabled")
var errWorkloadIdentityUnwired = errors.New("object storage workload identity is not wired until hosting is selected")

func mapStorageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, media.ErrInvalidObjectKey) || errors.Is(err, media.ErrStorageRequired) ||
		errors.Is(err, media.ErrUnavailable) {
		return err
	}
	return media.ErrUnavailable
}

func errorContainsSecret(err error, secret string) bool {
	if err == nil || secret == "" {
		return false
	}
	return strings.Contains(err.Error(), secret)
}
