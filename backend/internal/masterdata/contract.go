package masterdata

import (
	"context"
	"errors"

	"backend/internal/masterdata/contracts"
)

var _ contracts.PublishedFormResolver = (*publishedFormsAPI)(nil)
var _ contracts.PublishedCategoryLookup = (*publishedCategoryAPI)(nil)
var _ contracts.EIDSRequirementLookup = (*eidsPolicyAPI)(nil)

type publishedFormsAPI struct {
	svc *Service
}

type publishedCategoryAPI struct {
	svc *Service
}

// NewPublishedCategories exposes published category existence through the contract.
func NewPublishedCategories(svc *Service) contracts.PublishedCategoryLookup {
	return publishedCategoryAPI{svc: svc}
}

func (a publishedCategoryAPI) RequirePublished(ctx context.Context, categoryID contracts.ID) error {
	if a.svc == nil || a.svc.store == nil {
		return contracts.ErrUnavailable
	}
	if categoryID.IsZero() {
		return contracts.ErrZeroID
	}
	cat, err := a.svc.store.GetCategory(ctx, ID(categoryID))
	if err != nil {
		return mapPublishedFormErr(err)
	}
	if cat.Status != StatusPublished {
		return contracts.ErrNotFound
	}
	return nil
}

type eidsPolicyAPI struct {
	svc *Service
}

func NewEIDSPolicy(svc *Service) contracts.EIDSRequirementLookup {
	return eidsPolicyAPI{svc: svc}
}

func (a eidsPolicyAPI) Requirement(ctx context.Context, categoryID contracts.ID) (contracts.EIDSRequirement, error) {
	if a.svc == nil || a.svc.store == nil {
		return "", contracts.ErrUnavailable
	}
	if categoryID.IsZero() {
		return "", contracts.ErrZeroID
	}
	cat, err := a.svc.store.GetCategory(ctx, ID(categoryID))
	if err != nil {
		return "", mapPublishedFormErr(err)
	}
	return contracts.EIDSRequirement(cat.EIDSRequirement.normalized()), nil
}

// NewPublishedForms exposes published schema resolve through the contract.
func NewPublishedForms(svc *Service) contracts.PublishedFormResolver {
	return publishedFormsAPI{svc: svc}
}

func (a publishedFormsAPI) ResolvePublishedForm(ctx context.Context, categoryID contracts.ID, schemaVersion int64) (contracts.PublishedForm, error) {
	if a.svc == nil {
		return contracts.PublishedForm{}, contracts.ErrUnavailable
	}
	if categoryID.IsZero() {
		return contracts.PublishedForm{}, contracts.ErrZeroID
	}
	if schemaVersion <= 0 {
		return contracts.PublishedForm{}, contracts.ErrNotFound
	}
	version := int(schemaVersion)
	if int64(version) != schemaVersion {
		return contracts.PublishedForm{}, contracts.ErrNotFound
	}
	form, err := a.svc.ResolvePublishedFormAt(ctx, ID(categoryID), version)
	if err != nil {
		return contracts.PublishedForm{}, mapPublishedFormErr(err)
	}
	return toPublishedForm(form), nil
}

func toPublishedForm(form FormDefinition) contracts.PublishedForm {
	fields := make([]contracts.PublishedField, 0, len(form.Fields))
	for _, f := range form.Fields {
		codes := make([]string, 0, len(f.Options))
		for _, opt := range f.Options {
			codes = append(codes, opt.Code)
		}
		fields = append(fields, contracts.PublishedField{
			Code:            f.Code,
			ValueType:       string(f.ValueType),
			Required:        f.Required,
			Constraints:     cloneConstraints(f.Constraints),
			EnumOptionCodes: codes,
		})
	}
	return contracts.PublishedForm{
		CategoryID:    contracts.ID(form.CategoryID),
		SchemaVersion: int64(form.SchemaVersion),
		Fields:        fields,
	}
}

func mapPublishedFormErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errInvalidSchema) || errors.Is(err, errInvalidCategory) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errStoreRequired) {
		return contracts.ErrUnavailable
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}
