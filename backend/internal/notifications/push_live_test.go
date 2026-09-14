package notifications

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/notifications/policy"
	"backend/internal/platform/config"
	"backend/internal/platform/crypto"
	"backend/internal/platform/observability"
)

func testPushKeys(t *testing.T) (*crypto.AEAD, crypto.HMACKey) {
	t.Helper()
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
	return aead, hmacKey
}

func webReg(endpoint string) PushRegistration {
	return PushRegistration{
		Channel:  policy.ChannelWebPush,
		Platform: PushPlatformWeb,
		Provider: PushProviderWebPush,
		Web: &WebPushMaterial{
			Endpoint: endpoint,
			P256dh:   "BNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w1",
			Auth:     "tBHItJI5svbpez7KI4CCXg",
		},
	}
}

func androidReg(token string) PushRegistration {
	return PushRegistration{
		Channel:  policy.ChannelMobilePush,
		Platform: PushPlatformAndroid,
		Provider: PushProviderFCM,
		Mobile:   &MobilePushMaterial{Token: token},
	}
}

func TestValidatePushRegistrationRejectsUnsupported(t *testing.T) {
	if err := ValidatePushRegistration(PushRegistration{
		Channel: policy.ChannelMobilePush, Platform: PushPlatformWeb, Provider: PushProviderFCM,
		Mobile: &MobilePushMaterial{Token: "aaaaaaaaaaaaaaaa"},
	}); err == nil {
		t.Fatal("android/web combo")
	}
	if err := ValidatePushRegistration(webReg("http://insecure.example/x")); err == nil {
		t.Fatal("http endpoint")
	}
}

func TestLivePushEndpointRegistry(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	aead, hmacKey := testPushKeys(t)
	svc, err := NewEndpointService(store, aead, hmacKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	userA, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	userB, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupNotifyUser(context.Background(), pool, userA)
		cleanupNotifyUser(context.Background(), pool, userB)
	})

	secretEndpoint := "https://push.example.test/subscription/live-secret-abc"
	fcmToken := "fcm-live-token-aaaaaaaaaaaaaaaa"
	var logBuf bytes.Buffer
	logger := observability.ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &logBuf)
	ctx = observability.WithLogger(ctx, logger)

	first, err := svc.Register(ctx, userA, webReg(secretEndpoint))
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Register(ctx, userA, webReg(secretEndpoint))
	if err != nil || again.ID != first.ID {
		t.Fatalf("idempotent id=%s again=%s err=%v", first.ID, again.ID, err)
	}
	second, err := svc.Register(ctx, userA, androidReg(fcmToken))
	if err != nil || second.ID == first.ID {
		t.Fatalf("multi device err=%v", err)
	}
	listed, err := svc.List(ctx, userA)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list=%d err=%v", len(listed), err)
	}
	other, err := svc.List(ctx, userB)
	if err != nil || len(other) != 0 {
		t.Fatalf("isolation list=%d", len(other))
	}
	if _, err := svc.Register(ctx, userB, webReg(secretEndpoint)); !errors.Is(err, errEndpointConflict) {
		t.Fatalf("cross-user err=%v", err)
	}

	refreshed := webReg(secretEndpoint)
	refreshed.Web = &WebPushMaterial{
		Endpoint: secretEndpoint,
		P256dh:   "CNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w2",
		Auth:     "uBHItJI5svbpez7KI4CCXh",
	}
	afterRefresh, err := svc.Register(ctx, userA, refreshed)
	if err != nil || afterRefresh.ID != first.ID {
		t.Fatalf("credential refresh must keep id=%s got=%s err=%v", first.ID, afterRefresh.ID, err)
	}
	listedAfter, err := svc.List(ctx, userA)
	if err != nil {
		t.Fatal(err)
	}
	webActive := 0
	for _, row := range listedAfter {
		if row.Channel == policy.ChannelWebPush && !row.Revoked {
			webActive++
			if row.ID != first.ID {
				t.Fatalf("unexpected extra web endpoint %s", row.ID)
			}
		}
	}
	if webActive != 1 {
		t.Fatalf("web active rows=%d want 1", webActive)
	}
	if _, err := svc.Register(ctx, userB, refreshed); !errors.Is(err, errEndpointConflict) {
		t.Fatalf("cross-user after refresh err=%v", err)
	}

	stored, err := store.GetPushEndpoint(ctx, first.ID, userA)
	if err != nil {
		t.Fatal(err)
	}
	if stored.KeyID != "v1" {
		t.Fatalf("endpoint_key_id = %q, want v1", stored.KeyID)
	}
	if bytes.Contains(stored.Ciphertext, []byte(secretEndpoint)) || bytes.Contains(stored.Nonce, []byte("https")) {
		t.Fatal("plaintext endpoint in db")
	}
	plain, err := aead.Open(crypto.Envelope{KeyID: stored.KeyID, Nonce: stored.Nonce, Ciphertext: stored.Ciphertext}, pushAAD(stored.ID, stored.UserID))
	if err != nil || !bytes.Contains(plain, []byte(secretEndpoint)) {
		t.Fatalf("decrypt err=%v", err)
	}
	if !bytes.Contains(plain, []byte(refreshed.Web.P256dh)) || bytes.Contains(plain, []byte("BNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w1")) {
		t.Fatal("ciphertext must carry refreshed web credentials, not the previous bundle")
	}
	wrong, err := crypto.NewAEAD(mustKey(t, bytes.Repeat([]byte{0x99}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Open(crypto.Envelope{KeyID: stored.KeyID, Nonce: stored.Nonce, Ciphertext: stored.Ciphertext}, pushAAD(stored.ID, stored.UserID)); err == nil {
		t.Fatal("wrong key opened")
	}
	if _, err := aead.Open(crypto.Envelope{KeyID: stored.KeyID, Nonce: stored.Nonce, Ciphertext: append([]byte{1}, stored.Ciphertext...)}, pushAAD(stored.ID, stored.UserID)); err == nil {
		t.Fatal("malformed opened")
	}

	revoked, err := svc.Revoke(ctx, userA, first.ID)
	if err != nil || !revoked.Revoked {
		t.Fatalf("revoke %+v %v", revoked, err)
	}
	reactivated, err := svc.Register(ctx, userA, webReg(secretEndpoint))
	if err != nil || reactivated.ID != first.ID || reactivated.Revoked {
		t.Fatalf("reactivate %+v err=%v", reactivated, err)
	}

	logs := logBuf.String()
	if strings.Contains(logs, secretEndpoint) || strings.Contains(logs, fcmToken) || strings.Contains(logs, "p256dh") {
		t.Fatalf("log leaked push material: %s", logs)
	}

	mat, err := NewMaterializer(store, staticDest{email: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userA, EventType: policy.EventSecurityLoginNew, DomainRef: "push-elig-1",
		Variables: map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if err != nil || !res.Created {
		t.Fatalf("materialize %+v err=%v", res, err)
	}
	chs, err := store.ListChannelDeliveries(ctx, res.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[policy.Channel]policy.DeliveryState{}
	for _, ch := range chs {
		seen[ch.Channel] = ch.State
	}
	if seen[policy.ChannelWebPush] != policy.DeliveryPending {
		t.Fatalf("web_push with endpoint = %s", seen[policy.ChannelWebPush])
	}
	if seen[policy.ChannelInApp] != policy.DeliveryAccepted {
		t.Fatalf("in_app=%s", seen[policy.ChannelInApp])
	}

	d, err := NewDispatcher(store, mat, nil, nil, nil, nil, DispatcherConfig{BatchSize: 10, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.ProcessBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unconfigured push must not claim, n=%d", n)
	}
	chs, err = store.ListChannelDeliveries(ctx, res.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chs {
		if ch.Channel == policy.ChannelWebPush && ch.State == policy.DeliveryAccepted {
			t.Fatal("fake accepted")
		}
	}
}

type scriptedPush struct {
	calls int
	errs  []error
}

func (s *scriptedPush) Send(_ context.Context, _ PushSendRequest) (SendResult, error) {
	s.calls++
	if len(s.errs) == 0 {
		return SendResult{ProviderRef: "ok"}, nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{ProviderRef: "ok"}, nil
}

func TestLivePushFanoutAggregation(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	aead, hmacKey := testPushKeys(t)
	svc, err := NewEndpointService(store, aead, hmacKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	user, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupNotifyUser(context.Background(), pool, user) })

	if _, err := svc.Register(ctx, user, webReg("https://push.example.test/fanout/a")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, user, webReg("https://push.example.test/fanout/b")); err != nil {
		t.Fatal(err)
	}

	mat, err := NewMaterializer(store, staticDest{email: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventSecurityLoginNew, DomainRef: "push-fanout-1",
		Variables: map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if err != nil || !res.Created {
		t.Fatalf("materialize %+v err=%v", res, err)
	}

	sender := &scriptedPush{errs: []error{ErrProviderEndpointInvalid, nil}}
	d, err := NewDispatcher(store, mat, nil, nil, nil, &PushDispatch{Web: sender, Endpoints: svc}, DispatcherConfig{BatchSize: 10, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.ProcessBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("configured web push must claim")
	}
	if sender.calls != 2 {
		t.Fatalf("both endpoints must be attempted, calls=%d", sender.calls)
	}
	chs, err := store.ListChannelDeliveries(ctx, res.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chs {
		if ch.Channel == policy.ChannelWebPush && ch.State != policy.DeliveryAccepted {
			t.Fatalf("partial success must accept, state=%s", ch.State)
		}
	}
	listed, err := svc.List(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("invalid endpoint must be revoked, remaining=%d", len(listed))
	}

	sender2 := &scriptedPush{errs: []error{ErrProviderRetryable, ErrProviderRetryable}}
	d2, err := NewDispatcher(store, mat, nil, nil, nil, &PushDispatch{Web: sender2, Endpoints: svc}, DispatcherConfig{BatchSize: 10, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventSecurityLoginNew, DomainRef: "push-fanout-2",
		Variables: map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if err != nil || !res2.Created {
		t.Fatalf("materialize2 %+v err=%v", res2, err)
	}
	if _, err := d2.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if sender2.calls != 1 {
		t.Fatalf("one remaining endpoint, calls=%d", sender2.calls)
	}
	chs, err = store.ListChannelDeliveries(ctx, res2.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chs {
		if ch.Channel == policy.ChannelWebPush && ch.State != policy.DeliveryRetryableFailed {
			t.Fatalf("retryable must not be hidden, state=%s", ch.State)
		}
	}
	listed, err = svc.List(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("transient failure must not revoke, remaining=%d", len(listed))
	}
}

func mustKey(t *testing.T, key []byte) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewSingleKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}
