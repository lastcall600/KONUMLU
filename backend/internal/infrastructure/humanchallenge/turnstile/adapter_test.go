package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"backend/internal/identity"
)

// Official Cloudflare dummy testing credentials (test/dev proof only).
// Never use these in production configuration.
const (
	testAlwaysPassSecret = "1x0000000000000000000000000000000AA"
	testAlwaysFailSecret = "2x0000000000000000000000000000000AA"
	testDuplicateSecret  = "3x0000000000000000000000000000000AA"
	testDummyToken       = "XXXX.DUMMY.TOKEN.XXXX"
	testSecretMarker     = "turnstile-secret-must-not-leak-aa11"
	testTokenMarker      = "turnstile-token-must-not-leak-bb22"
)

func testAdapter(t *testing.T, url string) *Adapter {
	t.Helper()
	ad, err := New(Config{
		Secret:           testSecretMarker,
		AllowedHostnames: []string{"app.example.test"},
		Timeout:          time.Second,
		SiteverifyURL:    url,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ad
}

func decodeForm(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range r.PostForm {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func TestNewRejectsEmptySecretAndWildcardHosts(t *testing.T) {
	if _, err := New(Config{AllowedHostnames: []string{"app.example.test"}}); err == nil {
		t.Fatal("missing secret")
	}
	if _, err := New(Config{Secret: "s", AllowedHostnames: nil}); err == nil {
		t.Fatal("missing hosts")
	}
	if _, err := New(Config{Secret: "s", AllowedHostnames: []string{"*.example.test"}}); err == nil {
		t.Fatal("wildcard")
	}
}

func TestValidSuccessAllows(t *testing.T) {
	var gotForm map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotForm = decodeForm(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "hostname": "app.example.test", "action": "password_login",
		})
	}))
	defer srv.Close()
	ad := testAdapter(t, srv.URL)
	got, err := ad.Verify(context.Background(), identity.HumanChallengeInput{
		Token:  testTokenMarker,
		Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || !got.OK {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, ok := gotForm[formRemoteIP]; ok {
		t.Fatal("remoteip must not be sent")
	}
	if _, ok := gotForm[formIdempotencyKey]; ok {
		t.Fatal("idempotency_key must not be sent")
	}
	if gotForm[formSecret] != testSecretMarker || gotForm[formResponse] != testTokenMarker {
		t.Fatalf("form = %#v", gotForm)
	}
}

func TestSuccessWrongActionRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "hostname": "app.example.test", "action": "signup_complete",
		})
	}))
	defer srv.Close()
	got, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || got.OK || got.FailureClass != ClassActionMismatch {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestSuccessWrongHostnameRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "hostname": "evil.example", "action": "password_login",
		})
	}))
	defer srv.Close()
	got, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || got.OK || got.FailureClass != ClassHostnameMismatch {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestSuccessFalseRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false, "error-codes": []string{"invalid-input-response"},
		})
	}))
	defer srv.Close()
	got, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || got.OK || got.FailureClass != ClassInvalidToken {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestTimeoutFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer srv.Close()
	ad, err := New(Config{
		Secret: testSecretMarker, AllowedHostnames: []string{"app.example.test"},
		Timeout: 20 * time.Millisecond, SiteverifyURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ad.Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err == nil || FailureClassOf(err) != ClassProviderUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestNon2xxFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()
	_, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err == nil || FailureClassOf(err) != ClassProviderUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestMalformedJSONFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{not-json`)
	}))
	defer srv.Close()
	_, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err == nil || FailureClassOf(err) != ClassMalformedProviderResponse {
		t.Fatalf("err=%v", err)
	}
}

func TestEmptyAndOversizedTokenNeverCallProvider(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	ad := testAdapter(t, srv.URL)
	got, err := ad.Verify(context.Background(), identity.HumanChallengeInput{
		Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || got.OK || called {
		t.Fatalf("empty: %+v err=%v called=%v", got, err, called)
	}
	huge := strings.Repeat("a", MaxTokenLength+1)
	got, err = ad.Verify(context.Background(), identity.HumanChallengeInput{
		Token: huge, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || got.OK || called || utf8.RuneCountInString(huge) <= MaxTokenLength {
		t.Fatalf("oversized: %+v err=%v called=%v", got, err, called)
	}
}

func TestTimeoutOrDuplicateMapsToExpiredOrDuplicate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false, "error-codes": []string{"timeout-or-duplicate"},
		})
	}))
	defer srv.Close()
	got, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err != nil || got.OK || got.FailureClass != ClassExpiredOrDuplicate {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestLocalReplayStillEnforcedWithAdapter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "hostname": "app.example.test", "action": "password_login",
		})
	}))
	defer srv.Close()
	ad := testAdapter(t, srv.URL)
	ch := identity.HumanChallengePolicy{
		Provider:         identity.HumanChallengeProviderTurnstile,
		Required:         map[identity.AuthOperation]struct{}{identity.AuthOpPasswordLogin: {}},
		AllowedHostnames: []string{"app.example.test"},
		ReplayTTL:        time.Minute,
	}
	eng, err := identity.NewAbuseEngine(&memCounter{}, mustPolicy(t), ch, ad)
	if err != nil {
		t.Fatal(err)
	}
	sub := identity.AbuseSubject{
		Operation: identity.AuthOpPasswordLogin, IP: "192.0.2.9",
		ChallengeToken: testTokenMarker, Hostname: "app.example.test",
	}
	first := eng.Evaluate(context.Background(), sub, identity.PhaseChallenge)
	if !first.Allow() {
		t.Fatalf("first: %+v", first)
	}
	second := eng.Evaluate(context.Background(), sub, identity.PhaseChallenge)
	if second.Allow() || second.Reason != identity.ReasonChallengeFailed {
		t.Fatalf("replay: %+v", second)
	}
}

func TestSecretAndTokenNeverAppearInErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, testSecretMarker+testTokenMarker)
	}))
	defer srv.Close()
	_, err := testAdapter(t, srv.URL).Verify(context.Background(), identity.HumanChallengeInput{
		Token: testTokenMarker, Action: identity.HumanChallengeAction(identity.AuthOpPasswordLogin),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	dump := fmt.Sprintf("%v %v %+v", err, errors.Unwrap(err), err)
	if strings.Contains(dump, testSecretMarker) || strings.Contains(dump, testTokenMarker) {
		t.Fatalf("leaked: %s", dump)
	}
}

func TestOfficialSiteverifyURLIsDefault(t *testing.T) {
	ad, err := New(Config{Secret: "s", AllowedHostnames: []string{"app.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if ad.verifyURL != OfficialSiteverifyURL {
		t.Fatalf("url = %s", ad.verifyURL)
	}
}

func TestNameIsTurnstile(t *testing.T) {
	ad, err := New(Config{Secret: "s", AllowedHostnames: []string{"app.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if ad.Name() != identity.HumanChallengeProviderTurnstile {
		t.Fatalf("name = %s", ad.Name())
	}
}

type memCounter struct {
	n map[string]int64
}

func (m *memCounter) Increment(_ context.Context, key string, _ time.Duration) (int64, error) {
	if m.n == nil {
		m.n = map[string]int64{}
	}
	m.n[key]++
	return m.n[key], nil
}

func mustPolicy(t *testing.T) identity.AuthAbusePolicy {
	t.Helper()
	p, err := identity.BuildAuthAbusePolicy(
		identity.RateLimitBucket{Max: 20, Window: time.Minute},
		identity.RateLimitBucket{Max: 10, Window: time.Minute},
		identity.RateLimitBucket{Max: 10, Window: time.Minute},
		identity.RateLimitBucket{Max: 20, Window: time.Minute},
		identity.RateLimitBucket{Max: 10, Window: time.Minute},
	)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
