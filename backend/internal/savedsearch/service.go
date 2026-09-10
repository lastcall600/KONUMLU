package savedsearch

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	store savedSearchStore
	now   func() time.Time
}

func NewService(store savedSearchStore, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, now: now}, nil
}

type CreateInput struct {
	Name    string
	Filters Filters
}

func (s *Service) Create(ctx context.Context, userID ID, in CreateInput) (SavedSearch, error) {
	if s == nil || s.store == nil {
		return SavedSearch{}, errStoreRequired
	}
	if userID.IsZero() {
		return errZeroIDWrap()
	}
	id, err := NewID()
	if err != nil {
		return SavedSearch{}, errUnavailable
	}
	now := s.now().UTC()
	row := SavedSearch{
		ID:        id,
		UserID:    userID,
		Name:      strings.TrimSpace(in.Name),
		Filters:   in.Filters,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if utf8.RuneCountInString(row.Name) > maxNameLen {
		return SavedSearch{}, errInvalid
	}
	if err := row.Validate(); err != nil {
		return SavedSearch{}, err
	}
	if err := s.store.Insert(ctx, row); err != nil {
		return SavedSearch{}, mapStoreErr(err)
	}
	return row, nil
}

func errZeroIDWrap() (SavedSearch, error) {
	return SavedSearch{}, errZeroID
}

func (s *Service) Get(ctx context.Context, userID, id ID) (SavedSearch, error) {
	if s == nil || s.store == nil {
		return SavedSearch{}, errStoreRequired
	}
	if userID.IsZero() || id.IsZero() {
		return SavedSearch{}, errZeroID
	}
	row, err := s.store.GetByUser(ctx, userID, id)
	if err != nil {
		return SavedSearch{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) List(ctx context.Context, userID ID) ([]SavedSearch, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) Delete(ctx context.Context, userID, id ID) error {
	if s == nil || s.store == nil {
		return errStoreRequired
	}
	if userID.IsZero() || id.IsZero() {
		return errZeroID
	}
	if err := s.store.DeleteByUser(ctx, userID, id); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errZeroID) || errors.Is(err, errInvalid) ||
		errors.Is(err, errStoreRequired) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
