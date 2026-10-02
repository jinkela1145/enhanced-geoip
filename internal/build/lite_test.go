package build

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"sort"
	"testing"

	"github.com/jinkela1145/enhanced-geoip/internal/geo"
)

var testTiers = []uint16{10, 25, 50, 100, 250, 500, 1000}

// lk builds a Lite key at the given coordinates (degrees, already on the
// 0.5° grid) with radius 50.
func lk(cc string, lat, lon float64) liteKey {
	return liteKey{country: cc, hasLoc: true, lat: roundTo(lat, 0.5), lon: roundTo(lon, 0.5), radius: 50}
}

func run(s, e string, k liteKey) liteRun {
	return liteRun{netip.MustParseAddr(s), netip.MustParseAddr(e), k}
}

func runsString(rs []liteRun) string {
	out := ""
	for _, r := range rs {
		out += fmt.Sprintf("[%s-%s %s %d,%d r%d a%v c%v %s] ", r.start, r.end, r.k.country, r.k.lat, r.k.lon, r.k.radius, r.k.anycast, r.k.cdn, r.k.cloud)
	}
	return out
}

func TestAggregateLite(t *testing.T) {
	berlin, munich := lk("DE", 52.5, 13.5), lk("DE", 48, 11.5)
	paris, lyon, marseille := lk("FR", 49, 2.5), lk("FR", 46, 5), lk("FR", 43.5, 5.5)
	cn, hk := lk("CN", 22.5, 114), lk("HK", 22.5, 114)
	cloud := berlin
	cloud.cloud = "aws"
	noLoc := liteKey{country: "DE"}

	parisLyon := geo.Haversine(49, 2.5, 46, 5) + 50 // ≈ 433 km -> tier 500
	if parisLyon < 400 || parisLyon > 500 {
		t.Fatalf("test assumption: Paris-Lyon reach %.0f km", parisLyon)
	}
	far := berlin
	far.radius = 500

	cases := []struct {
		name   string
		bits   int
		in     []liteRun
		want   []liteRun
		merged int
		kept   int
	}{
		{
			name: "majority location wins, radius kept when it covers two thirds",
			bits: 24,
			in: []liteRun{
				run("5.1.2.0", "5.1.2.127", berlin),
				run("5.1.2.128", "5.1.2.191", munich),
				run("5.1.2.192", "5.1.2.255", berlin),
			},
			want:   []liteRun{run("5.1.2.0", "5.1.2.255", berlin)},
			merged: 1,
		},
		{
			name: "radius widens until two thirds are covered",
			bits: 24,
			in: []liteRun{
				run("5.1.4.0", "5.1.4.127", paris),
				run("5.1.4.128", "5.1.4.191", lyon),
				run("5.1.4.192", "5.1.4.255", marseille),
			},
			want:   []liteRun{run("5.1.4.0", "5.1.4.255", liteKey{country: "FR", hasLoc: true, lat: 98, lon: 5, radius: 500})},
			merged: 1,
		},
		{
			name: "different countries are never merged (HK stays HK)",
			bits: 24,
			in: []liteRun{
				run("223.3.0.0", "223.3.0.127", cn),
				run("223.3.0.128", "223.3.0.255", hk),
			},
			want: []liteRun{
				run("223.3.0.0", "223.3.0.127", cn),
				run("223.3.0.128", "223.3.0.255", hk),
			},
			kept: 1,
		},
		{
			name: "different network flags are never merged",
			bits: 24,
			in: []liteRun{
				run("5.1.6.0", "5.1.6.191", berlin),
				run("5.1.6.192", "5.1.6.255", cloud),
			},
			want: []liteRun{
				run("5.1.6.0", "5.1.6.191", berlin),
				run("5.1.6.192", "5.1.6.255", cloud),
			},
			kept: 1,
		},
		{
			name: "blocks with gaps are left alone",
			bits: 24,
			in: []liteRun{
				run("5.1.5.0", "5.1.5.127", berlin),
				run("5.1.5.128", "5.1.5.191", munich),
			},
			want: []liteRun{
				run("5.1.5.0", "5.1.5.127", berlin),
				run("5.1.5.128", "5.1.5.191", munich),
			},
			kept: 1,
		},
		{
			name: "parts without a location are left alone",
			bits: 24,
			in: []liteRun{
				run("5.1.7.0", "5.1.7.127", berlin),
				run("5.1.7.128", "5.1.7.255", noLoc),
			},
			want: []liteRun{
				run("5.1.7.0", "5.1.7.127", berlin),
				run("5.1.7.128", "5.1.7.255", noLoc),
			},
			kept: 1,
		},
		{
			name: "runs spanning whole blocks pass through and partial ends are merged",
			bits: 24,
			in: []liteRun{
				run("5.1.8.0", "5.1.10.199", berlin), // 5.1.8.0/24, 5.1.9.0/24 whole, then 200 of 5.1.10.0/24
				run("5.1.10.200", "5.1.11.9", munich),
				run("5.1.11.10", "5.1.11.255", berlin),
			},
			want:   []liteRun{run("5.1.8.0", "5.1.11.255", berlin)},
			merged: 2,
		},
		{
			name: "a minority part reaching only with its own large radius",
			bits: 24,
			in: []liteRun{
				run("5.1.12.0", "5.1.12.99", far),       // 100 addresses, radius 500
				run("5.1.12.100", "5.1.12.255", munich), // 156 addresses, radius 50
			},
			// Munich holds 61 % < 2/3, so Berlin's part is needed:
			// distance Munich-Berlin (~520 km) + its own 500 km -> tier 1000.
			want:   []liteRun{run("5.1.12.0", "5.1.12.255", liteKey{country: "DE", hasLoc: true, lat: 96, lon: 23, radius: 1000})},
			merged: 1,
		},
		{
			name: "aggregation off",
			bits: 0,
			in: []liteRun{
				run("5.1.2.0", "5.1.2.127", berlin),
				run("5.1.2.128", "5.1.2.255", munich),
			},
			want: []liteRun{
				run("5.1.2.0", "5.1.2.127", berlin),
				run("5.1.2.128", "5.1.2.255", munich),
			},
		},
		{
			name: "IPv6 /40 blocks",
			bits: 40,
			in: []liteRun{
				run("2a02:1::", "2a02:1:7f:ffff:ffff:ffff:ffff:ffff", berlin),
				run("2a02:1:80::", "2a02:1:bf:ffff:ffff:ffff:ffff:ffff", munich),
				run("2a02:1:c0::", "2a02:2:ff:ffff:ffff:ffff:ffff:ffff", berlin),
			},
			want:   []liteRun{run("2a02:1::", "2a02:2:ff:ffff:ffff:ffff:ffff:ffff", berlin)},
			merged: 1,
		},
	}
	for _, c := range cases {
		got, st := aggregateLite(c.in, c.bits, 0.5, testTiers)
		if runsString(got) != runsString(c.want) {
			t.Errorf("%s:\n got  %s\n want %s", c.name, runsString(got), runsString(c.want))
		}
		if st.MergedBlocks != c.merged || st.KeptBlocks != c.kept || st.RunsBefore != len(c.in) || st.RunsAfter != len(got) {
			t.Errorf("%s: stats %+v", c.name, st)
		}
	}
}

func TestAggregateLiteMovedPercent(t *testing.T) {
	berlin, munich := lk("DE", 52.5, 13.5), lk("DE", 48, 11.5)
	in := []liteRun{
		run("5.1.2.0", "5.1.2.191", berlin),
		run("5.1.2.192", "5.1.2.255", munich), // 64 of 512 addresses move
		run("5.1.3.0", "5.1.3.255", munich),
	}
	_, st := aggregateLite(in, 24, 0.5, testTiers)
	if st.MovedPercent != 12.5 {
		t.Errorf("moved = %v, want 12.5", st.MovedPercent)
	}
}

// TestAggregateLiteInvariants checks random inputs: the output covers
// exactly the input addresses, stays sorted and joined, never changes a
// country or network flags, and only changes locations inside split blocks.
func TestAggregateLiteInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	countries := []string{"CN", "HK", "DE"}
	merged, kept := 0, 0
	for iter := 0; iter < 300; iter++ {
		var in []liteRun
		a := netip.MustParseAddr("10.0.0.0").As4()
		cur := uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
		for i := 0; i < 40; i++ {
			if rng.IntN(6) == 0 {
				cur += uint32(rng.IntN(300)) // gap
			}
			n := uint32(1 + rng.IntN(700))
			k := liteKey{country: countries[rng.IntN(3)], hasLoc: rng.IntN(10) > 0, lat: int32(rng.IntN(3)), lon: int32(rng.IntN(3)), radius: testTiers[rng.IntN(len(testTiers))]}
			if k.hasLoc && rng.IntN(8) == 0 {
				k.cloud = "aws"
			}
			if !k.hasLoc {
				k.lat, k.lon, k.radius = 0, 0, 0
			}
			r := liteRun{u32(cur), u32(cur + n - 1), k}
			if m := len(in); m > 0 && in[m-1].k == k && in[m-1].end.Next() == r.start {
				in[m-1].end = r.end
			} else {
				in = append(in, r)
			}
			cur += n
		}
		out, st := aggregateLite(in, 24, 0.5, testTiers)
		merged += st.MergedBlocks
		kept += st.KeptBlocks
		for i := 1; i < len(out); i++ {
			if out[i].start.Compare(out[i-1].end) <= 0 {
				t.Fatalf("iter %d: output not sorted: %s", iter, runsString(out))
			}
			if out[i].k == out[i-1].k && out[i-1].end.Next() == out[i].start {
				t.Fatalf("iter %d: adjacent equal runs not joined", iter)
			}
		}
		// Compare address by address.
		lookup := func(rs []liteRun, x netip.Addr) (liteKey, bool) {
			i := sort.Search(len(rs), func(i int) bool { return rs[i].end.Compare(x) >= 0 })
			if i < len(rs) && rs[i].start.Compare(x) <= 0 {
				return rs[i].k, true
			}
			return liteKey{}, false
		}
		for _, r := range in {
			for x := r.start; ; x = x.Next() {
				got, ok := lookup(out, x)
				if !ok {
					t.Fatalf("iter %d: %s lost", iter, x)
				}
				if got.country != r.k.country || got.cloud != r.k.cloud || got.anycast != r.k.anycast || got.cdn != r.k.cdn || got.hasLoc != r.k.hasLoc {
					t.Fatalf("iter %d: %s changed country/flags: %+v -> %+v", iter, x, r.k, got)
				}
				if got != r.k {
					b := blockOf(x, 24)
					if r.start.Compare(b.Start) <= 0 && b.End.Compare(r.end) <= 0 {
						t.Fatalf("iter %d: %s changed although its block was not split", iter, x)
					}
				}
				if x == r.end {
					break
				}
			}
		}
		for _, r := range out {
			for x := r.start; ; x = x.Next() {
				if _, ok := lookup(in, x); !ok {
					t.Fatalf("iter %d: %s was not in the input", iter, x)
				}
				if x == r.end {
					break
				}
			}
		}
	}
	if merged == 0 || kept == 0 {
		t.Errorf("random inputs did not exercise both paths: merged %d, kept %d", merged, kept)
	}
}

func u32(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
