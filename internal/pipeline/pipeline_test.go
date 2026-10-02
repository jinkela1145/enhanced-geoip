package pipeline

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/jinkela1145/enhanced-geoip/internal/config"
	"github.com/jinkela1145/enhanced-geoip/internal/sources"
	"github.com/jinkela1145/enhanced-geoip/internal/testutil"
	"github.com/jinkela1145/enhanced-geoip/internal/verify"
)

func gz(t *testing.T, s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fixture struct {
	t       *testing.T
	srv     *httptest.Server
	cfg     *config.Config
	cache   string
	data    string
	dbipGz  []byte
	months  map[string]bool // which DB-IP months exist
	iptoasn []byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{t: t, cache: t.TempDir(), data: t.TempDir(), months: map[string]bool{"2026-10": true}}
	p := filepath.Join(t.TempDir(), "dbip.mmdb.gz")
	if err := testutil.WriteFakeDBIP(p, testutil.DefaultFakeNets); err != nil {
		t.Fatal(err)
	}
	var err error
	if fx.dbipGz, err = os.ReadFile(p); err != nil {
		t.Fatal(err)
	}
	fx.iptoasn = gz(t, testutil.IPtoASNSample)
	mux := http.NewServeMux()
	mux.HandleFunc("/dbip/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dbip/")
		month := strings.TrimSuffix(strings.TrimPrefix(name, "dbip-city-lite-"), ".mmdb.gz")
		if !fx.months[month] {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(fx.dbipGz)
	})
	static := map[string][]byte{
		"/iptoasn.tsv.gz": nil,
		"/cf4":            []byte("104.16.0.0/13\n"),
		"/cf6":            []byte("2400:cb00::/32\n"),
		"/fastly":         []byte(testutil.FastlySample),
		"/aws":            []byte(testutil.AWSSample),
		"/gcp":            []byte(testutil.GCPSample),
		"/oracle":         []byte(testutil.OracleSample),
		"/apnic":          []byte(testutil.DelegatedSample),
	}
	for path, body := range static {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if path == "/iptoasn.tsv.gz" {
				_, _ = w.Write(fx.iptoasn)
				return
			}
			_, _ = w.Write(body)
		})
	}
	fx.srv = httptest.NewServer(mux)
	t.Cleanup(fx.srv.Close)

	cfg, err := config.Load("../../config.json")
	if err != nil {
		t.Fatal(err)
	}
	u := fx.srv.URL
	cfg.Sources = map[string]string{
		config.SrcDBIPCity:     u + "/dbip/dbip-city-lite-{YYYY}-{MM}.mmdb.gz",
		config.SrcDBIPASN:      u + "/dbip/dbip-asn-lite-{YYYY}-{MM}.mmdb.gz",
		config.SrcIPtoASN:      u + "/iptoasn.tsv.gz",
		config.SrcCloudflareV4: u + "/cf4",
		config.SrcCloudflareV6: u + "/cf6",
		config.SrcFastly:       u + "/fastly",
		config.SrcAWS:          u + "/aws",
		config.SrcGCP:          u + "/gcp",
		config.SrcOracle:       u + "/oracle",
		config.SrcAPNIC:        u + "/apnic",
	}
	cfg.DisabledSources = []string{config.SrcAzurePage}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	fx.cfg = cfg

	write := func(name string, header []string, rows ...string) {
		content := strings.Join(header, ",") + "\n" + strings.Join(rows, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(fx.data, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(FileCNAdmin, sources.HeaderCNAdmin,
		"BJ,北京市,Beijing,Beijing,2038349,北京,Beijing,1816670,39.9075,116.39723,250",
		"FJ,福建省,Fujian,Fujian,1811017,福州,Fuzhou,1810821,26.06139,119.30611,250",
		"GD,广东省,Guangdong,Guangdong,1809935,广州,Guangzhou,1809858,23.11667,113.25,250",
		"JS,江苏省,Jiangsu,Jiangsu,1806260,南京,Nanjing,1799962,32.06167,118.77778,250")
	write(FileCNCities, sources.HeaderCNCities, "GD,深圳市,Shenzhen,1795565,22.54554,114.0683")
	write(FileCNASN, sources.HeaderCNASN, "56046,JS,CMNET-JIANGSU-AP,iptoasn AS_description,test")
	write(FileAnycastNS, sources.HeaderAnycastNets,
		"1.1.1.0/24,true,false,Cloudflare 1.1.1.1,test",
		"8.8.8.0/24,true,false,Google Public DNS,test")
	write(FileAnycastAS, sources.HeaderAnycastASNs, "13335,false,true,Cloudflare,test")
	write(FileOverrides, sources.HeaderOverrides,
		"39.0.1.0/24,CN,广东,深圳,,,,test row",
		"3.115.0.0-3.115.0.255,JP,Osaka,Osaka,34.69,135.50,25,test row")
	return fx
}

func (fx *fixture) run(out string) *Manifest {
	fx.t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 2, 17, 0, 0, time.UTC)
	in, err := Fetch(ctx, FetchOptions{Config: fx.cfg, CacheDir: fx.cache, DataDir: fx.data, Now: now}, filepath.Join(out, "inputs.json"))
	if err != nil {
		fx.t.Fatal(err)
	}
	m, err := Build(BuildOptions{Config: fx.cfg, Inputs: in, CacheDir: fx.cache, DataDir: fx.data, OutDir: out, Now: now})
	if err != nil {
		fx.t.Fatal(err)
	}
	return m
}

type fullRec struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Continent struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"continent"`
	Subdivisions []struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
	ASN     uint32 `maxminddb:"autonomous_system_number"`
	ASOrg   string `maxminddb:"autonomous_system_organization"`
	Network struct {
		Anycast     bool   `maxminddb:"anycast"`
		CDN         bool   `maxminddb:"cdn"`
		Cloud       string `maxminddb:"cloud"`
		CloudRegion string `maxminddb:"cloud_region"`
	} `maxminddb:"network"`
	Source string `maxminddb:"source"`
}

func lookup(t *testing.T, r *maxminddb.Reader, ip string, v any) bool {
	t.Helper()
	res := r.Lookup(netip.MustParseAddr(ip))
	if err := res.Err(); err != nil {
		t.Fatalf("%s: %v", ip, err)
	}
	if !res.Found() {
		return false
	}
	if err := res.Decode(v); err != nil {
		t.Fatalf("%s: %v", ip, err)
	}
	return true
}

func TestEndToEnd(t *testing.T) {
	fx := newFixture(t)
	out := t.TempDir()
	m := fx.run(out)

	if m.Sources[config.SrcDBIPCity].Version != "2026-10" || m.Fingerprint == "" {
		t.Errorf("manifest sources/fingerprint: %+v", m.Sources[config.SrcDBIPCity])
	}
	full, err := maxminddb.Open(filepath.Join(out, fx.cfg.FullFile()))
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	if full.Metadata.DatabaseType != "EnhancedGeo-City" || full.Metadata.IPVersion != 6 {
		t.Errorf("metadata %+v", full.Metadata)
	}

	cases := []struct {
		ip                    string
		country, subISO, city string
		source                string
		asn                   uint32
		anycast, cdn          bool
		cloud, region         string
		radius                uint16
		lat, lon              float64 // checked when non-zero
	}{
		// China ASN layer: DB-IP says Beijing, AS56046 is Jiangsu Mobile -> corrected.
		{ip: "36.0.0.1", country: "CN", subISO: "JS", source: "bgp-asn", asn: 56046, radius: 250, lat: 32.06167, lon: 118.77778},
		// DB-IP has no province -> filled.
		{ip: "36.1.0.1", country: "CN", subISO: "JS", source: "bgp-asn", asn: 56046, radius: 250},
		// DB-IP already in Jiangsu -> keep DB-IP's city.
		{ip: "39.0.0.1", country: "CN", city: "Nanjing", source: "dbip", asn: 56046, radius: 50},
		// Override wins over everything; city coordinates come from cn_cities.csv.
		{ip: "39.0.1.1", country: "CN", subISO: "GD", city: "Shenzhen", source: "override", asn: 56046, radius: 50, lat: 22.54554, lon: 114.0683},
		// Backbone AS4134 is not in the table -> DB-IP untouched.
		{ip: "1.0.8.1", country: "CN", city: "Guangzhou", source: "dbip", asn: 4134, radius: 50},
		{ip: "240e::1", country: "CN", city: "Beijing", source: "dbip", asn: 4134, radius: 50},
		{ip: "2409:8000::1", country: "CN", city: "Guangzhou", source: "dbip", asn: 9808, radius: 50},
		{ip: "2409:8020::1", country: "CN", subISO: "JS", source: "bgp-asn", asn: 56046, radius: 250},
		// HK / MO / TW keep their own country codes.
		{ip: "223.0.0.1", country: "HK", city: "Hong Kong", source: "dbip", asn: 4760, radius: 50},
		{ip: "223.1.0.1", country: "TW", city: "Taipei", source: "dbip", radius: 50},
		{ip: "223.2.0.1", country: "MO", city: "Macau", source: "dbip", radius: 50},
		// Anycast and CDN flags.
		{ip: "1.1.1.1", country: "AU", source: "dbip", asn: 13335, anycast: true, cdn: true, radius: 1000},
		{ip: "8.8.8.8", country: "US", source: "dbip", asn: 15169, anycast: true, radius: 1000},
		{ip: "104.16.0.1", country: "US", source: "dbip", asn: 13335, anycast: true, cdn: true, radius: 1000},
		{ip: "2400:cb00::1", country: "US", source: "dbip", asn: 13335, anycast: true, cdn: true, radius: 1000},
		// Cloud flags.
		{ip: "3.112.0.1", country: "JP", source: "dbip", asn: 16509, cloud: "aws", region: "ap-northeast-1", radius: 50},
		{ip: "3.113.0.1", country: "JP", source: "dbip", asn: 16509, cloud: "aws", region: "ap-northeast-1", cdn: true, radius: 50},
		{ip: "3.114.0.1", country: "JP", source: "dbip", asn: 16509, cloud: "aws", region: "ap-northeast-1", anycast: true, radius: 1000},
		{ip: "3.115.0.1", country: "JP", city: "Osaka", source: "override", asn: 16509, cloud: "aws", region: "ap-northeast-1", radius: 25},
	}
	for _, c := range cases {
		var got fullRec
		if !lookup(t, full, c.ip, &got) {
			t.Errorf("%s: not found", c.ip)
			continue
		}
		if got.Country.ISOCode != c.country || got.Source != c.source || got.ASN != c.asn ||
			got.Network.Anycast != c.anycast || got.Network.CDN != c.cdn || got.Network.Cloud != c.cloud ||
			got.Network.CloudRegion != c.region || got.Location.AccuracyRadius != c.radius {
			t.Errorf("%s: got %+v", c.ip, got)
		}
		if c.subISO != "" && (len(got.Subdivisions) == 0 || got.Subdivisions[0].ISOCode != c.subISO) {
			t.Errorf("%s: subdivision %+v, want %s", c.ip, got.Subdivisions, c.subISO)
		}
		if c.city != "" && got.City.Names["en"] != c.city {
			t.Errorf("%s: city %q, want %q", c.ip, got.City.Names["en"], c.city)
		}
		if c.source == "bgp-asn" {
			if got.City.Names != nil || got.Subdivisions[0].Names["zh-CN"] != "江苏省" || got.Country.Names["zh-CN"] != "中国" {
				t.Errorf("%s: China overlay names wrong: %+v", c.ip, got)
			}
		}
		if c.lat != 0 && (math.Abs(got.Location.Latitude-c.lat) > 1e-6 || math.Abs(got.Location.Longitude-c.lon) > 1e-6) {
			t.Errorf("%s: location %v,%v want %v,%v", c.ip, got.Location.Latitude, got.Location.Longitude, c.lat, c.lon)
		}
	}
	var none fullRec
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "2001:db8::1", "::1"} {
		if lookup(t, full, ip, &none) {
			t.Errorf("%s should not be in the database", ip)
		}
	}
	// IPv4-mapped and 6to4 addresses resolve through the standard aliases.
	var mapped fullRec
	if !lookup(t, full, "::ffff:223.0.0.1", &mapped) || mapped.Country.ISOCode != "HK" {
		t.Errorf("IPv4-mapped lookup: %+v", mapped)
	}

	// Lite database: exact schema and rounding.
	lite, err := maxminddb.Open(filepath.Join(out, fx.cfg.LiteFile()))
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	var lrec map[string]any
	lookup(t, lite, "36.0.0.1", &lrec)
	want := map[string]any{
		"country":  map[string]any{"iso_code": "CN"},
		"location": map[string]any{"latitude": 32.0, "longitude": 119.0, "accuracy_radius": uint64(250)},
	}
	if b1, b2 := mustJSON(t, lrec), mustJSON(t, want); b1 != b2 {
		t.Errorf("lite 36.0.0.1 = %s, want %s", b1, b2)
	}
	lrec = nil
	lookup(t, lite, "1.1.1.1", &lrec)
	if n, _ := lrec["network"].(map[string]any); n["anycast"] != true || n["cdn"] != true || len(n) != 2 {
		t.Errorf("lite 1.1.1.1 network = %v", lrec["network"])
	}
	lrec = nil
	lookup(t, lite, "3.112.0.1", &lrec)
	if n, _ := lrec["network"].(map[string]any); n["cloud"] != "aws" || len(n) != 1 {
		t.Errorf("lite 3.112.0.1 network = %v (cloud_region must not be in Lite)", lrec["network"])
	}

	// Lite aggregation to /24 and /40 blocks; the full database keeps the detail.
	liteCases := []struct {
		ip       string
		country  string
		lat, lon float64
		radius   uint64
		missing  bool
	}{
		{ip: "5.1.2.1", country: "DE", lat: 52.5, lon: 13.5, radius: 50},
		{ip: "5.1.2.130", country: "DE", lat: 52.5, lon: 13.5, radius: 50}, // Munich in the full database
		{ip: "5.1.4.200", country: "FR", lat: 49, lon: 2.5, radius: 500},   // Paris + Lyon needed for 2/3
		{ip: "5.1.3.1", country: "NL", lat: 52.5, lon: 5, radius: 50},      // mixed countries: kept
		{ip: "5.1.3.129", country: "BE", lat: 51, lon: 4.5, radius: 50},
		{ip: "5.1.5.1", country: "DE", lat: 52.5, lon: 13.5, radius: 50}, // gap: kept
		{ip: "5.1.5.130", country: "DE", lat: 48, lon: 11.5, radius: 50},
		{ip: "5.1.5.200", missing: true},
		{ip: "223.3.0.1", country: "CN", lat: 22.5, lon: 114, radius: 50}, // CN and HK share a /24: never merged
		{ip: "223.3.0.129", country: "HK", lat: 22.5, lon: 114, radius: 50},
		{ip: "2a02:1:90::1", country: "DE", lat: 52.5, lon: 13.5, radius: 50}, // Hamburg in the full database
	}
	for _, c := range liteCases {
		var got struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
			Location struct {
				Latitude       float64 `maxminddb:"latitude"`
				Longitude      float64 `maxminddb:"longitude"`
				AccuracyRadius uint64  `maxminddb:"accuracy_radius"`
			} `maxminddb:"location"`
		}
		found := lookup(t, lite, c.ip, &got)
		if c.missing {
			if found {
				t.Errorf("lite %s should not be in the database", c.ip)
			}
			continue
		}
		if !found || got.Country.ISOCode != c.country || got.Location.Latitude != c.lat || got.Location.Longitude != c.lon || got.Location.AccuracyRadius != c.radius {
			t.Errorf("lite %s = %+v, want %s %v,%v r%d", c.ip, got, c.country, c.lat, c.lon, c.radius)
		}
	}
	for ip, city := range map[string]string{"5.1.2.130": "Munich", "2a02:1:90::1": "Hamburg", "5.1.4.200": "Marseille"} {
		var got fullRec
		if !lookup(t, full, ip, &got) || got.City.Names["en"] != city {
			t.Errorf("full %s: city %q, want %q", ip, got.City.Names["en"], city)
		}
	}
	if agg := m.Stats.LiteAggregation; agg["ipv4"].PrefixLen != 24 || agg["ipv4"].MergedBlocks != 2 || agg["ipv4"].KeptBlocks != 3 ||
		agg["ipv6"].PrefixLen != 40 || agg["ipv6"].MergedBlocks != 1 || agg["ipv4"].MovedPercent <= 0 {
		t.Errorf("lite aggregation stats: %+v", agg)
	}

	// The gzip copy decompresses to the Lite database.
	gzOut, ok := m.Outputs["lite_gz"]
	if !ok || gzOut.File != fx.cfg.LiteGzipFile() || gzOut.Compression != "gzip" || gzOut.DatabaseType != "EnhancedGeo-City-Lite" {
		t.Fatalf("lite_gz output: %+v", gzOut)
	}
	gf, err := os.Open(filepath.Join(out, gzOut.File))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(gf)
	if err != nil {
		t.Fatal(err)
	}
	if zr.Header.Name != "" || !zr.Header.ModTime.IsZero() {
		t.Errorf("gzip header must not carry a name or time: %+v", zr.Header)
	}
	plain, _ := io.ReadAll(zr)
	gf.Close()
	if h := sha256.Sum256(plain); hex.EncodeToString(h[:]) != m.Outputs["lite"].SHA256 {
		t.Error("lite .gz does not decompress to the Lite database")
	}

	// verify package: structure + known addresses.
	known := filepath.Join(t.TempDir(), "known.csv")
	_ = os.WriteFile(known, []byte(strings.Join(verify.KnownHeader, ",")+"\n"+
		"1.1.1.1,,,,,true,,test\n"+
		"223.0.0.1,HK,22.3,114.2,50,false,,test\n"+
		"39.0.0.1,CN,32.06,118.80,50,,,test\n"), 0o644)
	if rep, err := verify.Run(fx.cfg, out, known); err != nil {
		t.Fatalf("verify: %v", err)
	} else if rep.KnownChecked != 3 || rep.FullNetworks == 0 || rep.LiteNetworks == 0 || !rep.GzipChecked {
		t.Errorf("verify report %+v", rep)
	}
	badKnown := filepath.Join(t.TempDir(), "bad.csv")
	_ = os.WriteFile(badKnown, []byte(strings.Join(verify.KnownHeader, ",")+"\n223.0.0.1,CN,,,,,,test\n"), 0o644)
	if _, err := verify.Run(fx.cfg, out, badKnown); err == nil {
		t.Error("verify should fail when a known address has the wrong country")
	}

	// Coverage statistics and reports.
	cov := m.Stats.CNCoverageIPv6
	if cov.Delegated == 0 || cov.Percent["country_cn"] <= 0 || cov.Percent["cn_asn_corrected"] <= 0 {
		t.Errorf("coverage: %+v", cov)
	}
	for _, f := range []string{"manifest.json", "ACCURACY.md", "RELEASE_NOTES.md", "reports/cn_asn_candidates.csv",
		fx.cfg.FullFile() + ".sha256", fx.cfg.LiteFile() + ".sha256", fx.cfg.LiteGzipFile() + ".sha256"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	cands, _ := os.ReadFile(filepath.Join(out, "reports/cn_asn_candidates.csv"))
	if !strings.Contains(string(cands), "56046,CMNET-JIANGSU-AP China Mobile communications corporation") || !strings.Contains(string(cands), ",JS,JS") {
		t.Errorf("candidates:\n%s", cands)
	}
	sum, _ := os.ReadFile(filepath.Join(out, fx.cfg.FullFile()+".sha256"))
	if !strings.HasPrefix(string(sum), m.Outputs["full"].SHA256+"  "+fx.cfg.FullFile()) {
		t.Errorf("sha256 file: %q", sum)
	}

	// Reproducible: same inputs give byte-identical databases.
	out2 := t.TempDir()
	m2 := fx.run(out2)
	if m2.Outputs["full"].SHA256 != m.Outputs["full"].SHA256 || m2.Outputs["lite"].SHA256 != m.Outputs["lite"].SHA256 ||
		m2.Outputs["lite_gz"].SHA256 != m.Outputs["lite_gz"].SHA256 {
		t.Error("builds are not reproducible")
	}

	// Change detection.
	in, _ := ReadInputs(filepath.Join(out2, "inputs.json"))
	if changed, _ := Changed(filepath.Join(out, "manifest.json"), in); changed {
		t.Error("unchanged inputs reported as changed")
	}
	_ = os.WriteFile(filepath.Join(fx.data, FileOverrides), []byte(strings.Join(sources.HeaderOverrides, ",")+"\n"), 0o644)
	in3, err := Fetch(context.Background(), FetchOptions{Config: fx.cfg, CacheDir: fx.cache, DataDir: fx.data,
		Now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := Changed(filepath.Join(out, "manifest.json"), in3); !changed {
		t.Error("editing a data file must change the fingerprint")
	}
}

func mustJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDBIPMonthFallback(t *testing.T) {
	fx := newFixture(t)
	fx.months = map[string]bool{"2026-09": true}
	in, err := Fetch(context.Background(), FetchOptions{Config: fx.cfg, CacheDir: fx.cache, DataDir: fx.data,
		Now: time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if in.DBIPMonth != "2026-09" {
		t.Errorf("month = %s", in.DBIPMonth)
	}
	fx.months = map[string]bool{}
	if _, err := Fetch(context.Background(), FetchOptions{Config: fx.cfg, CacheDir: t.TempDir(), DataDir: fx.data,
		Now: time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)}, ""); err == nil {
		t.Error("missing DB-IP file must fail the build")
	}
}

func TestRepositoryDataFilesAreValid(t *testing.T) {
	dir := "../../data"
	if _, err := sources.ReadCNAdmin(filepath.Join(dir, FileCNAdmin)); err != nil {
		t.Error(err)
	}
	if _, err := sources.ReadCNCities(filepath.Join(dir, FileCNCities)); err != nil {
		t.Error(err)
	}
	if _, err := sources.ReadCNASN(filepath.Join(dir, FileCNASN)); err != nil {
		t.Error(err)
	}
	if _, err := sources.ReadAnycastASNs(filepath.Join(dir, FileAnycastAS)); err != nil {
		t.Error(err)
	}
	if es, err := sources.ReadAnycastNets(filepath.Join(dir, FileAnycastNS)); err != nil || len(es) == 0 {
		t.Error(err)
	}
	if _, err := sources.ReadOverrides(filepath.Join(dir, FileOverrides)); err != nil {
		t.Error(err)
	}
	if _, err := verify.ReadKnownIPs("../../testdata/known_ips.csv"); err != nil {
		t.Error(err)
	}
	if _, err := config.Load("../../config.json"); err != nil {
		t.Error(err)
	}
}
