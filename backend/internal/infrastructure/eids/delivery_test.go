package eidsadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/eids/trdecision"
)

func TestDeliveryClientPostsJSONAndAuth(t *testing.T) {
	k, err := trdecision.GeneratePrivateKey("tr-v1")
	if err != nil {
		t.Fatal(err)
	}
	iss, err := trdecision.NewIssuer(k, func() time.Time {
		return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := iss.Issue(trdecision.ProviderResult{
		SubjectRef:       "abababababababababababababababababababababababababababababababab",
		VerificationType: trdecision.TypeProperty,
		Status:           trdecision.StatusApproved,
		ValidUntil:       time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/internal/tr-compliance/v1/verification-decisions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["tckn"]; ok {
			t.Fatal("tckn in body")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c, err := NewDeliveryClient(srv.URL, "ingress-token", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	if err := c.PostDecision(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer ingress-token" {
		t.Fatalf("auth=%s", gotAuth)
	}
}
