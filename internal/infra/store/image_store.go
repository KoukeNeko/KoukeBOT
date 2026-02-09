package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

const maxImageSize = 5 * 1024 * 1024 // 5MB per Gemini limit

// ImageStore persists images as files on disk, keyed by content hash.
type ImageStore struct {
	mu       sync.RWMutex
	imageDir string
}

// NewImageStore creates an ImageStore. The directory is created if needed.
func NewImageStore(imageDir string) (*ImageStore, error) {
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return nil, fmt.Errorf("create image directory: %w", err)
	}
	return &ImageStore{imageDir: imageDir}, nil
}

// Save persists an image to disk and returns an ImageRef.
// If the image already exists (same content hash), it is a no-op.
func (s *ImageStore) Save(img domain.ImageInput) (domain.ImageRef, error) {
	if len(img.Data) > maxImageSize {
		return domain.ImageRef{}, fmt.Errorf("image too large: %d bytes (max %d)", len(img.Data), maxImageSize)
	}

	key := computeKey(img.Data, img.MimeType)

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.imageDir, key)
	if _, err := os.Stat(path); err == nil {
		// File already exists (content-addressed dedup)
		return domain.ImageRef{
			StorageKey:       key,
			MimeType:         img.MimeType,
			OriginalFilename: img.Filename,
		}, nil
	}

	if err := os.WriteFile(path, img.Data, 0o644); err != nil {
		return domain.ImageRef{}, fmt.Errorf("write image file: %w", err)
	}

	return domain.ImageRef{
		StorageKey:       key,
		MimeType:         img.MimeType,
		OriginalFilename: img.Filename,
	}, nil
}

// Load reads an image from disk by its storage key.
func (s *ImageStore) Load(ref domain.ImageRef) (domain.ImageInput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.imageDir, ref.StorageKey)
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.ImageInput{}, fmt.Errorf("read image %s: %w", ref.StorageKey, err)
	}

	return domain.ImageInput{
		Data:     data,
		MimeType: ref.MimeType,
		Filename: ref.OriginalFilename,
	}, nil
}

// DeleteKeys removes specific image files from disk (best-effort).
func (s *ImageStore) DeleteKeys(keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range keys {
		path := filepath.Join(s.imageDir, key)
		_ = os.Remove(path)
	}
}

// computeKey generates a content-addressed filename from image bytes.
func computeKey(data []byte, mimeType string) string {
	hash := sha256.Sum256(data)
	prefix := hex.EncodeToString(hash[:8]) // 16 hex chars
	return prefix + mimeToExt(mimeType)
}

func mimeToExt(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
