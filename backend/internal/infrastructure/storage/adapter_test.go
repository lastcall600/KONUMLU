package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"backend/internal/media"
)

func TestIssueUploadTargetSignsExactKeyAndBucket(t *testing.T) {
	a, err := New(validCfg())
	if err != nil {
		t.Fatal(err)
	}
	key := mustListingKey(t)
	got, err := a.IssueUploadTarget(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.ObjectKey != key {
		t.Fatalf("object key = %q", got.ObjectKey)
	}
	if got.MaxBytes != defaultMaxUploadBytes {
		t.Fatalf("max bytes = %d", got.MaxBytes)
	}
	if got.ExpiresAt.IsZero() || got.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("expiry = %s", got.ExpiresAt)
	}
	u, err := url.Parse(got.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "127.0.0.1:9000" {
		t.Fatalf("host = %q", u.Host)
	}
	if !strings.Contains(u.Path, "/konumlu-media/") {
		t.Fatalf("path-style bucket missing: %q", u.Path)
	}
	if !strings.Contains(u.Path, key) {
		t.Fatalf("key missing from path: %q", u.Path)
	}
	if strings.Contains(got.UploadURL, validCfg().SecretKey) {
		t.Fatal("secret leaked into upload URL")
	}
	q := u.Query()
	if q.Get("X-Amz-Expires") == "" || q.Get("X-Amz-Signature") == "" {
		t.Fatalf("not a signed query: %v", q)
	}
	for k, v := range got.RequiredHeaders {
		if strings.EqualFold(k, "Content-Type") {
			t.Fatalf("must not require client MIME as truth: %s=%s", k, v)
		}
	}
}

func TestCustomEndpointPathStyleVsVirtualHost(t *testing.T) {
	cfg := validCfg()
	pathAdapter, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !pathAdapter.usePathStyle || pathAdapter.endpoint != cfg.Endpoint {
		t.Fatalf("path adapter endpoint=%q pathStyle=%v", pathAdapter.endpoint, pathAdapter.usePathStyle)
	}
	key := mustListingKey(t)
	pathTarget, err := pathAdapter.IssueUploadTarget(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	pathURL, err := url.Parse(pathTarget.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	if pathURL.Host != "127.0.0.1:9000" || !strings.Contains(pathURL.Path, "/konumlu-media/") {
		t.Fatalf("path-style URL = %q", pathTarget.UploadURL)
	}

	cfg.Endpoint = "http://minio.local:9000"
	cfg.UsePathStyle = false
	vh, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if vh.usePathStyle || vh.endpoint != cfg.Endpoint {
		t.Fatalf("virtual-host adapter endpoint=%q pathStyle=%v", vh.endpoint, vh.usePathStyle)
	}
	got, err := vh.IssueUploadTarget(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u.Host, "konumlu-media") || !strings.Contains(u.Host, "minio.local") {
		t.Fatalf("virtual-host bucket missing: %q", u.Host)
	}
	if strings.HasPrefix(u.Path, "/konumlu-media/") {
		t.Fatalf("path-style path leaked into virtual-host URL: %q", u.Path)
	}
}

func TestIssueUploadTargetRejectsUnsafeKey(t *testing.T) {
	a, err := New(validCfg())
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.IssueUploadTarget(context.Background(), "media/listing-images/../secret")
	if !errors.Is(err, media.ErrInvalidObjectKey) {
		t.Fatalf("err = %v", err)
	}
	_, err = a.IssueUploadTarget(context.Background(), "uploads/photo.jpg")
	if !errors.Is(err, media.ErrInvalidObjectKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestIssueUploadTargetPresignFailureUnavailableNoSecret(t *testing.T) {
	secret := "local-secret"
	a := &Adapter{
		bucket:         "konumlu-media",
		endpoint:       "http://127.0.0.1:9000",
		usePathStyle:   true,
		uploadTTL:      time.Minute,
		maxUploadBytes: 10,
		presign:        &fakePresign{err: fmt.Errorf("AccessDenied secret=%s akid=local-access", secret)},
		now:            func() time.Time { return time.Unix(0, 0).UTC() },
	}
	key := mustListingKey(t)
	_, err := a.IssueUploadTarget(context.Background(), key)
	if !errors.Is(err, media.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("leaked provider detail: %v", err)
	}
}

func TestStatMapsHeadObject(t *testing.T) {
	ct := "image/jpeg"
	size := int64(2048)
	api := &fakeAPI{
		headOut: &s3.HeadObjectOutput{
			ContentLength: &size,
			ContentType:   &ct,
		},
	}
	a := &Adapter{bucket: "konumlu-media", api: api}
	key := mustListingKey(t)
	got, err := a.Stat(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || got.SizeBytes != size || got.ContentType != ct {
		t.Fatalf("stat = %+v", got)
	}
	if api.headIn == nil || aws.ToString(api.headIn.Bucket) != "konumlu-media" || aws.ToString(api.headIn.Key) != key {
		t.Fatalf("head input = %+v", api.headIn)
	}
}

func TestStatMissingIsNotError(t *testing.T) {
	a := &Adapter{
		bucket: "konumlu-media",
		api:    &fakeAPI{headErr: &types.NotFound{Message: aws.String("missing")}},
	}
	got, err := a.Stat(context.Background(), mustListingKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Exists {
		t.Fatal("missing object must not exist")
	}
}

func TestStatProviderFailureUnavailable(t *testing.T) {
	a := &Adapter{
		bucket: "konumlu-media",
		api:    &fakeAPI{headErr: errors.New("dial tcp 127.0.0.1:9000: connect: connection refused secret=local-secret")},
	}
	_, err := a.Stat(context.Background(), mustListingKey(t))
	if !errors.Is(err, media.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "local-secret") || strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("leaked: %v", err)
	}
}

func TestDeleteMapsBucketAndKey(t *testing.T) {
	api := &fakeAPI{}
	a := &Adapter{bucket: "konumlu-media", api: api}
	key := mustListingKey(t)
	if err := a.DeleteObject(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if api.delIn == nil || aws.ToString(api.delIn.Bucket) != "konumlu-media" || aws.ToString(api.delIn.Key) != key {
		t.Fatalf("delete input = %+v", api.delIn)
	}
}

func TestDeleteMissingIsOK(t *testing.T) {
	a := &Adapter{
		bucket: "konumlu-media",
		api:    &fakeAPI{delErr: &types.NoSuchKey{Message: aws.String("gone")}},
	}
	if err := a.DeleteObject(context.Background(), mustListingKey(t)); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteProviderFailureUnavailable(t *testing.T) {
	a := &Adapter{
		bucket: "konumlu-media",
		api:    &fakeAPI{delErr: errors.New("403 Forbidden SignatureDoesNotMatch secret=local-secret")},
	}
	err := a.DeleteObject(context.Background(), mustListingKey(t))
	if !errors.Is(err, media.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "local-secret") || strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("leaked: %v", err)
	}
}

func TestMapStorageErrorDropsSecrets(t *testing.T) {
	secret := "local-secret"
	err := mapStorageError(fmt.Errorf("provider boom %s", secret))
	if !errors.Is(err, media.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if errorContainsSecret(err, secret) {
		t.Fatalf("secret leaked: %v", err)
	}
}

func mustProcessedKey(t *testing.T) string {
	t.Helper()
	owner, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	asset, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return media.ProcessedObjectKeyFor(owner, asset, strings.Repeat("cd", 16))
}

func TestIssueGetTargetSignsProcessedKeyOnly(t *testing.T) {
	a, err := New(validCfg())
	if err != nil {
		t.Fatal(err)
	}
	key := mustProcessedKey(t)
	got, err := a.IssueGetTarget(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL == "" || got.ExpiresAt.IsZero() {
		t.Fatalf("target = %+v", got)
	}
	if !strings.Contains(got.URL, key) {
		t.Fatalf("processed key missing from url: %q", got.URL)
	}
	if strings.Contains(got.URL, validCfg().SecretKey) {
		t.Fatal("secret leaked into get URL")
	}
}

func TestIssueGetTargetRejectsOriginalKey(t *testing.T) {
	a, err := New(validCfg())
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.IssueGetTarget(context.Background(), mustListingKey(t))
	if !errors.Is(err, media.ErrInvalidObjectKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestIssueGetTargetPublicBaseURL(t *testing.T) {
	cfg := validCfg()
	cfg.PublicBaseURL = "https://media.example.test/public"
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := mustProcessedKey(t)
	got, err := a.IssueGetTarget(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://media.example.test/public/"+key {
		t.Fatalf("url = %q", got.URL)
	}
	if !got.ExpiresAt.IsZero() {
		t.Fatal("composed public URL must not look signed")
	}
}

func mustListingKey(t *testing.T) string {
	t.Helper()
	owner, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	asset, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ListingImageObjectKey(owner.String(), asset.String(), strings.Repeat("cd", 16))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type fakeAPI struct {
	headIn  *s3.HeadObjectInput
	delIn   *s3.DeleteObjectInput
	getIn   *s3.GetObjectInput
	putIn   *s3.PutObjectInput
	headOut *s3.HeadObjectOutput
	headErr error
	delErr  error
	getErr  error
	putErr  error
	getBody []byte
}

func (f *fakeAPI) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	f.headIn = params
	if f.headErr != nil {
		return nil, f.headErr
	}
	return f.headOut, nil
}

func (f *fakeAPI) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.delIn = params
	if f.delErr != nil {
		return nil, f.delErr
	}
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeAPI) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.getIn = params
	if f.getErr != nil {
		return nil, f.getErr
	}
	body := f.getBody
	if body == nil {
		body = []byte{}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func (f *fakeAPI) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putIn = params
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &s3.PutObjectOutput{}, nil
}

type fakePresign struct {
	err    error
	putURL string
	getURL string
}

func (f *fakePresign) PresignPutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.putURL != "" {
		return &v4.PresignedHTTPRequest{URL: f.putURL, Method: "PUT"}, nil
	}
	return &v4.PresignedHTTPRequest{URL: "http://127.0.0.1:9000/konumlu-media/x", Method: "PUT"}, nil
}

func (f *fakePresign) PresignGetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.getURL != "" {
		return &v4.PresignedHTTPRequest{URL: f.getURL, Method: "GET"}, nil
	}
	key := ""
	if params != nil && params.Key != nil {
		key = *params.Key
	}
	return &v4.PresignedHTTPRequest{URL: "http://127.0.0.1:9000/konumlu-media/" + key + "?X-Amz-Signature=sig", Method: "GET"}, nil
}
