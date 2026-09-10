package masterdata

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
)

// Service orchestrates Master Data taxonomy and schema-driven form definitions.
type Service struct {
	store store
	now   func() time.Time
}

func NewService(st store, now func() time.Time) (*Service, error) {
	if st == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: st, now: now}, nil
}

func (s *Service) CreateCategory(ctx context.Context, code string, parentID *ID) (Category, error) {
	if s == nil || s.store == nil {
		return Category{}, errStoreRequired
	}
	if parentID != nil {
		if _, err := s.store.GetCategory(ctx, *parentID); err != nil {
			return Category{}, mapStoreErr(err)
		}
	}
	cat, err := NewCategory(code, parentID, s.now().UTC())
	if err != nil {
		return Category{}, err
	}
	if err := s.store.CreateCategory(ctx, cat); err != nil {
		return Category{}, mapStoreErr(err)
	}
	return cat, nil
}

func (s *Service) UpsertCategoryLabel(ctx context.Context, categoryID ID, locale Locale, label string, description *string) (CategoryLabel, error) {
	if s == nil || s.store == nil {
		return CategoryLabel{}, errStoreRequired
	}
	if _, err := s.store.GetCategory(ctx, categoryID); err != nil {
		return CategoryLabel{}, mapStoreErr(err)
	}
	row, err := NewCategoryLabel(categoryID, locale, label, description)
	if err != nil {
		return CategoryLabel{}, err
	}
	if err := s.store.UpsertCategoryLabel(ctx, row); err != nil {
		return CategoryLabel{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) SubmitCategoryForReview(ctx context.Context, categoryID ID) (Category, error) {
	return s.mutateCategory(ctx, categoryID, func(c Category, now time.Time) (Category, error) {
		return c.SubmitForReview(now)
	})
}

func (s *Service) ApproveCategory(ctx context.Context, categoryID ID) (Category, error) {
	return s.mutateCategory(ctx, categoryID, func(c Category, now time.Time) (Category, error) {
		return c.Approve(now)
	})
}

func (s *Service) SetCategoryEIDSRequirement(ctx context.Context, categoryID ID, req EIDSRequirement) (Category, error) {
	return s.mutateCategory(ctx, categoryID, func(c Category, now time.Time) (Category, error) {
		return c.WithEIDSRequirement(req, now)
	})
}

func (s *Service) CreateSchemaVersion(ctx context.Context, categoryID ID) (Schema, error) {
	if s == nil || s.store == nil {
		return Schema{}, errStoreRequired
	}
	if _, err := s.store.GetCategory(ctx, categoryID); err != nil {
		return Schema{}, mapStoreErr(err)
	}
	max, err := s.store.MaxSchemaVersion(ctx, categoryID)
	if err != nil {
		return Schema{}, mapStoreErr(err)
	}
	schema, err := NewSchema(categoryID, max+1, s.now().UTC())
	if err != nil {
		return Schema{}, err
	}
	if err := s.store.CreateSchema(ctx, schema); err != nil {
		return Schema{}, mapStoreErr(err)
	}
	return schema, nil
}

func (s *Service) AddAttribute(ctx context.Context, schemaID ID, code string, valueType ValueType, required, filterable, sortable bool, sortOrder int, constraints Constraints) (Attribute, error) {
	if s == nil || s.store == nil {
		return Attribute{}, errStoreRequired
	}
	schema, err := s.store.GetSchema(ctx, schemaID)
	if err != nil {
		return Attribute{}, mapStoreErr(err)
	}
	if err := schema.assertDraft(); err != nil {
		return Attribute{}, err
	}
	attr, err := NewAttribute(schemaID, code, valueType, required, filterable, sortable, sortOrder, constraints)
	if err != nil {
		return Attribute{}, err
	}
	if err := s.store.CreateAttribute(ctx, attr); err != nil {
		return Attribute{}, mapStoreErr(err)
	}
	return attr, nil
}

func (s *Service) UpsertAttributeLabel(ctx context.Context, attributeID ID, locale Locale, label string, helpText *string) (AttributeLabel, error) {
	if s == nil || s.store == nil {
		return AttributeLabel{}, errStoreRequired
	}
	attr, err := s.store.GetAttribute(ctx, attributeID)
	if err != nil {
		return AttributeLabel{}, mapStoreErr(err)
	}
	schema, err := s.store.GetSchema(ctx, attr.SchemaID)
	if err != nil {
		return AttributeLabel{}, mapStoreErr(err)
	}
	if err := schema.assertDraft(); err != nil {
		return AttributeLabel{}, err
	}
	row, err := NewAttributeLabel(attributeID, locale, label, helpText)
	if err != nil {
		return AttributeLabel{}, err
	}
	if err := s.store.UpsertAttributeLabel(ctx, row); err != nil {
		return AttributeLabel{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) AddEnumOption(ctx context.Context, attributeID ID, code string, sortOrder int) (AttributeOption, error) {
	if s == nil || s.store == nil {
		return AttributeOption{}, errStoreRequired
	}
	attr, err := s.store.GetAttribute(ctx, attributeID)
	if err != nil {
		return AttributeOption{}, mapStoreErr(err)
	}
	if attr.ValueType != ValueTypeEnum {
		return AttributeOption{}, errOptionsNotAllowed
	}
	schema, err := s.store.GetSchema(ctx, attr.SchemaID)
	if err != nil {
		return AttributeOption{}, mapStoreErr(err)
	}
	if err := schema.assertDraft(); err != nil {
		return AttributeOption{}, err
	}
	opt, err := NewAttributeOption(attributeID, code, sortOrder)
	if err != nil {
		return AttributeOption{}, err
	}
	if err := s.store.CreateOption(ctx, opt); err != nil {
		return AttributeOption{}, mapStoreErr(err)
	}
	return opt, nil
}

func (s *Service) UpsertOptionLabel(ctx context.Context, optionID ID, attributeID ID, locale Locale, label string) (OptionLabel, error) {
	if s == nil || s.store == nil {
		return OptionLabel{}, errStoreRequired
	}
	attr, err := s.store.GetAttribute(ctx, attributeID)
	if err != nil {
		return OptionLabel{}, mapStoreErr(err)
	}
	schema, err := s.store.GetSchema(ctx, attr.SchemaID)
	if err != nil {
		return OptionLabel{}, mapStoreErr(err)
	}
	if err := schema.assertDraft(); err != nil {
		return OptionLabel{}, err
	}
	opts, err := s.store.ListOptions(ctx, attributeID)
	if err != nil {
		return OptionLabel{}, mapStoreErr(err)
	}
	found := false
	for _, o := range opts {
		if o.ID == optionID {
			found = true
			break
		}
	}
	if !found {
		return OptionLabel{}, errNotFound
	}
	row, err := NewOptionLabel(optionID, locale, label)
	if err != nil {
		return OptionLabel{}, err
	}
	if err := s.store.UpsertOptionLabel(ctx, row); err != nil {
		return OptionLabel{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) SubmitSchemaForReview(ctx context.Context, schemaID ID) (Schema, error) {
	return s.mutateSchema(ctx, schemaID, func(schema Schema, now time.Time) (Schema, error) {
		if err := s.validateSchemaForReview(ctx, schema); err != nil {
			return Schema{}, err
		}
		return schema.SubmitForReview(now)
	})
}

func (s *Service) ApproveSchema(ctx context.Context, schemaID ID) (Schema, error) {
	return s.mutateSchema(ctx, schemaID, func(schema Schema, now time.Time) (Schema, error) {
		return schema.Approve(now)
	})
}

func (s *Service) PublishSchema(ctx context.Context, schemaID ID) (Schema, error) {
	if s == nil || s.store == nil {
		return Schema{}, errStoreRequired
	}
	schema, err := s.store.GetSchema(ctx, schemaID)
	if err != nil {
		return Schema{}, mapStoreErr(err)
	}
	if err := s.validateSchemaForReview(ctx, schema); err != nil {
		return Schema{}, err
	}
	now := s.now().UTC()
	next, err := schema.Publish(now)
	if err != nil {
		return Schema{}, err
	}
	category, err := s.store.GetCategory(ctx, schema.CategoryID)
	if err != nil {
		return Schema{}, mapStoreErr(err)
	}
	publishedCat, err := category.Publish(now)
	if err != nil {
		return Schema{}, err
	}
	if err := s.store.UpdateSchema(ctx, next); err != nil {
		return Schema{}, mapStoreErr(err)
	}
	if err := s.store.UpdateCategory(ctx, publishedCat); err != nil {
		return Schema{}, mapStoreErr(err)
	}
	return next, nil
}

// PublishedCategory is the public catalog row for a published taxonomy node.
type PublishedCategory struct {
	ID               ID
	Code             string
	ParentID         *ID
	Labels           []CategoryLabel
	HasPublishedForm bool
	SchemaVersion    int
}

func (s *Service) ListPublishedCategories(ctx context.Context) ([]PublishedCategory, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	cats, err := s.store.ListPublishedCategories(ctx)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	out := make([]PublishedCategory, 0, len(cats))
	for _, cat := range cats {
		labels, err := s.store.ListCategoryLabels(ctx, cat.ID)
		if err != nil {
			return nil, mapStoreErr(err)
		}
		item := PublishedCategory{
			ID:       cat.ID,
			Code:     cat.Code,
			ParentID: cloneIDPtr(cat.ParentID),
			Labels:   labels,
		}
		schema, err := s.store.GetPublishedSchema(ctx, cat.ID)
		if err != nil {
			if !errors.Is(err, errNotFound) && !errors.Is(err, db.ErrNoRows) {
				return nil, mapStoreErr(err)
			}
		} else {
			item.HasPublishedForm = true
			item.SchemaVersion = schema.Version
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) ResolvePublishedForm(ctx context.Context, categoryID ID) (FormDefinition, error) {
	if s == nil || s.store == nil {
		return FormDefinition{}, errStoreRequired
	}
	category, err := s.store.GetCategory(ctx, categoryID)
	if err != nil {
		return FormDefinition{}, mapStoreErr(err)
	}
	schema, err := s.store.GetPublishedSchema(ctx, categoryID)
	if err != nil {
		return FormDefinition{}, mapStoreErr(err)
	}
	return s.formDefinition(ctx, category, schema)
}

func (s *Service) ResolvePublishedFormAt(ctx context.Context, categoryID ID, version int) (FormDefinition, error) {
	if s == nil || s.store == nil {
		return FormDefinition{}, errStoreRequired
	}
	if categoryID.IsZero() {
		return FormDefinition{}, errZeroID
	}
	if version <= 0 {
		return FormDefinition{}, errInvalidSchema
	}
	category, err := s.store.GetCategory(ctx, categoryID)
	if err != nil {
		return FormDefinition{}, mapStoreErr(err)
	}
	schema, err := s.store.GetSchemaByCategoryVersion(ctx, categoryID, version)
	if err != nil {
		return FormDefinition{}, mapStoreErr(err)
	}
	if schema.Status != StatusPublished {
		return FormDefinition{}, errNotFound
	}
	return s.formDefinition(ctx, category, schema)
}

func (s *Service) formDefinition(ctx context.Context, category Category, schema Schema) (FormDefinition, error) {
	catLabels, err := s.store.ListCategoryLabels(ctx, category.ID)
	if err != nil {
		return FormDefinition{}, mapStoreErr(err)
	}
	attrs, err := s.store.ListAttributes(ctx, schema.ID)
	if err != nil {
		return FormDefinition{}, mapStoreErr(err)
	}
	fields := make([]FormField, 0, len(attrs))
	for _, attr := range attrs {
		labels, err := s.store.ListAttributeLabels(ctx, attr.ID)
		if err != nil {
			return FormDefinition{}, mapStoreErr(err)
		}
		optRows, err := s.store.ListOptions(ctx, attr.ID)
		if err != nil {
			return FormDefinition{}, mapStoreErr(err)
		}
		options := make([]FormOption, 0, len(optRows))
		for _, opt := range optRows {
			olabels, err := s.store.ListOptionLabels(ctx, opt.ID)
			if err != nil {
				return FormDefinition{}, mapStoreErr(err)
			}
			options = append(options, FormOption{
				OptionID:  opt.ID,
				Code:      opt.Code,
				SortOrder: opt.SortOrder,
				Labels:    olabels,
			})
		}
		fields = append(fields, FormField{
			AttributeID: attr.ID,
			Code:        attr.Code,
			ValueType:   attr.ValueType,
			Required:    attr.Required,
			Filterable:  attr.Filterable,
			Sortable:    attr.Sortable,
			SortOrder:   attr.SortOrder,
			Constraints: cloneConstraints(attr.Constraints),
			Labels:      labels,
			Options:     options,
		})
	}
	return FormDefinition{
		CategoryID:     category.ID,
		CategoryCode:   category.Code,
		SchemaID:       schema.ID,
		SchemaVersion:  schema.Version,
		CategoryLabels: catLabels,
		Fields:         fields,
	}, nil
}

func (s *Service) mutateCategory(ctx context.Context, id ID, fn func(Category, time.Time) (Category, error)) (Category, error) {
	if s == nil || s.store == nil {
		return Category{}, errStoreRequired
	}
	current, err := s.store.GetCategory(ctx, id)
	if err != nil {
		return Category{}, mapStoreErr(err)
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return Category{}, err
	}
	if err := s.store.UpdateCategory(ctx, next); err != nil {
		return Category{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) mutateSchema(ctx context.Context, id ID, fn func(Schema, time.Time) (Schema, error)) (Schema, error) {
	if s == nil || s.store == nil {
		return Schema{}, errStoreRequired
	}
	current, err := s.store.GetSchema(ctx, id)
	if err != nil {
		return Schema{}, mapStoreErr(err)
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return Schema{}, err
	}
	if err := s.store.UpdateSchema(ctx, next); err != nil {
		return Schema{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) validateSchemaForReview(ctx context.Context, schema Schema) error {
	attrs, err := s.store.ListAttributes(ctx, schema.ID)
	if err != nil {
		return mapStoreErr(err)
	}
	for _, attr := range attrs {
		opts, err := s.store.ListOptions(ctx, attr.ID)
		if err != nil {
			return mapStoreErr(err)
		}
		switch attr.ValueType {
		case ValueTypeEnum:
			if len(opts) == 0 {
				return errEnumRequiresOptions
			}
		default:
			if len(opts) > 0 {
				return errOptionsNotAllowed
			}
		}
		if err := attr.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, errConflict) || errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidCategory) || errors.Is(err, errInvalidSchema) ||
		errors.Is(err, errInvalidAttribute) || errors.Is(err, errInvalidOption) ||
		errors.Is(err, errInvalidLabel) || errors.Is(err, errInvalidLocale) ||
		errors.Is(err, errInvalidCode) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidConstraints) ||
		errors.Is(err, errImmutablePublished) || errors.Is(err, errEnumRequiresOptions) ||
		errors.Is(err, errOptionsNotAllowed) {
		return err
	}
	return errUnavailable
}
