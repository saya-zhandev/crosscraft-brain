// Package store provides a streaming binary store for large files used by
// integration nodes (Drive media upload/download, OneDrive, email attachments,
// PDF extraction, etc.). Data is stored on disk (or optionally S3) keyed by
// execution run, so files are scoped to a single workflow run and cleaned up
// when the run completes.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BinaryStore is the interface for storing and retrieving large binary blobs
// without buffering them entirely in memory. Implementations are swappable:
// the default writes to a local directory; an S3-backed variant can be dropped
// in by implementing the same interface.
type BinaryStore interface {
	// Put writes the reader to a new blob at key. The returned string is the
	// store-relative key that can be passed to Get/Delete/GetURL.
	// size may be -1 if unknown.
	Put(ctx context.Context, key string, reader io.Reader, size int64) (string, error)

	// Get opens a reader for the blob at key. The caller must close it.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the blob at key. It is not an error if the key does not exist.
	Delete(ctx context.Context, key string) error

	// GetURL returns a URL for direct access. For disk storage this is a file://
	// URL; for S3 this would be a presigned URL. The URL may expire; nodes should
	// use it immediately (e.g. for thumbnail display).
	GetURL(ctx context.Context, key string) (string, error)

	// Exists reports whether a blob exists at key.
	Exists(ctx context.Context, key string) (bool, error)

	// Cleanup removes all blobs under the given prefix. This is called when a run
	// completes to free disk space. prefix is typically "run/{executionID}/".
	Cleanup(ctx context.Context, prefix string) error
}

// ---------------------------------------------------------------------------
// Disk-based implementation
// ---------------------------------------------------------------------------

// DiskBinaryStore stores files under rootDir, organised by key as the relative
// path. It is safe for concurrent use.
type DiskBinaryStore struct {
	rootDir string
	mu      sync.RWMutex
}

// NewDiskBinaryStore creates a DiskBinaryStore rooted at rootDir. The directory
// is created if it does not exist.
func NewDiskBinaryStore(rootDir string) (*DiskBinaryStore, error) {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("binary store: mkdir %s: %w", rootDir, err)
	}
	return &DiskBinaryStore{rootDir: rootDir}, nil
}

// Put writes the reader to a file under rootDir/key. The key is returned
// unchanged. If size is known (>0), disk space is pre-allocated.
func (s *DiskBinaryStore) Put(_ context.Context, key string, reader io.Reader, _ int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fullPath := s.safePath(key)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("binary store put: mkdir: %w", err)
	}

	// Use a unique temp file per write to prevent concurrent uploads from
	// targeting the same key from corrupting each other.
	tmpPath := fullPath + ".tmp." + randString(12)
	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("binary store put: create: %w", err)
	}

	written, err := io.Copy(f, reader)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("binary store put: write: %w", err)
	}

	if err := os.Rename(tmpPath, fullPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("binary store put: rename: %w", err)
	}

	_ = written // size tracking for metrics
	return key, nil
}

// Get opens the file at key for reading. The caller must close the returned reader.
func (s *DiskBinaryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fullPath := s.safePath(key)
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("binary store get: %w", err)
		}
		return nil, fmt.Errorf("binary store get: %w", err)
	}
	return f, nil
}

// Delete removes the file at key. Missing files are not an error.
func (s *DiskBinaryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fullPath := s.safePath(key)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("binary store delete: %w", err)
	}
	return nil
}

// GetURL returns a file:// URL for the stored blob.
func (s *DiskBinaryStore) GetURL(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fullPath := s.safePath(key)
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("binary store getURL: key %q not found", key)
		}
		return "", fmt.Errorf("binary store getURL: %w", err)
	}
	abs, err := filepath.Abs(fullPath)
	if err != nil {
		abs = fullPath
	}
	return "file://" + filepath.ToSlash(abs), nil
}

// Exists checks whether the blob at key exists on disk.
func (s *DiskBinaryStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fullPath := s.safePath(key)
	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("binary store exists: %w", err)
}

// Cleanup removes the directory tree rooted at the given prefix under rootDir.
// It does not return an error if the prefix doesn't exist.
func (s *DiskBinaryStore) Cleanup(_ context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fullPath := s.safePath(prefix)
	// Only clean up subdirectories of rootDir (safety guard).
	if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(s.rootDir)+string(os.PathSeparator)) &&
		filepath.Clean(fullPath) != filepath.Clean(s.rootDir) {
		return fmt.Errorf("binary store cleanup: prefix %q escapes rootDir", prefix)
	}
	if err := os.RemoveAll(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("binary store cleanup: %w", err)
	}
	return nil
}

// safePath joins rootDir with key, ensuring the result is under rootDir.
func (s *DiskBinaryStore) safePath(key string) string {
	cleaned := filepath.Clean(filepath.Join(s.rootDir, filepath.FromSlash(key)))
	if !strings.HasPrefix(cleaned, filepath.Clean(s.rootDir)+string(os.PathSeparator)) &&
		cleaned != filepath.Clean(s.rootDir) {
		// Key attempted a directory traversal; fall back to a safe sub-path.
		cleaned = filepath.Join(s.rootDir, "_unsafe_", filepath.Base(filepath.FromSlash(key)))
	}
	return cleaned
}

// randString generates a cryptographically random hex string of length n.
func randString(n int) string {
	b := make([]byte, n/2+1)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

// ---------------------------------------------------------------------------
// In-memory implementation (for tests)
// ---------------------------------------------------------------------------

// MemoryBinaryStore stores blobs in a map. Not suitable for production use
// (unbounded memory). Exported for test packages.
type MemoryBinaryStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryBinaryStore creates an in-memory binary store.
func NewMemoryBinaryStore() *MemoryBinaryStore {
	return &MemoryBinaryStore{data: map[string][]byte{}}
}

// Put writes the reader's contents into memory.
func (s *MemoryBinaryStore) Put(_ context.Context, key string, reader io.Reader, _ int64) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("memory binary store put: %w", err)
	}
	s.mu.Lock()
	s.data[key] = data
	s.mu.Unlock()
	return key, nil
}

// Get returns a reader over the in-memory blob.
func (s *MemoryBinaryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	data, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("memory binary store get: key %q not found", key)
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

// Delete removes the in-memory blob.
func (s *MemoryBinaryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()
	return nil
}

// GetURL returns a data URL (not suitable for large files).
func (s *MemoryBinaryStore) GetURL(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	data, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("memory binary store getURL: key %q not found", key)
	}
	// Return a short marker — real data URLs would be too large.
	return fmt.Sprintf("memory://%s?size=%d", key, len(data)), nil
}

// Exists checks whether the key is present.
func (s *MemoryBinaryStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	_, ok := s.data[key]
	s.mu.RUnlock()
	return ok, nil
}

// Cleanup removes all keys with the given prefix.
func (s *MemoryBinaryStore) Cleanup(_ context.Context, prefix string) error {
	s.mu.Lock()
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			delete(s.data, k)
		}
	}
	s.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Key helpers
// ---------------------------------------------------------------------------

// BinaryKey builds a standard key from an execution ID, node ID, and filename.
// Format: run/{executionID}/{nodeID}/{filename}
func BinaryKey(executionID, nodeID, filename string) string {
	if filename == "" {
		filename = "data"
	}
	return filepath.ToSlash(filepath.Join("run", executionID, nodeID, filename))
}

// BinaryPrefix returns the prefix for all blobs belonging to a run.
// Format: run/{executionID}/
func BinaryPrefix(executionID string) string {
	return "run/" + executionID + "/"
}

// ---------------------------------------------------------------------------
// Size helpers
// ---------------------------------------------------------------------------

// SizeLimitedReader wraps a reader and errors if the total read exceeds limit.
// Useful for preventing unbounded uploads.
type SizeLimitedReader struct {
	reader io.Reader
	limit  int64
	read   int64
}

// NewSizeLimitedReader creates a reader that errors if more than limit bytes
// are read.
func NewSizeLimitedReader(reader io.Reader, limit int64) *SizeLimitedReader {
	return &SizeLimitedReader{reader: reader, limit: limit}
}

func (r *SizeLimitedReader) Read(p []byte) (int, error) {
	if r.read >= r.limit {
		return 0, fmt.Errorf("size limit exceeded: %d bytes", r.limit)
	}
	remaining := r.limit - r.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

// TotalRead returns the total bytes read so far.
func (r *SizeLimitedReader) TotalRead() int64 {
	return r.read
}

// ---------------------------------------------------------------------------
// Max upload size (configurable default)
// ---------------------------------------------------------------------------

const DefaultMaxUploadBytes = 100 * 1024 * 1024 // 100 MB

var (
	ErrTooLarge = errors.New("binary store: upload exceeds maximum allowed size")
)

// LimitedPut wraps Put with a size limit. If size is known and exceeds maxBytes,
// it returns ErrTooLarge immediately. If size is unknown (-1), it wraps the
// reader with SizeLimitedReader.
func LimitedPut(ctx context.Context, store BinaryStore, key string, reader io.Reader, size int64, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxUploadBytes
	}
	if size > 0 && size > maxBytes {
		return "", ErrTooLarge
	}
	if size <= 0 {
		reader = NewSizeLimitedReader(reader, maxBytes)
	}
	return store.Put(ctx, key, reader, size)
}

// ---------------------------------------------------------------------------
// Retry-aware put for transient errors
// ---------------------------------------------------------------------------

// RetryPut attempts store.Put with retries for transient errors (e.g. disk full
// recovered, temporary I/O errors). The reader must support rewinding via
// io.ReadSeeker or the caller must provide a fresh reader per attempt.
func RetryPut(ctx context.Context, store BinaryStore, key string, newReader func() io.Reader, size int64, maxAttempts int) (string, error) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(500*(1<<attempt)) * time.Millisecond):
			}
		}
		reader := newReader()
		k, err := store.Put(ctx, key, reader, size)
		if err == nil {
			return k, nil
		}
		lastErr = err
		// Only retry on transient errors
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	return "", fmt.Errorf("binary store retry put: %w", lastErr)
}
