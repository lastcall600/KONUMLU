package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/notifications/policy"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

func liveNotifyPool(t *testing.T) *db.Pool {
	t.Helper()
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
	return pool
}

func cleanupNotifyUser(ctx context.Context, pool *db.Pool, userID ID) {
	_, _ = pool.Exec(ctx, `DELETE FROM notifications.inbox_items WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM notifications.channel_deliveries WHERE intent_id IN (SELECT id FROM notifications.intents WHERE recipient_user_id = $1)`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM notifications.intents WHERE recipient_user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM notifications.consent_decisions WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM notifications.preference_settings WHERE user_id = $1`, userID)
}

func TestLivePreferenceConsentIntentInbox(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
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

	now := time.Now().UTC()
	if err := store.UpsertPreferenceSetting(ctx, userA, policy.PreferenceSetting{
		Channel: policy.ChannelMobilePush, ScopeType: policy.ScopeCategory, ScopeKey: "messages", Enabled: false,
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertPreferenceSetting(ctx, userA, policy.PreferenceSetting{
		Channel: policy.ChannelInApp, ScopeType: policy.ScopeCategory, ScopeKey: "messages", Enabled: true,
	}, now); err != nil {
		t.Fatal(err)
	}
	prefs, err := store.ListPreferenceSettings(ctx, userA)
	if err != nil || len(prefs) != 2 {
		t.Fatalf("prefs=%v err=%v", prefs, err)
	}
	other, err := store.ListPreferenceSettings(ctx, userB)
	if err != nil || len(other) != 0 {
		t.Fatalf("user b prefs=%v err=%v", other, err)
	}

	first, err := store.InsertConsentDecision(ctx, userA, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: now, Source: policy.ConsentSourceSettingsWeb,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.InsertConsentDecision(ctx, userA, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentWithdrawn,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: now.Add(time.Second), Source: policy.ConsentSourceSettingsWeb,
	})
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.InsertConsentDecision(ctx, userA, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: now.Add(2 * time.Second), Source: policy.ConsentSourceSettingsWeb,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(first.RecordedSeq < second.RecordedSeq && second.RecordedSeq < third.RecordedSeq) {
		t.Fatalf("seq %d %d %d", first.RecordedSeq, second.RecordedSeq, third.RecordedSeq)
	}
	cur, err := store.CurrentConsent(ctx, userA, policy.ConsentCommercialEmail)
	if err != nil || cur.Status != policy.ConsentGranted || cur.RecordedSeq != third.RecordedSeq {
		t.Fatalf("current=%+v err=%v", cur, err)
	}
	hist, err := store.ListConsentHistory(ctx, userA, policy.ConsentCommercialEmail)
	if err != nil || len(hist) != 3 {
		t.Fatalf("hist=%d err=%v", len(hist), err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.InsertConsentDecision(ctx, userA, policy.ConsentSnapshot{
				Type: policy.ConsentCommercialSMS, Status: policy.ConsentGranted,
				PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
			})
		}()
	}
	wg.Wait()
	smsHist, err := store.ListConsentHistory(ctx, userA, policy.ConsentCommercialSMS)
	if err != nil || len(smsHist) != 8 {
		t.Fatalf("concurrent inserts=%d err=%v", len(smsHist), err)
	}

	mat, err := NewMaterializer(store, staticDest{email: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userA,
		EventType:       policy.EventSecurityLoginNew,
		DomainRef:       "evt-1",
		Variables:       map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if err != nil || !res.Created {
		t.Fatalf("materialize=%+v err=%v", res, err)
	}
	again, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userA,
		EventType:       policy.EventSecurityLoginNew,
		DomainRef:       "evt-1",
		Variables:       map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if err != nil || again.Created || again.Intent.ID != res.Intent.ID {
		t.Fatalf("dedupe=%+v err=%v", again, err)
	}
	otherUser, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userB,
		EventType:       policy.EventSecurityLoginNew,
		DomainRef:       "evt-1",
		Variables:       map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if err != nil || !otherUser.Created || otherUser.Intent.ID == res.Intent.ID {
		t.Fatalf("same dedupe different user=%+v", otherUser)
	}
	chs, err := store.ListChannelDeliveries(ctx, res.Intent.ID)
	if err != nil || len(chs) == 0 {
		t.Fatalf("channels=%v err=%v", chs, err)
	}
	seen := map[policy.Channel]policy.DeliveryState{}
	for _, ch := range chs {
		seen[ch.Channel] = ch.State
	}
	if seen[policy.ChannelInApp] != policy.DeliveryAccepted {
		t.Fatalf("in_app state=%s", seen[policy.ChannelInApp])
	}
	if seen[policy.ChannelEmail] != policy.DeliveryPending {
		t.Fatalf("email must stay pending without provider, got %s", seen[policy.ChannelEmail])
	}
	if seen[policy.ChannelWebPush] != policy.DeliverySuppressed {
		t.Fatalf("web_push=%s", seen[policy.ChannelWebPush])
	}

	_, err = mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userA, EventType: "not.a.catalog.event", DomainRef: "x",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userA, EventType: policy.EventOfferReceived, DomainRef: "offer-1",
		Variables: map[string]string{"otp": "123456", "offer_id": "o", "need_id": "n"},
	})
	if !errors.Is(err, policy.ErrArbitraryMetadata) {
		t.Fatalf("secret vars err=%v", err)
	}

	page, err := store.ListInbox(ctx, userA, nil, 20)
	if err != nil || len(page) == 0 {
		t.Fatalf("inbox a=%d err=%v", len(page), err)
	}
	foreign, err := store.ListInbox(ctx, userB, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range foreign {
		if it.ID == page[0].ID {
			t.Fatal("user b listed user a item")
		}
	}
	got, err := store.MarkInboxRead(ctx, userA, page[0].ID, time.Now().UTC())
	if err != nil || got.ReadAt == nil {
		t.Fatalf("mark one=%+v err=%v", got, err)
	}
	againRead, err := store.MarkInboxRead(ctx, userA, page[0].ID, time.Now().UTC())
	if err != nil || againRead.ReadAt == nil || !againRead.ReadAt.Equal(*got.ReadAt) {
		t.Fatalf("idempotent read=%+v", againRead)
	}
	if _, err := store.MarkInboxRead(ctx, userB, page[0].ID, time.Now().UTC()); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign mark-read=%v", err)
	}
	_, _ = mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userA, EventType: policy.EventSecuritySessionsRevoked, DomainRef: "rev-1",
		Variables: map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	_, _ = mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: userB, EventType: policy.EventSecuritySessionsRevoked, DomainRef: "rev-b",
		Variables: map[string]string{"created_at": now.Format(time.RFC3339)},
	})
	if _, err := store.MarkAllInboxRead(ctx, userA, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	unreadA, err := store.CountUnreadInbox(ctx, userA)
	if err != nil || unreadA != 0 {
		t.Fatalf("unread a=%d err=%v", unreadA, err)
	}
	unreadB, err := store.CountUnreadInbox(ctx, userB)
	if err != nil || unreadB == 0 {
		t.Fatalf("user b unread should remain, got %d err=%v", unreadB, err)
	}
}

func TestLiveAuthSecurityHandlerMaterializesOnce(t *testing.T) {
	pool := liveNotifyPool(t)
	store := NewPostgresStore(pool)
	user, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupNotifyUser(context.Background(), pool, user) })
	mat, err := NewMaterializer(store, staticDest{email: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthSecurityNotifyHandler(mat)
	payload, _ := json.Marshal(map[string]string{
		"event": "auth.passkey.added", "user_id": user.String(), "result": "success",
	})
	event := outbox.Event{
		ID:           outboxIDFrom(t, user),
		EventType:    authSecurityOutboxType,
		EventVersion: authSecurityOutboxVersion,
		Payload:      payload,
		CreatedAt:    time.Now().UTC(),
		AvailableAt:  time.Now().UTC(),
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	intents, err := store.ListInbox(context.Background(), user, nil, 10)
	if err != nil || len(intents) != 1 {
		t.Fatalf("inbox=%d err=%v", len(intents), err)
	}
	failPayload, _ := json.Marshal(map[string]string{"event": "auth.login.failed", "user_id": user.String(), "result": "failed"})
	event.ID = outboxIDFrom(t, user)
	event.Payload = failPayload
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	intents, err = store.ListInbox(context.Background(), user, nil, 10)
	if err != nil || len(intents) != 1 {
		t.Fatalf("audit-only must not notify, inbox=%d", len(intents))
	}
}

func TestLiveHTTPPreferencesConsentInbox(t *testing.T) {
	pool := liveNotifyPool(t)
	store := NewPostgresStore(pool)
	svc, err := NewConsumerService(store, nil)
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
	got, err := svc.GetPreferences(context.Background(), userA)
	if err != nil || len(got) == 0 {
		t.Fatalf("get prefs=%v err=%v", got, err)
	}
	patched, err := svc.PatchPreferences(context.Background(), userA, policy.PreferencePatch{Overrides: []policy.PreferenceOverride{{
		Channel: policy.ChannelEmail, ScopeType: policy.ScopeChannel, ScopeKey: "*", Enabled: false,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range patched {
		if row.Channel == policy.ChannelEmail && row.ScopeType == policy.ScopeChannel {
			if row.Stored == nil || *row.Stored != false {
				t.Fatalf("stored override missing: %+v", row)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("email channel row missing")
	}
	cons, err := svc.GetConsents(context.Background(), userA)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cons {
		if c.Decision != nil {
			t.Fatalf("expected missing consent, got %+v", c)
		}
	}
	_, err = svc.RecordConsent(context.Background(), userA, policy.ConsentCommercialEmail, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.RecordConsent(context.Background(), userA, policy.ConsentCommercialEmail, false)
	if err != nil {
		t.Fatal(err)
	}
	cons, err = svc.GetConsents(context.Background(), userA)
	if err != nil {
		t.Fatal(err)
	}
	var email *policy.ConsentStatus
	for _, c := range cons {
		if c.Type == policy.ConsentCommercialEmail {
			email = c.Decision
		}
	}
	if email == nil || *email != policy.ConsentWithdrawn {
		t.Fatalf("current decision=%v", email)
	}
}

type staticDest struct {
	email, phone, fail bool
}

func (s staticDest) ReadNotificationEligibility(_ context.Context, _ identitycontracts.ID) (identitycontracts.NotificationEligibility, error) {
	if s.fail {
		return identitycontracts.NotificationEligibility{}, identitycontracts.ErrUnavailable
	}
	return identitycontracts.NotificationEligibility{EmailVerified: s.email, PhoneVerified: s.phone}, nil
}

func outboxIDFrom(t *testing.T, seed ID) outbox.ID {
	t.Helper()
	var id outbox.ID
	copy(id[:], seed[:])
	if id == (outbox.ID{}) {
		raw, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		copy(id[:], raw[:])
	}
	id[15] ^= 1
	return id
}

func TestChannelTransitionRules(t *testing.T) {
	if policy.AllowedChannelTransition(policy.DeliveryAccepted, policy.DeliveryPending) {
		t.Fatal("accepted must not regress to pending")
	}
	if policy.AllowedChannelTransition(policy.DeliverySuppressed, policy.DeliveryAccepted) {
		t.Fatal("suppressed must not become accepted")
	}
	if !policy.AllowedChannelTransition(policy.DeliveryPending, policy.DeliveryAccepted) {
		t.Fatal("pending to accepted")
	}
}
