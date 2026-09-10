package listings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	mdcontracts "backend/internal/masterdata/contracts"
)

type stubPublishedForms struct {
	fields      []mdcontracts.PublishedField
	byVersion   map[int64][]mdcontracts.PublishedField
	err         error
	lastVersion int64
}

func defaultPublishedForms() *stubPublishedForms {
	return &stubPublishedForms{
		fields: []mdcontracts.PublishedField{{
			Code:            "condition",
			ValueType:       mdcontracts.ValueTypeEnum,
			Required:        true,
			EnumOptionCodes: []string{"used"},
		}},
	}
}

func (s *stubPublishedForms) ResolvePublishedForm(_ context.Context, categoryID mdcontracts.ID, schemaVersion int64) (mdcontracts.PublishedForm, error) {
	s.lastVersion = schemaVersion
	if s.err != nil {
		return mdcontracts.PublishedForm{}, s.err
	}
	fields := s.fields
	if s.byVersion != nil {
		f, ok := s.byVersion[schemaVersion]
		if !ok {
			return mdcontracts.PublishedForm{}, mdcontracts.ErrNotFound
		}
		fields = f
	}
	return mdcontracts.PublishedForm{
		CategoryID:    categoryID,
		SchemaVersion: schemaVersion,
		Fields:        fields,
	}, nil
}

func TestCreateDraftAcceptsValidAttributes(t *testing.T) {
	svc, _, _ := mustService(t)
	if _, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t))); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDraftRejectsMissingRequired(t *testing.T) {
	svc, _, _ := mustService(t)
	content := validContent(mustID(t))
	content.Attributes = Attributes{}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDraftRejectsUnknownAttribute(t *testing.T) {
	svc, _, _ := mustService(t)
	content := validContent(mustID(t))
	content.Attributes = Attributes{"condition": "used", "extra": "x"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDraftRejectsWrongType(t *testing.T) {
	svc, _, _ := mustService(t)
	svc.forms = &stubPublishedForms{fields: []mdcontracts.PublishedField{{
		Code:      "year",
		ValueType: mdcontracts.ValueTypeInteger,
		Required:  true,
	}}}
	content := validContent(mustID(t))
	content.Attributes = Attributes{"year": "2018"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("err = %v", err)
	}
	content.Attributes = Attributes{"year": json.Number("2018.5")}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("decimal as integer err = %v", err)
	}
	content.Attributes = Attributes{"year": json.Number("2018")}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDraftEnumOptionRules(t *testing.T) {
	svc, _, _ := mustService(t)
	content := validContent(mustID(t))
	content.Attributes = Attributes{"condition": "new"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("invalid enum err = %v", err)
	}
	content.Attributes = Attributes{"condition": "used"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDraftNumericAndTextConstraints(t *testing.T) {
	svc, _, _ := mustService(t)
	svc.forms = &stubPublishedForms{fields: []mdcontracts.PublishedField{
		{Code: "year", ValueType: mdcontracts.ValueTypeInteger, Required: true, Constraints: map[string]any{"min": 1990, "max": 2030}},
		{Code: "note", ValueType: mdcontracts.ValueTypeText, Required: false, Constraints: map[string]any{"minLength": 2, "max_length": 5}},
	}}
	content := validContent(mustID(t))
	content.Attributes = Attributes{"year": 1989}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("min err = %v", err)
	}
	content.Attributes = Attributes{"year": 2031}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("max err = %v", err)
	}
	content.Attributes = Attributes{"year": 2000, "note": "x"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("minLength err = %v", err)
	}
	content.Attributes = Attributes{"year": 2000, "note": "toolong"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("maxLength err = %v", err)
	}
	content.Attributes = Attributes{"year": 2000, "note": "ok"}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), content); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDraftRejectsUnpublishedSchema(t *testing.T) {
	svc, _, _ := mustService(t)
	svc.forms = &stubPublishedForms{err: mdcontracts.ErrNotFound}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t))); !errors.Is(err, errInvalidCategorySchema) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDraftUsesExactStoredSchemaVersion(t *testing.T) {
	forms := &stubPublishedForms{byVersion: map[int64][]mdcontracts.PublishedField{
		1: {{Code: "condition", ValueType: mdcontracts.ValueTypeEnum, Required: true, EnumOptionCodes: []string{"used"}}},
		2: {{Code: "year", ValueType: mdcontracts.ValueTypeInteger, Required: true}},
	}}
	store := NewMemoryStore()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	svc, err := NewService(store, forms, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	content := validContent(mustID(t))
	content.CategorySchemaVersion = 1
	got, err := svc.CreateDraft(context.Background(), mustID(t), content)
	if err != nil {
		t.Fatal(err)
	}
	if forms.lastVersion != 1 || got.CategorySchemaVersion != 1 {
		t.Fatalf("version = %d listing=%d", forms.lastVersion, got.CategorySchemaVersion)
	}
}

func TestUpdateDraftValidatesAgainstOriginalSchemaVersion(t *testing.T) {
	forms := &stubPublishedForms{byVersion: map[int64][]mdcontracts.PublishedField{
		1: {{Code: "condition", ValueType: mdcontracts.ValueTypeEnum, Required: true, EnumOptionCodes: []string{"used"}}},
		2: {{Code: "year", ValueType: mdcontracts.ValueTypeInteger, Required: true}},
	}}
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, forms, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	content := validContent(created.CategoryID)
	content.CategorySchemaVersion = 1
	content.Attributes = Attributes{"condition": "used"}
	patched, err := svc.UpdateDraft(context.Background(), created.ID, created.UpdatedAt, content)
	if err != nil {
		t.Fatal(err)
	}
	if forms.lastVersion != 1 || patched.CategorySchemaVersion != 1 {
		t.Fatalf("patch version = %d listing=%d", forms.lastVersion, patched.CategorySchemaVersion)
	}
	content.CategorySchemaVersion = 2
	if _, err := svc.UpdateDraft(context.Background(), created.ID, patched.UpdatedAt, content); !errors.Is(err, errInvalidContent) {
		t.Fatalf("schema switch err = %v", err)
	}
}

func TestCreateDraftMasterDataUnavailable(t *testing.T) {
	svc, _, _ := mustService(t)
	svc.forms = &stubPublishedForms{err: mdcontracts.ErrUnavailable}
	if _, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t))); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
