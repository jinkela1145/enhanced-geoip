package sources

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
	"github.com/jinkela1145/enhanced-geoip/internal/testutil"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func findSeg(segs []iprange.Seg[int32], a netip.Addr) (iprange.Seg[int32], bool) {
	for _, s := range segs {
		if s.Range().Contains(a) {
			return s, true
		}
	}
	return iprange.Seg[int32]{}, false
}

func TestReadDBIPCity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbip.mmdb.gz")
	if err := testutil.WriteFakeDBIP(path, testutil.DefaultFakeNets); err != nil {
		t.Fatal(err)
	}
	data, err := ReadDBIPCity(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if data.DatabaseType != "DBIP-City-Lite" {
		t.Errorf("database type %q", data.DatabaseType)
	}
	seg, ok := findSeg(data.V4, netip.MustParseAddr("1.0.8.1"))
	if !ok {
		t.Fatal("1.0.8.1 missing")
	}
	rec := data.Records[seg.Val]
	if rec.Country.ISOCode != "CN" || rec.City.Names["en"] != "Guangzhou" || rec.Subdivs[0].Names["en"] != "Guangdong" ||
		!rec.HasLoc || rec.Country.Names["zh-CN"] != "中国" || rec.Continent.Code != "AS" {
		t.Errorf("unexpected record %+v", rec)
	}
	if _, ok := findSeg(data.V4, netip.MustParseAddr("10.1.2.3")); ok {
		t.Error("private network 10.0.0.0/8 must be excluded")
	}
	if _, ok := findSeg(data.V6, netip.MustParseAddr("2001:1::1")); ok {
		t.Error("reserved network 2001:1::/32 must be excluded")
	}
	seg, ok = findSeg(data.V6, netip.MustParseAddr("240e:1::1"))
	if !ok || data.Records[seg.Val].Country.ISOCode != "CN" {
		t.Error("240e::/20 missing")
	}
	if data.Countries["HK"] == nil || data.ContinentOf["HK"] == nil {
		t.Error("country index incomplete")
	}
	// Records are deduplicated: the two "Beijing" networks share one record.
	a, _ := findSeg(data.V4, netip.MustParseAddr("36.0.0.1"))
	b, _ := findSeg(data.V6, netip.MustParseAddr("2001:250::1"))
	if a.Val != b.Val {
		t.Error("identical records should be shared")
	}
	noSub, _ := findSeg(data.V4, netip.MustParseAddr("36.1.0.1"))
	if r := data.Records[noSub.Val]; len(r.Subdivs) != 0 || r.City != nil {
		t.Errorf("36.1.0.0/16 should have no subdivision or city: %+v", r)
	}
}

func TestReadIPtoASN(t *testing.T) {
	p := writeTemp(t, "ip2asn.tsv", testutil.IPtoASNSample)
	d, err := ReadIPtoASN(p)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := findSeg(d.V4, netip.MustParseAddr("1.0.5.1")); ok {
		t.Errorf("not routed range kept: %+v", s)
	}
	s, ok := findSeg(d.V4, netip.MustParseAddr("36.0.1.1"))
	if !ok || d.Infos[s.Val].ASN != 56046 {
		t.Fatalf("36.0.1.1 -> %+v", s)
	}
	// 36.0.0.0/16 and 36.1.0.0/16 have the same AS and are merged.
	if s.End != netip.MustParseAddr("36.1.255.255") {
		t.Errorf("adjacent same-AS ranges not merged: %v", s.End)
	}
	if _, err := ReadIPtoASN(writeTemp(t, "bad.tsv", "1.0.0.0\t1.0.0.255\tX\n")); err == nil {
		t.Error("malformed line accepted")
	}
}

func TestNetLists(t *testing.T) {
	cf, err := ParseCIDRList(strings.NewReader("173.245.48.0/20\n\n103.21.244.0/22\n"), NetFlags{Anycast: true, CDN: true}, "cloudflare")
	if err != nil || len(cf) != 2 || !cf[0].Flags.Anycast || cf[0].Bits != 20 {
		t.Fatalf("cloudflare: %v %+v", err, cf)
	}
	if _, err := ParseCIDRList(strings.NewReader("<html>"), NetFlags{CDN: true}, "cloudflare"); err == nil {
		t.Error("html accepted as CIDR list")
	}
	fa, err := ParseFastly(strings.NewReader(testutil.FastlySample), NetFlags{CDN: true})
	if err != nil || len(fa) != 2 {
		t.Fatalf("fastly: %v %+v", err, fa)
	}
	aws, ver, err := ParseAWS(strings.NewReader(testutil.AWSSample))
	if err != nil || ver != "2026-10-01-00-00-00" || len(aws) != 5 {
		t.Fatalf("aws: %v %q %d", err, ver, len(aws))
	}
	var sawCDN, sawAnycast bool
	for _, e := range aws {
		if e.Flags.Cloud != "aws" {
			t.Errorf("aws entry without cloud flag: %+v", e)
		}
		if e.Source == "aws:CLOUDFRONT" {
			sawCDN = e.Flags.CDN && e.Flags.Region == ""
		}
		if e.Source == "aws:GLOBALACCELERATOR" {
			sawAnycast = e.Flags.Anycast
		}
	}
	if !sawCDN || !sawAnycast {
		t.Error("CLOUDFRONT / GLOBALACCELERATOR flags missing")
	}
	gcp, _, err := ParseGCP(strings.NewReader(testutil.GCPSample))
	if err != nil || len(gcp) != 2 || gcp[0].Flags.Region != "africa-south1" {
		t.Fatalf("gcp: %v %+v", err, gcp)
	}
	oci, _, err := ParseOracle(strings.NewReader(testutil.OracleSample))
	if err != nil || len(oci) != 1 || oci[0].Flags != (NetFlags{Cloud: "oracle", Region: "ap-tokyo-1"}) {
		t.Fatalf("oracle: %v %+v", err, oci)
	}
	az, ver, err := ParseAzure(strings.NewReader(testutil.AzureSample))
	if err != nil || ver != "350" || len(az) != 4 {
		t.Fatalf("azure: %v %q %+v", err, ver, az)
	}
	for _, e := range az {
		if e.Source == "azure:AzureFrontDoor.Frontend" && !e.Flags.CDN {
			t.Error("front door not marked cdn")
		}
	}
}

func TestFindAzureURL(t *testing.T) {
	plain := `<a href="https://download.microsoft.com/download/7/1/d/71d86715/ServiceTags_Public_20260928.json">`
	escaped := `{"url":"https:\/\/download.microsoft.com\/download\/7\/1\/d\/71d86715\/ServiceTags_Public_20260928.json"}`
	want := "https://download.microsoft.com/download/7/1/d/71d86715/ServiceTags_Public_20260928.json"
	for _, page := range []string{plain, escaped} {
		got, err := FindAzureURL([]byte(page))
		if err != nil || got != want {
			t.Errorf("FindAzureURL(%q) = %q, %v", page, got, err)
		}
	}
	if _, err := FindAzureURL([]byte("<html>nothing</html>")); err == nil {
		t.Error("expected error")
	}
}

func TestReadDelegated(t *testing.T) {
	v4, v6, err := ReadDelegated(writeTemp(t, "delegated", testutil.DelegatedSample), "CN")
	if err != nil {
		t.Fatal(err)
	}
	if len(v4) != 2 || v4[0].String() != "1.0.1.0-1.0.3.255" || v4[1].String() != "36.0.0.0-36.1.255.255" {
		t.Errorf("v4 = %v", v4)
	}
	if len(v6) != 2 || v6[0].Start != netip.MustParseAddr("2409:8000::") {
		t.Errorf("v6 = %v", v6)
	}
}

func TestCuratedTables(t *testing.T) {
	admin := writeTemp(t, "cn_admin.csv", strings.Join(HeaderCNAdmin, ",")+"\n"+
		"# comment line\n"+
		"GD,广东省,Guangdong,Guangdong;Guangdong Province,1809935,广州,Guangzhou,1809858,23.11667,113.25,250\n")
	provs, err := ReadCNAdmin(admin)
	if err != nil || len(provs) != 1 || provs[0].DBIPNames[1] != "Guangdong Province" || provs[0].RadiusKM != 250 {
		t.Fatalf("cn_admin: %v %+v", err, provs)
	}
	asn := writeTemp(t, "cn_asn.csv", strings.Join(HeaderCNASN, ",")+"\n56046,JS,CMNET-JIANGSU-AP,iptoasn AS_description,\n")
	if rows, err := ReadCNASN(asn); err != nil || rows[0].ProvinceISO != "JS" {
		t.Fatalf("cn_asn: %v %+v", err, rows)
	}
	noEvidence := writeTemp(t, "cn_asn2.csv", strings.Join(HeaderCNASN, ",")+"\n56046,JS,X,,\n")
	if _, err := ReadCNASN(noEvidence); err == nil {
		t.Error("row without evidence accepted")
	}
	badHeader := writeTemp(t, "bad.csv", "asn,province\n1,GD\n")
	if _, err := ReadCNASN(badHeader); err == nil {
		t.Error("bad header accepted")
	}
	missing, err := ReadCNASN(filepath.Join(t.TempDir(), "nope.csv"))
	if err != nil || missing != nil {
		t.Error("missing file should yield no rows")
	}
	nets := writeTemp(t, "anycast.csv", strings.Join(HeaderAnycastNets, ",")+"\n1.1.1.0/24,true,false,Cloudflare,https://example.com\n")
	if es, err := ReadAnycastNets(nets); err != nil || !es[0].Flags.Anycast || es[0].Flags.CDN {
		t.Fatalf("anycast nets: %v %+v", err, es)
	}
	asns := writeTemp(t, "asns.csv", strings.Join(HeaderAnycastASNs, ",")+"\n20940,false,true,Akamai,https://example.com\n")
	if m, err := ReadAnycastASNs(asns); err != nil || m[20940] != (NetFlags{CDN: true}) {
		t.Fatalf("anycast asns: %v %+v", err, m)
	}
	ov := writeTemp(t, "overrides.csv", strings.Join(HeaderOverrides, ",")+"\n"+
		"1.2.3.0/24,CN,广东,深圳,22.54,114.06,50,https://example.com/依据\n"+
		"5.6.7.8-5.6.7.20,RU,Moscow,,,,,manual check\n")
	rows, err := ReadOverrides(ov)
	if err != nil || len(rows) != 2 || !rows[0].HasLoc || rows[1].HasLoc || rows[1].R.String() != "5.6.7.8-5.6.7.20" {
		t.Fatalf("overrides: %v %+v", err, rows)
	}
	for _, bad := range []string{
		"10.0.0.0/24,CN,广东,,,,,x\n",    // private
		"1.2.3.0/24,CHN,,,,,,x\n",      // bad country
		"1.2.3.0/24,CN,广东,,22.5,,,x\n", // lat without lon
		"1.2.3.0/24,CN,广东,,,,,\n",      // no evidence
		"1.2.3.0/24,CN,,深圳,,,,x\n",     // city without province
	} {
		p := writeTemp(t, "bad_ov.csv", strings.Join(HeaderOverrides, ",")+"\n"+bad)
		if _, err := ReadOverrides(p); err == nil {
			t.Errorf("bad override accepted: %q", bad)
		}
	}
}

func TestExcluded(t *testing.T) {
	for _, s := range []string{"10.1.1.1", "192.168.1.1", "127.0.0.1", "224.0.0.1", "::1", "fe80::1", "2001:db8::1", "2002::1", "2001::1"} {
		if !IsExcluded(netip.MustParseAddr(s)) {
			t.Errorf("%s should be excluded", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "8.8.8.8", "2400:3200::1", "240e::1", "2001:250::1"} {
		if IsExcluded(netip.MustParseAddr(s)) {
			t.Errorf("%s should not be excluded", s)
		}
	}
}
