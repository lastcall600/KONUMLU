package storage_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HugoSmits86/nativewebp"

	objstorage "backend/internal/infrastructure/storage"
	"backend/internal/media"
)

func TestLiveMinIOUploadProcessAndReject(t *testing.T) {
	if os.Getenv("LIVE_MEDIA_MINIO") != "1" {
		t.Skip("set LIVE_MEDIA_MINIO=1 against local MinIO")
	}
	ctx := context.Background()
	cfg, err := objstorage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatal("OBJECT_STORAGE_ENABLED must be true")
	}
	objects, err := objstorage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	store := media.NewMemoryStore()
	svc, err := media.NewService(store, objects, func() time.Time { return time.Now().UTC() })
	if err != nil {
		t.Fatal(err)
	}
	svc.SetMaxUploadBytes(objects.MaxUploadBytes())
	enq := &media.MemoryEnqueuer{}
	svc.SetOutbox(store, enq)

	owner := mustID(t)
	src := liveJPEG(t, 32, 20)
	t.Logf("source_bytes=%d source_format=jpeg source_dims=32x20", len(src))

	name := "proof.jpg"
	asset, target, err := svc.CreatePending(ctx, owner, &name)
	if err != nil {
		t.Fatal(err)
	}
	if target.UploadURL == "" || strings.Contains(target.UploadURL, cfg.SecretKey) {
		t.Fatal("upload target missing or leaked secret")
	}
	if err := putPresigned(ctx, target.UploadURL, src); err != nil {
		t.Fatal(err)
	}

	unsigned := strings.Split(target.UploadURL, "?")[0]
	unauth, err := http.Get(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, unauth.Body)
	unauth.Body.Close()
	if unauth.StatusCode == http.StatusOK {
		t.Fatal("quarantine object must not be anonymously readable")
	}

	uploaded, err := svc.ConfirmUpload(ctx, asset.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Status != media.StatusUploaded {
		t.Fatalf("status = %s", uploaded.Status)
	}
	if _, ok := enq.Last(); !ok {
		t.Fatal("expected outbox process event")
	}

	if err := svc.ProcessAsset(ctx, asset.ID); err != nil {
		t.Fatal(err)
	}
	asset, err = svc.Get(ctx, asset.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Status != media.StatusReady || asset.ContentType == nil || *asset.ContentType != "image/webp" {
		t.Fatalf("ready asset = %+v", asset)
	}
	processed, stat, err := objects.GetObject(ctx, asset.ProcessedObjectKey)
	if err != nil || !stat.Exists {
		t.Fatalf("processed missing: %+v %v", stat, err)
	}
	img, err := nativewebp.Decode(bytes.NewReader(processed))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("processed_bytes=%d processed_format=webp output_dims=%dx%d db_status=%s original_prefix=%s processed_key=%s",
		len(processed), img.Bounds().Dx(), img.Bounds().Dy(), asset.Status,
		media.ObjectKeyPrefix(asset.OwnerUserID, asset.ID), asset.ProcessedObjectKey)
	if !media.IsOriginalObjectKey(asset.ObjectKey) || !media.IsProcessedObjectKey(asset.ProcessedObjectKey) {
		t.Fatal("unexpected key shape")
	}

	get, err := objects.IssueGetTarget(ctx, asset.ProcessedObjectKey)
	if err != nil || get.URL == "" {
		t.Fatalf("delivery = %+v err=%v", get, err)
	}
	resp, err := http.Get(get.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		t.Fatalf("processed GET status=%d n=%d", resp.StatusCode, len(body))
	}
	if _, err := objects.IssueGetTarget(ctx, asset.ObjectKey); err == nil {
		t.Fatal("quarantine key must not issue public GET")
	}

	other := mustID(t)
	if _, err := svc.ConfirmUpload(ctx, asset.ID, other); !errors.Is(err, media.ErrForbidden) && !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("foreign finalize err = %v", err)
	}
	if _, err := svc.ConfirmUpload(ctx, asset.ID, owner); !errors.Is(err, media.ErrInvalidTransition) {
		t.Fatalf("duplicate after ready err = %v", err)
	}

	rejectLive(t, svc, owner, "spoof.jpg", []byte("this is not jpeg"))
	rejectLive(t, svc, owner, "bad.jpg", []byte{0xFF, 0xD8, 0xFF, 0x00})
}

func rejectLive(t *testing.T, svc *media.Service, owner media.ID, filename string, payload []byte) {
	t.Helper()
	ctx := context.Background()
	name := filename
	asset, target, err := svc.CreatePending(ctx, owner, &name)
	if err != nil {
		t.Fatal(err)
	}
	if err := putPresigned(ctx, target.UploadURL, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmUpload(ctx, asset.ID, owner); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessAsset(ctx, asset.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, asset.ID, owner)
	if err != nil || got.Status != media.StatusRejected {
		t.Fatalf("%s status = %+v err=%v", filename, got, err)
	}
}

func putPresigned(ctx context.Context, rawURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.New(http.StatusText(resp.StatusCode) + ": " + string(b))
	}
	return nil
}

func liveJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, G: 10, B: 10, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustID(t *testing.T) media.ID {
	t.Helper()
	id, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
