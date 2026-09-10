package masterdata

import "context"

type store interface {
	CreateCategory(ctx context.Context, category Category) error
	GetCategory(ctx context.Context, id ID) (Category, error)
	UpdateCategory(ctx context.Context, category Category) error
	ListPublishedCategories(ctx context.Context) ([]Category, error)
	UpsertCategoryLabel(ctx context.Context, label CategoryLabel) error
	ListCategoryLabels(ctx context.Context, categoryID ID) ([]CategoryLabel, error)

	CreateSchema(ctx context.Context, schema Schema) error
	GetSchema(ctx context.Context, id ID) (Schema, error)
	UpdateSchema(ctx context.Context, schema Schema) error
	MaxSchemaVersion(ctx context.Context, categoryID ID) (int, error)
	GetPublishedSchema(ctx context.Context, categoryID ID) (Schema, error)
	GetSchemaByCategoryVersion(ctx context.Context, categoryID ID, version int) (Schema, error)

	CreateAttribute(ctx context.Context, attribute Attribute) error
	GetAttribute(ctx context.Context, id ID) (Attribute, error)
	ListAttributes(ctx context.Context, schemaID ID) ([]Attribute, error)
	UpsertAttributeLabel(ctx context.Context, label AttributeLabel) error
	ListAttributeLabels(ctx context.Context, attributeID ID) ([]AttributeLabel, error)

	CreateOption(ctx context.Context, option AttributeOption) error
	ListOptions(ctx context.Context, attributeID ID) ([]AttributeOption, error)
	UpsertOptionLabel(ctx context.Context, label OptionLabel) error
	ListOptionLabels(ctx context.Context, optionID ID) ([]OptionLabel, error)
}
