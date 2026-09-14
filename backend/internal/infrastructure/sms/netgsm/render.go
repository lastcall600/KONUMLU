package netgsm

import (
	"strings"
	"unicode/utf8"

	"backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/notifications/policy"
)

func renderOTP(req notifications.SMSSendRequest) (string, error) {
	if strings.TrimSpace(req.VerificationSecret) == "" {
		return "", notifications.ErrInvalidDelivery
	}
	if !contracts.ValidTemplateCode(req.TemplateCode) || !contracts.ValidLocale(req.Locale) {
		return "", notifications.ErrInvalidDelivery
	}
	// Official OTP endpoint forbids Turkish characters. Copy stays ASCII.
	secret := strings.TrimSpace(req.VerificationSecret)
	var text string
	switch req.TemplateCode {
	case contracts.TemplateIdentityPasswordReset:
		text = "KONUMLU reset code: " + secret
	default:
		text = "KONUMLU code: " + secret
	}
	if err := validateOTPBody(text); err != nil {
		return "", err
	}
	return text, nil
}

func renderChannel(req notifications.ChannelSendRequest) (string, error) {
	if req.Channel != policy.ChannelSMS {
		return "", notifications.ErrProviderPermanent
	}
	if strings.TrimSpace(req.TemplateKey) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return "", notifications.ErrInvalidDelivery
	}
	body := notifications.RenderPlain(req.TemplateKey, req.Locale, req.Variables)
	if err := validateTransactionalBody(body); err != nil {
		return "", err
	}
	return body, nil
}

func validateOTPBody(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return notifications.ErrProviderPermanent
	}
	if utf8.RuneCountInString(text) > maxOTPRunes {
		return notifications.ErrProviderPermanent
	}
	if !otpCharsetOK(text) {
		return notifications.ErrProviderPermanent
	}
	return nil
}

func validateTransactionalBody(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return notifications.ErrProviderPermanent
	}
	if utf8.RuneCountInString(text) > maxTransactionalRunes {
		return notifications.ErrProviderPermanent
	}
	return nil
}

func otpCharsetOK(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return false
		}
		switch r {
		case 'ç', 'Ç', 'ğ', 'Ğ', 'ı', 'İ', 'ö', 'Ö', 'ş', 'Ş', 'ü', 'Ü':
			return false
		}
	}
	return true
}

func needsTREncoding(s string) bool {
	for _, r := range s {
		switch r {
		case 'ç', 'Ç', 'ğ', 'Ğ', 'ı', 'İ', 'ö', 'Ö', 'ş', 'Ş', 'ü', 'Ü':
			return true
		}
	}
	return false
}
