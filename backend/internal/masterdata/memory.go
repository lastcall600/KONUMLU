package masterdata

import (
	"context"
	"sort"
	"sync"
)

type labelKey struct {
	id     ID
	locale Locale
}

// MemoryStore is an in-process Master Data store for tests. PostgreSQL remains SoT.
type MemoryStore struct {
	mu              sync.Mutex
	categories      map[ID]Category
	categoryByCode  map[string]ID
	categoryLabels  map[labelKey]CategoryLabel
	schemas         map[ID]Schema
	attributes      map[ID]Attribute
	attributeLabels map[labelKey]AttributeLabel
	options         map[ID]AttributeOption
	optionLabels    map[labelKey]OptionLabel
	fail            error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		categories:      make(map[ID]Category),
		categoryByCode:  make(map[string]ID),
		categoryLabels:  make(map[labelKey]CategoryLabel),
		schemas:         make(map[ID]Schema),
		attributes:      make(map[ID]Attribute),
		attributeLabels: make(map[labelKey]AttributeLabel),
		options:         make(map[ID]AttributeOption),
		optionLabels:    make(map[labelKey]OptionLabel),
	}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) lockedErr(ctx context.Context) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.fail != nil {
		return m.fail
	}
	return nil
}

func (m *MemoryStore) CreateCategory(ctx context.Context, category Category) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := category.Validate(); err != nil {
		return err
	}
	if _, ok := m.categories[category.ID]; ok {
		return errConflict
	}
	if _, ok := m.categoryByCode[category.Code]; ok {
		return errConflict
	}
	m.categories[category.ID] = cloneCategory(category)
	m.categoryByCode[category.Code] = category.ID
	return nil
}

func (m *MemoryStore) GetCategory(ctx context.Context, id ID) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Category{}, err
	}
	c, ok := m.categories[id]
	if !ok {
		return Category{}, errNotFound
	}
	return cloneCategory(c), nil
}

func (m *MemoryStore) ListPublishedCategories(ctx context.Context) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return nil, err
	}
	out := make([]Category, 0)
	for _, c := range m.categories {
		if c.Status == StatusPublished {
			out = append(out, cloneCategory(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

func (m *MemoryStore) UpdateCategory(ctx context.Context, category Category) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := category.Validate(); err != nil {
		return err
	}
	current, ok := m.categories[category.ID]
	if !ok {
		return errNotFound
	}
	if current.Code != category.Code {
		return errInvalidCategory
	}
	m.categories[category.ID] = cloneCategory(category)
	return nil
}

func (m *MemoryStore) UpsertCategoryLabel(ctx context.Context, label CategoryLabel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := label.Validate(); err != nil {
		return err
	}
	if _, ok := m.categories[label.CategoryID]; !ok {
		return errNotFound
	}
	m.categoryLabels[labelKey{id: label.CategoryID, locale: label.Locale}] = cloneCategoryLabel(label)
	return nil
}

func (m *MemoryStore) ListCategoryLabels(ctx context.Context, categoryID ID) ([]CategoryLabel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return nil, err
	}
	out := make([]CategoryLabel, 0)
	for _, l := range m.categoryLabels {
		if l.CategoryID == categoryID {
			out = append(out, cloneCategoryLabel(l))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })
	return out, nil
}

func (m *MemoryStore) CreateSchema(ctx context.Context, schema Schema) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := schema.Validate(); err != nil {
		return err
	}
	if _, ok := m.schemas[schema.ID]; ok {
		return errConflict
	}
	for _, existing := range m.schemas {
		if existing.CategoryID == schema.CategoryID && existing.Version == schema.Version {
			return errConflict
		}
	}
	m.schemas[schema.ID] = cloneSchema(schema)
	return nil
}

func (m *MemoryStore) GetSchema(ctx context.Context, id ID) (Schema, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Schema{}, err
	}
	schema, ok := m.schemas[id]
	if !ok {
		return Schema{}, errNotFound
	}
	return cloneSchema(schema), nil
}

func (m *MemoryStore) UpdateSchema(ctx context.Context, schema Schema) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := schema.Validate(); err != nil {
		return err
	}
	current, ok := m.schemas[schema.ID]
	if !ok {
		return errNotFound
	}
	if current.Status == StatusPublished {
		return errImmutablePublished
	}
	m.schemas[schema.ID] = cloneSchema(schema)
	return nil
}

func (m *MemoryStore) MaxSchemaVersion(ctx context.Context, categoryID ID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return 0, err
	}
	max := 0
	for _, schema := range m.schemas {
		if schema.CategoryID == categoryID && schema.Version > max {
			max = schema.Version
		}
	}
	return max, nil
}

func (m *MemoryStore) GetPublishedSchema(ctx context.Context, categoryID ID) (Schema, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Schema{}, err
	}
	var best *Schema
	for _, schema := range m.schemas {
		if schema.CategoryID != categoryID || schema.Status != StatusPublished {
			continue
		}
		if best == nil || schema.Version > best.Version {
			cp := cloneSchema(schema)
			best = &cp
		}
	}
	if best == nil {
		return Schema{}, errNotFound
	}
	return *best, nil
}

func (m *MemoryStore) GetSchemaByCategoryVersion(ctx context.Context, categoryID ID, version int) (Schema, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Schema{}, err
	}
	for _, schema := range m.schemas {
		if schema.CategoryID == categoryID && schema.Version == version {
			return cloneSchema(schema), nil
		}
	}
	return Schema{}, errNotFound
}

func (m *MemoryStore) CreateAttribute(ctx context.Context, attribute Attribute) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := attribute.Validate(); err != nil {
		return err
	}
	if _, ok := m.attributes[attribute.ID]; ok {
		return errConflict
	}
	for _, existing := range m.attributes {
		if existing.SchemaID == attribute.SchemaID && existing.Code == attribute.Code {
			return errConflict
		}
	}
	m.attributes[attribute.ID] = cloneAttribute(attribute)
	return nil
}

func (m *MemoryStore) GetAttribute(ctx context.Context, id ID) (Attribute, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Attribute{}, err
	}
	a, ok := m.attributes[id]
	if !ok {
		return Attribute{}, errNotFound
	}
	return cloneAttribute(a), nil
}

func (m *MemoryStore) ListAttributes(ctx context.Context, schemaID ID) ([]Attribute, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return nil, err
	}
	out := make([]Attribute, 0)
	for _, a := range m.attributes {
		if a.SchemaID == schemaID {
			out = append(out, cloneAttribute(a))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].Code < out[j].Code
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func (m *MemoryStore) UpsertAttributeLabel(ctx context.Context, label AttributeLabel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := label.Validate(); err != nil {
		return err
	}
	if _, ok := m.attributes[label.AttributeID]; !ok {
		return errNotFound
	}
	m.attributeLabels[labelKey{id: label.AttributeID, locale: label.Locale}] = cloneAttributeLabel(label)
	return nil
}

func (m *MemoryStore) ListAttributeLabels(ctx context.Context, attributeID ID) ([]AttributeLabel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return nil, err
	}
	out := make([]AttributeLabel, 0)
	for _, l := range m.attributeLabels {
		if l.AttributeID == attributeID {
			out = append(out, cloneAttributeLabel(l))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })
	return out, nil
}

func (m *MemoryStore) CreateOption(ctx context.Context, option AttributeOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := option.Validate(); err != nil {
		return err
	}
	if _, ok := m.options[option.ID]; ok {
		return errConflict
	}
	for _, existing := range m.options {
		if existing.AttributeID == option.AttributeID && existing.Code == option.Code {
			return errConflict
		}
	}
	m.options[option.ID] = option
	return nil
}

func (m *MemoryStore) ListOptions(ctx context.Context, attributeID ID) ([]AttributeOption, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return nil, err
	}
	out := make([]AttributeOption, 0)
	for _, o := range m.options {
		if o.AttributeID == attributeID {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].Code < out[j].Code
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func (m *MemoryStore) UpsertOptionLabel(ctx context.Context, label OptionLabel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := label.Validate(); err != nil {
		return err
	}
	if _, ok := m.options[label.OptionID]; !ok {
		return errNotFound
	}
	m.optionLabels[labelKey{id: label.OptionID, locale: label.Locale}] = label
	return nil
}

func (m *MemoryStore) ListOptionLabels(ctx context.Context, optionID ID) ([]OptionLabel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return nil, err
	}
	out := make([]OptionLabel, 0)
	for _, l := range m.optionLabels {
		if l.OptionID == optionID {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })
	return out, nil
}

func cloneCategory(c Category) Category {
	c.ParentID = cloneIDPtr(c.ParentID)
	c.PublishedAt = cloneTimePtr(c.PublishedAt)
	return c
}

func cloneCategoryLabel(l CategoryLabel) CategoryLabel {
	if l.Description != nil {
		d := *l.Description
		l.Description = &d
	}
	return l
}

func cloneSchema(s Schema) Schema {
	s.PublishedAt = cloneTimePtr(s.PublishedAt)
	return s
}

func cloneAttribute(a Attribute) Attribute {
	a.Constraints = cloneConstraints(a.Constraints)
	return a
}

func cloneAttributeLabel(l AttributeLabel) AttributeLabel {
	if l.HelpText != nil {
		h := *l.HelpText
		l.HelpText = &h
	}
	return l
}

var _ store = (*MemoryStore)(nil)
