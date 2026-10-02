package sources

import (
	"net/netip"
	"slices"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// Networks never written to the databases. The IPv4 list mirrors the reserved
// networks of mmdbwriter (inserting into them is an error). For IPv6 only the
// global unicast space 2000::/3 is kept, minus documentation, the reserved
// parts of 2001::/23, Teredo (2001::/32) and 6to4 (2002::/16); the last two
// are aliased to the IPv4 tree by mmdbwriter, as in MaxMind's own databases.
var (
	excludedV4 = []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/29", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	}
	excludedV6 = []string{
		"::-1fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"2001::/23",
		"2001:db8::/32",
		"2002::/16",
		"4000::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	}
)

// Excluded returns the sorted, merged list of excluded ranges for a family.
func Excluded(is4 bool) []iprange.Range {
	src := excludedV6
	if is4 {
		src = excludedV4
	}
	var rs []iprange.Range
	for _, s := range src {
		r, err := iprange.Parse(s)
		if err != nil {
			panic(err)
		}
		rs = append(rs, r)
	}
	slices.SortFunc(rs, func(a, b iprange.Range) int { return a.Start.Compare(b.Start) })
	// merge overlaps
	out := rs[:0]
	for _, r := range rs {
		if n := len(out); n > 0 && r.Start.Compare(out[n-1].End.Next()) <= 0 {
			if r.End.Compare(out[n-1].End) > 0 {
				out[n-1].End = r.End
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// IsExcluded reports whether a falls into an excluded range.
func IsExcluded(a netip.Addr) bool {
	a = a.Unmap()
	for _, r := range Excluded(a.Is4()) {
		if r.Contains(a) {
			return true
		}
	}
	return false
}
