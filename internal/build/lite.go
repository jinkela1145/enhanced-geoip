package build

import (
	"math"
	"net/netip"
	"slices"

	"github.com/jinkela1145/enhanced-geoip/internal/geo"
	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// liteRun is a maximal run of addresses that share one Lite value.
type liteRun struct {
	start, end netip.Addr
	k          liteKey
}

// LiteAggStats describes what aggregateLite did to one address family.
type LiteAggStats struct {
	// PrefixLen is the block size; 0 means aggregation was off.
	PrefixLen  int `json:"prefix_len"`
	RunsBefore int `json:"runs_before"`
	RunsAfter  int `json:"runs_after"`
	// MergedBlocks were split between several locations and now carry one.
	MergedBlocks int `json:"merged_blocks"`
	// KeptBlocks were split but left alone because their parts differ in
	// country or network flags, lack a location, or leave gaps.
	KeptBlocks int `json:"kept_blocks"`
	// MovedPercent is the share of the family's addresses whose Lite
	// location changed through aggregation.
	MovedPercent float64 `json:"moved_percent"`
}

// liteConfidence is the share of a merged block that must lie within the
// accuracy radius around the chosen location. MaxMind defines
// accuracy_radius with 67 % confidence; we follow that.
const liteConfidence = 2.0 / 3.0

// aggregateLite gives every block of the given prefix length that is split
// between several locations a single location: the rounded location that
// covers most of the block's addresses. The accuracy radius is widened until
// it covers two thirds of the block (counting each part's own radius), so it
// stays honest. A block is only merged when all its parts have the same
// country, the same network flags and a location, and cover the block
// without gaps; otherwise it is left exactly as it was. Countries and
// network flags therefore never change.
//
// runs must be sorted, non-overlapping and of one address family; adjacent
// runs with equal keys must already be joined.
func aggregateLite(runs []liteRun, bits int, step float64, tiers []uint16) ([]liteRun, LiteAggStats) {
	st := LiteAggStats{PrefixLen: bits, RunsBefore: len(runs), RunsAfter: len(runs)}
	if bits <= 0 || len(runs) == 0 {
		return runs, st
	}
	if runs[0].start.Is4() && bits > 32 {
		bits = 32
	}
	a := &liteAgg{bits: bits, step: step, tiers: tiers, out: make([]liteRun, 0, len(runs))}
	for _, r := range runs {
		a.total += rangeSize(r.start, r.end)
		s := r.start
		for {
			b := blockOf(s, bits)
			e := r.end
			if e.Compare(b.End) > 0 {
				e = b.End
			}
			if s == b.Start && e == b.End {
				// r covers this block and maybe more whole blocks: copy
				// them through in one piece.
				a.flush()
				last := r.end
				if lb := blockOf(r.end, bits); lb.End != r.end {
					last = lb.Start.Prev()
				}
				a.emit(liteRun{s, last, r.k})
				if last == r.end {
					break
				}
				s = last.Next()
				continue
			}
			if len(a.parts) > 0 && a.block != b {
				a.flush()
			}
			a.block = b
			a.parts = append(a.parts, liteRun{s, e, r.k})
			if e == r.end {
				break
			}
			s = e.Next()
		}
	}
	a.flush()
	a.st.PrefixLen, a.st.RunsBefore, a.st.RunsAfter = bits, len(runs), len(a.out)
	if a.total > 0 {
		a.st.MovedPercent = round3(100 * a.moved / a.total)
	}
	return a.out, a.st
}

type liteAgg struct {
	bits         int
	step         float64
	tiers        []uint16
	out          []liteRun
	st           LiteAggStats
	total, moved float64

	block iprange.Range // block the pending parts belong to
	parts []liteRun
}

func (a *liteAgg) emit(r liteRun) {
	if n := len(a.out); n > 0 && a.out[n-1].k == r.k && a.out[n-1].end.Next() == r.start {
		a.out[n-1].end = r.end
		return
	}
	a.out = append(a.out, r)
}

func (a *liteAgg) flush() {
	parts := a.parts
	if len(parts) == 0 {
		return
	}
	a.parts = a.parts[:0]
	if len(parts) == 1 || !a.mergeable(parts) {
		if len(parts) > 1 {
			a.st.KeptBlocks++
		}
		for _, p := range parts {
			a.emit(p)
		}
		return
	}
	k, moved := a.merge(parts)
	a.moved += moved
	a.st.MergedBlocks++
	a.emit(liteRun{a.block.Start, a.block.End, k})
}

func (a *liteAgg) mergeable(parts []liteRun) bool {
	if parts[0].start != a.block.Start || parts[len(parts)-1].end != a.block.End {
		return false
	}
	k0 := parts[0].k
	for i, p := range parts {
		if i > 0 && parts[i-1].end.Next() != p.start {
			return false
		}
		k := p.k
		if !k.hasLoc || k.country != k0.country || k.anycast != k0.anycast || k.cdn != k0.cdn || k.cloud != k0.cloud {
			return false
		}
	}
	return true
}

// merge picks the location of a mergeable block and the radius that covers
// liteConfidence of it. It also returns the number of addresses that moved.
func (a *liteAgg) merge(parts []liteRun) (liteKey, float64) {
	type cell struct {
		lat, lon int32
		w        float64
	}
	var cells []cell // first-seen order keeps ties deterministic
	weights := make([]float64, len(parts))
	total := 0.0
	for i, p := range parts {
		w := rangeSize(p.start, p.end)
		weights[i] = w
		total += w
		found := false
		for j := range cells {
			if cells[j].lat == p.k.lat && cells[j].lon == p.k.lon {
				cells[j].w += w
				found = true
				break
			}
		}
		if !found {
			cells = append(cells, cell{p.k.lat, p.k.lon, w})
		}
	}
	best := cells[0]
	for _, c := range cells[1:] {
		if c.w > best.w {
			best = c
		}
	}
	lat, lon := cleanZero(float64(best.lat)*a.step), cleanZero(float64(best.lon)*a.step)
	type reach struct{ d, w float64 }
	reaches := make([]reach, len(parts))
	moved := 0.0
	for i, p := range parts {
		d := float64(p.k.radius)
		if p.k.lat != best.lat || p.k.lon != best.lon {
			d += geo.Haversine(lat, lon, cleanZero(float64(p.k.lat)*a.step), cleanZero(float64(p.k.lon)*a.step))
			moved += weights[i]
		}
		reaches[i] = reach{d, weights[i]}
	}
	slices.SortStableFunc(reaches, func(x, y reach) int {
		switch {
		case x.d < y.d:
			return -1
		case x.d > y.d:
			return 1
		}
		return 0
	})
	need := liteConfidence * total * (1 - 1e-9)
	covered, radius := 0.0, 0.0
	for _, r := range reaches {
		covered += r.w
		radius = r.d
		if covered >= need {
			break
		}
	}
	k := parts[0].k
	k.lat, k.lon = best.lat, best.lon
	k.radius = radiusTier(uint16(math.Min(math.Ceil(radius), math.MaxUint16)), a.tiers)
	return k, moved
}

func blockOf(a netip.Addr, bits int) iprange.Range {
	return iprange.FromPrefix(netip.PrefixFrom(a, bits).Masked())
}

func rangeSize(s, e netip.Addr) float64 {
	return iprange.Range{Start: s, End: e}.Size().Float64()
}
