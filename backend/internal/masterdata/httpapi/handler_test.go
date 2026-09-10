package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"backend/internal/masterdata"
	"backend/internal/platform/db"
)

func TestListCategoriesReturnsOnlyPublished(t *testing.T) {
	h, svc, now := mustHandler(t)
	ctx := context.Background()
	draft, err := svc.CreateCategory(ctx, "test.draft", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertCategoryLabel(ctx, draft.ID, masterdata.LocaleTR, "Taslak", nil); err != nil {
		t.Fatal(err)
	}
	published := mustPublishedCategory(t, svc, now, "test.published", "Yayında", nil)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.Bytes())
	}
	if rec.Header().Get("Cache-Control") != publicCacheControl {
		t.Fatalf("cache = %q", rec.Header().Get("Cache-Control"))
	}
	var body categoriesResponse
	decode(t, rec, &body)
	if len(body.Categories) != 1 {
		t.Fatalf("categories = %+v", body.Categories)
	}
	if body.Categories[0].ID != published.ID.String() || body.Categories[0].Code != "test.published" {
		t.Fatalf("row = %+v", body.Categories[0])
	}
	if body.Categories[0].ParentID != nil || !body.Categories[0].HasPublishedForm || body.Categories[0].SchemaVersion == nil || *body.Categories[0].SchemaVersion != 1 {
		t.Fatalf("published fields = %+v", body.Categories[0])
	}
}

func TestListCategoriesLocaleLabelsAndFallback(t *testing.T) {
	h, svc, now := mustHandler(t)
	cat := mustPublishedCategory(t, svc, now, "test.goods", "Mallar", map[masterdata.Locale]string{
		masterdata.LocaleEN: "Goods",
		masterdata.LocaleRU: "Товары",
		masterdata.LocaleAR: "سلع",
	})
	cases := []struct {
		query string
		want  string
	}{
		{"", "Mallar"},
		{"?locale=tr", "Mallar"},
		{"?locale=en", "Goods"},
		{"?locale=ru", "Товары"},
		{"?locale=ar", "سلع"},
		{"?locale=en", "Goods"},
	}
	for _, tc := range cases {
		rec := serve(h, http.MethodGet, "/v1/master-data/categories"+tc.query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %s status = %d", tc.query, rec.Code)
		}
		var body categoriesResponse
		decode(t, rec, &body)
		if len(body.Categories) != 1 || body.Categories[0].Label != tc.want || body.Categories[0].ID != cat.ID.String() {
			t.Fatalf("query %s = %+v", tc.query, body.Categories)
		}
	}
	rec := serve(h, http.MethodGet, "/v1/master-data/categories?locale=xx", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid locale status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
	if rec.Header().Get("Cache-Control") == publicCacheControl {
		t.Fatal("errors must not be cached as public catalog")
	}
}

func TestListCategoriesMissingTranslationFallsBackToTR(t *testing.T) {
	h, svc, now := mustHandler(t)
	mustPublishedCategory(t, svc, now, "test.goods", "Mallar", nil)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories?locale=en", nil)
	var body categoriesResponse
	decode(t, rec, &body)
	if rec.Code != http.StatusOK || len(body.Categories) != 1 || body.Categories[0].Label != "Mallar" {
		t.Fatalf("fallback = %+v status = %d", body.Categories, rec.Code)
	}
}

func TestCurrentPublishedFormAndOrder(t *testing.T) {
	h, svc, now := mustHandler(t)
	cat, v1 := mustTwoPublishedSchemas(t, svc, now)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories/"+cat.ID.String()+"/form?locale=tr", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.Bytes())
	}
	var body formDTO
	decode(t, rec, &body)
	if body.CategoryID != cat.ID.String() || body.CategoryCode != "test.goods" || body.SchemaVersion != 2 {
		t.Fatalf("form identity = %+v (v1=%d)", body, v1)
	}
	if body.Label != "Mallar" {
		t.Fatalf("label = %q", body.Label)
	}
	if len(body.Fields) != 2 || body.Fields[0].Code != "condition" || body.Fields[1].Code != "year" {
		t.Fatalf("field order = %+v", body.Fields)
	}
	if body.Fields[0].Label != "Durum" || body.Fields[0].HelpText == nil || *body.Fields[0].HelpText != "Seçin" {
		t.Fatalf("field0 labels = %+v", body.Fields[0])
	}
	if len(body.Fields[0].Options) != 2 || body.Fields[0].Options[0].Code != "used" || body.Fields[0].Options[1].Code != "new" {
		t.Fatalf("option order = %+v", body.Fields[0].Options)
	}
	if body.Fields[0].Options[0].Label != "İkinci el" {
		t.Fatalf("option label = %+v", body.Fields[0].Options[0])
	}
	if _, ok := body.Fields[1].Constraints["min"]; !ok {
		t.Fatalf("constraints = %+v", body.Fields[1].Constraints)
	}
}

func TestExactOlderPublishedSchema(t *testing.T) {
	h, svc, now := mustHandler(t)
	cat, v1 := mustTwoPublishedSchemas(t, svc, now)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories/"+cat.ID.String()+"/schemas/1/form?locale=en", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.Bytes())
	}
	var body formDTO
	decode(t, rec, &body)
	if body.SchemaVersion != v1 || body.Label != "Mallar" {
		t.Fatalf("older form = %+v", body)
	}
	if len(body.Fields) != 1 || body.Fields[0].Code != "condition" {
		t.Fatalf("older fields = %+v", body.Fields)
	}
}

func TestUnpublishedSchemaIs404(t *testing.T) {
	h, svc, now := mustHandler(t)
	cat := mustApprovedCategory(t, svc, "test.goods", "Mallar")
	schema, err := svc.CreateSchemaVersion(context.Background(), cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories/"+cat.ID.String()+"/form", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("current unpublished status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
	rec = serve(h, http.MethodGet, "/v1/master-data/categories/"+cat.ID.String()+"/schemas/"+strconv.Itoa(schema.Version)+"/form", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("version unpublished status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestMalformedIDsVersionAndLocaleAre400(t *testing.T) {
	h, _, _ := mustHandler(t)
	cases := []string{
		"/v1/master-data/categories/not-a-uuid/form",
		"/v1/master-data/categories/00000000-0000-0000-0000-000000000000/form",
		"/v1/master-data/categories/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/schemas/abc/form",
		"/v1/master-data/categories/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/schemas/0/form",
		"/v1/master-data/categories?locale=TR",
		"/v1/master-data/categories?locale=fr",
	}
	for _, path := range cases {
		rec := serve(h, http.MethodGet, path, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		assertErrorCode(t, rec, "bad_request")
	}
}

func TestDBUnavailableIs503WithoutInternalDetail(t *testing.T) {
	store := masterdata.NewMemoryStore()
	svc, err := masterdata.NewService(store, func() time.Time { return time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(svc)
	if err != nil {
		t.Fatal(err)
	}
	store.SetFail(db.ErrUnavailable)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.Bytes())
	}
	if rec.Body.String() != "{\"error\":\"unavailable\"}\n" {
		t.Fatalf("leaked body = %q", rec.Body.String())
	}
}

func TestFormLocaleFallbackAndRTLLocales(t *testing.T) {
	h, svc, now := mustHandler(t)
	cat, _ := mustTwoPublishedSchemas(t, svc, now)
	rec := serve(h, http.MethodGet, "/v1/master-data/categories/"+cat.ID.String()+"/form?locale=ar", nil)
	var body formDTO
	decode(t, rec, &body)
	if rec.Code != http.StatusOK || body.Label != "Mallar" || body.Fields[0].Label != "Durum" || body.Fields[0].Options[0].Label != "İkinci el" {
		t.Fatalf("ar fallback = %+v", body)
	}
}

type frozenNow struct {
	now time.Time
}

func mustHandler(t *testing.T) (*Handler, *masterdata.Service, *frozenNow) {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)}
	svc, err := masterdata.NewService(masterdata.NewMemoryStore(), func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(svc)
	if err != nil {
		t.Fatal(err)
	}
	return h, svc, clock
}

func mustApprovedCategory(t *testing.T, svc *masterdata.Service, code, trLabel string) masterdata.Category {
	t.Helper()
	ctx := context.Background()
	cat, err := svc.CreateCategory(ctx, code, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertCategoryLabel(ctx, cat.ID, masterdata.LocaleTR, trLabel, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitCategoryForReview(ctx, cat.ID); err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveCategory(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func mustPublishedCategory(t *testing.T, svc *masterdata.Service, now *frozenNow, code, trLabel string, extra map[masterdata.Locale]string) masterdata.Category {
	t.Helper()
	ctx := context.Background()
	cat := mustApprovedCategory(t, svc, code, trLabel)
	for locale, label := range extra {
		if _, err := svc.UpsertCategoryLabel(ctx, cat.ID, locale, label, nil); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SubmitSchemaForReview(ctx, schema.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ApproveSchema(ctx, schema.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.PublishSchema(ctx, schema.ID); err != nil {
		t.Fatal(err)
	}
	return cat
}

func mustTwoPublishedSchemas(t *testing.T, svc *masterdata.Service, now *frozenNow) (masterdata.Category, int) {
	t.Helper()
	ctx := context.Background()
	cat := mustApprovedCategory(t, svc, "test.goods", "Mallar")
	v1, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	help := "Seçin"
	attr, err := svc.AddAttribute(ctx, v1.ID, "condition", masterdata.ValueTypeEnum, true, true, false, 0, masterdata.Constraints{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAttributeLabel(ctx, attr.ID, masterdata.LocaleTR, "Durum", &help); err != nil {
		t.Fatal(err)
	}
	used, err := svc.AddEnumOption(ctx, attr.ID, "used", 0)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := svc.AddEnumOption(ctx, attr.ID, "new", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, used.ID, attr.ID, masterdata.LocaleTR, "İkinci el"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, newer.ID, attr.ID, masterdata.LocaleTR, "Sıfır"); err != nil {
		t.Fatal(err)
	}
	publishSchema(t, svc, now, v1.ID)

	v2, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	attr2, err := svc.AddAttribute(ctx, v2.ID, "condition", masterdata.ValueTypeEnum, true, true, false, 0, masterdata.Constraints{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAttributeLabel(ctx, attr2.ID, masterdata.LocaleTR, "Durum", &help); err != nil {
		t.Fatal(err)
	}
	used2, err := svc.AddEnumOption(ctx, attr2.ID, "used", 0)
	if err != nil {
		t.Fatal(err)
	}
	new2, err := svc.AddEnumOption(ctx, attr2.ID, "new", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, used2.ID, attr2.ID, masterdata.LocaleTR, "İkinci el"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, new2.ID, attr2.ID, masterdata.LocaleTR, "Sıfır"); err != nil {
		t.Fatal(err)
	}
	year, err := svc.AddAttribute(ctx, v2.ID, "year", masterdata.ValueTypeInteger, false, true, true, 1, masterdata.Constraints{"min": 1990})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAttributeLabel(ctx, year.ID, masterdata.LocaleTR, "Yıl", nil); err != nil {
		t.Fatal(err)
	}
	publishSchema(t, svc, now, v2.ID)
	return cat, v1.Version
}

func publishSchema(t *testing.T, svc *masterdata.Service, now *frozenNow, schemaID masterdata.ID) {
	t.Helper()
	ctx := context.Background()
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SubmitSchemaForReview(ctx, schemaID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ApproveSchema(ctx, schemaID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.PublishSchema(ctx, schemaID); err != nil {
		t.Fatal(err)
	}
}

func serve(h *Handler, method, path string, header http.Header) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(method, path, nil)
	if header != nil {
		req.Header = header
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v body = %s", err, rec.Body.Bytes())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body errorResponse
	decode(t, rec, &body)
	if body.Error != code {
		t.Fatalf("error = %q want %q", body.Error, code)
	}
}
