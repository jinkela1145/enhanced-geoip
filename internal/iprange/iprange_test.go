package iprange

import (
	"math/rand"
	"net/netip"
	"testing"
)

func mustRange(t *testing.T, s string) Range {
	t.Helper()
	r, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return r
}

func TestParse(t *testing.T) {
	cases := map[string]string{
		"1.2.3.0/24":          "1.2.3.0-1.2.3.255",
		"10.0.0.1 - 10.0.0.9": "10.0.0.1-10.0.0.9",
		"8.8.8.8":             "8.8.8.8-8.8.8.8",
		"2001:db8::/32":       "2001:db8::-2001:db8:ffff:ffff:ffff:ffff:ffff:ffff",
		"::ffff:1.2.3.0/120":  "1.2.3.0-1.2.3.255",
		"0.0.0.0/0":           "0.0.0.0-255.255.255.255",
		"2400:3200::-2400:3200:ffff:ffff:ffff:ffff:ffff:ffff": "2400:3200::-2400:3200:ffff:ffff:ffff:ffff:ffff:ffff",
	}
	for in, want := range cases {
		if got := mustRange(t, in).String(); got != want {
			t.Errorf("Parse(%q) = %s, want %s", in, got, want)
		}
	}
	for _, bad := range []string{"", "1.2.3.4/24", "1.2.3.9-1.2.3.1", "1.2.3.4-::1", "nonsense"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestSize(t *testing.T) {
	if got := mustRange(t, "1.0.0.0/8").Size(); got != (U128{0, 1 << 24}) {
		t.Errorf("size /8 = %v", got)
	}
	if got := mustRange(t, "2001:db8::/32").Size(); got != (U128{1 << 32, 0}) {
		t.Errorf("size /32 = %v", got)
	}
	if got := mustRange(t, "::/0").Size(); got != (U128{0, 0}) {
		// 2^128 wraps to zero; documented limitation, never used for a full family.
		t.Errorf("size ::/0 = %v", got)
	}
}

// brute force check: prefixes cover exactly the range, are aligned and minimal.
func checkPrefixes(t *testing.T, r Range) {
	t.Helper()
	ps := r.Prefixes()
	if len(ps) == 0 {
		t.Fatalf("no prefixes for %s", r)
	}
	next := r.Start
	for i, p := range ps {
		pr := FromPrefix(p)
		if pr.Start != next {
			t.Fatalf("%s: prefix %d (%s) starts at %s, want %s", r, i, p, pr.Start, next)
		}
		if p.Masked() != p {
			t.Fatalf("%s: prefix %s not aligned", r, p)
		}
		// minimality: the block could not have been one bit larger
		if p.Bits() > 0 {
			bigger := netip.PrefixFrom(p.Addr(), p.Bits()-1).Masked()
			br := FromPrefix(bigger)
			if bigger.Addr() == p.Addr() && br.End.Compare(r.End) <= 0 {
				t.Fatalf("%s: prefix %s could be %s", r, p, bigger)
			}
		}
		next = pr.End.Next()
	}
	last := FromPrefix(ps[len(ps)-1]).End
	if last != r.End {
		t.Fatalf("%s: prefixes end at %s", r, last)
	}
}

func TestPrefixesRandomV4(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 3000; i++ {
		a := rng.Uint32()
		b := a + uint32(rng.Intn(1<<uint(rng.Intn(20))))
		if b < a {
			b = ^uint32(0)
		}
		r := Range{u32(a), u32(b)}
		checkPrefixes(t, r)
	}
	checkPrefixes(t, mustRange(t, "0.0.0.0-255.255.255.255"))
	checkPrefixes(t, mustRange(t, "255.255.255.255"))
}

func TestPrefixesV6(t *testing.T) {
	for _, s := range []string{
		"2001:db8::1-2001:db8::ff",
		"2001:db8::-2001:db9::5",
		"::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"2400:3200::7-2400:3201::",
		"ffff:ffff:ffff:ffff:ffff:ffff:ffff:fff0-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	} {
		checkPrefixes(t, mustRange(t, s))
	}
}

func u32(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func TestAppendMerged(t *testing.T) {
	var s []Seg[int]
	s = AppendMerged(s, Seg[int]{u32(0), u32(9), 1})
	s = AppendMerged(s, Seg[int]{u32(10), u32(19), 1}) // merges
	s = AppendMerged(s, Seg[int]{u32(21), u32(29), 1}) // gap, no merge
	s = AppendMerged(s, Seg[int]{u32(30), u32(39), 2}) // other value
	if len(s) != 3 || s[0].End != u32(19) {
		t.Fatalf("unexpected merge result %+v", s)
	}
}

// pointValue returns the folded value at address v using brute force.
func TestFlattenMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for round := 0; round < 200; round++ {
		var entries []Entry[int]
		for i := 0; i < 1+rng.Intn(8); i++ {
			a := uint32(rng.Intn(200))
			b := a + uint32(rng.Intn(60))
			entries = append(entries, Entry[int]{R: Range{u32(a), u32(b)}, Val: i, Rank: rng.Intn(3)})
		}
		// fold: value of the highest rank (last), i.e. "most specific wins".
		segs := Flatten(entries, func(v []int) (int, bool) { return v[len(v)-1], true })
		if err := CheckSorted(segs); err != nil {
			t.Fatal(err)
		}
		for x := uint32(0); x < 300; x++ {
			// brute force
			best, bestRank, found := -1, -1, false
			for i, e := range entries {
				if e.R.Contains(u32(x)) && (e.Rank > bestRank || (e.Rank == bestRank && i > best)) {
					best, bestRank, found = i, e.Rank, true
				}
			}
			got, gotFound := -1, false
			for _, s := range segs {
				if s.Range().Contains(u32(x)) {
					got, gotFound = s.Val, true
				}
			}
			if found != gotFound || (found && entries[best].Val != got) {
				t.Fatalf("round %d addr %d: got (%d,%v) want (%d,%v)", round, x, got, gotFound, best, found)
			}
		}
	}
}

func TestFlattenMaxAddr(t *testing.T) {
	entries := []Entry[string]{
		{R: mustRange(t, "255.255.255.0/24"), Val: "a"},
		{R: mustRange(t, "255.255.255.128/25"), Val: "b", Rank: 1},
	}
	segs := Flatten(entries, func(v []string) (string, bool) { return v[len(v)-1], true })
	if len(segs) != 2 || segs[1].End != MaxAddr(true) || segs[1].Val != "b" {
		t.Fatalf("unexpected %+v", segs)
	}
}

func TestSubtract(t *testing.T) {
	segs := []Seg[int]{
		{u32(0), u32(99), 1},
		{u32(200), u32(299), 2},
	}
	holes := []Range{{u32(10), u32(19)}, {u32(90), u32(210)}, {u32(250), u32(400)}}
	got := Subtract(segs, holes)
	want := []Seg[int]{{u32(0), u32(9), 1}, {u32(20), u32(89), 1}, {u32(211), u32(249), 2}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seg %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestRefine(t *testing.T) {
	base := SegSpans[int]{{u32(0), u32(99), 0}, {u32(100), u32(199), 1}, {u32(300), u32(399), 2}}
	layerA := SegSpans[string]{{u32(50), u32(149), "a"}}
	layerB := RangeSpans{{u32(120), u32(130)}, {u32(350), u32(1000)}}
	type piece struct {
		s, e, b, a, bb int
	}
	var got []piece
	Refine(base, []Spans{layerA, layerB}, func(s, e netip.Addr, bi int, li []int) {
		got = append(got, piece{int(s.As4()[3]) + int(s.As4()[2])*256, int(e.As4()[3]) + int(e.As4()[2])*256, bi, li[0], li[1]})
	})
	want := []piece{
		{0, 49, 0, -1, -1},
		{50, 99, 0, 0, -1},
		{100, 119, 1, 0, -1},
		{120, 130, 1, 0, 0},
		{131, 149, 1, 0, -1},
		{150, 199, 1, -1, -1},
		{300, 349, 2, -1, -1},
		{350, 399, 2, -1, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("piece %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestRefineV6Boundaries(t *testing.T) {
	base := SegSpans[int]{{mustRange(t, "2001:db8::/32").Start, mustRange(t, "2001:db8::/32").End, 0}}
	l := RangeSpans{mustRange(t, "2001:db8:1::/48")}
	n := 0
	var total U128
	Refine(base, []Spans{l}, func(s, e netip.Addr, bi int, li []int) {
		n++
		total = total.Add(Range{s, e}.Size())
	})
	if n != 3 || total != mustRange(t, "2001:db8::/32").Size() {
		t.Fatalf("pieces=%d total=%v", n, total)
	}
}
