package ses

import (
	"fmt"
	"html"
	"strings"

	"backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/notifications/policy"
)

type rendered struct {
	Subject string
	Text    string
	HTML    string
}

func renderVerification(req notifications.EmailSendRequest) (rendered, error) {
	if strings.TrimSpace(req.Destination) == "" || !validDestination(req.Destination) {
		return rendered{}, notifications.ErrProviderPermanent
	}
	if strings.TrimSpace(req.VerificationSecret) == "" {
		return rendered{}, notifications.ErrInvalidDelivery
	}
	if !contracts.ValidTemplateCode(req.TemplateCode) || !contracts.ValidLocale(req.Locale) {
		return rendered{}, notifications.ErrInvalidDelivery
	}
	secret := req.VerificationSecret
	locale := req.Locale
	var subject, text string
	switch req.TemplateCode {
	case contracts.TemplateIdentityPasswordReset:
		subject, text = passwordResetCopy(locale, secret)
	case contracts.TemplateIdentityVerificationSignup:
		subject, text = signupCopy(locale, secret)
	default:
		subject = "KONUMLU"
		text = secret
	}
	return rendered{
		Subject: subject,
		Text:    text,
		HTML:    "<p>" + html.EscapeString(text) + "</p>",
	}, nil
}

func renderChannel(req notifications.ChannelSendRequest) (rendered, error) {
	if req.Channel != policy.ChannelEmail {
		return rendered{}, notifications.ErrProviderPermanent
	}
	if strings.TrimSpace(req.Destination) == "" || !validDestination(req.Destination) {
		return rendered{}, notifications.ErrProviderPermanent
	}
	if strings.TrimSpace(req.TemplateKey) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return rendered{}, notifications.ErrInvalidDelivery
	}
	body := notifications.RenderPlain(req.TemplateKey, req.Locale, req.Variables)
	subject := strings.TrimSpace(req.TemplateKey)
	if subject == "" {
		subject = "KONUMLU"
	}
	return rendered{
		Subject: subject,
		Text:    body,
	}, nil
}

func signupCopy(locale, secret string) (string, string) {
	switch locale {
	case contracts.LocaleTR:
		return "KONUMLU doğrulama", fmt.Sprintf("Doğrulama kodunuz: %s", secret)
	case contracts.LocaleRU:
		return "KONUMLU подтверждение", fmt.Sprintf("Код подтверждения: %s", secret)
	case contracts.LocaleAR:
		return "KONUMLU تحقق", fmt.Sprintf("رمز التحقق: %s", secret)
	default:
		return "KONUMLU verification", fmt.Sprintf("Your verification code: %s", secret)
	}
}

func passwordResetCopy(locale, secret string) (string, string) {
	switch locale {
	case contracts.LocaleTR:
		return "KONUMLU parola sıfırlama", fmt.Sprintf("Parola sıfırlama kodunuz: %s", secret)
	case contracts.LocaleRU:
		return "KONUMLU сброс пароля", fmt.Sprintf("Код сброса пароля: %s", secret)
	case contracts.LocaleAR:
		return "KONUMLU إعادة تعيين كلمة المرور", fmt.Sprintf("رمز إعادة التعيين: %s", secret)
	default:
		return "KONUMLU password reset", fmt.Sprintf("Your password reset code: %s", secret)
	}
}
