package build

import (
	"fmt"
	"math/rand"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/jinkela1145/enhanced-geoip/internal/config"
	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
	"github.com/jinkela1145/enhanced-geoip/internal/sources"
)

// TestScale builds databases from large synthetic inputs to estimate time and
// memory on real data. Run with EGEO_SCALE=<networks per family>.
func TestScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("EGEO_SCALE"))
	if n == 0 {
		t.Skip("set EGEO_SCALE to run")
	}
	rng := rand.New(rand.NewSource(1))
	cfg, err := config.Load("../../config.json")
	if err != nil {
		t.Fatal(err)
	}
	base := &sources.BaseData{Countries: map[string]*sources.Country{}, ContinentOf: map[string]*sources.Continent{}, BuildEpoch: 1790000000,
		Languages: []string{"de", "en", "es", "fa", "fr", "ja", "ko", "pt-BR", "ru", "zh-CN"}}
	cont := &sources.Continent{Code: "AS", GeonameID: 6255147, Names: sources.Names{"en": "Asia", "zh-CN": "亚洲", "de": "Asien", "fr": "Asie", "ja": "アジア", "ru": "Азия"}}
	var countries []*sources.Country
	for i := 0; i < 240; i++ {
		iso := string(rune('A'+i/26)) + string(rune('A'+i%26))
		c := &sources.Country{ISOCode: iso, GeonameID: uint32(1000 + i), Names: sources.Names{"en": "Country " + iso, "zh-CN": "国家" + iso, "de": "Land " + iso, "fr": "Pays " + iso}}
		countries = append(countries, c)
		base.Countries[iso] = c
		base.ContinentOf[iso] = cont
	}
	// ~ n/8 distinct city records, like DB-IP where many ranges share a city.
	nrec := n / 8
	for i := 0; i < nrec; i++ {
		c := countries[rng.Intn(len(countries))]
		sub := &sources.Place{Names: sources.Names{"en": fmt.Sprintf("Region %d", i%5000)}}
		city := &sources.Place{Names: sources.Names{"en": fmt.Sprintf("City %d", i)}}
		base.Records = append(base.Records, sources.BaseRecord{Continent: cont, Country: c, Subdivs: []*sources.Place{sub}, City: city,
			Lat: rng.Float64()*170 - 85, Lon: rng.Float64()*360 - 180, HasLoc: true})
	}
	// IPv4: n ranges covering 1.0.0.0 - 223.255.255.255 with random sizes.
	start := uint32(1 << 24)
	end := uint32(224 << 24)
	step := (end - start) / uint32(n)
	for i := 0; i < n; i++ {
		s := start + uint32(i)*step
		e := s + step - 1
		base.V4 = append(base.V4, iprange.Seg[int32]{Start: u4(s), End: u4(e), Val: int32(rng.Intn(nrec))})
	}
	// IPv6: n /48s spread over 2400::/12.
	for i := 0; i < n; i++ {
		var b [16]byte
		b[0], b[1] = 0x24, 0x00
		v := uint64(i) * 4096
		for k := 0; k < 8; k++ {
			b[2+k] = byte(v >> (56 - 8*k))
		}
		a := netip.AddrFrom16(b)
		r := iprange.FromPrefix(netip.PrefixFrom(a, 48).Masked())
		base.V6 = append(base.V6, iprange.Seg[int32]{Start: r.Start, End: r.End, Val: int32(rng.Intn(nrec))})
	}
	iprange.SortSegs(base.V6)
	base.V6 = dedupe(base.V6)
	// ASN layer: n/12 ranges per family, ~ 80k distinct ASes.
	asn := &sources.ASNData{}
	for i := 0; i < 80000; i++ {
		asn.Infos = append(asn.Infos, sources.ASNInfo{ASN: uint32(i + 1), Org: fmt.Sprintf("AS-ORG-%d Example Networks Ltd", i)})
	}
	for i := 0; i < n/12; i++ {
		s := start + uint32(i)*step*12
		asn.V4 = append(asn.V4, iprange.Seg[int32]{Start: u4(s), End: u4(s + step*12 - 1), Val: int32(rng.Intn(80000))})
	}
	for i := 0; i < len(base.V6); i += 12 {
		asn.V6 = append(asn.V6, iprange.Seg[int32]{Start: base.V6[i].Start, End: base.V6[min(i+11, len(base.V6)-1)].End, Val: int32(rng.Intn(80000))})
	}
	var flags []sources.FlagEntry
	for i := 0; i < 20000; i++ {
		s := start + uint32(rng.Intn(int(end-start)))&^0xff
		flags = append(flags, sources.FlagEntry{R: iprange.Range{Start: u4(s), End: u4(s + 255)}, Bits: 24, Flags: sources.NetFlags{Cloud: "aws", Region: "us-east-1"}})
	}
	in := &Inputs{Base: base, ASN: asn, Flags: flags}
	out := t.TempDir()
	var ms runtime.MemStats
	t0 := time.Now()
	done := make(chan struct{})
	var peak uint64
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(200 * time.Millisecond):
				runtime.ReadMemStats(&ms)
				if ms.HeapInuse > peak {
					peak = ms.HeapInuse
				}
			}
		}
	}()
	res, err := Build(in, Options{Config: cfg, OutDir: out, BuildEpoch: 1790000000})
	close(done)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("n=%d records=%d: %s, peak heap %.1f GB", n, nrec, time.Since(t0).Round(time.Second), float64(peak)/(1<<30))
	t.Logf("full: %.1f MB, %d ranges; lite: %.1f MB, %d ranges, record size %d",
		float64(res.Full.Size)/(1<<20), res.Full.Ranges, float64(res.Lite.Size)/(1<<20), res.Lite.Ranges, res.Lite.RecordSize)
}

func u4(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func dedupe(s []iprange.Seg[int32]) []iprange.Seg[int32] {
	out := s[:0]
	for _, x := range s {
		if n := len(out); n > 0 && x.Start.Compare(out[n-1].End) <= 0 {
			continue
		}
		out = append(out, x)
	}
	return out
}
