package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func newTestFetcher(t *testing.T) *Fetcher {
	f := New(t.TempDir(), "test-agent (+https://example.invalid)")
	f.Backoff = time.Millisecond
	return f
}

func TestFetchAndConditional(t *testing.T) {
	var hits, notModified atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != "test-agent (+https://example.invalid)" {
			t.Errorf("missing user agent, got %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Thu, 01 Oct 2026 00:00:00 GMT")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	f := newTestFetcher(t)
	path, m, err := f.Fetch(context.Background(), "x", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "hello" || m.Size != 5 || m.ETag != `"v1"` || m.Cached {
		t.Fatalf("unexpected first fetch: %q %+v", b, m)
	}
	if m.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("sha256 = %s", m.SHA256)
	}
	path2, m2, err := f.Fetch(context.Background(), "x", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if path2 != path || !m2.Cached || m2.SHA256 != m.SHA256 || notModified.Load() != 1 {
		t.Fatalf("conditional request not used: %+v", m2)
	}
}

func TestFetchNotFoundIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, _, err := newTestFetcher(t).Fetch(context.Background(), "x", srv.URL)
	if !errors.Is(err, ErrNotFound) || hits.Load() != 1 {
		t.Fatalf("err=%v hits=%d", err, hits.Load())
	}
}

func TestFetchRetriesServerErrors(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	_, m, err := newTestFetcher(t).Fetch(context.Background(), "x", srv.URL)
	if err != nil || m.Size != 2 || hits.Load() != 3 {
		t.Fatalf("err=%v meta=%+v hits=%d", err, m, hits.Load())
	}
}

func TestFetchGivesUpAndKeepsNoPartialFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	f := newTestFetcher(t)
	f.Attempts = 2
	if _, _, err := f.Fetch(context.Background(), "x", srv.URL); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(f.Path("x")); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
}

func TestFetchSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()
	f := newTestFetcher(t)
	f.MaxBytes = 10
	if _, _, err := f.Fetch(context.Background(), "x", srv.URL); err == nil {
		t.Fatal("expected size error")
	}
}
