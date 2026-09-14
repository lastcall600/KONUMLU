package push

import (
	"encoding/json"
	"strings"
	"testing"

	domain "backend/internal/notifications"
)

func TestPublicJSONStaysMinimal(t *testing.T) {
	raw, err := PublicJSON(domain.PushPublicPayload{
		Category:    "security",
		TemplateKey: "security.login_new",
		ReferenceID: "ref-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["category"] != "security" || m["template_key"] != "security.login_new" || m["reference_id"] != "ref-1" {
		t.Fatalf("payload=%s", raw)
	}
	forbidden := []string{"otp", "tckn", "token", "body", "message", "email", "phone", "address", "payment"}
	s := strings.ToLower(string(raw))
	for _, k := range forbidden {
		if strings.Contains(s, `"`+k+`"`) {
			t.Fatalf("forbidden field %s in %s", k, raw)
		}
	}
}
