package notifications

import (
	"context"

	"backend/internal/notifications/contracts"
)

// EmailSender delivers transactional email. Adapters must not choose templates.
type EmailSender interface {
	Send(ctx context.Context, req EmailSendRequest) (SendResult, error)
}

// SMSSender delivers transactional SMS. Adapters must not choose templates.
type SMSSender interface {
	Send(ctx context.Context, req SMSSendRequest) (SendResult, error)
}

// EmailSendRequest is transient provider input. It must never be logged.
type EmailSendRequest struct {
	Destination        string
	TemplateCode       string
	TemplateVersion    int
	Locale             string
	VerificationSecret string
	IdempotencyKey     string
}

func (r EmailSendRequest) String() string {
	return "notifications.EmailSendRequest"
}

func (r EmailSendRequest) GoString() string {
	return "notifications.EmailSendRequest{}"
}

func (r EmailSendRequest) valid() bool {
	return r.Destination != "" &&
		contracts.ValidTemplateCode(r.TemplateCode) &&
		r.TemplateVersion > 0 &&
		contracts.ValidLocale(r.Locale) &&
		r.VerificationSecret != "" &&
		r.IdempotencyKey != ""
}

// SMSSendRequest is transient provider input. It must never be logged.
type SMSSendRequest struct {
	Destination        string
	TemplateCode       string
	TemplateVersion    int
	Locale             string
	VerificationSecret string
	IdempotencyKey     string
}

func (r SMSSendRequest) String() string {
	return "notifications.SMSSendRequest"
}

func (r SMSSendRequest) GoString() string {
	return "notifications.SMSSendRequest{}"
}

func (r SMSSendRequest) valid() bool {
	return r.Destination != "" &&
		contracts.ValidTemplateCode(r.TemplateCode) &&
		r.TemplateVersion > 0 &&
		contracts.ValidLocale(r.Locale) &&
		r.VerificationSecret != "" &&
		r.IdempotencyKey != ""
}

// SendResult is provider-owned delivery metadata. It must not include the secret.
type SendResult struct {
	ProviderRef string
}
