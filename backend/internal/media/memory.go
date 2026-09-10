package media

import (
	"context"
	"sort"
	"sync"
	"time"

	"backend/internal/platform/outbox"
)

// MemoryStore is an in-process media metadata store for tests.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[ID]Asset
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Asset)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Create(ctx context.Context, asset Asset) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := asset.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[asset.ID]; ok {
		return errConflict
	}
	for _, existing := range m.byID {
		if existing.ObjectKey == asset.ObjectKey {
			return errConflict
		}
	}
	m.byID[asset.ID] = cloneAsset(asset)
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Asset, error) {
	if m == nil {
		return Asset{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Asset{}, m.fail
	}
	asset, ok := m.byID[id]
	if !ok {
		return Asset{}, errNotFound
	}
	return cloneAsset(asset), nil
}

func (m *MemoryStore) Begin(ctx context.Context) (transaction, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return memoryTx{}, nil
}

func (m *MemoryStore) UpdateInTx(ctx context.Context, _ outbox.Execer, asset Asset, expectedUpdatedAt time.Time) error {
	return m.Update(ctx, asset, expectedUpdatedAt)
}

func (m *MemoryStore) Update(ctx context.Context, asset Asset, expectedUpdatedAt time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := asset.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[asset.ID]
	if !ok {
		return errNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[asset.ID] = cloneAsset(asset)
	return nil
}

func (m *MemoryStore) Snapshot() map[ID]Asset {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[ID]Asset, len(m.byID))
	for k, v := range m.byID {
		out[k] = cloneAsset(v)
	}
	return out
}

func (m *MemoryStore) Restore(in map[ID]Asset) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID = make(map[ID]Asset, len(in))
	for k, v := range in {
		m.byID[k] = cloneAsset(v)
	}
}

func (m *MemoryStore) ListByListing(ctx context.Context, listingID ID) ([]Asset, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]Asset, 0)
	for _, asset := range m.byID {
		if asset.ListingID != nil && *asset.ListingID == listingID {
			out = append(out, cloneAsset(asset))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := out[i].SortOrder, out[j].SortOrder
		if oi == nil && oj != nil {
			return false
		}
		if oi != nil && oj == nil {
			return true
		}
		if oi != nil && oj != nil && *oi != *oj {
			return *oi < *oj
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}

var _ assetStore = (*MemoryStore)(nil)

// MemoryObjectStorage is an in-process object port for tests. It is not a provider adapter.
type storedObject struct {
	stat  ObjectStat
	bytes []byte
}

type MemoryObjectStorage struct {
	mu       sync.Mutex
	objects  map[string]storedObject
	fail     error
	deleted  []string
	MaxBytes int64
}

func NewMemoryObjectStorage() *MemoryObjectStorage {
	return &MemoryObjectStorage{objects: make(map[string]storedObject)}
}

func (m *MemoryObjectStorage) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryObjectStorage) PutUntrusted(objectKey string, stat ObjectStat) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stat.Exists = true
	m.objects[objectKey] = storedObject{stat: stat}
}

func (m *MemoryObjectStorage) PutBytes(objectKey string, data []byte, contentType string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[objectKey] = storedObject{
		stat: ObjectStat{
			Exists:      true,
			SizeBytes:   int64(len(data)),
			ContentType: contentType,
		},
		bytes: append([]byte(nil), data...),
	}
}

func (m *MemoryObjectStorage) IssueUploadTarget(ctx context.Context, objectKey string) (UploadTarget, error) {
	if m == nil {
		return UploadTarget{}, errStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return UploadTarget{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return UploadTarget{}, m.fail
	}
	max := m.MaxBytes
	if max <= 0 {
		max = 10 << 20
	}
	return UploadTarget{
		ObjectKey: objectKey,
		UploadURL: "https://objects.test/upload/" + objectKey,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		MaxBytes:  max,
	}, nil
}

func (m *MemoryObjectStorage) IssueGetTarget(ctx context.Context, objectKey string) (GetTarget, error) {
	if m == nil {
		return GetTarget{}, errStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return GetTarget{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return GetTarget{}, m.fail
	}
	if !IsProcessedObjectKey(objectKey) {
		return GetTarget{}, errInvalidObjectKey
	}
	return GetTarget{
		URL:       "https://objects.test/public/" + objectKey,
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}, nil
}

func (m *MemoryObjectStorage) Stat(ctx context.Context, objectKey string) (ObjectStat, error) {
	if m == nil {
		return ObjectStat{}, errStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return ObjectStat{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return ObjectStat{}, m.fail
	}
	obj, ok := m.objects[objectKey]
	if !ok {
		return ObjectStat{Exists: false}, nil
	}
	return obj.stat, nil
}

func (m *MemoryObjectStorage) GetObject(ctx context.Context, objectKey string) ([]byte, ObjectStat, error) {
	if m == nil {
		return nil, ObjectStat{}, errStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, ObjectStat{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, ObjectStat{}, m.fail
	}
	obj, ok := m.objects[objectKey]
	if !ok {
		return nil, ObjectStat{Exists: false}, nil
	}
	return append([]byte(nil), obj.bytes...), obj.stat, nil
}

func (m *MemoryObjectStorage) PutObject(ctx context.Context, objectKey string, data []byte, contentType string) error {
	if m == nil {
		return errStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.objects[objectKey] = storedObject{
		stat: ObjectStat{
			Exists:      true,
			SizeBytes:   int64(len(data)),
			ContentType: contentType,
		},
		bytes: append([]byte(nil), data...),
	}
	return nil
}

func (m *MemoryObjectStorage) DeleteObject(ctx context.Context, objectKey string) error {
	if m == nil {
		return errStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.deleted = append(m.deleted, objectKey)
	delete(m.objects, objectKey)
	return nil
}

func (m *MemoryObjectStorage) DeletedKeys() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.deleted))
	copy(out, m.deleted)
	return out
}

type memoryTx struct{}

func (memoryTx) Exec(context.Context, string, ...any) (int64, error) { return 1, nil }
func (memoryTx) Commit(context.Context) error                        { return nil }
func (memoryTx) Rollback(context.Context) error                      { return nil }

// MemoryEnqueuer records process events for tests. It is not a production outbox.
type MemoryEnqueuer struct {
	mu     sync.Mutex
	Events []outbox.Event
}

func (e *MemoryEnqueuer) Enqueue(ctx context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	if e == nil {
		return outbox.Event{}, errUnavailable
	}
	if err := ctx.Err(); err != nil {
		return outbox.Event{}, err
	}
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, errUnavailable
	}
	now := time.Now().UTC()
	ev := outbox.Event{
		ID:           id,
		EventType:    in.EventType,
		EventVersion: in.EventVersion,
		Payload:      append([]byte(nil), in.Payload...),
		CreatedAt:    now,
		AvailableAt:  now,
	}
	if in.AggregateType != "" {
		v := in.AggregateType
		ev.AggregateType = &v
	}
	if in.AggregateID != "" {
		v := in.AggregateID
		ev.AggregateID = &v
	}
	if in.IdempotencyKey != "" {
		v := in.IdempotencyKey
		ev.IdempotencyKey = &v
	}
	if err := ev.Validate(); err != nil {
		return outbox.Event{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Events = append(e.Events, ev)
	return ev, nil
}

func (e *MemoryEnqueuer) Last() (outbox.Event, bool) {
	if e == nil {
		return outbox.Event{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.Events) == 0 {
		return outbox.Event{}, false
	}
	return e.Events[len(e.Events)-1], true
}

var _ ObjectStorage = (*MemoryObjectStorage)(nil)
