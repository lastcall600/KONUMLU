package businesses

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxServiceTitleRunes = 120

type ServiceStatus string

const (
	ServiceStatusDraft  ServiceStatus = "draft"
	ServiceStatusActive ServiceStatus = "active"
	ServiceStatusPaused ServiceStatus = "paused"
	ServiceStatusClosed ServiceStatus = "closed"
)

func (s ServiceStatus) valid() bool {
	switch s {
	case ServiceStatusDraft, ServiceStatusActive, ServiceStatusPaused, ServiceStatusClosed:
		return true
	default:
		return false
	}
}

func (s ServiceStatus) PubliclyReadable() bool {
	return s == ServiceStatusActive
}

func (s ServiceStatus) ownerEditable() bool {
	return s == ServiceStatusDraft || s == ServiceStatusActive || s == ServiceStatusPaused
}

type PriceModel string

const (
	PriceModelNone          PriceModel = ""
	PriceModelFixed         PriceModel = "fixed"
	PriceModelStartingFrom  PriceModel = "starting_from"
	PriceModelQuoteRequired PriceModel = "quote_required"
)

func (m PriceModel) valid() bool {
	switch m {
	case PriceModelNone, PriceModelFixed, PriceModelStartingFrom, PriceModelQuoteRequired:
		return true
	default:
		return false
	}
}

var (
	priceAmountPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern    = regexp.MustCompile(`^[A-Z]{3}$`)
)

// ServiceContent is owner-entered catalog text. No language-specific rewrite.
type ServiceContent struct {
	Title       string
	Description string
	Price       ServicePrice
	CategoryID  *ID
}

func (c ServiceContent) normalized() (ServiceContent, error) {
	title, err := normalizeServiceTitle(c.Title)
	if err != nil {
		return ServiceContent{}, err
	}
	if title == "" {
		return ServiceContent{}, errInvalidContent
	}
	desc, err := normalizeDescription(c.Description)
	if err != nil {
		return ServiceContent{}, err
	}
	if c.CategoryID != nil && c.CategoryID.IsZero() {
		return ServiceContent{}, errInvalidCategory
	}
	price, err := c.Price.normalized()
	if err != nil {
		return ServiceContent{}, err
	}
	return ServiceContent{Title: title, Description: desc, Price: price, CategoryID: cloneIDPtr(c.CategoryID)}, nil
}

type ServicePrice struct {
	Model    PriceModel
	Amount   *string
	Currency *string
}

func (p ServicePrice) normalized() (ServicePrice, error) {
	model := PriceModel(strings.TrimSpace(string(p.Model)))
	if !model.valid() {
		return ServicePrice{}, errInvalidPrice
	}
	amount, currency := clonePrice(p.Amount, p.Currency)
	switch model {
	case PriceModelNone, PriceModelQuoteRequired:
		if amount != nil || currency != nil {
			return ServicePrice{}, errInvalidPrice
		}
		return ServicePrice{Model: model}, nil
	case PriceModelFixed, PriceModelStartingFrom:
		if amount == nil || currency == nil {
			return ServicePrice{}, errInvalidPrice
		}
		a := strings.TrimSpace(*amount)
		cur := strings.TrimSpace(*currency)
		if a == "" || !priceAmountPattern.MatchString(a) || !currencyPattern.MatchString(cur) {
			return ServicePrice{}, errInvalidPrice
		}
		return ServicePrice{Model: model, Amount: &a, Currency: &cur}, nil
	default:
		return ServicePrice{}, errInvalidPrice
	}
}

func clonePrice(amount, currency *string) (*string, *string) {
	if amount == nil && currency == nil {
		return nil, nil
	}
	var a, c *string
	if amount != nil {
		v := strings.TrimSpace(*amount)
		if v != "" {
			a = &v
		}
	}
	if currency != nil {
		v := strings.TrimSpace(*currency)
		if v != "" {
			c = &v
		}
	}
	return a, c
}

// OfferedService is a business-owned catalog offering. Booking and payments are out of scope.
type OfferedService struct {
	ID          ID
	BusinessID  ID
	Title       string
	Description string
	Status      ServiceStatus
	Price       ServicePrice
	CategoryID  *ID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (s OfferedService) Validate() error {
	if s.ID.IsZero() || s.BusinessID.IsZero() {
		return errZeroID
	}
	if !s.Status.valid() {
		return errInvalidStatus
	}
	content := ServiceContent{Title: s.Title, Description: s.Description, Price: s.Price, CategoryID: s.CategoryID}
	normalized, err := content.normalized()
	if err != nil {
		return err
	}
	if s.Title != normalized.Title {
		return errInvalidContent
	}
	if s.Description != normalized.Description {
		return errInvalidContent
	}
	if s.Price.Model != normalized.Price.Model {
		return errInvalidPrice
	}
	if (s.Price.Amount == nil) != (normalized.Price.Amount == nil) ||
		(s.Price.Currency == nil) != (normalized.Price.Currency == nil) {
		return errInvalidPrice
	}
	if s.Price.Amount != nil && *s.Price.Amount != *normalized.Price.Amount {
		return errInvalidPrice
	}
	if s.Price.Currency != nil && *s.Price.Currency != *normalized.Price.Currency {
		return errInvalidPrice
	}
	if (s.CategoryID == nil) != (normalized.CategoryID == nil) {
		return errInvalidCategory
	}
	if s.CategoryID != nil && *s.CategoryID != *normalized.CategoryID {
		return errInvalidCategory
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return errInvalidOfferedService
	}
	return nil
}

func (s OfferedService) PubliclyReadable() bool {
	return s.Status.PubliclyReadable()
}

func NewDraftOfferedService(businessID ID, content ServiceContent, now time.Time) (OfferedService, error) {
	if businessID.IsZero() {
		return OfferedService{}, errZeroID
	}
	if now.IsZero() {
		return OfferedService{}, errInvalidOfferedService
	}
	content, err := content.normalized()
	if err != nil {
		return OfferedService{}, err
	}
	id, err := NewID()
	if err != nil {
		return OfferedService{}, errUnavailable
	}
	svc := OfferedService{
		ID:          id,
		BusinessID:  businessID,
		Title:       content.Title,
		Description: content.Description,
		Status:      ServiceStatusDraft,
		Price:       content.Price,
		CategoryID:  cloneIDPtr(content.CategoryID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := svc.Validate(); err != nil {
		return OfferedService{}, err
	}
	return svc, nil
}

func (s OfferedService) UpdateContent(content ServiceContent, now time.Time) (OfferedService, error) {
	if !s.Status.ownerEditable() {
		return OfferedService{}, errInvalidTransition
	}
	content, err := content.normalized()
	if err != nil {
		return OfferedService{}, err
	}
	if now.Before(s.CreatedAt) {
		return OfferedService{}, errInvalidOfferedService
	}
	s.Title = content.Title
	s.Description = content.Description
	s.Price = content.Price
	s.CategoryID = cloneIDPtr(content.CategoryID)
	s.UpdatedAt = now
	if err := s.Validate(); err != nil {
		return OfferedService{}, err
	}
	return s, nil
}

func (s OfferedService) Activate(now time.Time) (OfferedService, error) {
	if s.Status != ServiceStatusDraft && s.Status != ServiceStatusPaused {
		return OfferedService{}, errInvalidTransition
	}
	return s.setStatus(ServiceStatusActive, now)
}

func (s OfferedService) Pause(now time.Time) (OfferedService, error) {
	if s.Status != ServiceStatusActive {
		return OfferedService{}, errInvalidTransition
	}
	return s.setStatus(ServiceStatusPaused, now)
}

func (s OfferedService) Close(now time.Time) (OfferedService, error) {
	if s.Status != ServiceStatusDraft && s.Status != ServiceStatusActive && s.Status != ServiceStatusPaused {
		return OfferedService{}, errInvalidTransition
	}
	return s.setStatus(ServiceStatusClosed, now)
}

func (s OfferedService) setStatus(next ServiceStatus, now time.Time) (OfferedService, error) {
	if now.Before(s.CreatedAt) {
		return OfferedService{}, errInvalidOfferedService
	}
	s.Status = next
	s.UpdatedAt = now
	if err := s.Validate(); err != nil {
		return OfferedService{}, err
	}
	return s, nil
}

func parentAllowsServiceMutation(status Status) bool {
	return status.ownerEditable()
}

func normalizeServiceTitle(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errInvalidContent
	}
	if utf8.RuneCountInString(trimmed) > MaxServiceTitleRunes {
		return "", errInvalidContent
	}
	for _, r := range trimmed {
		if r == 0x7f || unicode.IsControl(r) {
			return "", errInvalidContent
		}
	}
	return trimmed, nil
}

func cloneOfferedService(s OfferedService) OfferedService {
	s.CategoryID = cloneIDPtr(s.CategoryID)
	s.Price.Amount, s.Price.Currency = clonePrice(s.Price.Amount, s.Price.Currency)
	return s
}
