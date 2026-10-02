package sources

import (
	"bufio"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// ReadDelegated parses an RIR statistics exchange file (for example APNIC's
// delegated-apnic-extended-latest) and returns the allocated or assigned
// ranges of one country, sorted and merged, per family.
func ReadDelegated(path, country string) (v4, v6 []iprange.Range, err error) {
	f, err := Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	seenHeader := false
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		cols := strings.Split(text, "|")
		if !seenHeader {
			// version line: 2|apnic|20261002|...
			seenHeader = true
			if len(cols) >= 2 && (cols[0] == "2" || cols[0] == "2.3") {
				continue
			}
		}
		if len(cols) < 7 {
			if len(cols) >= 6 && cols[5] == "summary" {
				continue
			}
			return nil, nil, fmt.Errorf("delegated line %d: expected at least 7 fields", line)
		}
		if cols[1] == "*" || cols[5] == "summary" {
			continue
		}
		if !strings.EqualFold(cols[1], country) {
			continue
		}
		status := cols[6]
		if status != "allocated" && status != "assigned" {
			continue
		}
		if cols[2] != "ipv4" && cols[2] != "ipv6" { // e.g. asn records
			continue
		}
		start, err := netip.ParseAddr(cols[3])
		if err != nil {
			return nil, nil, fmt.Errorf("delegated line %d: %w", line, err)
		}
		switch cols[2] {
		case "ipv4":
			n, err := strconv.ParseUint(cols[4], 10, 32)
			if err != nil || n == 0 {
				return nil, nil, fmt.Errorf("delegated line %d: bad count %q", line, cols[4])
			}
			b := start.As4()
			v := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
			e := v + n - 1
			if e > 0xffffffff {
				return nil, nil, fmt.Errorf("delegated line %d: range overflows", line)
			}
			end := netip.AddrFrom4([4]byte{byte(e >> 24), byte(e >> 16), byte(e >> 8), byte(e)})
			v4 = append(v4, iprange.Range{Start: start, End: end})
		case "ipv6":
			bits, err := strconv.Atoi(cols[4])
			if err != nil || bits < 0 || bits > 128 {
				return nil, nil, fmt.Errorf("delegated line %d: bad prefix length %q", line, cols[4])
			}
			v6 = append(v6, iprange.FromPrefix(netip.PrefixFrom(start, bits)))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, nil, fmt.Errorf("no delegations for %s found (format changed?)", country)
	}
	return mergeRanges(v4), mergeRanges(v6), nil
}

func mergeRanges(rs []iprange.Range) []iprange.Range {
	slices.SortFunc(rs, func(a, b iprange.Range) int { return a.Start.Compare(b.Start) })
	out := rs[:0]
	for _, r := range rs {
		if n := len(out); n > 0 && (r.Start.Compare(out[n-1].End) <= 0 || r.Start == out[n-1].End.Next()) {
			if r.End.Compare(out[n-1].End) > 0 {
				out[n-1].End = r.End
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
