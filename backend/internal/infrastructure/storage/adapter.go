package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"backend/internal/media"
)

type objectAPI interface {
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type objectPresigner interface {
	PresignPutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// Adapter implements media.ObjectStorage against an S3-compatible endpoint.
type Adapter struct {
	bucket         string
	endpoint       string
	usePathStyle   bool
	uploadTTL      time.Duration
	getTTL         time.Duration
	publicBaseURL  string
	maxUploadBytes int64
	api            objectAPI
	presign        objectPresigner
	now            func() time.Time
}

func New(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errStorageDisabled
	}
	if cfg.CredentialSource == CredentialWorkload {
		return nil, errWorkloadIdentityUnwired
	}

	awsCfg := aws.Config{
		Region: cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if cfg.Endpoint != "" {
		awsCfg.BaseEndpoint = aws.String(cfg.Endpoint)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	return &Adapter{
		bucket:         cfg.Bucket,
		endpoint:       cfg.Endpoint,
		usePathStyle:   cfg.UsePathStyle,
		uploadTTL:      cfg.UploadTTL,
		getTTL:         cfg.GetTTL,
		publicBaseURL:  strings.TrimRight(cfg.PublicBaseURL, "/"),
		maxUploadBytes: cfg.MaxUploadBytes,
		api:            client,
		presign:        s3.NewPresignClient(client),
		now:            func() time.Time { return time.Now().UTC() },
	}, nil
}

func (a *Adapter) MaxUploadBytes() int64 {
	if a == nil {
		return 0
	}
	return a.maxUploadBytes
}

func (a *Adapter) IssueUploadTarget(ctx context.Context, objectKey string) (media.UploadTarget, error) {
	if a == nil || a.presign == nil {
		return media.UploadTarget{}, media.ErrStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return media.UploadTarget{}, err
	}
	if !media.IsOriginalObjectKey(objectKey) {
		return media.UploadTarget{}, media.ErrInvalidObjectKey
	}

	in := &s3.PutObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	}
	req, err := a.presign.PresignPutObject(ctx, in, func(opts *s3.PresignOptions) {
		opts.Expires = a.uploadTTL
	})
	if err != nil {
		return media.UploadTarget{}, mapStorageError(err)
	}
	if req == nil || req.URL == "" {
		return media.UploadTarget{}, media.ErrUnavailable
	}
	if !signedPutMatches(req.URL, a.bucket, objectKey, a.usePathStyle) {
		return media.UploadTarget{}, media.ErrUnavailable
	}

	expires := a.now().Add(a.uploadTTL)
	headers := requiredUploaderHeaders(req.SignedHeader)
	return media.UploadTarget{
		ObjectKey:       objectKey,
		UploadURL:       req.URL,
		ExpiresAt:       expires,
		RequiredHeaders: headers,
		MaxBytes:        a.maxUploadBytes,
	}, nil
}

func (a *Adapter) IssueGetTarget(ctx context.Context, objectKey string) (media.GetTarget, error) {
	if a == nil {
		return media.GetTarget{}, media.ErrStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return media.GetTarget{}, err
	}
	if !media.IsProcessedObjectKey(objectKey) {
		return media.GetTarget{}, media.ErrInvalidObjectKey
	}

	if a.publicBaseURL != "" {
		url := a.publicBaseURL + "/" + objectKey
		if !strings.Contains(url, objectKey) {
			return media.GetTarget{}, media.ErrUnavailable
		}
		return media.GetTarget{URL: url}, nil
	}

	if a.presign == nil {
		return media.GetTarget{}, media.ErrStorageRequired
	}
	ttl := a.getTTL
	if ttl <= 0 {
		ttl = defaultGetTTL
	}
	in := &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	}
	req, err := a.presign.PresignGetObject(ctx, in, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return media.GetTarget{}, mapStorageError(err)
	}
	if req == nil || req.URL == "" {
		return media.GetTarget{}, media.ErrUnavailable
	}
	if !signedGetMatches(req.URL, a.bucket, objectKey, a.usePathStyle) {
		return media.GetTarget{}, media.ErrUnavailable
	}
	return media.GetTarget{
		URL:       req.URL,
		ExpiresAt: a.now().Add(ttl),
	}, nil
}

func (a *Adapter) Stat(ctx context.Context, objectKey string) (media.ObjectStat, error) {
	if a == nil || a.api == nil {
		return media.ObjectStat{}, media.ErrStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return media.ObjectStat{}, err
	}
	if !media.IsServerObjectKey(objectKey) {
		return media.ObjectStat{}, media.ErrInvalidObjectKey
	}

	out, err := a.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		if isNotFound(err) {
			return media.ObjectStat{Exists: false}, nil
		}
		return media.ObjectStat{}, mapStorageError(err)
	}

	stat := media.ObjectStat{Exists: true}
	if out != nil {
		if out.ContentLength != nil {
			stat.SizeBytes = *out.ContentLength
		}
		if out.ContentType != nil {
			stat.ContentType = *out.ContentType
		}
	}
	return stat, nil
}

func (a *Adapter) GetObject(ctx context.Context, objectKey string) ([]byte, media.ObjectStat, error) {
	if a == nil || a.api == nil {
		return nil, media.ObjectStat{}, media.ErrStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, media.ObjectStat{}, err
	}
	if !media.IsServerObjectKey(objectKey) {
		return nil, media.ObjectStat{}, media.ErrInvalidObjectKey
	}
	out, err := a.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, media.ObjectStat{Exists: false}, nil
		}
		return nil, media.ObjectStat{}, mapStorageError(err)
	}
	if out == nil || out.Body == nil {
		return nil, media.ObjectStat{}, media.ErrUnavailable
	}
	defer out.Body.Close()
	limit := a.maxUploadBytes
	if limit <= 0 {
		limit = defaultMaxUploadBytes
	}
	data, err := io.ReadAll(io.LimitReader(out.Body, limit+1))
	if err != nil {
		return nil, media.ObjectStat{}, mapStorageError(err)
	}
	stat := media.ObjectStat{Exists: true, SizeBytes: int64(len(data))}
	if out.ContentType != nil {
		stat.ContentType = *out.ContentType
	}
	if out.ContentLength != nil {
		stat.SizeBytes = *out.ContentLength
	}
	return data, stat, nil
}

func (a *Adapter) PutObject(ctx context.Context, objectKey string, data []byte, contentType string) error {
	if a == nil || a.api == nil {
		return media.ErrStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !media.IsProcessedObjectKey(objectKey) {
		return media.ErrInvalidObjectKey
	}
	in := &s3.PutObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
		Body:   bytes.NewReader(data),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	_, err := a.api.PutObject(ctx, in)
	if err != nil {
		return mapStorageError(err)
	}
	return nil
}

func (a *Adapter) DeleteObject(ctx context.Context, objectKey string) error {
	if a == nil || a.api == nil {
		return media.ErrStorageRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !media.IsServerObjectKey(objectKey) {
		return media.ErrInvalidObjectKey
	}

	_, err := a.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return mapStorageError(err)
	}
	return nil
}

func requiredUploaderHeaders(signed map[string][]string) map[string]string {
	if len(signed) == 0 {
		return nil
	}
	out := make(map[string]string, len(signed))
	for k, vals := range signed {
		lk := strings.ToLower(k)
		if lk == "host" || lk == "authorization" {
			continue
		}
		if len(vals) == 0 {
			continue
		}
		out[k] = vals[0]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func signedPutMatches(rawURL, bucket, objectKey string, pathStyle bool) bool {
	if rawURL == "" || bucket == "" || objectKey == "" {
		return false
	}
	if !strings.Contains(rawURL, objectKey) {
		return false
	}
	if pathStyle {
		return strings.Contains(rawURL, "/"+bucket+"/")
	}
	return strings.Contains(rawURL, bucket)
}

func signedGetMatches(rawURL, bucket, objectKey string, pathStyle bool) bool {
	return signedPutMatches(rawURL, bucket, objectKey, pathStyle)
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nfe *types.NotFound
	if errors.As(err, &nfe) {
		return true
	}
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket":
			return true
		}
		if api.ErrorCode() == "404" {
			return true
		}
	}
	return false
}

var _ media.ObjectStorage = (*Adapter)(nil)
