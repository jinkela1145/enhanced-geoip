package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jinkela1145/enhanced-geoip/internal/testutil"
)

// TestWriteFixtures writes the synthetic upstream files to $EGEO_FIXTURES so
// that the command line tool can be smoke-tested against a local web server.
func TestWriteFixtures(t *testing.T) {
	dir := os.Getenv("EGEO_FIXTURES")
	if dir == "" {
		t.Skip("set EGEO_FIXTURES to write fixtures")
	}
	if err := os.MkdirAll(filepath.Join(dir, "dbip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteFakeDBIP(filepath.Join(dir, "dbip", "dbip-city-lite-2026-10.mmdb.gz"), testutil.DefaultFakeNets); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"iptoasn.tsv.gz": gz(t, testutil.IPtoASNSample),
		"cf4":            []byte("104.16.0.0/13\n"),
		"cf6":            []byte("2400:cb00::/32\n"),
		"fastly":         []byte(testutil.FastlySample),
		"aws":            []byte(testutil.AWSSample),
		"gcp":            []byte(testutil.GCPSample),
		"oracle":         []byte(testutil.OracleSample),
		"apnic":          []byte(testutil.DelegatedSample),
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
