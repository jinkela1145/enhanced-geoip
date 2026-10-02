// Package iprange provides inclusive IP ranges, sorted non-overlapping segment
// lists and the sweep-line helpers used to overlay data layers.
//
// All functions expect every range in one call to belong to the same address
// family; callers process IPv4 and IPv6 separately.
package iprange

import (
	"cmp"
	"errors"
	"fmt"
	"math/bits"
	"net/netip"
	"slices"
	"strings"
)

// Range is an inclusive range of addresses of one family.
type Range struct {
	Start, End netip.Addr
}

// Seg is a range carrying a value.
type Seg[V any] struct {
	Start, End netip.Addr
	Val        V
}

// Range returns the segment's address range.
func (s Seg[V]) Range() Range { return Range{s.Start, s.End} }

// Valid reports whether r is well formed.
func (r Range) Valid() bool {
	return r.Start.IsValid() && r.End.IsValid() && r.Start.Is4() == r.End.Is4() &&
		r.Start.Compare(r.End) <= 0
}

// Is4 reports whether r is an IPv4 range.
func (r Range) Is4() bool { return r.Start.Is4() }

// Contains reports whether a is inside r.
func (r Range) Contains(a netip.Addr) bool {
	return a.Is4() == r.Start.Is4() && r.Start.Compare(a) <= 0 && a.Compare(r.End) <= 0
}

// Overlaps reports whether r and o share at least one address.
func (r Range) Overlaps(o Range) bool {
	return r.Is4() == o.Is4() && r.Start.Compare(o.End) <= 0 && o.Start.Compare(r.End) <= 0
}

func (r Range) String() string { return r.Start.String() + "-" + r.End.String() }

// FromPrefix converts a prefix to a range. IPv4-mapped IPv6 prefixes are
// unmapped so that every IPv4 address is represented in 4-byte form.
func FromPrefix(p netip.Prefix) Range {
	p = p.Masked()
	a := p.Addr()
	bitsLen := p.Bits()
	if a.Is4In6() && bitsLen >= 96 {
		a = a.Unmap()
		bitsLen -= 96
	}
	return Range{a, lastAddr(a, bitsLen)}
}

func lastAddr(a netip.Addr, prefixLen int) netip.Addr {
	if a.Is4() {
		b := a.As4()
		v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		if prefixLen < 32 {
			v |= ^uint32(0) >> prefixLen
		}
		return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	u := toU128(a)
	host := U128{^uint64(0), ^uint64(0)}.Rsh(uint(prefixLen))
	if prefixLen >= 128 {
		host = U128{}
	}
	return fromU128(U128{u.Hi | host.Hi, u.Lo | host.Lo})
}

// Parse accepts "CIDR", "start-end" or a single address.
func Parse(s string) (Range, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Range{}, errors.New("empty range")
	}
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return Range{}, err
		}
		if p.Masked() != p {
			return Range{}, fmt.Errorf("%s has host bits set", s)
		}
		return FromPrefix(p), nil
	}
	if a, b, ok := strings.Cut(s, "-"); ok {
		start, err := netip.ParseAddr(strings.TrimSpace(a))
		if err != nil {
			return Range{}, err
		}
		end, err := netip.ParseAddr(strings.TrimSpace(b))
		if err != nil {
			return Range{}, err
		}
		r := Range{start.Unmap(), end.Unmap()}
		if !r.Valid() {
			return Range{}, fmt.Errorf("invalid range %s", s)
		}
		return r, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return Range{}, err
	}
	a = a.Unmap()
	return Range{a, a}, nil
}

// MaxAddr returns the highest address of the family of a.
func MaxAddr(is4 bool) netip.Addr {
	if is4 {
		return netip.AddrFrom4([4]byte{255, 255, 255, 255})
	}
	return fromU128(U128{^uint64(0), ^uint64(0)})
}

// U128 is an unsigned 128-bit integer used to count addresses.
type U128 struct{ Hi, Lo uint64 }

// Add returns a+b (wrapping on overflow, which cannot happen for address
// counts of a single family below 2^128).
func (a U128) Add(b U128) U128 {
	lo, c := bits.Add64(a.Lo, b.Lo, 0)
	hi, _ := bits.Add64(a.Hi, b.Hi, c)
	return U128{hi, lo}
}

// Sub returns a-b.
func (a U128) Sub(b U128) U128 {
	lo, br := bits.Sub64(a.Lo, b.Lo, 0)
	hi, _ := bits.Sub64(a.Hi, b.Hi, br)
	return U128{hi, lo}
}

// Rsh returns a >> n.
func (a U128) Rsh(n uint) U128 {
	switch {
	case n == 0:
		return a
	case n >= 128:
		return U128{}
	case n >= 64:
		return U128{0, a.Hi >> (n - 64)}
	default:
		return U128{a.Hi >> n, a.Lo>>n | a.Hi<<(64-n)}
	}
}

// IsZero reports whether a == 0.
func (a U128) IsZero() bool { return a.Hi == 0 && a.Lo == 0 }

// Float64 converts a to float64 (approximately for large values).
func (a U128) Float64() float64 { return float64(a.Hi)*18446744073709551616.0 + float64(a.Lo) }

func toU128(a netip.Addr) U128 {
	b := a.As16()
	var hi, lo uint64
	for i := 0; i < 8; i++ {
		hi = hi<<8 | uint64(b[i])
		lo = lo<<8 | uint64(b[i+8])
	}
	return U128{hi, lo}
}

func fromU128(u U128) netip.Addr {
	var b [16]byte
	for i := 0; i < 8; i++ {
		b[7-i] = byte(u.Hi >> (8 * i))
		b[15-i] = byte(u.Lo >> (8 * i))
	}
	return netip.AddrFrom16(b)
}

// Size returns the number of addresses in r.
func (r Range) Size() U128 {
	if r.Is4() {
		s, e := r.Start.As4(), r.End.As4()
		sv := uint64(s[0])<<24 | uint64(s[1])<<16 | uint64(s[2])<<8 | uint64(s[3])
		ev := uint64(e[0])<<24 | uint64(e[1])<<16 | uint64(e[2])<<8 | uint64(e[3])
		return U128{0, ev - sv + 1}
	}
	return toU128(r.End).Sub(toU128(r.Start)).Add(U128{0, 1})
}

// Prefixes returns the minimal list of CIDR prefixes that exactly cover r.
func (r Range) Prefixes() []netip.Prefix {
	var out []netip.Prefix
	total := 128
	if r.Is4() {
		total = 32
	}
	start, end := r.Start, r.End
	for {
		// Largest block aligned at start that does not pass end.
		var tz int
		if r.Is4() {
			b := start.As4()
			v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
			tz = bits.TrailingZeros32(v)
			if v == 0 {
				tz = 32
			}
		} else {
			u := toU128(start)
			switch {
			case u.Lo != 0:
				tz = bits.TrailingZeros64(u.Lo)
			case u.Hi != 0:
				tz = 64 + bits.TrailingZeros64(u.Hi)
			default:
				tz = 128
			}
		}
		bitsLen := total - tz
		for {
			p := netip.PrefixFrom(start, bitsLen)
			if lastAddr(start, bitsLen).Compare(end) <= 0 {
				out = append(out, p)
				break
			}
			bitsLen++
		}
		last := lastAddr(start, bitsLen)
		if last.Compare(end) >= 0 {
			return out
		}
		start = last.Next()
	}
}

// SortSegs sorts segments by start address.
func SortSegs[V any](segs []Seg[V]) {
	slices.SortFunc(segs, func(a, b Seg[V]) int { return a.Start.Compare(b.Start) })
}

// AppendMerged appends s to segs, extending the last segment instead when it
// is directly adjacent and carries an equal value.
func AppendMerged[V comparable](segs []Seg[V], s Seg[V]) []Seg[V] {
	if n := len(segs); n > 0 {
		last := &segs[n-1]
		if last.Val == s.Val && last.End.Next() == s.Start && last.End.Is4() == s.Start.Is4() {
			last.End = s.End
			return segs
		}
	}
	return append(segs, s)
}

// CheckSorted returns an error unless segs are sorted and non-overlapping.
func CheckSorted[V any](segs []Seg[V]) error {
	for i := 1; i < len(segs); i++ {
		if segs[i].Start.Compare(segs[i-1].End) <= 0 {
			return fmt.Errorf("segments overlap or are unsorted: %s-%s then %s-%s",
				segs[i-1].Start, segs[i-1].End, segs[i].Start, segs[i].End)
		}
	}
	return nil
}

// Entry is an input to Flatten. Entries may overlap.
type Entry[V any] struct {
	R    Range
	Val  V
	Rank int // entries are folded in ascending Rank order (then input order)
}

// Flatten turns possibly overlapping entries into sorted, non-overlapping
// segments. For every piece of address space, fold receives the values of all
// entries covering it, ordered by ascending Rank; returning ok=false leaves the
// piece out. Adjacent pieces with equal results are merged.
func Flatten[V any, R comparable](entries []Entry[V], fold func([]V) (R, bool)) []Seg[R] {
	if len(entries) == 0 {
		return nil
	}
	type event struct {
		at  netip.Addr
		idx int
		add bool
	}
	evs := make([]event, 0, 2*len(entries))
	for i, e := range entries {
		evs = append(evs, event{e.R.Start, i, true})
		if e.R.End != MaxAddr(e.R.Is4()) {
			evs = append(evs, event{e.R.End.Next(), i, false})
		}
	}
	slices.SortStableFunc(evs, func(a, b event) int { return a.at.Compare(b.at) })
	is4 := entries[0].R.Is4()
	active := map[int]struct{}{}
	var out []Seg[R]
	order := make([]int, 0, 8)
	vals := make([]V, 0, 8)
	for i := 0; i < len(evs); {
		at := evs[i].at
		for i < len(evs) && evs[i].at == at {
			if evs[i].add {
				active[evs[i].idx] = struct{}{}
			} else {
				delete(active, evs[i].idx)
			}
			i++
		}
		if len(active) == 0 {
			continue
		}
		end := MaxAddr(is4)
		if i < len(evs) {
			end = evs[i].at.Prev()
		}
		order = order[:0]
		for idx := range active {
			order = append(order, idx)
		}
		slices.SortFunc(order, func(a, b int) int {
			if c := cmp.Compare(entries[a].Rank, entries[b].Rank); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		})
		vals = vals[:0]
		for _, idx := range order {
			vals = append(vals, entries[idx].Val)
		}
		if v, ok := fold(vals); ok {
			out = AppendMerged(out, Seg[R]{Start: at, End: end, Val: v})
		}
	}
	return out
}

// Subtract removes the holes (sorted, non-overlapping, same family) from
// segs (sorted, non-overlapping).
func Subtract[V any](segs []Seg[V], holes []Range) []Seg[V] {
	if len(holes) == 0 {
		return segs
	}
	out := make([]Seg[V], 0, len(segs))
	h := 0
	for _, s := range segs {
		cur := s
		for {
			for h < len(holes) && holes[h].End.Compare(cur.Start) < 0 {
				h++
			}
			if h >= len(holes) || holes[h].Start.Compare(cur.End) > 0 {
				out = append(out, cur)
				break
			}
			hole := holes[h]
			if hole.Start.Compare(cur.Start) > 0 {
				out = append(out, Seg[V]{Start: cur.Start, End: hole.Start.Prev(), Val: cur.Val})
			}
			if hole.End.Compare(cur.End) >= 0 {
				break
			}
			cur.Start = hole.End.Next()
		}
	}
	return out
}

// Spans is a read-only view of a sorted, non-overlapping list of ranges.
type Spans interface {
	Len() int
	Span(i int) (start, end netip.Addr)
}

// SegSpans adapts a segment slice to Spans.
type SegSpans[V any] []Seg[V]

// Len implements Spans.
func (s SegSpans[V]) Len() int { return len(s) }

// Span implements Spans.
func (s SegSpans[V]) Span(i int) (netip.Addr, netip.Addr) { return s[i].Start, s[i].End }

// RangeSpans adapts a range slice to Spans.
type RangeSpans []Range

// Len implements Spans.
func (s RangeSpans) Len() int { return len(s) }

// Span implements Spans.
func (s RangeSpans) Span(i int) (netip.Addr, netip.Addr) { return s[i].Start, s[i].End }

// Refine walks the base spans and splits them at every boundary of every
// layer. For each resulting piece it calls emit with the base index and, per
// layer, the index of the covering span or -1. The layerIdx slice is reused
// between calls. Address space not covered by base is never emitted.
func Refine(base Spans, layers []Spans, emit func(start, end netip.Addr, baseIdx int, layerIdx []int)) {
	cur := make([]int, len(layers))
	idx := make([]int, len(layers))
	for bi := 0; bi < base.Len(); bi++ {
		bs, be := base.Span(bi)
		p := bs
		for {
			nb := be
			for k, l := range layers {
				c, n := cur[k], l.Len()
				for c < n {
					if _, e := l.Span(c); e.Compare(p) < 0 {
						c++
						continue
					}
					break
				}
				cur[k] = c
				idx[k] = -1
				if c < n {
					s, e := l.Span(c)
					if s.Compare(p) <= 0 {
						idx[k] = c
						if e.Compare(nb) < 0 {
							nb = e
						}
					} else if prev := s.Prev(); prev.Compare(nb) < 0 {
						nb = prev
					}
				}
			}
			emit(p, nb, bi, idx)
			if nb.Compare(be) >= 0 {
				break
			}
			p = nb.Next()
		}
	}
}
