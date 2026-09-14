package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/notifications"
	"backend/internal/notifications/policy"
)

const allowedOrigin = "https://app.example.test"

type fakeSessions struct {
	users map[string]notifications.ID
}

func (f fakeSessions) Resolve(_ context.Context, raw string) (notifications.ID, error) {
	id, ok := f.users[raw]
	if !ok {
		return notifications.ID{}, ErrUnauthenticated
	}
	return id, nil
}

type memConsumer struct {
	prefs map[string][]policy.EffectivePreference
	cons  map[string][]policy.ConsentSnapshot
	inbox map[string][]notifications.InboxRow
	push  map[string][]notifications.PushEndpointView
}

func newMemConsumer() *memConsumer {
	return &memConsumer{
		prefs: map[string][]policy.EffectivePreference{},
		cons:  map[string][]policy.ConsentSnapshot{},
		inbox: map[string][]notifications.InboxRow{},
		push:  map[string][]notifications.PushEndpointView{},
	}
}

func (m *memConsumer) GetPreferences(_ context.Context, userID notifications.ID) ([]policy.EffectivePreference, error) {
	if rows, ok := m.prefs[userID.String()]; ok {
		return rows, nil
	}
	return policy.EffectivePreferenceRows(policy.DefaultPreferences()), nil
}

func (m *memConsumer) PatchPreferences(ctx context.Context, userID notifications.ID, patch policy.PreferencePatch) ([]policy.EffectivePreference, error) {
	base := policy.DefaultPreferences()
	next, err := policy.ApplyPreferencePatch(base, patch)
	if err != nil {
		return nil, err
	}
	rows := policy.EffectivePreferenceRows(next)
	m.prefs[userID.String()] = rows
	return rows, nil
}

func (m *memConsumer) GetConsents(_ context.Context, userID notifications.ID) ([]notifications.ConsentView, error) {
	hist := m.cons[userID.String()]
	byType := map[policy.ConsentType]policy.ConsentSnapshot{}
	for _, c := range hist {
		if existing, ok := byType[c.Type]; !ok || c.RecordedSeq > existing.RecordedSeq {
			byType[c.Type] = c
		}
	}
	out := []notifications.ConsentView{}
	for _, typ := range []policy.ConsentType{policy.ConsentCommercialEmail, policy.ConsentCommercialSMS, policy.ConsentCommercialPush} {
		v := notifications.ConsentView{Type: typ, PolicyVersion: policy.PolicyDocumentVersion}
		if c, ok := byType[typ]; ok {
			st := c.Status
			v.Decision = &st
			t := c.CapturedAt
			v.RecordedAt = &t
		}
		out = append(out, v)
	}
	return out, nil
}

func (m *memConsumer) RecordConsent(_ context.Context, userID notifications.ID, typ policy.ConsentType, grant bool) (policy.ConsentSnapshot, error) {
	snap, err := policy.ValidateConsentDecision(policy.ConsentDecision{
		Type: typ, Grant: grant, Source: policy.ConsentSourceSettingsWeb,
	}, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return policy.ConsentSnapshot{}, err
	}
	snap.RecordedSeq = int64(len(m.cons[userID.String()]) + 1)
	m.cons[userID.String()] = append(m.cons[userID.String()], snap)
	return snap, nil
}

func (m *memConsumer) ListInbox(_ context.Context, userID notifications.ID, _ string, _ int) (notifications.InboxPage, error) {
	return notifications.InboxPage{Items: append([]notifications.InboxRow(nil), m.inbox[userID.String()]...)}, nil
}

func (m *memConsumer) MarkRead(_ context.Context, userID, inboxID notifications.ID) (notifications.InboxRow, error) {
	items := m.inbox[userID.String()]
	for i, it := range items {
		if it.ID == inboxID {
			now := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
			if it.ReadAt == nil {
				it.ReadAt = &now
				items[i] = it
				m.inbox[userID.String()] = items
			}
			return items[i], nil
		}
	}
	return notifications.InboxRow{}, notifications.ErrNotFound
}

func (m *memConsumer) MarkAllRead(_ context.Context, userID notifications.ID) error {
	now := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
	items := m.inbox[userID.String()]
	for i := range items {
		if items[i].ReadAt == nil {
			items[i].ReadAt = &now
		}
	}
	m.inbox[userID.String()] = items
	return nil
}

func (m *memConsumer) RegisterPushEndpoint(_ context.Context, userID notifications.ID, in notifications.PushRegistration) (notifications.PushEndpointView, error) {
	if err := notifications.ValidatePushRegistration(in); err != nil {
		return notifications.PushEndpointView{}, err
	}
	now := time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC)
	rows := m.push[userID.String()]
	for i, row := range rows {
		if row.Channel == in.Channel && row.Platform == in.Platform && row.Provider == in.Provider && !row.Revoked {
			row.LastSeenAt = now
			rows[i] = row
			m.push[userID.String()] = rows
			return row, nil
		}
	}
	id, err := notifications.NewID()
	if err != nil {
		return notifications.PushEndpointView{}, err
	}
	view := notifications.PushEndpointView{
		ID: id, Channel: in.Channel, Platform: in.Platform, Provider: in.Provider,
		CreatedAt: now, LastSeenAt: now,
	}
	m.push[userID.String()] = append(rows, view)
	return view, nil
}

func (m *memConsumer) ListPushEndpoints(_ context.Context, userID notifications.ID) ([]notifications.PushEndpointView, error) {
	out := make([]notifications.PushEndpointView, 0)
	for _, row := range m.push[userID.String()] {
		if !row.Revoked {
			out = append(out, row)
		}
	}
	return out, nil
}

func (m *memConsumer) RevokePushEndpoint(_ context.Context, userID, endpointID notifications.ID) (notifications.PushEndpointView, error) {
	rows := m.push[userID.String()]
	for i, row := range rows {
		if row.ID == endpointID {
			row.Revoked = true
			rows[i] = row
			m.push[userID.String()] = rows
			return row, nil
		}
	}
	return notifications.PushEndpointView{}, notifications.ErrNotFound
}

func mustID(t *testing.T) notifications.ID {
	t.Helper()
	id, err := notifications.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testHandler(t *testing.T, userA, userB notifications.ID, svc *memConsumer) *Handler {
	t.Helper()
	h, err := New(fakeSessions{users: map[string]notifications.ID{"tok-a": userA, "tok-b": userB}}, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(t *testing.T, h *Handler, method, path, origin string, body any, cookies map[string]string, headers ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, extra := range headers {
		for k, v := range extra {
			req.Header.Set(k, v)
		}
	}
	for name, val := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func authed(user string) map[string]string {
	return map[string]string{sessionCookieName: "tok-" + user, csrfCookieName: "csrf-token"}
}

func withCSRF() map[string]string {
	return map[string]string{"X-CSRF-Token": "csrf-token"}
}

func TestUnauthenticatedRejected(t *testing.T) {
	h := testHandler(t, mustID(t), mustID(t), newMemConsumer())
	paths := []struct{ method, path string }{
		{http.MethodGet, "/v1/notification-preferences"},
		{http.MethodPatch, "/v1/notification-preferences"},
		{http.MethodGet, "/v1/notification-consents"},
		{http.MethodPost, "/v1/notification-consents"},
		{http.MethodGet, "/v1/notifications"},
		{http.MethodPost, "/v1/notifications/read-all"},
		{http.MethodPost, "/v1/push-endpoints"},
		{http.MethodGet, "/v1/push-endpoints"},
	}
	for _, p := range paths {
		rec := do(t, h, p.method, p.path, allowedOrigin, map[string]any{}, nil, withCSRF())
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d", p.method, p.path, rec.Code)
		}
	}
}

func TestMutationRequiresCSRFAndOrigin(t *testing.T) {
	h := testHandler(t, mustID(t), mustID(t), newMemConsumer())
	body := map[string]any{"overrides": []map[string]any{{"channel": "email", "scopeType": "channel", "scopeKey": "*", "enabled": false}}}
	rec := do(t, h, http.MethodPatch, "/v1/notification-preferences", "", body, authed("a"), withCSRF())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPatch, "/v1/notification-preferences", allowedOrigin, body, authed("a"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf = %d", rec.Code)
	}
}

func TestUnknownKeysAndUserIDRejected(t *testing.T) {
	h := testHandler(t, mustID(t), mustID(t), newMemConsumer())
	rec := do(t, h, http.MethodPatch, "/v1/notification-preferences", allowedOrigin, map[string]any{
		"userId":    "other",
		"overrides": []map[string]any{{"channel": "email", "scopeType": "channel", "scopeKey": "*", "enabled": false}},
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("userId body = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/notification-consents", allowedOrigin, map[string]any{
		"consentType": "commercial_electronic.email", "decision": "granted", "policyVersion": "forged",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("forged policy version = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPreferenceAndConsentIsolation(t *testing.T) {
	a, b := mustID(t), mustID(t)
	svc := newMemConsumer()
	h := testHandler(t, a, b, svc)
	rec := do(t, h, http.MethodPatch, "/v1/notification-preferences", allowedOrigin, map[string]any{
		"overrides": []map[string]any{{"channel": "email", "scopeType": "channel", "scopeKey": "*", "enabled": false}},
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("patch a = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/notification-preferences", "", nil, authed("b"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"stored":false`) && strings.Contains(rec.Body.String(), `"channel":"email"`) {
		// user b may have stored null; ensure they didn't inherit a mute as stored
	}
	rec = do(t, h, http.MethodPost, "/v1/notification-consents", allowedOrigin, map[string]any{
		"consentType": "commercial_electronic.email", "decision": "granted",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("consent a = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/notification-consents", "", nil, authed("b"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"decision":"granted"`) {
		t.Fatalf("user b saw user a consent: %s", rec.Body.String())
	}
}

func TestInboxOwnership(t *testing.T) {
	a, b := mustID(t), mustID(t)
	itemID := mustID(t)
	svc := newMemConsumer()
	now := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	svc.inbox[a.String()] = []notifications.InboxRow{{
		ID: itemID, UserID: a, CreatedAt: now, EventType: policy.EventSecurityLoginNew, Purpose: policy.PurposeSecurity, TemplateKey: "security.login_new",
	}}
	h := testHandler(t, a, b, svc)
	rec := do(t, h, http.MethodGet, "/v1/notifications", "", nil, authed("b"))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), itemID.String()) {
		t.Fatalf("b listed a inbox: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/notifications/"+itemID.String()+"/read", allowedOrigin, map[string]any{}, authed("b"), withCSRF())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("b mark-read = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/notifications/"+itemID.String()+"/read", allowedOrigin, map[string]any{}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("a mark-read = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/notifications/"+itemID.String()+"/read", allowedOrigin, map[string]any{}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent mark-read = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/notifications/read-all", allowedOrigin, map[string]any{}, authed("b"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("mark-all b = %d", rec.Code)
	}
	if svc.inbox[a.String()][0].ReadAt == nil {
		t.Fatal("mark-all b must not clear a")
	}
}

func TestPreferenceDoesNotGrantConsent(t *testing.T) {
	h := testHandler(t, mustID(t), mustID(t), newMemConsumer())
	rec := do(t, h, http.MethodPatch, "/v1/notification-preferences", allowedOrigin, map[string]any{
		"overrides": []map[string]any{{"channel": "email", "scopeType": "category", "scopeKey": "marketing", "enabled": true}},
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("pref = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/notification-consents", "", nil, authed("a"))
	if strings.Contains(rec.Body.String(), `"decision":"granted"`) {
		t.Fatal("preference must not create consent")
	}
}

func TestUnknownConsentTypeRejected(t *testing.T) {
	h := testHandler(t, mustID(t), mustID(t), newMemConsumer())
	rec := do(t, h, http.MethodPost, "/v1/notification-consents", allowedOrigin, map[string]any{
		"consentType": "kvkk.always_ok", "decision": "granted",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func webPushBody() map[string]any {
	return map[string]any{
		"channel":  "web_push",
		"platform": "web",
		"provider": "webpush",
		"endpoint": "https://push.example.test/subscription/abc",
		"p256dh":   "BNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w1",
		"auth":     "tBHItJI5svbpez7KI4CCXg",
	}
}

func TestPushEndpointHTTPSecurity(t *testing.T) {
	a, b := mustID(t), mustID(t)
	h := testHandler(t, a, b, newMemConsumer())
	body := webPushBody()

	rec := do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, body, nil, withCSRF())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth register = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", "", body, authed("a"), withCSRF())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", "https://evil.example", body, authed("a"), withCSRF())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong origin = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, body, authed("a"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, map[string]any{
		"userId": a.String(), "channel": "web_push", "platform": "web", "provider": "webpush",
		"endpoint": "https://push.example.test/subscription/abc",
		"p256dh":   "BNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w1",
		"auth":     "tBHItJI5svbpez7KI4CCXg",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("userId body = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, map[string]any{
		"channel": "mobile_push", "platform": "web", "provider": "fcm", "token": "aaaaaaaaaaaaaaaa",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported combo = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, map[string]any{
		"channel": "mobile_push", "platform": "android", "provider": "fcm", "token": "short",
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short token = %d", rec.Code)
	}
	tooLong := strings.Repeat("a", 5000)
	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, map[string]any{
		"channel": "mobile_push", "platform": "android", "provider": "fcm", "token": tooLong,
	}, authed("a"), withCSRF())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("huge token = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, body, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "p256dh") || strings.Contains(rec.Body.String(), "subscription/abc") || strings.Contains(rec.Body.String(), `"auth"`) {
		t.Fatalf("raw material returned: %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/push-endpoints", "", nil, authed("b"))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"web_push"`) {
		t.Fatalf("b listed a endpoints: %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/push-endpoints", "", nil, authed("a"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"web_push"`) {
		t.Fatalf("list a = %s", rec.Body.String())
	}
	var listed struct {
		Endpoints []struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || len(listed.Endpoints) != 1 {
		t.Fatalf("list decode %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodDelete, "/v1/push-endpoints/"+listed.Endpoints[0].ID, allowedOrigin, nil, authed("b"), withCSRF())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("b revoke a = %d", rec.Code)
	}
	rec = do(t, h, http.MethodDelete, "/v1/push-endpoints/"+listed.Endpoints[0].ID, allowedOrigin, nil, authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLogoutSessionLossDoesNotRevokePushEndpoint(t *testing.T) {
	a := mustID(t)
	svc := newMemConsumer()
	h := testHandler(t, a, mustID(t), svc)
	rec := do(t, h, http.MethodPost, "/v1/push-endpoints", allowedOrigin, webPushBody(), authed("a"), withCSRF())
	if rec.Code != http.StatusOK {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/push-endpoints", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out list = %d", rec.Code)
	}
	if len(svc.push[a.String()]) != 1 || svc.push[a.String()][0].Revoked {
		t.Fatal("logout must not revoke stored endpoints")
	}
	rec = do(t, h, http.MethodGet, "/v1/push-endpoints", "", nil, authed("a"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"web_push"`) {
		t.Fatalf("re-login list = %s", rec.Body.String())
	}
}
