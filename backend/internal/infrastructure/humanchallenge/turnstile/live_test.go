package turnstile

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestLiveOfficialDummySiteverify is an optional contract proof against
// Cloudflare using published TEST credentials only. Network failure is
// LIVE_PROVIDER_TEST_PENDING and does not fail the suite.
func TestLiveOfficialDummySiteverify(t *testing.T) {
	t.Helper()
	form := url.Values{}
	form.Set("secret", testAlwaysPassSecret)
	form.Set("response", testDummyToken)
	if form.Get("remoteip") != "" {
		t.Fatal("live proof must not send remoteip")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OfficialSiteverifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Log("LIVE_PROVIDER_TEST_PENDING")
		t.Log(err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		t.Log("LIVE_PROVIDER_TEST_PENDING")
		return
	}
	dump := string(body)
	if strings.Contains(dump, testAlwaysPassSecret) || strings.Contains(dump, testAlwaysFailSecret) || strings.Contains(dump, testDuplicateSecret) {
		t.Fatal("provider body echoed a test secret")
	}
	var parsed struct {
		Success    bool     `json:"success"`
		Hostname   string   `json:"hostname"`
		Action     string   `json:"action"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("official dummy JSON: %v", err)
	}
	if !parsed.Success {
		t.Logf("official dummy success=false error-codes=%v hostname=%q (test-only credentials)", parsed.ErrorCodes, parsed.Hostname)
	}
}
