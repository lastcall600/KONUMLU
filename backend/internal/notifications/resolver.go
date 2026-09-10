package notifications

import (
	"context"

	"backend/internal/notifications/contracts"
)

// MaterialKind is the channel-compatible kind of verification destination.
type MaterialKind string

const (
	MaterialKindEmail MaterialKind = "email"
	MaterialKindPhone MaterialKind = "phone"
)

func (k MaterialKind) Matches(channel contracts.Channel) bool {
	switch k {
	case MaterialKindEmail:
		return channel == contracts.ChannelEmail
	case MaterialKindPhone:
		return channel == contracts.ChannelSMS
	default:
		return false
	}
}

// VerificationMaterial is transient delivery plaintext. It must not be persisted or logged.
type VerificationMaterial struct {
	Kind        MaterialKind
	Destination string
	Secret      string
}

func (m VerificationMaterial) String() string {
	return "notifications.VerificationMaterial"
}

func (m VerificationMaterial) GoString() string {
	return "notifications.VerificationMaterial{}"
}

func (m VerificationMaterial) valid() bool {
	if !m.Kind.Matches(contracts.ChannelEmail) && !m.Kind.Matches(contracts.ChannelSMS) {
		return false
	}
	return m.Destination != "" && m.Secret != ""
}

// VerificationMaterialResolver loads transient verification secrets by challenge id.
// Notifications must not import Identity; the composition root supplies the adapter.
type VerificationMaterialResolver interface {
	Resolve(ctx context.Context, challengeID ID) (VerificationMaterial, error)
}
