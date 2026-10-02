// Package fetch downloads upstream files with retries, timeouts and a local
// cache that supports conditional requests (ETag / Last-Modified).
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrNotFound is returned when the server answers 404 or 410.
var ErrNotFound = errors.New("not found")

// Meta describes one downloaded file.
type Meta struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	FetchedAt    string `json:"fetched_at"`
	// Cached is true when the server answered 304 and the cached copy was used.
	Cached bool `json:"cached"`
}

// Fetcher downloads files into CacheDir.
type Fetcher struct {
	Client    *http.Client
	UserAgent string
	CacheDir  string
	Attempts  int
	Backoff   time.Duration
	Timeout   time.Duration // per attempt
	MaxBytes  int64
	Now       func() time.Time
	Logf      func(format string, args ...any)
}

// New returns a Fetcher with sensible defaults.
func New(cacheDir, userAgent string) *Fetcher {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 30 * time.Second
	tr.ResponseHeaderTimeout = 90 * time.Second
	return &Fetcher{
		Client:    &http.Client{Transport: tr},
		UserAgent: userAgent,
		CacheDir:  cacheDir,
		Attempts:  4,
		Backoff:   5 * time.Second,
		Timeout:   15 * time.Minute,
		MaxBytes:  2 << 30,
		Now:       time.Now,
		Logf:      func(string, ...any) {},
	}
}

// Path returns the cache path of a source.
func (f *Fetcher) Path(name string) string { return filepath.Join(f.CacheDir, name+".data") }

func (f *Fetcher) metaPath(name string) string { return filepath.Join(f.CacheDir, name+".meta.json") }

// Cached returns the cached copy of a source, if any.
func (f *Fetcher) Cached(name string) (string, Meta, bool) {
	var m Meta
	b, err := os.ReadFile(f.metaPath(name))
	if err != nil || json.Unmarshal(b, &m) != nil {
		return "", Meta{}, false
	}
	if st, err := os.Stat(f.Path(name)); err != nil || st.Size() != m.Size {
		return "", Meta{}, false
	}
	return f.Path(name), m, true
}

type statusError struct {
	code  int
	url   string
	retry bool
}

func (e *statusError) Error() string { return fmt.Sprintf("GET %s: HTTP %d", e.url, e.code) }

// Fetch downloads url into the cache under name. When a cached copy of the
// same URL exists, a conditional request is sent and the copy is reused on
// 304 Not Modified.
func (f *Fetcher) Fetch(ctx context.Context, name, url string) (string, Meta, error) {
	if err := os.MkdirAll(f.CacheDir, 0o755); err != nil {
		return "", Meta{}, err
	}
	prevPath, prev, havePrev := f.Cached(name)
	if havePrev && prev.URL != url {
		havePrev = false
	}
	var lastErr error
	for attempt := 1; attempt <= max(1, f.Attempts); attempt++ {
		if attempt > 1 {
			wait := f.Backoff * time.Duration(1<<(attempt-2))
			f.Logf("%s: retry %d/%d in %s after: %v", name, attempt, f.Attempts, wait, lastErr)
			select {
			case <-ctx.Done():
				return "", Meta{}, ctx.Err()
			case <-time.After(wait):
			}
		}
		m, notModified, err := f.once(ctx, name, url, prev, havePrev)
		if err == nil {
			if notModified {
				prev.Cached = true
				prev.FetchedAt = f.Now().UTC().Format(time.RFC3339)
				if err := f.writeMeta(name, prev); err != nil {
					return "", Meta{}, err
				}
				return prevPath, prev, nil
			}
			return f.Path(name), m, nil
		}
		lastErr = err
		var se *statusError
		if errors.Is(err, ErrNotFound) || (errors.As(err, &se) && !se.retry) || ctx.Err() != nil {
			break
		}
	}
	return "", Meta{}, fmt.Errorf("%s: %w", name, lastErr)
}

func (f *Fetcher) once(ctx context.Context, name, url string, prev Meta, havePrev bool) (Meta, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Meta{}, false, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	if havePrev {
		if prev.ETag != "" {
			req.Header.Set("If-None-Match", prev.ETag)
		}
		if prev.LastModified != "" {
			req.Header.Set("If-Modified-Since", prev.LastModified)
		}
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return Meta{}, false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified && havePrev:
		return Meta{}, true, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return Meta{}, false, fmt.Errorf("GET %s: HTTP %d: %w", url, resp.StatusCode, ErrNotFound)
	case resp.StatusCode != http.StatusOK:
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return Meta{}, false, &statusError{resp.StatusCode, url, retry}
	}
	tmp, err := os.CreateTemp(f.CacheDir, name+".*.tmp")
	if err != nil {
		return Meta{}, false, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, f.MaxBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Meta{}, false, fmt.Errorf("reading %s: %w", url, err)
	}
	if n > f.MaxBytes {
		return Meta{}, false, fmt.Errorf("%s is larger than %d bytes", url, f.MaxBytes)
	}
	if n == 0 {
		return Meta{}, false, &statusError{resp.StatusCode, url + " (empty body)", true}
	}
	if resp.ContentLength > 0 && n != resp.ContentLength {
		return Meta{}, false, fmt.Errorf("%s: got %d bytes, expected %d", url, n, resp.ContentLength)
	}
	if err := os.Rename(tmp.Name(), f.Path(name)); err != nil {
		return Meta{}, false, err
	}
	m := Meta{
		Name:         name,
		URL:          url,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		SHA256:       hex.EncodeToString(h.Sum(nil)),
		Size:         n,
		FetchedAt:    f.Now().UTC().Format(time.RFC3339),
	}
	return m, false, f.writeMeta(name, m)
}

func (f *Fetcher) writeMeta(name string, m Meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.metaPath(name), b, 0o644)
}

// FileSHA256 returns the hex SHA-256 of a file.
func FileSHA256(path string) (string, int64, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
