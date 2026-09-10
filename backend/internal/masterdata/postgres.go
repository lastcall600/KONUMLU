package masterdata

import (
	"context"
	"encoding/json"
	"errors"

	"backend/internal/platform/db"
)

var _ store = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) CreateCategory(ctx context.Context, category Category) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := category.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO master_data.categories (
			id, code, parent_id, status, eids_requirement, created_at, updated_at, published_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		category.ID, category.Code, category.ParentID, string(category.Status), string(category.EIDSRequirement.normalized()),
		category.CreatedAt, category.UpdatedAt, category.PublishedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetCategory(ctx context.Context, id ID) (Category, error) {
	if p == nil || p.db == nil {
		return Category{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, code, parent_id, status, eids_requirement, created_at, updated_at, published_at
		FROM master_data.categories WHERE id = $1`, id)
	got, err := scanCategory(row)
	if err != nil {
		return Category{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListPublishedCategories(ctx context.Context) ([]Category, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, code, parent_id, status, eids_requirement, created_at, updated_at, published_at
		FROM master_data.categories
		WHERE status = 'published'
		ORDER BY code ASC`)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Category, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpdateCategory(ctx context.Context, category Category) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := category.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE master_data.categories SET
			parent_id = $2, status = $3, eids_requirement = $4, updated_at = $5, published_at = $6
		WHERE id = $1`,
		category.ID, category.ParentID, string(category.Status), string(category.EIDSRequirement.normalized()), category.UpdatedAt, category.PublishedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) UpsertCategoryLabel(ctx context.Context, label CategoryLabel) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := label.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO master_data.category_labels (category_id, locale, label, description)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (category_id, locale) DO UPDATE SET label = EXCLUDED.label, description = EXCLUDED.description`,
		label.CategoryID, string(label.Locale), label.Label, label.Description,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) ListCategoryLabels(ctx context.Context, categoryID ID) ([]CategoryLabel, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT category_id, locale, label, description
		FROM master_data.category_labels WHERE category_id = $1 ORDER BY locale`, categoryID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]CategoryLabel, 0)
	for rows.Next() {
		l, err := scanCategoryLabel(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) CreateSchema(ctx context.Context, schema Schema) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := schema.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO master_data.category_schemas (id, category_id, version, status, created_at, published_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		schema.ID, schema.CategoryID, schema.Version, string(schema.Status), schema.CreatedAt, schema.PublishedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetSchema(ctx context.Context, id ID) (Schema, error) {
	if p == nil || p.db == nil {
		return Schema{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, category_id, version, status, created_at, published_at
		FROM master_data.category_schemas WHERE id = $1`, id)
	got, err := scanSchema(row)
	if err != nil {
		return Schema{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) UpdateSchema(ctx context.Context, schema Schema) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := schema.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE master_data.category_schemas SET status = $2, published_at = $3
		WHERE id = $1 AND status <> 'published'`,
		schema.ID, string(schema.Status), schema.PublishedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		current, getErr := p.GetSchema(ctx, schema.ID)
		if errors.Is(getErr, errNotFound) {
			return errNotFound
		}
		if getErr != nil {
			return getErr
		}
		if current.Status == StatusPublished {
			return errImmutablePublished
		}
		return errConflict
	}
	return nil
}

func (p *PostgresStore) MaxSchemaVersion(ctx context.Context, categoryID ID) (int, error) {
	if p == nil || p.db == nil {
		return 0, errUnavailable
	}
	var max *int
	if err := p.db.QueryRow(ctx, `
		SELECT MAX(version) FROM master_data.category_schemas WHERE category_id = $1`, categoryID).Scan(&max); err != nil {
		return 0, mapDBErr(err)
	}
	if max == nil {
		return 0, nil
	}
	return *max, nil
}

func (p *PostgresStore) GetPublishedSchema(ctx context.Context, categoryID ID) (Schema, error) {
	if p == nil || p.db == nil {
		return Schema{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, category_id, version, status, created_at, published_at
		FROM master_data.category_schemas
		WHERE category_id = $1 AND status = 'published'
		ORDER BY version DESC
		LIMIT 1`, categoryID)
	got, err := scanSchema(row)
	if err != nil {
		return Schema{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetSchemaByCategoryVersion(ctx context.Context, categoryID ID, version int) (Schema, error) {
	if p == nil || p.db == nil {
		return Schema{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, category_id, version, status, created_at, published_at
		FROM master_data.category_schemas
		WHERE category_id = $1 AND version = $2`, categoryID, version)
	got, err := scanSchema(row)
	if err != nil {
		return Schema{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) CreateAttribute(ctx context.Context, attribute Attribute) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := attribute.Validate(); err != nil {
		return err
	}
	raw, err := marshalConstraints(attribute.Constraints)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO master_data.attributes (
			id, schema_id, code, value_type, required, filterable, sortable, sort_order, constraints
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		attribute.ID, attribute.SchemaID, attribute.Code, string(attribute.ValueType),
		attribute.Required, attribute.Filterable, attribute.Sortable, attribute.SortOrder, raw,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetAttribute(ctx context.Context, id ID) (Attribute, error) {
	if p == nil || p.db == nil {
		return Attribute{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, schema_id, code, value_type, required, filterable, sortable, sort_order, constraints
		FROM master_data.attributes WHERE id = $1`, id)
	got, err := scanAttribute(row)
	if err != nil {
		return Attribute{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListAttributes(ctx context.Context, schemaID ID) ([]Attribute, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, schema_id, code, value_type, required, filterable, sortable, sort_order, constraints
		FROM master_data.attributes WHERE schema_id = $1
		ORDER BY sort_order ASC, code ASC`, schemaID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Attribute, 0)
	for rows.Next() {
		a, err := scanAttribute(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpsertAttributeLabel(ctx context.Context, label AttributeLabel) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := label.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO master_data.attribute_labels (attribute_id, locale, label, help_text)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (attribute_id, locale) DO UPDATE SET label = EXCLUDED.label, help_text = EXCLUDED.help_text`,
		label.AttributeID, string(label.Locale), label.Label, label.HelpText,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) ListAttributeLabels(ctx context.Context, attributeID ID) ([]AttributeLabel, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT attribute_id, locale, label, help_text
		FROM master_data.attribute_labels WHERE attribute_id = $1 ORDER BY locale`, attributeID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]AttributeLabel, 0)
	for rows.Next() {
		l, err := scanAttributeLabel(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) CreateOption(ctx context.Context, option AttributeOption) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := option.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO master_data.attribute_options (id, attribute_id, code, sort_order)
		VALUES ($1,$2,$3,$4)`,
		option.ID, option.AttributeID, option.Code, option.SortOrder,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) ListOptions(ctx context.Context, attributeID ID) ([]AttributeOption, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, attribute_id, code, sort_order
		FROM master_data.attribute_options WHERE attribute_id = $1
		ORDER BY sort_order ASC, code ASC`, attributeID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]AttributeOption, 0)
	for rows.Next() {
		o, err := scanOption(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpsertOptionLabel(ctx context.Context, label OptionLabel) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := label.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO master_data.attribute_option_labels (option_id, locale, label)
		VALUES ($1,$2,$3)
		ON CONFLICT (option_id, locale) DO UPDATE SET label = EXCLUDED.label`,
		label.OptionID, string(label.Locale), label.Label,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) ListOptionLabels(ctx context.Context, optionID ID) ([]OptionLabel, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT option_id, locale, label
		FROM master_data.attribute_option_labels WHERE option_id = $1 ORDER BY locale`, optionID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]OptionLabel, 0)
	for rows.Next() {
		l, err := scanOptionLabel(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCategory(row scanner) (Category, error) {
	var c Category
	var status, eidsReq string
	if err := row.Scan(&c.ID, &c.Code, &c.ParentID, &status, &eidsReq, &c.CreatedAt, &c.UpdatedAt, &c.PublishedAt); err != nil {
		return Category{}, err
	}
	c.Status = Status(status)
	c.EIDSRequirement = EIDSRequirement(eidsReq).normalized()
	return c, nil
}

func scanCategoryLabel(row scanner) (CategoryLabel, error) {
	var l CategoryLabel
	var locale string
	if err := row.Scan(&l.CategoryID, &locale, &l.Label, &l.Description); err != nil {
		return CategoryLabel{}, err
	}
	l.Locale = Locale(locale)
	return l, nil
}

func scanSchema(row scanner) (Schema, error) {
	var s Schema
	var status string
	if err := row.Scan(&s.ID, &s.CategoryID, &s.Version, &status, &s.CreatedAt, &s.PublishedAt); err != nil {
		return Schema{}, err
	}
	s.Status = Status(status)
	return s, nil
}

func scanAttribute(row scanner) (Attribute, error) {
	var a Attribute
	var valueType string
	var raw []byte
	if err := row.Scan(&a.ID, &a.SchemaID, &a.Code, &valueType, &a.Required, &a.Filterable, &a.Sortable, &a.SortOrder, &raw); err != nil {
		return Attribute{}, err
	}
	a.ValueType = ValueType(valueType)
	c, err := unmarshalConstraints(raw)
	if err != nil {
		return Attribute{}, err
	}
	a.Constraints = c
	return a, nil
}

func scanAttributeLabel(row scanner) (AttributeLabel, error) {
	var l AttributeLabel
	var locale string
	if err := row.Scan(&l.AttributeID, &locale, &l.Label, &l.HelpText); err != nil {
		return AttributeLabel{}, err
	}
	l.Locale = Locale(locale)
	return l, nil
}

func scanOption(row scanner) (AttributeOption, error) {
	var o AttributeOption
	if err := row.Scan(&o.ID, &o.AttributeID, &o.Code, &o.SortOrder); err != nil {
		return AttributeOption{}, err
	}
	return o, nil
}

func scanOptionLabel(row scanner) (OptionLabel, error) {
	var l OptionLabel
	var locale string
	if err := row.Scan(&l.OptionID, &locale, &l.Label); err != nil {
		return OptionLabel{}, err
	}
	l.Locale = Locale(locale)
	return l, nil
}

func marshalConstraints(c Constraints) ([]byte, error) {
	if c == nil {
		c = Constraints{}
	}
	if err := ValidateConstraints(c); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, errInvalidConstraints
	}
	return b, nil
}

func unmarshalConstraints(raw []byte) (Constraints, error) {
	if len(raw) == 0 {
		return Constraints{}, nil
	}
	var c Constraints
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, errInvalidConstraints
	}
	if c == nil {
		c = Constraints{}
	}
	if err := ValidateConstraints(c); err != nil {
		return nil, err
	}
	return c, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, errImmutablePublished) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
