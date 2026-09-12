package httpapi

import (
	"bytes"
	"context"
	"fmt"
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
	"backend/internal/listings/contracts"
	"backend/internal/media"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

func TestLiveOutboxWorkerMinIOProcess(t *testing.T) {
	if os.Getenv("LIVE_MEDIA_WORKER") != "1" {
		t.Skip("set LIVE_MEDIA_WORKER=1 against local PostgreSQL, MinIO, and cmd/worker")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ready(ctx); err != nil {
		t.Fatal(err)
	}

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

	store := media.NewPostgresStore(pool)
	svc, err := media.NewService(store, objects, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetMaxUploadBytes(objects.MaxUploadBytes())
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         10,
		Lease:             15 * time.Second,
		BackoffBase:       time.Second,
		BackoffMultiplier: 2,
		BackoffCap:        10 * time.Second,
		Jitter:            0,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetOutbox(store, ob)

	owner := mustMediaID(t)
	listingID := mustMediaID(t)
	sessions := &fakeSessions{userID: owner}
	h, err := New(sessions, svc, &fakeOwnership{owner: asListingUser(owner)}, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}

	src := liveJPEG(t, 48, 32)
	t.Logf("source_bytes=%d source_format=jpeg source_dims=48x32", len(src))

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"originalFilename": "owner.jpg",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created createResponse
	decode(t, rec, &created)
	if created.AssetID == "" || created.UploadURL == "" {
		t.Fatalf("authorize missing: %+v", created)
	}
	body := rec.Body.String()
	if strings.Contains(strings.ToLower(body), "x-amz-signature") || strings.Contains(body, cfg.SecretKey) || strings.Contains(body, cfg.AccessKey) {
		t.Fatal("authorize JSON must not include signature or storage keys")
	}
	if err := putPresigned(ctx, created.UploadURL, src); err != nil {
		t.Fatal(err)
	}

	assetID, err := media.ParseID(created.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Get(ctx, assetID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachListing(ctx, pending.ID, media.ListingBind{ListingID: listingID, ActorUserID: owner}, pending.UpdatedAt); err != nil {
		t.Fatal(err)
	}

	rec = do(t, h, http.MethodPost, "/v1/media/listing-images/"+created.AssetID+"/confirm", allowedOrigin, map[string]any{
		"objectKey":   "client-injected",
		"contentType": "image/png",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", rec.Code, rec.Body.String())
	}
	var confirmed confirmResponse
	decode(t, rec, &confirmed)
	if confirmed.Status != string(media.StatusUploaded) {
		t.Fatalf("confirm = %+v", confirmed)
	}

	eventID, eventType, attempts, completed, err := lookupProcessEvent(ctx, pool, assetID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("outbox_event_id=%s event_type=%s attempts=%d completed=%v asset_id=%s confirm_status=%s",
		eventID, eventType, attempts, completed, assetID, confirmed.Status)

	ready := waitStatus(t, svc, assetID, owner, media.StatusReady, 25*time.Second)
	eventID, eventType, attempts, completed, err = lookupProcessEvent(ctx, pool, assetID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worker_claim attempts=%d completed=%v event_id=%s type=%s db_status=%s", attempts, completed, eventID, eventType, ready.Status)
	if attempts < 1 || !completed {
		t.Fatalf("real worker must claim and complete the outbox event attempts=%d completed=%v", attempts, completed)
	}
	if ready.ProcessedObjectKey == "" || !media.IsProcessedObjectKey(ready.ProcessedObjectKey) {
		t.Fatalf("processed key missing: %+v", ready)
	}
	if ready.ContentType == nil || *ready.ContentType != "image/webp" {
		t.Fatalf("content type = %v", ready.ContentType)
	}
	processed, stat, err := objects.GetObject(ctx, ready.ProcessedObjectKey)
	if err != nil || !stat.Exists {
		t.Fatalf("processed object missing: %+v %v", stat, err)
	}
	img, err := nativewebp.Decode(bytes.NewReader(processed))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("processed_object_key=%s processed_bytes=%d processed_format=webp output_dims=%dx%d",
		ready.ProcessedObjectKey, len(processed), img.Bounds().Dx(), img.Bounds().Dy())
	if _, err := objects.IssueGetTarget(ctx, ready.ObjectKey); err == nil {
		t.Fatal("quarantine original must not be approved for public GET")
	}
	public, err := svc.ListPublicListingMedia(ctx, listingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].AssetID != assetID {
		t.Fatalf("public media = %+v", public)
	}

	firstKey := ready.ProcessedObjectKey
	if err := releaseOutbox(ctx, pool, eventID); err != nil {
		t.Fatal(err)
	}
	retryOK := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, _, retryAttempts, retryDone, lookupErr := lookupProcessEvent(ctx, pool, assetID)
		again, getErr := svc.Get(ctx, assetID, owner)
		if lookupErr == nil && getErr == nil && retryDone && retryAttempts >= 2 && again.Status == media.StatusReady && again.ProcessedObjectKey == firstKey {
			t.Logf("retry_idempotent attempts=%d status=%s processed_key=%s", retryAttempts, again.Status, again.ProcessedObjectKey)
			retryOK = true
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if !retryOK {
		t.Fatal("retry/idempotency not proven")
	}

	rec = do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"originalFilename": "bad.jpg",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid authorize status=%d body=%s", rec.Code, rec.Body.String())
	}
	var badCreated createResponse
	decode(t, rec, &badCreated)
	if err := putPresigned(ctx, badCreated.UploadURL, []byte("not-an-image")); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodPost, "/v1/media/listing-images/"+badCreated.AssetID+"/confirm", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid confirm status=%d body=%s", rec.Code, rec.Body.String())
	}
	badID, err := media.ParseID(badCreated.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	rejected := waitStatus(t, svc, badID, owner, media.StatusRejected, 25*time.Second)
	if rejected.ProcessedObjectKey != "" {
		t.Fatal("rejected asset must not gain a processed key")
	}
	t.Logf("invalid_asset_id=%s invalid_status=%s", badID, rejected.Status)
}

func waitStatus(t *testing.T, svc *media.Service, id, owner media.ID, want media.Status, d time.Duration) media.Asset {
	t.Helper()
	deadline := time.Now().Add(d)
	var last media.Asset
	for time.Now().Before(deadline) {
		got, err := svc.Get(context.Background(), id, owner)
		if err == nil {
			last = got
			if got.Status == want {
				return got
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s last_status=%s", want, last.Status)
	return media.Asset{}
}

func lookupProcessEvent(ctx context.Context, pool *db.Pool, assetID media.ID) (id, typ string, attempts int, completed bool, err error) {
	row := pool.QueryRow(ctx, `
		SELECT id::text, event_type, attempts, completed_at IS NOT NULL
		FROM platform.outbox_events
		WHERE idempotency_key = $1
		ORDER BY created_at DESC
		LIMIT 1`, "media.image.process:"+assetID.String())
	err = row.Scan(&id, &typ, &attempts, &completed)
	return
}

func releaseOutbox(ctx context.Context, pool *db.Pool, eventID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE platform.outbox_events
		SET completed_at = NULL,
		    claimed_at = NULL,
		    claim_until = NULL,
		    available_at = NOW()
		WHERE id = $1::uuid`, eventID)
	return err
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
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("presigned put status %d", resp.StatusCode)
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

func mustMediaID(t *testing.T) media.ID {
	t.Helper()
	id, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func asListingUser(id media.ID) contracts.ID {
	var out contracts.ID
	copy(out[:], id[:])
	return out
}
