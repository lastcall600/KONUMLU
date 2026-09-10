package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
)

const jpegReencodeQuality = 85

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
	asset, err := s.store.Get(ctx, id)
	if err != nil {
		return mapStoreErr(err)
	}
	switch asset.Status {
	case StatusReady, StatusRejected, StatusDeleted, StatusPendingUpload:
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
		return mapStoreErr(err)
	}
	if !stat.Exists {
		return s.rejectAndComplete(ctx, asset)
	}
	maxBytes := s.processMaxBytes()
	if int64(len(data)) > maxBytes || (stat.SizeBytes > 0 && stat.SizeBytes > maxBytes) {
		return s.rejectAndComplete(ctx, asset)
	}

	format, err := inspectMagic(data)
	if err != nil {
		return s.rejectAndComplete(ctx, asset)
	}

	if err := s.requireMalware(ctx, asset.ObjectKey, data); err != nil {
		if errors.Is(err, errInvalidImage) {
			return s.rejectAndComplete(ctx, asset)
		}
		return err
	}

	normalized, meta, err := normalizeImage(data, format, s.processMaxWidth(), s.processMaxHeight())
	if err != nil {
		return s.rejectAndComplete(ctx, asset)
	}

	if err := s.requireModeration(ctx, asset.ObjectKey, normalized, meta.ContentType); err != nil {
		if errors.Is(err, errInvalidImage) {
			return s.rejectAndComplete(ctx, asset)
		}
		return err
	}

	processedKey, err := ProcessedObjectKeyFromOriginal(asset.OwnerUserID, asset.ID, asset.ObjectKey)
	if err != nil {
		return err
	}
	if err := s.objects.PutObject(ctx, processedKey, normalized, meta.ContentType); err != nil {
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
	return nil
}

func (s *Service) rejectAndComplete(ctx context.Context, asset Asset) error {
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

func normalizeImage(data []byte, format imageFormat, maxW, maxH int) ([]byte, ValidatedMetadata, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	if err := validateDimensions(cfg.Width, cfg.Height, maxW, maxH); err != nil {
		return nil, ValidatedMetadata{}, err
	}

	img, err := decodeImage(data, format)
	if err != nil {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if err := validateDimensions(width, height, maxW, maxH); err != nil {
		return nil, ValidatedMetadata{}, err
	}

	var out bytes.Buffer
	contentType := ""
	switch format {
	case formatJPEG:
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegReencodeQuality}); err != nil {
			return nil, ValidatedMetadata{}, errInvalidImage
		}
		contentType = "image/jpeg"
	case formatPNG:
		if err := png.Encode(&out, img); err != nil {
			return nil, ValidatedMetadata{}, errInvalidImage
		}
		contentType = "image/png"
	default:
		return nil, ValidatedMetadata{}, errUnsupportedImage
	}
	encoded := out.Bytes()
	if len(encoded) == 0 {
		return nil, ValidatedMetadata{}, errInvalidImage
	}
	meta := ValidatedMetadata{
		ContentType: contentType,
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

func validateDimensions(w, h, maxW, maxH int) error {
	if w <= 0 || h <= 0 {
		return errInvalidImage
	}
	if maxW > 0 && w > maxW {
		return errInvalidImage
	}
	if maxH > 0 && h > maxH {
		return errInvalidImage
	}
	if int64(w)*int64(h) > maxImagePixels {
		return errInvalidImage
	}
	return nil
}
