package policy

import (
	"sync"
	"time"
)

type InboxItem struct {
	ID          string
	UserID      string
	EventType   EventType
	TemplateKey string
	Variables   map[string]string
	ResourceRef string
	CreatedAt   time.Time
	ReadAt      *time.Time
}

type MemoryStore struct {
	mu    sync.Mutex
	prefs map[string]PreferenceDocument
	cons  map[string][]ConsentSnapshot
	inbox map[string][]InboxItem
	seq   int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		prefs: map[string]PreferenceDocument{},
		cons:  map[string][]ConsentSnapshot{},
		inbox: map[string][]InboxItem{},
	}
}

func (s *MemoryStore) GetPreferences(actorID string) (PreferenceDocument, error) {
	if actorID == "" {
		return PreferenceDocument{}, ErrInvalidActor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if doc, ok := s.prefs[actorID]; ok {
		return clonePrefs(doc), nil
	}
	return DefaultPreferences(), nil
}

func (s *MemoryStore) PatchPreferences(actorID string, patch PreferencePatch) (PreferenceDocument, error) {
	if actorID == "" {
		return PreferenceDocument{}, ErrInvalidActor
	}
	base, err := s.GetPreferences(actorID)
	if err != nil {
		return PreferenceDocument{}, err
	}
	next, err := ApplyPreferencePatch(base, patch)
	if err != nil {
		return PreferenceDocument{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefs[actorID] = clonePrefs(next)
	return clonePrefs(next), nil
}

func (s *MemoryStore) ListConsents(actorID string) ([]ConsentSnapshot, error) {
	if actorID == "" {
		return nil, ErrInvalidActor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]ConsentSnapshot(nil), s.cons[actorID]...)
	return out, nil
}

func (s *MemoryStore) RecordConsent(actorID string, d ConsentDecision, now time.Time) (ConsentSnapshot, error) {
	if actorID == "" {
		return ConsentSnapshot{}, ErrInvalidActor
	}
	snap, err := ValidateConsentDecision(d, now)
	if err != nil {
		return ConsentSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	snap.RecordedSeq = s.seq
	s.cons[actorID] = append(s.cons[actorID], snap)
	return snap, nil
}

func (s *MemoryStore) PutInbox(item InboxItem) error {
	if item.UserID == "" || item.ID == "" {
		return ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.inbox[item.UserID]
	for i, existing := range items {
		if existing.ID == item.ID {
			items[i] = item
			s.inbox[item.UserID] = items
			return nil
		}
	}
	s.inbox[item.UserID] = append(items, item)
	return nil
}

func (s *MemoryStore) ListInbox(actorID string) ([]InboxItem, error) {
	if actorID == "" {
		return nil, ErrInvalidActor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]InboxItem(nil), s.inbox[actorID]...)
	return out, nil
}

func (s *MemoryStore) MarkRead(actorID, itemID string, now time.Time) (InboxItem, error) {
	if actorID == "" {
		return InboxItem{}, ErrInvalidActor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.inbox[actorID]
	for i, it := range items {
		if it.ID == itemID {
			if it.ReadAt == nil {
				t := now.UTC()
				it.ReadAt = &t
				items[i] = it
				s.inbox[actorID] = items
			}
			return items[i], nil
		}
	}
	return InboxItem{}, ErrNotFound
}

func (s *MemoryStore) MarkAllRead(actorID string, now time.Time) error {
	if actorID == "" {
		return ErrInvalidActor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := now.UTC()
	items := s.inbox[actorID]
	for i := range items {
		if items[i].ReadAt == nil {
			items[i].ReadAt = &t
		}
	}
	s.inbox[actorID] = items
	return nil
}

func RejectClientUserID(sessionUserID, bodyUserID string) error {
	if bodyUserID != "" && bodyUserID != sessionUserID {
		return ErrInvalidActor
	}
	return nil
}
