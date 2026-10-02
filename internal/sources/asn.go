package sources

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// ASNInfo is an autonomous system with its organisation name.
type ASNInfo struct {
	ASN     uint32
	Org     string
	Country string // registry country of the AS as reported by the source
}

// ASNData maps address ranges to ASNInfo entries.
type ASNData struct {
	V4, V6 []iprange.Seg[int32]
	Infos  []ASNInfo
	Source string
}

type asnInterner struct {
	data  *ASNData
	index map[ASNInfo]int32
}

func (a *asnInterner) id(info ASNInfo) int32 {
	if id, ok := a.index[info]; ok {
		return id
	}
	id := int32(len(a.data.Infos))
	a.data.Infos = append(a.data.Infos, info)
	a.index[info] = id
	return id
}

func (a *asnInterner) add(r iprange.Range, info ASNInfo) {
	seg := iprange.Seg[int32]{Start: r.Start, End: r.End, Val: a.id(info)}
	if r.Is4() {
		a.data.V4 = append(a.data.V4, seg)
	} else {
		a.data.V6 = append(a.data.V6, seg)
	}
}

func (a *asnInterner) finish() error {
	for _, segs := range []*[]iprange.Seg[int32]{&a.data.V4, &a.data.V6} {
		iprange.SortSegs(*segs)
		// Drop overlaps defensively: keep the earlier range, trim later ones.
		out := (*segs)[:0]
		for _, s := range *segs {
			if n := len(out); n > 0 && s.Start.Compare(out[n-1].End) <= 0 {
				if s.End.Compare(out[n-1].End) <= 0 {
					continue
				}
				s.Start = out[n-1].End.Next()
			}
			out = iprange.AppendMerged(out, s)
		}
		*segs = out
	}
	if len(a.data.V4) == 0 || len(a.data.V6) == 0 {
		return fmt.Errorf("%s: no IPv4 or no IPv6 ranges (format changed?)", a.data.Source)
	}
	return nil
}

// ReadIPtoASN parses iptoasn.com's ip2asn-combined.tsv(.gz). Columns:
// range_start, range_end, AS_number, country_code, AS_description.
func ReadIPtoASN(path string) (*ASNData, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a := &asnInterner{data: &ASNData{Source: "iptoasn"}, index: map[ASNInfo]int32{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		if text == "" {
			continue
		}
		cols := strings.SplitN(text, "\t", 5)
		if len(cols) != 5 {
			return nil, fmt.Errorf("iptoasn line %d: expected 5 tab-separated columns, got %d", line, len(cols))
		}
		asn, err := strconv.ParseUint(cols[2], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("iptoasn line %d: bad AS number %q", line, cols[2])
		}
		r, err := iprange.Parse(cols[0] + "-" + cols[1])
		if err != nil {
			return nil, fmt.Errorf("iptoasn line %d: %w", line, err)
		}
		if asn == 0 { // "Not routed"
			continue
		}
		country := cols[3]
		if country == "None" {
			country = ""
		}
		a.add(r, ASNInfo{ASN: uint32(asn), Org: strings.TrimSpace(cols[4]), Country: country})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading iptoasn: %w", err)
	}
	if err := a.finish(); err != nil {
		return nil, err
	}
	return a.data, nil
}

// ReadDBIPASN reads DB-IP's ASN Lite MMDB; it is only used as a fallback.
func ReadDBIPASN(path, tmpDir string) (*ASNData, error) {
	mmdbPath, err := decompressToTemp(path, tmpDir)
	if err != nil {
		return nil, err
	}
	defer os.Remove(mmdbPath)
	r, err := maxminddb.Open(mmdbPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if !strings.Contains(r.Metadata.DatabaseType, "ASN") {
		return nil, fmt.Errorf("unexpected database_type %q", r.Metadata.DatabaseType)
	}
	a := &asnInterner{data: &ASNData{Source: "dbip-asn"}, index: map[ASNInfo]int32{}}
	byOffset := map[uintptr]ASNInfo{}
	for res := range r.Networks() {
		if err := res.Err(); err != nil {
			return nil, err
		}
		info, ok := byOffset[res.Offset()]
		if !ok {
			var raw struct {
				ASN uint32 `maxminddb:"autonomous_system_number"`
				Org string `maxminddb:"autonomous_system_organization"`
			}
			if err := res.Decode(&raw); err != nil {
				return nil, err
			}
			info = ASNInfo{ASN: raw.ASN, Org: raw.Org}
			byOffset[res.Offset()] = info
		}
		if info.ASN == 0 {
			continue
		}
		a.add(iprange.FromPrefix(res.Prefix()), info)
	}
	if err := a.finish(); err != nil {
		return nil, err
	}
	return a.data, nil
}
