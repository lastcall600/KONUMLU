package notifications

import (
	"context"
	"strings"
	"time"

	"backend/internal/notifications/policy"
)

type PushEndpointRecord struct {
	ID         ID
	UserID     ID
	Channel    policy.Channel
	Platform   PushPlatform
	Provider   PushProvider
	Hash       []byte
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

func (PushEndpointRecord) String() string   { return "notifications.PushEndpointRecord" }
func (PushEndpointRecord) GoString() string { return "notifications.PushEndpointRecord{}" }

func (r PushEndpointRecord) Active() bool {
	return r.RevokedAt == nil
}

type PushEndpointView struct {
	ID         ID
	Channel    policy.Channel
	Platform   PushPlatform
	Provider   PushProvider
	CreatedAt  time.Time
	LastSeenAt time.Time
	Revoked    bool
}

type PushProviderMaterial struct {
	Web    *WebPushMaterial
	Mobile *MobilePushMaterial
}

func (PushProviderMaterial) String() string   { return "notifications.PushProviderMaterial" }
func (PushProviderMaterial) GoString() string { return "notifications.PushProviderMaterial{}" }

// PushPublicPayload is lock-screen-safe. It must not include message bodies,
// dispute evidence, contact info, addresses, payment data, TCKN, OTP, or tokens.
type PushPublicPayload struct {
	Category    string
	ReferenceID string
	TemplateKey string
}

func (PushPublicPayload) String() string   { return "notifications.PushPublicPayload" }
func (PushPublicPayload) GoString() string { return "notifications.PushPublicPayload{}" }

type PushSendRequest struct {
	EndpointID     ID
	Channel        policy.Channel
	Platform       PushPlatform
	Provider       PushProvider
	Material       PushProviderMaterial
	Payload        PushPublicPayload
	IdempotencyKey string
}

func (PushSendRequest) String() string   { return "notifications.PushSendRequest" }
func (PushSendRequest) GoString() string { return "notifications.PushSendRequest{}" }

type PushSender interface {
	Send(ctx context.Context, req PushSendRequest) (SendResult, error)
}

type PushDispatch struct {
	Web       PushSender
	FCM       PushSender
	APNs      PushSender
	Endpoints *EndpointService
}

func (p *PushDispatch) sender(provider PushProvider) PushSender {
	if p == nil {
		return nil
	}
	switch provider {
	case PushProviderWebPush:
		return p.Web
	case PushProviderFCM:
		return p.FCM
	case PushProviderAPNs:
		return p.APNs
	default:
		return nil
	}
}

func (p *PushDispatch) webConfigured() bool {
	return p != nil && p.Web != nil
}

func (p *PushDispatch) mobileConfigured() bool {
	return p != nil && (p.FCM != nil || p.APNs != nil)
}

func SafePushPayload(templateKey, domainRef, category string) PushPublicPayload {
	return PushPublicPayload{
		Category:    clampRunes(strings.TrimSpace(category), 64),
		ReferenceID: clampRunes(strings.TrimSpace(domainRef), 128),
		TemplateKey: clampRunes(strings.TrimSpace(templateKey), 128),
	}
}

func clampRunes(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
