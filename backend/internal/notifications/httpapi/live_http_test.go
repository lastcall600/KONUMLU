package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"backend/internal/notifications"
	"backend/internal/notifications/policy"
	"backend/internal/platform/crypto"
	"backend/internal/platform/db"
)

func TestLiveHTTPActorIsolationAndConsentHistory(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, url, 2*time.Second)
	if err != nil {
		t.Skip("postgres not configured")
	}
	if err := pool.Ready(ctx); err != nil {
		pool.Close()
		t.Skip("local postgres/postgis not reachable")
	}
	t.Cleanup(pool.Close)

	store := notifications.NewPostgresStore(pool)
	svc, err := notifications.NewConsumerService(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	userA, err := notifications.NewID()
	if err != nil {
		t.Fatal(err)
	}
	userB, err := notifications.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.push_endpoints WHERE user_id = $1 OR user_id = $2`, userA, userB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.inbox_items WHERE user_id = $1 OR user_id = $2`, userA, userB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.channel_deliveries WHERE intent_id IN (SELECT id FROM notifications.intents WHERE recipient_user_id IN ($1,$2))`, userA, userB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.intents WHERE recipient_user_id IN ($1,$2)`, userA, userB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.consent_decisions WHERE user_id IN ($1,$2)`, userA, userB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.preference_settings WHERE user_id IN ($1,$2)`, userA, userB)
	})

	h, err := New(fakeSessions{users: map[string]notifications.ID{"tok-a": userA, "tok-b": userB}}, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodGet, "/v1/notification-preferences", "", nil, authed("a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("get prefs = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPatch, "/v1/notification-preferences", allowedOrigin, map[string]any{
		"overrides": []map[string]any{{"channel": "mobile_push", "scopeType": "category", "scopeKey": "messages", "enabled": false}},
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/notification-preferences", "", nil, authed("a"))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"scopeKey":"messages"`)) {
		t.Fatalf("get after patch = %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/notification-consents", allowedOrigin, map[string]any{
		"consentType": "commercial_electronic.email", "decision": "granted",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("grant = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/notification-consents", allowedOrigin, map[string]any{
		"consentType": "commercial_electronic.email", "decision": "withdrawn",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw = %d %s", rec.Code, rec.Body.String())
	}
	hist, err := store.ListConsentHistory(context.Background(), userA, policy.ConsentCommercialEmail)
	if err != nil || len(hist) != 2 {
		t.Fatalf("history=%d err=%v", len(hist), err)
	}
	rec = do(t, h, http.MethodGet, "/v1/notification-consents", "", nil, authed("b"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var listed consentListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	for _, c := range listed.Consents {
		if c.Decision != nil {
			t.Fatalf("user b saw consent %+v", c)
		}
	}

	mat, err := notifications.NewMaterializer(store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mat.Materialize(context.Background(), notifications.MaterializeInput{
		RecipientUserID: userA,
		EventType:       policy.EventSecurityLoginNew,
		DomainRef:       "live-http-1",
		Variables:       map[string]string{"created_at": time.Now().UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/notifications", "", nil, authed("a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox a = %d %s", rec.Code, rec.Body.String())
	}
	var box inboxListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &box); err != nil || len(box.Items) == 0 {
		t.Fatalf("inbox decode %v %s", err, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/notifications", "", nil, authed("b"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var boxB inboxListDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &boxB)
	if len(boxB.Items) != 0 {
		t.Fatalf("user b inbox=%v", boxB.Items)
	}
	rec = do(t, h, http.MethodPost, "/v1/notifications/"+box.Items[0].ID+"/read", allowedOrigin, map[string]any{}, authed("b"), withCSRF())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign read = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/notifications/"+box.Items[0].ID+"/read", allowedOrigin, map[string]any{}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("mark read = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLiveHTTPPushEndpoints(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, url, 2*time.Second)
	if err != nil {
		t.Skip("postgres not configured")
	}
	if err := pool.Ready(ctx); err != nil {
		pool.Close()
		t.Skip("local postgres/postgis not reachable")
	}
	t.Cleanup(pool.Close)

	kr, err := crypto.NewSingleKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := crypto.NewAEAD(kr)
	if err != nil {
		t.Fatal(err)
	}
	hmacKey, err := crypto.NewHMACKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := notifications.NewPostgresStore(pool)
	endpoints, err := notifications.NewEndpointService(store, aead, hmacKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := notifications.NewConsumerService(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc = svc.WithEndpoints(endpoints)
	userA, err := notifications.NewID()
	if err != nil {
		t.Fatal(err)
	}
	userB, err := notifications.NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.push_endpoints WHERE user_id = $1 OR user_id = $2`, userA, userB)
	})
	h, err := New(fakeSessions{users: map[string]notifications.ID{"tok-a": userA, "tok-b": userB}}, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"channel": "web_push", "platform": "web", "provider": "webpush",
		"endpoint": "https://push.example.test/live-http-secret",
		"p256dh":   "BNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w1",
		"auth":     "tBHItJI5svbpez7KI4CCXg",
	}
	rec := do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, body, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("live-http-secret")) || bytes.Contains(rec.Body.Bytes(), []byte("p256dh")) {
		t.Fatalf("raw material: %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, body, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, body, authed("b"), withCSRF())
	if rec.Code != http.StatusConflict {
		t.Fatalf("cross-user = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/push-endpoints", "", nil, authed("b"))
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("web_push")) {
		t.Fatalf("b list = %s", rec.Body.String())
	}
}
