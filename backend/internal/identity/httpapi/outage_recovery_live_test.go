package httpapi

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
)

func TestLiveValkeyAndPostgresOutageRecovery(t *testing.T) {
	if strings.TrimSpace(os.Getenv("AUTH_C_LIVE_OUTAGE")) != "1" {
		t.Skip("AUTH_C_LIVE_OUTAGE=1 required for real local dependency stop/start")
	}
	base := strings.TrimRight(os.Getenv("AUTH_C_LIVE_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	origin := strings.TrimSpace(os.Getenv("AUTH_C_LIVE_ORIGIN"))
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
	user, email := seedLiveLoginUser(t, ctx, pool, store, passwords, "authc-outage")
	t.Cleanup(func() { cleanupLiveLoginUser(context.Background(), pool, user) })

	client := &http.Client{Timeout: 8 * time.Second}
	if liveJSON(t, client, http.MethodGet, base+"/healthz", "", nil, nil).StatusCode != http.StatusOK {
		t.Fatal("precondition healthz")
	}
	if liveJSON(t, client, http.MethodGet, base+"/readyz", "", nil, nil).StatusCode != http.StatusOK {
		t.Fatal("precondition readyz")
	}

	login := liveJSON(t, client, http.MethodPost, base+"/v1/auth/password/login", origin, map[string]any{
		"kind": "email", "identifier": email, "password": liveAPIPassword,
	}, nil)
	if login.StatusCode != http.StatusOK {
		t.Fatalf("baseline login = %d %s", login.StatusCode, login.Body)
	}
	session := login.Cookies[sessionCookieName]
	csrf := login.Cookies[csrfCookieName]
	if session == "" {
		t.Fatal("baseline session cookie missing")
	}
	if liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session,
	}).StatusCode != http.StatusOK {
		t.Fatal("baseline session resolve")
	}

	dockerRun(t, "stop", "konumlu-valkey")
	t.Cleanup(func() { _ = exec.Command("docker", "start", "konumlu-valkey").Run() })

	sessionDown := liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session,
	})
	t.Logf("valkey-down session GET = %d %s", sessionDown.StatusCode, sessionDown.Body)
	if sessionDown.StatusCode != http.StatusOK {
		t.Fatalf("session must fail open to PostgreSQL, got %d", sessionDown.StatusCode)
	}

	abuseDown := liveJSON(t, client, http.MethodPost, base+"/v1/auth/password/login", origin, map[string]any{
		"kind": "email", "identifier": email, "password": liveAPIPassword,
	}, nil)
	t.Logf("valkey-down password login = %d %s", abuseDown.StatusCode, abuseDown.Body)
	if abuseDown.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("abuse-protected login must fail closed, got %d %s", abuseDown.StatusCode, abuseDown.Body)
	}

	stepDown := liveJSON(t, client, http.MethodPost, base+"/v1/auth/passkey/register/begin", origin, map[string]any{
		"name": "outage", "displayName": "Outage",
	}, map[string]string{
		sessionCookieName: session,
		csrfCookieName:    csrf,
	}, withCSRF(csrf))
	t.Logf("valkey-down step-up passkey begin = %d %s", stepDown.StatusCode, stepDown.Body)
	if stepDown.StatusCode == http.StatusOK {
		t.Fatal("step-up protected operation must not gain elevation while Valkey is down")
	}

	dockerRun(t, "start", "konumlu-valkey")
	waitHealthy(t, "konumlu-valkey")
	waitHTTP(t, client, base+"/readyz", http.StatusOK, 30*time.Second)

	sessionUp := liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session,
	})
	t.Logf("valkey-restart session GET = %d %s", sessionUp.StatusCode, sessionUp.Body)
	if sessionUp.StatusCode != http.StatusOK {
		t.Fatalf("session must continue after Valkey restart, got %d", sessionUp.StatusCode)
	}

	otherEmail := "authc-outage-abuse-" + user.String()[:8] + "@example.test"
	abuseUp := liveJSON(t, client, http.MethodPost, base+"/v1/auth/password/login", origin, map[string]any{
		"kind": "email", "identifier": otherEmail, "password": "wrong-password",
	}, nil)
	t.Logf("valkey-restart unknown login = %d %s", abuseUp.StatusCode, abuseUp.Body)
	if abuseUp.StatusCode != http.StatusUnauthorized && abuseUp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("abuse controls must work after Valkey restart, got %d %s", abuseUp.StatusCode, abuseUp.Body)
	}

	stepUp := liveJSON(t, client, http.MethodPost, base+"/v1/auth/passkey/register/begin", origin, map[string]any{
		"name": "outage", "displayName": "Outage",
	}, map[string]string{
		sessionCookieName: session,
		csrfCookieName:    csrf,
	}, withCSRF(csrf))
	t.Logf("valkey-restart step-up passkey begin = %d %s", stepUp.StatusCode, stepUp.Body)
	if stepUp.StatusCode == http.StatusOK {
		t.Fatal("Valkey restart must not grant step-up")
	}

	dockerRun(t, "stop", "konumlu-postgres")
	t.Cleanup(func() { _ = exec.Command("docker", "start", "konumlu-postgres").Run() })
	time.Sleep(2 * time.Second)

	healthDown := liveJSON(t, client, http.MethodGet, base+"/healthz", "", nil, nil)
	t.Logf("postgres-down healthz = %d %s", healthDown.StatusCode, healthDown.Body)
	if healthDown.StatusCode != http.StatusOK {
		t.Fatalf("liveness must stay up, got %d", healthDown.StatusCode)
	}
	readyDown := liveJSON(t, client, http.MethodGet, base+"/readyz", "", nil, nil)
	t.Logf("postgres-down readyz = %d %s", readyDown.StatusCode, readyDown.Body)
	if readyDown.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness must fail, got %d %s", readyDown.StatusCode, readyDown.Body)
	}
	authGet := liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session,
	})
	t.Logf("postgres-down GET session = %d %s", authGet.StatusCode, authGet.Body)
	if authGet.StatusCode == http.StatusOK {
		t.Fatal("authenticated durable request must not fake success")
	}
	authMut := liveJSON(t, client, http.MethodPost, base+"/v1/auth/logout", origin, nil, map[string]string{
		sessionCookieName: session,
		csrfCookieName:    csrf,
	}, withCSRF(csrf))
	t.Logf("postgres-down logout = %d %s", authMut.StatusCode, authMut.Body)
	if authMut.StatusCode == http.StatusOK {
		t.Fatal("auth mutation must not fake success")
	}

	dockerRun(t, "start", "konumlu-postgres")
	waitHealthy(t, "konumlu-postgres")
	waitHTTP(t, client, base+"/readyz", http.StatusOK, 45*time.Second)

	readyUp := liveJSON(t, client, http.MethodGet, base+"/readyz", "", nil, nil)
	t.Logf("postgres-restart readyz = %d %s", readyUp.StatusCode, readyUp.Body)
	sessionRecover := liveJSON(t, client, http.MethodGet, base+"/v1/auth/session", "", nil, map[string]string{
		sessionCookieName: session,
	})
	t.Logf("postgres-restart session GET = %d %s", sessionRecover.StatusCode, sessionRecover.Body)
	if sessionRecover.StatusCode != http.StatusOK {
		login2 := liveJSON(t, client, http.MethodPost, base+"/v1/auth/password/login", origin, map[string]any{
			"kind": "email", "identifier": email, "password": liveAPIPassword,
		}, nil)
		t.Logf("postgres-restart login = %d %s", login2.StatusCode, login2.Body)
		if login2.StatusCode != http.StatusOK {
			t.Fatalf("auth must recover after PostgreSQL restart: session=%d login=%d", sessionRecover.StatusCode, login2.StatusCode)
		}
	}
}

func dockerRun(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v: %v %s", args, err, out)
	}
}

func waitHealthy(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}", name)
		out, err := cmd.CombinedOutput()
		status := strings.TrimSpace(string(out))
		if err == nil && (status == "healthy" || status == "running") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s did not become healthy", name)
}

func waitHTTP(t *testing.T, client *http.Client, url string, want int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var last int
	for time.Now().Before(deadline) {
		rec := liveJSON(t, client, http.MethodGet, url, "", nil, nil)
		last = rec.StatusCode
		if rec.StatusCode == want {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s did not return %d (last %d)", url, want, last)
}
