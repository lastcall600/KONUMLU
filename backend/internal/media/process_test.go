package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

func TestConfirmUploadEnqueuesProcessEventWithoutSecrets(t *testing.T) {
	svc, _, objects, clock, enq := mustProcessService(t)
	owner := mustID(t)
	asset, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	objects.PutUntrusted(asset.ObjectKey, ObjectStat{SizeBytes: 40, ContentType: "image/png"})
	clock.now = clock.now.Add(time.Second)
	uploaded, err := svc.ConfirmUpload(context.Background(), asset.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Status != StatusUploaded {
		t.Fatalf("status = %s", uploaded.Status)
	}
	ev, ok := enq.Last()
	if !ok {
		t.Fatal("expected process event")
	}
	if ev.EventType != ProcessEventType || ev.EventVersion != ProcessEventVersion {
		t.Fatalf("event = %s v%d", ev.EventType, ev.EventVersion)
	}
	if ev.IdempotencyKey == nil || *ev.IdempotencyKey != processIdempotencyPref+asset.ID.String() {
		t.Fatalf("idempotency = %v", ev.IdempotencyKey)
	}
	raw := string(ev.Payload)
	if strings.Contains(raw, "http") || strings.Contains(strings.ToLower(raw), "secret") ||
		strings.Contains(raw, asset.ObjectKey) {
		t.Fatalf("payload leaked storage details: %s", raw)
	}
	var p ProcessPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.AssetID != asset.ID.String() {
		t.Fatalf("payload = %+v", p)
	}
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessUploadedToReady(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	owner := mustID(t)
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, jpegWithExif(t, 12, 8), "evil.exe", "application/octet-stream")
	if err := svc.ProcessAsset(context.Background(), asset.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusReady || got.ContentType == nil || *got.ContentType != "image/jpeg" {
		t.Fatalf("got = %+v", got)
	}
	if got.Width == nil || *got.Width != 12 || got.Height == nil || *got.Height != 8 {
		t.Fatalf("dims = %v x %v", got.Width, got.Height)
	}
	if got.ProcessedObjectKey == "" || got.ProcessedObjectKey == got.ObjectKey {
		t.Fatalf("processed key = %q original = %q", got.ProcessedObjectKey, got.ObjectKey)
	}
	if !IsProcessedObjectKey(got.ProcessedObjectKey) {
		t.Fatalf("processed key shape = %q", got.ProcessedObjectKey)
	}
	data, stat, err := objects.GetObject(context.Background(), got.ProcessedObjectKey)
	if err != nil || !stat.Exists || len(data) == 0 {
		t.Fatalf("processed object missing: stat=%+v err=%v", stat, err)
	}
	if bytes.Contains(data, []byte("Exif")) || bytes.Contains(data, []byte("GPS")) {
		t.Fatal("re-encoded image still contains EXIF/GPS")
	}
	if got.OriginalFilename != nil && *got.OriginalFilename == "evil.exe" && *got.ContentType == "application/octet-stream" {
		t.Fatal("trusted type must not remain client claim")
	}
}

func TestProcessRejectsUnsupportedAndInvalid(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	owner := mustID(t)

	webp := mustUploadedWithBytes(t, svc, objects, clock, owner, []byte("RIFF....WEBPXXXX"), "a.webp", "image/webp")
	if err := svc.ProcessAsset(context.Background(), webp.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(context.Background(), webp.ID)
	if got.Status != StatusRejected {
		t.Fatalf("webp status = %s", got.Status)
	}

	bad := mustUploadedWithBytes(t, svc, objects, clock, owner, []byte{0xFF, 0xD8, 0xFF, 0x00}, "x.jpg", "image/jpeg")
	if err := svc.ProcessAsset(context.Background(), bad.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(context.Background(), bad.ID)
	if got.Status != StatusRejected {
		t.Fatalf("decode failure status = %s", got.Status)
	}
}

func TestProcessRejectsOversized(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	svc.SetProcessingPolicy(ProcessingPolicy{MaxBytes: 20})
	owner := mustID(t)
	data := testJPEG(t, 24, 24)
	if len(data) <= 20 {
		t.Fatalf("fixture too small: %d", len(data))
	}
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, data, "big.jpg", "image/jpeg")
	if err := svc.ProcessAsset(context.Background(), asset.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(context.Background(), asset.ID)
	if got.Status != StatusRejected {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestProcessScannerRequiredUnavailableRetries(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	svc.SetProcessingPolicy(ProcessingPolicy{RequireMalwareScan: true})
	owner := mustID(t)
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, testJPEG(t, 8, 8), "a.jpg", "image/jpeg")
	err := svc.ProcessAsset(context.Background(), asset.ID)
	if !errors.Is(err, errScannerUnavailable) {
		t.Fatalf("err = %v", err)
	}
	got, _ := store.Get(context.Background(), asset.ID)
	if got.Status != StatusProcessing {
		t.Fatalf("must stay processing: %s", got.Status)
	}

	svc.SetProcessingPorts(&stubScanner{verdict: ScanUnavailable}, nil)
	err = svc.ProcessAsset(context.Background(), asset.ID)
	if !errors.Is(err, errScannerUnavailable) {
		t.Fatalf("unavailable scanner err = %v", err)
	}
}

func TestProcessScannerRejects(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	svc.SetProcessingPolicy(ProcessingPolicy{RequireMalwareScan: true})
	svc.SetProcessingPorts(&stubScanner{verdict: ScanRejected}, nil)
	owner := mustID(t)
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, testJPEG(t, 8, 8), "a.jpg", "image/jpeg")
	if err := svc.ProcessAsset(context.Background(), asset.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(context.Background(), asset.ID)
	if got.Status != StatusRejected {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestProcessHandlerIdempotentReplayAndReadySkip(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	h, err := NewProcessHandler(svc)
	if err != nil {
		t.Fatal(err)
	}
	owner := mustID(t)
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, testJPEG(t, 8, 8), "a.jpg", "image/jpeg")
	event, err := encodeProcessEvent(asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	obEvent := outbox.Event{
		EventType:    event.EventType,
		EventVersion: event.EventVersion,
		Payload:      event.Payload,
	}
	if err := h.Handle(context.Background(), obEvent); err != nil {
		t.Fatal(err)
	}
	ready, _ := store.Get(context.Background(), asset.ID)
	if ready.Status != StatusReady {
		t.Fatalf("status = %s", ready.Status)
	}
	if err := h.Handle(context.Background(), obEvent); err != nil {
		t.Fatal(err)
	}
	again, _ := store.Get(context.Background(), asset.ID)
	if again.Status != StatusReady || again.UpdatedAt != ready.UpdatedAt {
		t.Fatalf("ready must not reprocess: %+v vs %+v", again, ready)
	}

	rejected := mustUploadedWithBytes(t, svc, objects, clock, owner, []byte("not-an-image"), "x.bin", "application/octet-stream")
	if err := svc.ProcessAsset(context.Background(), rejected.ID); err != nil {
		t.Fatal(err)
	}
	rejEvent, err := encodeProcessEvent(rejected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), outbox.Event{EventType: rejEvent.EventType, EventVersion: rejEvent.EventVersion, Payload: rejEvent.Payload}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessStorageFailureRetryable(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	owner := mustID(t)
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, testJPEG(t, 8, 8), "a.jpg", "image/jpeg")
	objects.SetFail(db.ErrUnavailable)
	err := svc.ProcessAsset(context.Background(), asset.ID)
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	got, _ := store.Get(context.Background(), asset.ID)
	if got.Status == StatusReady {
		t.Fatal("must not mark ready on storage failure")
	}
}

func TestProcessPNGReady(t *testing.T) {
	svc, store, objects, clock, _ := mustProcessService(t)
	owner := mustID(t)
	asset := mustUploadedWithBytes(t, svc, objects, clock, owner, testPNG(t, 4, 5), "a.png", "text/plain")
	if err := svc.ProcessAsset(context.Background(), asset.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(context.Background(), asset.ID)
	if got.Status != StatusReady || got.ContentType == nil || *got.ContentType != "image/png" {
		t.Fatalf("got = %+v", got)
	}
	if got.Width == nil || *got.Width != 4 || *got.Height != 5 {
		t.Fatalf("dims = %v x %v", got.Width, got.Height)
	}
}

func mustProcessService(t *testing.T) (*Service, *MemoryStore, *MemoryObjectStorage, *frozenNow, *MemoryEnqueuer) {
	t.Helper()
	store := NewMemoryStore()
	objects := NewMemoryObjectStorage()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, objects, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	enq := &MemoryEnqueuer{}
	svc.SetOutbox(store, enq)
	return svc, store, objects, clock, enq
}

func mustUploadedWithBytes(t *testing.T, svc *Service, objects *MemoryObjectStorage, clock *frozenNow, owner ID, data []byte, filename, clientType string) Asset {
	t.Helper()
	name := filename
	pending, _, err := svc.CreatePending(context.Background(), owner, &name)
	if err != nil {
		t.Fatal(err)
	}
	objects.PutBytes(pending.ObjectKey, data, clientType)
	clock.now = clock.now.Add(time.Second)
	uploaded, err := svc.MarkUploaded(context.Background(), pending.ID, owner, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return uploaded
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegWithExif(t *testing.T, w, h int) []byte {
	t.Helper()
	raw := testJPEG(t, w, h)
	payload := []byte{'E', 'x', 'i', 'f', 0, 0, 'G', 'P', 'S', 1, 2, 3, 4}
	n := len(payload) + 2
	header := []byte{0xFF, 0xE1, byte(n >> 8), byte(n)}
	out := make([]byte, 0, 2+len(header)+len(payload)+len(raw))
	out = append(out, raw[:2]...)
	out = append(out, header...)
	out = append(out, payload...)
	out = append(out, raw[2:]...)
	return out
}

type stubScanner struct {
	verdict ScanVerdict
	err     error
}

func (s *stubScanner) Scan(context.Context, string, []byte) (ScanVerdict, error) {
	if s.err != nil {
		return ScanUnavailable, s.err
	}
	return s.verdict, nil
}
