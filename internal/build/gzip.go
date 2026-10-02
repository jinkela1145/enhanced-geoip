package build

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"

	"github.com/jinkela1145/enhanced-geoip/internal/fetch"
)

// gzipFile writes a gzip copy of src to dst. The gzip header carries no
// file name and no modification time, so the output only depends on the
// input bytes (and the Go version), which keeps builds reproducible.
func gzipFile(src, dst string) (Output, error) {
	in, err := os.Open(src)
	if err != nil {
		return Output{}, err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return Output{}, err
	}
	fail := func(err error) (Output, error) {
		out.Close()
		os.Remove(tmp)
		return Output{}, err
	}
	zw, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return fail(err)
	}
	if _, err := io.Copy(zw, in); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return Output{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return Output{}, err
	}
	sum, size, err := fetch.FileSHA256(dst)
	if err != nil {
		return Output{}, err
	}
	return Output{File: filepath.Base(dst), Size: size, SHA256: sum}, nil
}
