package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"time"

	"github.com/HugoSmits86/nativewebp"
	xdraw "golang.org/x/image/draw"

	"backend/internal/platform/observability"
)

const defaultOutputLongEdge = 1600

type imageFormat int

const (
	formatUnknown imageFormat = iota
	formatJPEG
	formatPNG
)

var (
	pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	webpRIFF = []byte("RIFF")
	webpWEBP = []byte("WEBP")
	gifMagic = []byte("GIF8")
)

func (s *Service) ProcessAsset(ctx context.Context, id ID) error {
	if s == nil || s.store == nil {
		return errStoreRequired
	}
	if s.objects == nil {
		return errStorageRequired
	}
	if id.IsZero() {
		return errZeroID
	}
	started := time.Now()
	asset, err := s.store.Get(ctx, id)
	if err != nil {
		return mapStoreErr(err)
	}
	switch asset.Status {
	case StatusReady, StatusRejected, StatusDeleted, StatusPendingUpload:
		s.logProcess(ctx, id, asset.Status, "skip", 0, nil)
		return nil
	case StatusUploaded:
		now := s.now().UTC()
		next, err := asset.MarkProcessing(now)
		if err != nil {
			return err
		}
		if err := s.store.Update(ctx, next, asset.UpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		asset = next
	case StatusProcessing:
	default:
		return errInvalidStatus
	}

	data, stat, err := s.objects.GetObject(ctx, asset.ObjectKey)
	if err != nil {
		s.logProcess(ctx, id, asset.Status, "retry", time.Since(started), err)
		return mapStoreErr(err)
	}
	if !stat.Exists {
		return s.rejectAndComplete(ctx, asset, started)
	}
	maxBytes := s.processMaxBytes()
	if int64(len(data)) > maxBytes || (stat.SizeBytes > 0 && stat.SizeBytes > maxBytes) {
		return s.rejectAndComplete(ctx, asset, started)
	}

	format, err := inspectMagic(data)
	if err != nil {
		return s.rejectAndComplete(ctx, asset, started)
	}

	if err := s.requireMalware(ctx, asset.ObjectKey, data); err != nil {
		if errors.Is(err, errInvalidImage) {
			return s.rejectAndComplete(ctx, asset, started)
		}
		s.logProcess(ctx, id, asset.Status, "retry", time.Since(started), err)
		return err
	}

	normalized, meta, err := normalizeImage(data, format, s.processOutputLongEdge())
	if err != nil {
		return s.rejectAndComplete(ctx, asset, started)
	}

	if err := s.requireModeration(ctx, asset.ObjectKey, normalized, meta.ContentType); err != nil {
		if errors.Is(err, errInvalidImage) {
			return s.rejectAndComplete(ctx, asset, started)
		}
		s.logProcess(ctx, id, asset.Status, "retry", time.Since(started), err)
		return err
	}

	processedKey, err := ProcessedObjectKeyFromOriginal(asset.OwnerUserID, asset.ID, asset.ObjectKey)
	if err != nil {
		return err
	}
	if err := s.objects.PutObject(ctx, processedKey, normalized, meta.ContentType); err != nil {
		s.logProcess(ctx, id, asset.Status, "retry", time.Since(started), err)
		return mapStoreErr(err)
	}

	now := s.now().UTC()
	ready, err := asset.MarkReady(meta, processedKey, now)
	if err != nil {
		return err
	}
	if err := s.store.Update(ctx, ready, asset.UpdatedAt); err != nil {
		return mapStoreErr(err)
	}
	_ = s.objects.DeleteObject(ctx, asset.ObjectKey)
	s.logProcess(ctx, id, StatusReady, "ready", time.Since(started), nil)
	return nil
}

func (s *Service) rejectAndComplete(ctx context.Context, asset Asset, started time.Time) error {
	now := s.now().UTC()
	rejected, err := asset.Reject(now)
	if err != nil {
		if errors.Is(err, errInvalidTransition) {
			return nil
		}
		return err
	}
	if err := s.store.Update(ctx, rejected, asset.UpdatedAt); err != nil {
		return mapStoreErr(err)
	}
	_ = s.objects.DeleteObject(ctx, asset.ObjectKey)
	s.logProcess(ctx, asset.ID, StatusRejected, "rejected", time.Since(started), errInvalidImage)
	return nil
}

func (s *Service) requireMalware(ctx context.Context, objectKey string, data []byte) error {
	if s == nil || !s.policy.RequireMalwareScan {
		return nil
	}
	if s.malware == nil {
		return errScannerUnavailable
	}
	verdict, err := s.malware.Scan(ctx, objectKey, data)
	if err != nil {
		return errScannerUnavailable
	}
	switch verdict {
	case ScanClean:
		return nil
	case ScanRejected:
		return errInvalidImage
	default:
		return errScannerUnavailable
	}
}

func (s *Service) requireModeration(ctx context.Context, objectKey string, data []byte, contentType string) error {
	if s == nil || !s.policy.RequireModeration {
		return nil
	}
	if s.moderator == nil {
		return errModeratorUnavailable
	}
	verdict, err := s.moderator.Moderate(ctx, objectKey, data, contentType)
	if err != nil {
		return errModeratorUnavailable
	}
	switch verdict {
	case ScanClean:
		return nil
	case ScanRejected:
		return errInvalidImage
	default:
		return errModeratorUnavailable
	}
}

func (s *Service) processMaxBytes() int64 {
	if s != nil && s.policy.MaxBytes > 0 {
		return s.policy.MaxBytes
	}
	if s != nil && s.maxUploadBytes > 0 {
		return s.maxUploadBytes
	}
	return defaultProcessMaxBytes
}

func (s *Service) processMaxWidth() int {
	if s != nil && s.policy.MaxWidth > 0 {
		return s.policy.MaxWidth
	}
	return maxImageWidth
}

func (s *Service) processMaxHeight() int {
	if s != nil && s.policy.MaxHeight > 0 {
		return s.policy.MaxHeight
	}
	return maxImageHeight
}

func (s *Service) processOutputLongEdge() int {
	maxW := s.processMaxWidth()
	maxH := s.processMaxHeight()
	edge := defaultOutputLongEdge
	if maxW > 0 && maxW < edge {
		edge = maxW
	}
	if maxH > 0 && maxH < edge {
		edge = maxH
	}
	return edge
}

func inspectMagic(data []byte) (imageFormat, error) {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return formatJPEG, nil
	}
	if len(data) >= 8 && bytes.Equal(data[:8], pngMagic) {
		return formatPNG, nil
	}
	if isWebP(data) || bytes.HasPrefix(data, gifMagic) {
		return formatUnknown, errUnsupportedImage
	}
	return formatUnknown, errUnsupportedImage
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[:4], webpRIFF) && bytes.Equal(data[8:12], webpWEBP)
}

func normalizeImage(data []byte, format imageFormat, outputLongEdge int) ([]byte, ValidatedMetadata, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	if err := validateDecodeBounds(cfg.Width, cfg.Height); err != nil {
		return nil, ValidatedMetadata{}, err
	}

	img, err := decodeImage(data, format)
	if err != nil {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if err := validateDecodeBounds(width, height); err != nil {
		return nil, ValidatedMetadata{}, err
	}

	outW, outH := fitLongEdge(width, height, outputLongEdge)
	if outW != width || outH != height {
		scaled := image.NewRGBA(image.Rect(0, 0, outW, outH))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), img, bounds, xdraw.Over, nil)
		img = scaled
		width, height = outW, outH
	}

	var out bytes.Buffer
	if err := nativewebp.Encode(&out, img, &nativewebp.Options{UseExtendedFormat: false}); err != nil {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	encoded := out.Bytes()
	if !isWebP(encoded) || len(encoded) == 0 {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	meta := ValidatedMetadata{
		ContentType: "image/webp",
		SizeBytes:   int64(len(encoded)),
		Width:       width,
		Height:      height,
	}
	if err := meta.Validate(); err != nil {
		return nil, ValidatedMetadata{}, err
	}
	return encoded, meta, nil
}

func decodeImage(data []byte, format imageFormat) (image.Image, error) {
	switch format {
	case formatJPEG:
		return jpeg.Decode(bytes.NewReader(data))
	case formatPNG:
		return png.Decode(bytes.NewReader(data))
	default:
		return nil, errUnsupportedImage
	}
}

func fitLongEdge(w, h, maxEdge int) (int, int) {
	if w <= 0 || h <= 0 {
		return w, h
	}
	if maxEdge <= 0 || (w <= maxEdge && h <= maxEdge) {
		return w, h
	}
	if w >= h {
		nw := maxEdge
		nh := int(float64(h) * float64(maxEdge) / float64(w))
		if nh < 1 {
			nh = 1
		}
		return nw, nh
	}
	nh := maxEdge
	nw := int(float64(w) * float64(maxEdge) / float64(h))
	if nw < 1 {
		nw = 1
	}
	return nw, nh
}

func validateDecodeBounds(w, h int) error {
	if w <= 0 || h <= 0 {
		return errInvalidImage
	}
	if w > maxImageWidth || h > maxImageHeight {
		return errInvalidImage
	}
	if int64(w)*int64(h) > maxImagePixels {
		return errInvalidImage
	}
	return nil
}

func (s *Service) logProcess(ctx context.Context, id ID, status Status, outcome string, dur time.Duration, err error) {
	attrs := []any{
		"media_id", id.String(),
		"media_status", string(status),
		"processing_outcome", outcome,
		"object_category", "listing-images",
		"duration_ms", dur.Milliseconds(),
	}
	if err != nil {
		attrs = append(attrs, "error_class", processErrorClass(err))
		observability.FromContext(ctx).Info("media_process", attrs...)
		return
	}
	observability.FromContext(ctx).Info("media_process", attrs...)
}

func processErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errInvalidImage), errors.Is(err, errUnsupportedImage):
		return "invalid_media"
	case errors.Is(err, errScannerUnavailable), errors.Is(err, errModeratorUnavailable):
		return "scanner_unavailable"
	case errors.Is(err, errUnavailable), errors.Is(err, errStorageRequired):
		return "storage_unavailable"
	default:
		return "media_error"
	}
}
