// Package push holds shared helpers for Web Push, FCM, and APNs adapters.
package push

import (
	"encoding/json"

	domain "backend/internal/notifications"
)

// PublicJSON encodes the lock-screen-safe payload. It must not add message
// bodies, OTP, tokens, TCKN, payment data, addresses, or contact fields.
func PublicJSON(p domain.PushPublicPayload) ([]byte, error) {
	return json.Marshal(struct {
		Category    string `json:"category,omitempty"`
		TemplateKey string `json:"template_key,omitempty"`
		ReferenceID string `json:"reference_id,omitempty"`
	}{
		Category:    p.Category,
		TemplateKey: p.TemplateKey,
		ReferenceID: p.ReferenceID,
	})
}

// PublicMap is the FCM data map (string values only).
func PublicMap(p domain.PushPublicPayload) map[string]string {
	out := make(map[string]string, 3)
	if p.Category != "" {
		out["category"] = p.Category
	}
	if p.TemplateKey != "" {
		out["template_key"] = p.TemplateKey
	}
	if p.ReferenceID != "" {
		out["reference_id"] = p.ReferenceID
	}
	return out
}
