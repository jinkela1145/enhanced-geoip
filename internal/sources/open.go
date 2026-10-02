// Package sources parses the upstream data files and the curated tables in
// data/.
package sources

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

// readCloser bundles a reader with the closers that must run after it.
type readCloser struct {
	io.Reader
	closers []io.Closer
}

func (r *readCloser) Close() error {
	var errs []error
	for i := len(r.closers) - 1; i >= 0; i-- {
		errs = append(errs, r.closers[i].Close())
	}
	return errors.Join(errs...)
}

// Open opens a file, transparently decompressing gzip content.
func Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(f, 1<<16)
	magic, err := br.Peek(2)
	if err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return &readCloser{Reader: zr, closers: []io.Closer{f, zr}}, nil
	}
	return &readCloser{Reader: br, closers: []io.Closer{f}}, nil
}

// decompressToTemp writes the (possibly gzip-compressed) file to a temporary
// file in dir and returns its path.
func decompressToTemp(path, dir string) (string, error) {
	in, err := Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, "decompressed-*.mmdb")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", fmt.Errorf("decompressing %s: %w", path, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}
