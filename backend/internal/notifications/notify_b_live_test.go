package notifications

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/notifications/policy"
	"backend/internal/platform/outbox"
)

type fakeChannelSender struct {
	mu    sync.Mutex
	calls int
	err   error
	dests []string
}

func (f *fakeChannelSender) Send(_ context.Context, req ChannelSendRequest) (SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.dests = append(f.dests, req.Destination)
	if f.err != nil {
		return SendResult{}, f.err
	}
	return SendResult{ProviderRef: "test-ref"}, nil
}

func (f *fakeChannelSender) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func enableMarketingEmail(t *testing.T, store *PostgresStore, user ID) {
	t.Helper()
	if err := store.UpsertPreferenceSetting(context.Background(), user, policy.PreferenceSetting{
		Channel: policy.ChannelEmail, ScopeType: policy.ScopeCategory, ScopeKey: string(policy.CategoryMarketing), Enabled: true,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

type memContact struct {
	email string
}

func (m memContact) ResolveVerifiedContact(_ context.Context, _ identitycontracts.ID, kind string) (identitycontracts.NotificationContact, error) {
	if kind != identitycontracts.NotificationContactEmail || m.email == "" {
		return identitycontracts.NotificationContact{}, identitycontracts.ErrNotFound
	}
	return identitycontracts.NotificationContact{Kind: kind, Value: m.email}, nil
}

func TestLiveDispatcherAcceptRetryPermanentAndConsentRace(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	sender := &fakeChannelSender{}
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{
		BatchSize: 10, ProcessingHold: time.Second, PollInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-accept",
		Variables: map[string]string{"campaign_id": "camp-accept"},
	})
	if err != nil || !res.Created {
		t.Fatalf("materialize=%+v err=%v", res, err)
	}
	if n, err := d.ProcessBatch(ctx); err != nil || n != 1 {
		t.Fatalf("accept batch n=%d err=%v", n, err)
	}
	if sender.Calls() != 1 {
		t.Fatalf("sender calls=%d", sender.Calls())
	}

	sender.err = ErrProviderRetryable
	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-retry",
		Variables: map[string]string{"campaign_id": "camp-retry"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListChannelDeliveries(ctx, intentIDForDomain(t, store, user, "camp-retry"))
	if err != nil {
		t.Fatal(err)
	}
	foundRetry := false
	for _, r := range rows {
		if r.Channel == policy.ChannelEmail && r.State == policy.DeliveryRetryableFailed {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatalf("expected retryable email row: %+v", rows)
	}

	sender.err = ErrProviderPermanent
	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-perm",
		Variables: map[string]string{"campaign_id": "camp-perm"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-withdraw-pending",
		Variables: map[string]string{"campaign_id": "camp-withdraw-pending"},
	}); err != nil {
		t.Fatal(err)
	}
	beforeCalls := sender.Calls()
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentWithdrawn,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.Calls() != beforeCalls {
		t.Fatalf("withdrawn consent must not call provider: before=%d after=%d", beforeCalls, sender.Calls())
	}
	rowsW, err := store.ListChannelDeliveries(ctx, intentIDForDomain(t, store, user, "camp-withdraw-pending"))
	if err != nil {
		t.Fatal(err)
	}
	suppressed := false
	for _, r := range rowsW {
		if r.Channel == policy.ChannelEmail && r.State == policy.DeliverySuppressed {
			suppressed = true
		}
	}
	if !suppressed {
		t.Fatalf("expected dispatch-time consent suppress: %+v", rowsW)
	}
}

func TestLiveDispatcherUnconfiguredDoesNotClaim(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, nil, nil, nil, DispatcherConfig{BatchSize: 10, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventSecurityLoginNew, DomainRef: "login-unconf",
		Variables: map[string]string{"created_at": time.Now().UTC().Format(time.RFC3339)},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := d.ProcessBatch(ctx)
	if err != nil || n != 0 {
		t.Fatalf("unconfigured must not claim n=%d err=%v", n, err)
	}
}

func TestLiveConcurrentClaimNoDoubleSend(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	sender := &fakeChannelSender{}
	d1, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{BatchSize: 20, ProcessingHold: 2 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{BatchSize: 20, ProcessingHold: 2 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	for i := 0; i < n; i++ {
		ref := "camp-conc-" + itoa(i)
		if _, err := mat.Materialize(ctx, MaterializeInput{
			RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: ref,
			Variables: map[string]string{"campaign_id": ref},
		}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = d1.ProcessBatch(ctx) }()
	go func() { defer wg.Done(); _, _ = d2.ProcessBatch(ctx) }()
	wg.Wait()
	if sender.Calls() != n {
		t.Fatalf("double send: calls=%d want=%d", sender.Calls(), n)
	}
}

func TestLivePollLoopSoak(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	sender := &fakeChannelSender{}
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{
		BatchSize: 5, ProcessingHold: time.Second, PollInterval: 15 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	obStore := outbox.NewPostgresStore(pool)
	ob, err := outbox.New(obStore, outbox.Policy{BatchSize: 5, Lease: time.Second, BackoffBase: time.Millisecond, BackoffMultiplier: 2, BackoffCap: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := outbox.NewRegistry()
	var outboxHandled atomic.Int32
	_ = reg.Register("notify.b.soak", 1, outbox.HandlerFunc(func(context.Context, outbox.Event) error {
		outboxHandled.Add(1)
		return nil
	}))
	relay, err := outbox.NewRelay(ob, reg, 15*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := ob.Enqueue(ctx, nil, outbox.NewEvent{
			EventType: "notify.b.soak", EventVersion: 1, Payload: []byte(`{"n":1}`),
			IdempotencyKey: "soak-" + user.String() + "-" + itoa(i),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := mat.Materialize(ctx, MaterializeInput{
			RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "soak-" + itoa(i),
			Variables: map[string]string{"campaign_id": "soak-" + itoa(i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { _ = relay.Run(ctx); close(done) }()
	go func() { _ = d.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if outboxHandled.Load() >= 5 && sender.Calls() >= 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if outboxHandled.Load() < 5 {
		t.Fatalf("outbox handled=%d", outboxHandled.Load())
	}
	if sender.Calls() < 5 {
		t.Fatalf("dispatch calls=%d", sender.Calls())
	}
	processing := countUserChannelState(t, store, user, policy.DeliveryProcessing)
	if processing != 0 {
		t.Fatalf("stuck processing=%d", processing)
	}
}

func TestLiveProducerFanoutDedupes(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if err := MaterializeFanout(ctx, mat, policy.EventMessagingMessageReceived, "actor", "msg-1", "conv-1", []string{user.String(), user.String()}, map[string]string{
		"conversation_id": "conv-1", "message_id": "msg-1",
	}); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListInbox(ctx, user, nil, 10)
	if err != nil || len(page) != 1 {
		t.Fatalf("inbox=%d err=%v", len(page), err)
	}
}

func TestLiveFanoutSkipsActor(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if err := MaterializeFanout(ctx, mat, policy.EventMessagingMessageReceived, user.String(), "msg-self", "conv-self", []string{user.String()}, map[string]string{
		"conversation_id": "conv-self", "message_id": "msg-self",
	}); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListInbox(ctx, user, nil, 10)
	if err != nil || len(page) != 0 {
		t.Fatalf("self notify inbox=%d err=%v", len(page), err)
	}
}

func TestLiveProcessingRowRecovered(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-crash",
		Variables: map[string]string{"campaign_id": "camp-crash"},
	}); err != nil {
		t.Fatal(err)
	}
	intent := intentIDForDomain(t, store, user, "camp-crash")
	stale := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := pool.Exec(ctx, `
		UPDATE notifications.channel_deliveries
		SET state = 'processing', created_at = $2, updated_at = $2
		WHERE intent_id = $1 AND channel = 'email'`, intent, stale); err != nil {
		t.Fatal(err)
	}
	sender := &fakeChannelSender{}
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{
		BatchSize: 10, ProcessingHold: time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.ProcessBatch(ctx)
	if err != nil || n != 1 || sender.Calls() != 1 {
		t.Fatalf("recover n=%d calls=%d err=%v", n, sender.Calls(), err)
	}
}

func TestLivePreferenceOffBeforeDispatch(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-pref-off",
		Variables: map[string]string{"campaign_id": "camp-pref-off"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertPreferenceSetting(ctx, user, policy.PreferenceSetting{
		Channel: policy.ChannelEmail, ScopeType: policy.ScopeCategory, ScopeKey: string(policy.CategoryMarketing), Enabled: false,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	sender := &fakeChannelSender{}
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{BatchSize: 10, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.Calls() != 0 {
		t.Fatalf("preference off still sent calls=%d", sender.Calls())
	}
}

func TestLiveAccountDisabledBeforeDispatch(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	user, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupNotifyUser(context.Background(), pool, user) })
	box := &liveAccountDest{email: true}
	mat, err := NewMaterializer(store, box, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	if _, err := mat.Materialize(ctx, MaterializeInput{
		RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: "camp-disabled",
		Variables: map[string]string{"campaign_id": "camp-disabled"},
	}); err != nil {
		t.Fatal(err)
	}
	box.disabled = true
	sender := &fakeChannelSender{}
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{BatchSize: 10, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.Calls() != 0 {
		t.Fatalf("disabled account still sent calls=%d", sender.Calls())
	}
}

func TestLiveBoundedThroughput(t *testing.T) {
	pool := liveNotifyPool(t)
	ctx := context.Background()
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
	if _, err := store.InsertConsentDecision(ctx, user, policy.ConsentSnapshot{
		Type: policy.ConsentCommercialEmail, Status: policy.ConsentGranted,
		PolicyVersion: policy.PolicyDocumentVersion, CapturedAt: time.Now().UTC(), Source: policy.ConsentSourceSettingsWeb,
	}); err != nil {
		t.Fatal(err)
	}
	enableMarketingEmail(t, store, user)
	const n = 20
	for i := 0; i < n; i++ {
		ref := "camp-thru-" + itoa(i)
		if _, err := mat.Materialize(ctx, MaterializeInput{
			RecipientUserID: user, EventType: policy.EventMarketingCampaign, DomainRef: ref,
			Variables: map[string]string{"campaign_id": ref},
		}); err != nil {
			t.Fatal(err)
		}
	}
	sender := &fakeChannelSender{}
	d, err := NewDispatcher(store, mat, memContact{email: "owner@example.test"}, sender, nil, nil, DispatcherConfig{BatchSize: 7, ProcessingHold: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed := 0
	for i := 0; i < 8 && sender.Calls() < n; i++ {
		got, err := d.ProcessBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		claimed += got
	}
	if sender.Calls() != n || claimed != n {
		t.Fatalf("throughput calls=%d claimed=%d want=%d", sender.Calls(), claimed, n)
	}
	processing := countUserChannelState(t, store, user, policy.DeliveryProcessing)
	if processing != 0 {
		t.Fatalf("stuck processing=%d", processing)
	}
}

type liveAccountDest struct {
	email, disabled bool
}

func (d *liveAccountDest) ReadNotificationEligibility(_ context.Context, _ identitycontracts.ID) (identitycontracts.NotificationEligibility, error) {
	return identitycontracts.NotificationEligibility{EmailVerified: d.email, Disabled: d.disabled}, nil
}

func countUserChannelState(t *testing.T, store *PostgresStore, user ID, state policy.DeliveryState) int {
	t.Helper()
	row := store.db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM notifications.channel_deliveries d
		JOIN notifications.intents i ON i.id = d.intent_id
		WHERE i.recipient_user_id = $1 AND d.state = $2`, user, string(state))
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func intentIDForDomain(t *testing.T, store *PostgresStore, user ID, domainRef string) ID {
	t.Helper()
	ctx := context.Background()
	row := store.db.QueryRow(ctx, `SELECT id FROM notifications.intents WHERE recipient_user_id = $1 AND domain_ref = $2`, user, domainRef)
	var id ID
	if err := row.Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
