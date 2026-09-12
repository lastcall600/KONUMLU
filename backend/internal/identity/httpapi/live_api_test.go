package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
	"backend/internal/platform/db"
)

func TestLiveAPISessionSecurityCenter(t *testing.T) {
	base := strings.TrimRight(os.Getenv("AUTH_B_LIVE_URL"), "/")
	if base == "" {
		t.Skip("AUTH_B_LIVE_URL not set")
	}
	origin := strings.TrimSpace(os.Getenv("AUTH_B_LIVE_ORIGIN"))
	if origin == "" {
		origin = base
	}
	ctx := context.Background()
	pool := liveAPIPool(t)
	store := identity.NewPostgresStore(pool)
	passwords, err := identity.NewPasswords(store, identity.PasswordPolicy{
		MemoryKiB: 19456, Iterations: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	userA, emailA := seedLiveLoginUser(t, ctx, pool, store, passwords, "authb-a")
	userB, _ := seedLiveLoginUser(t, ctx, pool, store, passwords, "authb-b")
	t.Cleanup(func() {
		cleanupLiveLoginUser(context.Background(), pool, userA)
		cleanupLiveLoginUser(context.Background(), pool, userB)
	})

	client := &http.Client{Timeout: 5 * time.Second}

	rec := liveJSON(t, client, http.MethodGet, base+"/healthz", "", nil, nil)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", rec.StatusCode)
	}
	rec = liveJSON(t, client, http.MethodGet, base+"/readyz", "", nil, nil)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d body=%s", rec.StatusCode, rec.Body)
	}

	rec = liveJSON(t, client, http.MethodGet, base+"/v1/auth/sessions", "", nil, nil)
	if rec.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth sessions = %d", rec.StatusCode)
	}
	rec = liveJSON(t, client, http.MethodPost, base+"/v1/auth/step-up/passkey/begin", origin, map[string]any{}, nil)
	if rec.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth step-up = %d", rec.StatusCode)
	}
	rec = liveJSON(t, client, http.MethodGet, base+"/v1/staff/moderation/reports", "", nil, map[string]string{
		sessionCookieName: "consumer-cookie",
	})
	if rec.StatusCode != http.StatusNotFound {
		t.Fatalf("staff with consumer cookie = %d", rec.StatusCode)
	}

	login1 := liveJSON(t, client, http.MethodPost, base+"/v1/auth/password/login", origin, map[string]any{
		"kind": "email", "identifier": emailA, "password": liveAPIPassword,
	}, nil)
	if login1.StatusCode != http.StatusOK {
		t.Fatalf("login1 = %d %s", login1.StatusCode, login1.Body)
	}
	session1 := login1.Cookies[sessionCookieName]
	csrf1 := login1.Cookies[csrfCookieName]
	if session1 == "" || csrf1 == "" {
		t.Fatal("login must set session and csrf cookies")
	}

	login2 := liveJSON(t, client, http.MethodPost, base+"/v1/auth/password/login", origin, map[string]any{
		"kind": "email", "identifier": emailA, "password": liveAPIPassword,
	}, nil)
	if login2.StatusCode != http.StatusOK {
		t.Fatalf("login2 = %d %s", login2.StatusCode, login2.Body)
	}
	session2 := login2.Cookies[sessionCookieName]
	csrf2 := login2.Cookies[csrfCookieName]
	if session2 == "" || session2 == session1 {
		t.Fatal("second login must issue a fresh session without sending the predecessor cookie")
	}

	stale := liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session1,
	})
	if stale.StatusCode != http.StatusOK {
		t.Fatalf("unrelated first session must remain valid after second login without predecessor cookie: %d %s", stale.StatusCode, stale.Body)
	}

	list := liveJSON(t, client, http.MethodGet, base+"/v1/auth/sessions", "", nil, map[string]string{
		sessionCookieName: session2,
	})
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list = %d %s", list.StatusCode, list.Body)
	}
	if strings.Contains(list.Body, session2) || strings.Contains(strings.ToLower(list.Body), "token") {
		t.Fatalf("list leaked secret material: %s", list.Body)
	}
	var listed sessionListResponse
	if err := json.Unmarshal([]byte(list.Body), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) < 2 {
		t.Fatalf("want at least two sessions, got %d (%s)", len(listed.Sessions), list.Body)
	}

	var otherID string
	for _, s := range listed.Sessions {
		if !s.Current {
			otherID = s.ID
			break
		}
	}
	if otherID == "" {
		t.Fatal("missing non-current session")
	}
	rev := liveJSON(t, client, http.MethodPost, base+"/v1/auth/sessions/"+otherID+"/revoke", origin, nil, map[string]string{
		sessionCookieName: session2,
		csrfCookieName:    csrf2,
	}, withCSRF(csrf2))
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("revoke other = %d %s", rev.StatusCode, rev.Body)
	}
	if liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session1,
	}).StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked other session must fail")
	}
	if liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session2,
	}).StatusCode != http.StatusOK {
		t.Fatal("current session must remain")
	}

	foreign := liveJSON(t, client, http.MethodGet, base+"/v1/auth/sessions", "", nil, map[string]string{
		sessionCookieName: session2,
	})
	if strings.Contains(foreign.Body, userB.String()) {
		t.Fatal("must not leak other user id")
	}

	begin := liveJSON(t, client, http.MethodPost, base+"/v1/auth/passkey/register/begin", origin, nil, map[string]string{
		sessionCookieName: session2,
		csrfCookieName:    csrf2,
	}, withCSRF(csrf2))
	if begin.StatusCode != http.StatusForbidden {
		t.Fatalf("passkey add without step-up = %d %s", begin.StatusCode, begin.Body)
	}

	wrongReauth := liveJSON(t, client, http.MethodPost, base+"/v1/auth/passkey/register/password-reauth", origin, map[string]any{
		"password": "not-the-password",
	}, map[string]string{
		sessionCookieName: session2,
		csrfCookieName:    csrf2,
	}, withCSRF(csrf2))
	if wrongReauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password reauth = %d %s", wrongReauth.StatusCode, wrongReauth.Body)
	}
	goodReauth := liveJSON(t, client, http.MethodPost, base+"/v1/auth/passkey/register/password-reauth", origin, map[string]any{
		"password": liveAPIPassword,
	}, map[string]string{
		sessionCookieName: session2,
		csrfCookieName:    csrf2,
	}, withCSRF(csrf2))
	if goodReauth.StatusCode != http.StatusOK {
		t.Fatalf("password reauth = %d %s", goodReauth.StatusCode, goodReauth.Body)
	}
	beginAfter := liveJSON(t, client, http.MethodPost, base+"/v1/auth/passkey/register/begin", origin, nil, map[string]string{
		sessionCookieName: session2,
		csrfCookieName:    csrf2,
	}, withCSRF(csrf2))
	if beginAfter.StatusCode == http.StatusForbidden {
		t.Fatalf("first passkey begin after password reauth still forbidden: %s", beginAfter.Body)
	}

	keys := liveJSON(t, client, http.MethodGet, base+"/v1/auth/passkeys", "", nil, map[string]string{
		sessionCookieName: session2,
	})
	if keys.StatusCode != http.StatusOK {
		t.Fatalf("list passkeys = %d %s", keys.StatusCode, keys.Body)
	}

	logout := liveJSON(t, client, http.MethodPost, base+"/v1/auth/logout", origin, nil, map[string]string{
		sessionCookieName: session2,
		csrfCookieName:    csrf2,
	}, withCSRF(csrf2))
	if logout.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d %s", logout.StatusCode, logout.Body)
	}
	if liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session2,
	}).StatusCode != http.StatusUnauthorized {
		t.Fatal("logout must invalidate current session")
	}
}

const liveAPIPassword = "correct-horse-battery-live"

type liveHTTPResult struct {
	StatusCode int
	Body       string
	Cookies    map[string]string
}

func liveJSON(t *testing.T, client *http.Client, method, url, origin string, body any, cookies map[string]string, opts ...func(*http.Request)) liveHTTPResult {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for _, opt := range opts {
		opt(req)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	got := liveHTTPResult{StatusCode: res.StatusCode, Body: string(raw), Cookies: map[string]string{}}
	for _, c := range res.Cookies() {
		got.Cookies[c.Name] = c.Value
	}
	return got
}

func liveAPIPool(t *testing.T) *db.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, url, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ready(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedLiveLoginUser(t *testing.T, ctx context.Context, pool *db.Pool, store *identity.PostgresStore, passwords *identity.Passwords, prefix string) (identity.ID, string) {
	t.Helper()
	id, err := identity.NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity.users (id, created_at, updated_at, session_epoch)
		VALUES ($1, $2, $2, 0)`, id, now); err != nil {
		t.Fatal(err)
	}
	email := prefix + "-" + id.String()[:8] + "@example.test"
	identID, err := identity.NewID()
	if err != nil {
		t.Fatal(err)
	}
	verified := now
	if err := store.InsertIdentifier(ctx, identity.UserIdentifier{
		ID: identID, UserID: id, Kind: identity.IdentifierEmail,
		ValueCanonical: email, VerifiedAt: &verified, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := passwords.Set(ctx, id, []byte(liveAPIPassword)); err != nil {
		t.Fatal(err)
	}
	return id, email
}

func cleanupLiveLoginUser(ctx context.Context, pool *db.Pool, userID identity.ID) {
	_, _ = pool.Exec(ctx, `DELETE FROM identity.sessions WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM identity.password_credentials WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM identity.user_identifiers WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM identity.passkey_credentials WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM identity.users WHERE id = $1`, userID)
}
