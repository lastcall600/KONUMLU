package masterdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/masterdata/contracts"
	"backend/internal/platform/db"
)

func TestServiceCategoryLifecycleAndLabels(t *testing.T) {
	svc, _, _ := mustService(t)
	ctx := context.Background()
	cat, err := svc.CreateCategory(ctx, "test.goods", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Status != StatusDraft {
		t.Fatalf("status = %s", cat.Status)
	}
	if cat.EIDSRequirement != EIDSRequirementNone {
		t.Fatalf("dev default eids = %s", cat.EIDSRequirement)
	}
	desc := "İkinci el"
	label, err := svc.UpsertCategoryLabel(ctx, cat.ID, LocaleTR, "İlan", &desc)
	if err != nil {
		t.Fatal(err)
	}
	if label.Label != "İlan" || *label.Description != "İkinci el" {
		t.Fatalf("label = %+v", label)
	}
	if _, err := svc.ApproveCategory(ctx, cat.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("approve from draft err = %v", err)
	}
	review, err := svc.SubmitCategoryForReview(ctx, cat.ID)
	if err != nil || review.Status != StatusReview {
		t.Fatalf("review = %+v err = %v", review, err)
	}
	approved, err := svc.ApproveCategory(ctx, cat.ID)
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("approved = %+v err = %v", approved, err)
	}
}

func TestPublishedCategoryLookupRequiresPublished(t *testing.T) {
	svc, _, now := mustService(t)
	ctx := context.Background()
	lookup := NewPublishedCategories(svc)
	draft, err := svc.CreateCategory(ctx, "test.need.cat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lookup.RequirePublished(ctx, contracts.ID(draft.ID)); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("draft lookup err = %v", err)
	}
	cat := mustApprovedCategory(t, svc)
	v1, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	attr, err := svc.AddAttribute(ctx, v1.ID, "condition", ValueTypeEnum, true, true, false, 0, Constraints{})
	if err != nil {
		t.Fatal(err)
	}
	opt, err := svc.AddEnumOption(ctx, attr.ID, "used", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, opt.ID, attr.ID, LocaleTR, "İkinci el"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAttributeLabel(ctx, attr.ID, LocaleTR, "Durum", nil); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SubmitSchemaForReview(ctx, v1.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ApproveSchema(ctx, v1.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.PublishSchema(ctx, v1.ID); err != nil {
		t.Fatal(err)
	}
	if err := lookup.RequirePublished(ctx, contracts.ID(cat.ID)); err != nil {
		t.Fatalf("published lookup err = %v", err)
	}
}

func TestEIDSPolicyDefaultsNoneAndKeepsKindsSeparate(t *testing.T) {
	svc, _, now := mustService(t)
	ctx := context.Background()
	policy := NewEIDSPolicy(svc)
	cat, err := svc.CreateCategory(ctx, "test.eids.policy", nil)
	if err != nil {
		t.Fatal(err)
	}
	req, err := policy.Requirement(ctx, contracts.ID(cat.ID))
	if err != nil || req != contracts.EIDSRequirementNone {
		t.Fatalf("default = %q err=%v", req, err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SetCategoryEIDSRequirement(ctx, cat.ID, EIDSRequirementProperty); err != nil {
		t.Fatal(err)
	}
	req, err = policy.Requirement(ctx, contracts.ID(cat.ID))
	if err != nil || req != contracts.EIDSRequirementProperty {
		t.Fatalf("property = %q err=%v", req, err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SetCategoryEIDSRequirement(ctx, cat.ID, EIDSRequirementVehicle); err != nil {
		t.Fatal(err)
	}
	req, err = policy.Requirement(ctx, contracts.ID(cat.ID))
	if err != nil || req != contracts.EIDSRequirementVehicle {
		t.Fatalf("vehicle = %q err=%v", req, err)
	}
	if contracts.EIDSRequirementProperty == contracts.EIDSRequirementVehicle {
		t.Fatal("kinds must stay separate")
	}
}

func TestServiceSchemaVersioningAndPublishedImmutability(t *testing.T) {
	svc, _, now := mustService(t)
	ctx := context.Background()
	cat := mustApprovedCategory(t, svc)
	v1, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil || v1.Version != 1 || v1.Status != StatusDraft {
		t.Fatalf("v1 = %+v err = %v", v1, err)
	}
	attr, err := svc.AddAttribute(ctx, v1.ID, "condition", ValueTypeEnum, true, true, false, 0, Constraints{})
	if err != nil {
		t.Fatal(err)
	}
	opt, err := svc.AddEnumOption(ctx, attr.ID, "used", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, opt.ID, attr.ID, LocaleTR, "İkinci el"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAttributeLabel(ctx, attr.ID, LocaleTR, "Durum", nil); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SubmitSchemaForReview(ctx, v1.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ApproveSchema(ctx, v1.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.PublishSchema(ctx, v1.ID)
	if err != nil || published.Status != StatusPublished || published.Version != 1 {
		t.Fatalf("published = %+v err = %v", published, err)
	}
	if _, err := svc.AddAttribute(ctx, v1.ID, "year", ValueTypeInteger, false, true, true, 1, Constraints{}); !errors.Is(err, errImmutablePublished) {
		t.Fatalf("edit published err = %v", err)
	}
	if _, err := svc.AddEnumOption(ctx, attr.ID, "new", 1); !errors.Is(err, errImmutablePublished) {
		t.Fatalf("option on published err = %v", err)
	}
	v2, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil || v2.Version != 2 || v2.Status != StatusDraft {
		t.Fatalf("v2 = %+v err = %v", v2, err)
	}
	attr2, err := svc.AddAttribute(ctx, v2.ID, "year", ValueTypeInteger, false, true, true, 0, Constraints{"min": 1990})
	if err != nil {
		t.Fatal(err)
	}
	if attr2.SchemaID != v2.ID {
		t.Fatalf("attr2 schema = %s", attr2.SchemaID)
	}
	form, err := svc.ResolvePublishedForm(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if form.SchemaVersion != 1 || form.SchemaID != v1.ID || len(form.Fields) != 1 || form.Fields[0].Code != "condition" {
		t.Fatalf("form still on v1 = %+v", form)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.SubmitSchemaForReview(ctx, v2.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ApproveSchema(ctx, v2.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.PublishSchema(ctx, v2.ID); err != nil {
		t.Fatal(err)
	}
	latest, err := svc.ResolvePublishedForm(ctx, cat.ID)
	if err != nil || latest.SchemaVersion != 2 {
		t.Fatalf("latest = %+v err = %v", latest, err)
	}
	bound, err := svc.ResolvePublishedFormAt(ctx, cat.ID, 1)
	if err != nil || bound.SchemaVersion != 1 || bound.SchemaID != v1.ID {
		t.Fatalf("exact v1 = %+v err = %v", bound, err)
	}
}

func TestServiceEnumAndNonEnumOptionRules(t *testing.T) {
	svc, _, _ := mustService(t)
	ctx := context.Background()
	cat := mustApprovedCategory(t, svc)
	schema, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	textAttr, err := svc.AddAttribute(ctx, schema.ID, "title_extra", ValueTypeText, false, false, true, 0, Constraints{"max_length": 80})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddEnumOption(ctx, textAttr.ID, "x", 0); !errors.Is(err, errOptionsNotAllowed) {
		t.Fatalf("non-enum options err = %v", err)
	}
	enumAttr, err := svc.AddAttribute(ctx, schema.ID, "condition", ValueTypeEnum, true, true, false, 1, Constraints{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitSchemaForReview(ctx, schema.ID); !errors.Is(err, errEnumRequiresOptions) {
		t.Fatalf("enum without options err = %v", err)
	}
	if _, err := svc.AddEnumOption(ctx, enumAttr.ID, "used", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitSchemaForReview(ctx, schema.ID); err != nil {
		t.Fatal(err)
	}
}

func TestServiceResolvePublishedFormDefinition(t *testing.T) {
	svc, _, now := mustService(t)
	ctx := context.Background()
	cat := mustApprovedCategory(t, svc)
	if _, err := svc.UpsertCategoryLabel(ctx, cat.ID, LocaleEN, "Goods", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolvePublishedForm(ctx, cat.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("unpublished form err = %v", err)
	}
	schema, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	attr, err := svc.AddAttribute(ctx, schema.ID, "condition", ValueTypeEnum, true, true, false, 0, Constraints{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAttributeLabel(ctx, attr.ID, LocaleTR, "Durum", nil); err != nil {
		t.Fatal(err)
	}
	opt, err := svc.AddEnumOption(ctx, attr.ID, "used", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertOptionLabel(ctx, opt.ID, attr.ID, LocaleTR, "İkinci el"); err != nil {
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
	form, err := svc.ResolvePublishedForm(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if form.CategoryID != cat.ID || form.CategoryCode != "test.goods" || form.SchemaVersion != 1 {
		t.Fatalf("form identity = %+v", form)
	}
	if len(form.CategoryLabels) != 2 {
		t.Fatalf("category labels = %+v", form.CategoryLabels)
	}
	if len(form.Fields) != 1 || form.Fields[0].ValueType != ValueTypeEnum || !form.Fields[0].Required {
		t.Fatalf("fields = %+v", form.Fields)
	}
	if len(form.Fields[0].Options) != 1 || form.Fields[0].Options[0].Labels[0].Label != "İkinci el" {
		t.Fatalf("options = %+v", form.Fields[0].Options)
	}
}

func TestServiceResolvePublishedFormAtRejectsUnpublishedAndMissing(t *testing.T) {
	svc, _, now := mustService(t)
	ctx := context.Background()
	cat := mustApprovedCategory(t, svc)
	schema, err := svc.CreateSchemaVersion(ctx, cat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolvePublishedFormAt(ctx, cat.ID, schema.Version); !errors.Is(err, errNotFound) {
		t.Fatalf("draft version err = %v", err)
	}
	if _, err := svc.ResolvePublishedFormAt(ctx, cat.ID, 99); !errors.Is(err, errNotFound) {
		t.Fatalf("missing version err = %v", err)
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
	got, err := svc.ResolvePublishedFormAt(ctx, cat.ID, schema.Version)
	if err != nil || got.SchemaVersion != schema.Version {
		t.Fatalf("published at = %+v err = %v", got, err)
	}
	api := NewPublishedForms(svc)
	form, err := api.ResolvePublishedForm(ctx, contracts.ID(cat.ID), int64(schema.Version))
	if err != nil || form.SchemaVersion != int64(schema.Version) || form.CategoryID != contracts.ID(cat.ID) {
		t.Fatalf("contract form = %+v err = %v", form, err)
	}
	if len(form.Fields) != 0 {
		t.Fatalf("empty schema fields = %+v", form.Fields)
	}
}

func TestServicePublishRequiresApprovedCategory(t *testing.T) {
	svc, _, now := mustService(t)
	ctx := context.Background()
	cat, err := svc.CreateCategory(ctx, "test.draft-only", nil)
	if err != nil {
		t.Fatal(err)
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
	if _, err := svc.PublishSchema(ctx, schema.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("publish with draft category err = %v", err)
	}
}

func TestServicePersistenceErrorMapping(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(store, func() time.Time { return time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	store.SetFail(db.ErrUnavailable)
	if _, err := svc.CreateCategory(context.Background(), "x", nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("unavailable err = %v", err)
	}
	store.SetFail(db.ErrNoRows)
	if _, err := svc.ResolvePublishedForm(context.Background(), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("not found err = %v", err)
	}
	store.SetFail(db.ErrConflict)
	if _, err := svc.CreateSchemaVersion(context.Background(), mustID(t)); !errors.Is(err, errConflict) {
		t.Fatalf("conflict err = %v", err)
	}
	if _, err := NewService(nil, nil); !errors.Is(err, errStoreRequired) {
		t.Fatalf("nil store err = %v", err)
	}
}

type frozenNow struct {
	now time.Time
}

func mustService(t *testing.T) (*Service, *MemoryStore, *frozenNow) {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	svc, err := NewService(store, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

func mustApprovedCategory(t *testing.T, svc *Service) Category {
	t.Helper()
	ctx := context.Background()
	cat, err := svc.CreateCategory(ctx, "test.goods", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertCategoryLabel(ctx, cat.ID, LocaleTR, "Mallar", nil); err != nil {
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
