package sources

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// Names maps a locale code to a name.
type Names map[string]string

// Continent is a continent record shared by many networks.
type Continent struct {
	Code      string
	GeonameID uint32
	Names     Names
}

// Country is a country record shared by many networks.
type Country struct {
	ISOCode   string
	GeonameID uint32
	IsInEU    bool
	Names     Names
}

// Place is a subdivision or a city.
type Place struct {
	GeonameID uint32
	ISOCode   string
	Names     Names
}

// BaseRecord is one distinct record of the base database. Pointers are shared
// between records.
type BaseRecord struct {
	Continent *Continent
	Country   *Country
	Subdivs   []*Place
	City      *Place
	Lat, Lon  float64
	HasLoc    bool
	Radius    uint16 // accuracy radius from upstream, 0 if absent
	TimeZone  string
}

// BaseData is the parsed base database.
type BaseData struct {
	Records []BaseRecord
	// V4 and V6 are sorted, non-overlapping segments pointing into Records.
	V4, V6       []iprange.Seg[int32]
	Countries    map[string]*Country   // by ISO code
	ContinentOf  map[string]*Continent // by country ISO code
	DatabaseType string
	BuildEpoch   uint64
	Languages    []string
	Networks     int
}

type dbipRaw struct {
	Continent struct {
		Code      string            `maxminddb:"code"`
		GeonameID uint32            `maxminddb:"geoname_id"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
	Country struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		IsInEU    bool              `maxminddb:"is_in_european_union"`
		ISOCode   string            `maxminddb:"iso_code"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		ISOCode   string            `maxminddb:"iso_code"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude       *float64 `maxminddb:"latitude"`
		Longitude      *float64 `maxminddb:"longitude"`
		AccuracyRadius uint16   `maxminddb:"accuracy_radius"`
		TimeZone       string   `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}

type interner struct {
	continents map[string]*Continent
	countries  map[string]*Country
	places     map[string]*Place
	strs       map[string]string
}

func newInterner() *interner {
	return &interner{
		continents: map[string]*Continent{},
		countries:  map[string]*Country{},
		places:     map[string]*Place{},
		strs:       map[string]string{},
	}
}

func (in *interner) str(s string) string {
	if v, ok := in.strs[s]; ok {
		return v
	}
	in.strs[s] = s
	return s
}

func (in *interner) names(m map[string]string) Names {
	if len(m) == 0 {
		return nil
	}
	out := make(Names, len(m))
	for k, v := range m {
		out[in.str(k)] = in.str(v)
	}
	return out
}

func namesKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(0)
		b.WriteString(m[k])
		b.WriteByte(1)
	}
	return b.String()
}

func (in *interner) record(raw *dbipRaw) BaseRecord {
	var rec BaseRecord
	if c := raw.Continent; c.Code != "" || len(c.Names) > 0 {
		key := c.Code + "|" + strconv.FormatUint(uint64(c.GeonameID), 10) + "|" + namesKey(c.Names)
		p, ok := in.continents[key]
		if !ok {
			p = &Continent{Code: in.str(c.Code), GeonameID: c.GeonameID, Names: in.names(c.Names)}
			in.continents[key] = p
		}
		rec.Continent = p
	}
	if c := raw.Country; c.ISOCode != "" {
		key := c.ISOCode + "|" + strconv.FormatUint(uint64(c.GeonameID), 10) + "|" + strconv.FormatBool(c.IsInEU) + "|" + namesKey(c.Names)
		p, ok := in.countries[key]
		if !ok {
			p = &Country{ISOCode: in.str(c.ISOCode), GeonameID: c.GeonameID, IsInEU: c.IsInEU, Names: in.names(c.Names)}
			in.countries[key] = p
		}
		rec.Country = p
	}
	country := raw.Country.ISOCode
	for _, s := range raw.Subdivisions {
		if len(s.Names) == 0 && s.ISOCode == "" && s.GeonameID == 0 {
			continue
		}
		rec.Subdivs = append(rec.Subdivs, in.place("S", country, s.GeonameID, s.ISOCode, s.Names))
	}
	if c := raw.City; len(c.Names) > 0 || c.GeonameID != 0 {
		rec.City = in.place("C", country, c.GeonameID, "", c.Names)
	}
	if l := raw.Location; l.Latitude != nil && l.Longitude != nil {
		rec.Lat, rec.Lon, rec.HasLoc = *l.Latitude, *l.Longitude, true
	}
	rec.Radius = raw.Location.AccuracyRadius
	rec.TimeZone = in.str(raw.Location.TimeZone)
	return rec
}

func (in *interner) place(kind, country string, id uint32, iso string, names map[string]string) *Place {
	key := kind + "|" + country + "|" + strconv.FormatUint(uint64(id), 10) + "|" + iso + "|" + namesKey(names)
	p, ok := in.places[key]
	if !ok {
		p = &Place{GeonameID: id, ISOCode: in.str(iso), Names: in.names(names)}
		in.places[key] = p
	}
	return p
}

// ReadDBIPCity reads a DB-IP City Lite MMDB (optionally gzip-compressed).
// tmpDir receives the decompressed copy, which is removed afterwards.
func ReadDBIPCity(path, tmpDir string) (*BaseData, error) {
	mmdbPath, err := decompressToTemp(path, tmpDir)
	if err != nil {
		return nil, err
	}
	defer os.Remove(mmdbPath)
	r, err := maxminddb.Open(mmdbPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer r.Close()
	if !strings.Contains(r.Metadata.DatabaseType, "City") {
		return nil, fmt.Errorf("unexpected database_type %q (format changed?)", r.Metadata.DatabaseType)
	}
	data := &BaseData{
		Countries:    map[string]*Country{},
		ContinentOf:  map[string]*Continent{},
		DatabaseType: r.Metadata.DatabaseType,
		BuildEpoch:   uint64(r.Metadata.BuildEpoch),
		Languages:    slices.Clone(r.Metadata.Languages),
	}
	in := newInterner()
	byOffset := map[uintptr]int32{}
	for res := range r.Networks() {
		if err := res.Err(); err != nil {
			return nil, fmt.Errorf("reading networks: %w", err)
		}
		off := res.Offset()
		idx, ok := byOffset[off]
		if !ok {
			var raw dbipRaw
			if err := res.Decode(&raw); err != nil {
				return nil, fmt.Errorf("decoding record for %s: %w", res.Prefix(), err)
			}
			rec := in.record(&raw)
			idx = int32(len(data.Records))
			data.Records = append(data.Records, rec)
			byOffset[off] = idx
			if rec.Country != nil {
				if _, seen := data.Countries[rec.Country.ISOCode]; !seen {
					data.Countries[rec.Country.ISOCode] = rec.Country
				}
				if rec.Continent != nil {
					if _, seen := data.ContinentOf[rec.Country.ISOCode]; !seen {
						data.ContinentOf[rec.Country.ISOCode] = rec.Continent
					}
				}
			}
		}
		data.Networks++
		rg := iprange.FromPrefix(res.Prefix())
		seg := iprange.Seg[int32]{Start: rg.Start, End: rg.End, Val: idx}
		if rg.Is4() {
			data.V4 = iprange.AppendMerged(data.V4, seg)
		} else {
			data.V6 = iprange.AppendMerged(data.V6, seg)
		}
	}
	if data.Networks == 0 {
		return nil, fmt.Errorf("%s contains no networks", path)
	}
	for _, segs := range []*[]iprange.Seg[int32]{&data.V4, &data.V6} {
		if iprange.CheckSorted(*segs) != nil {
			iprange.SortSegs(*segs)
			if err := iprange.CheckSorted(*segs); err != nil {
				return nil, err
			}
		}
	}
	data.V4 = iprange.Subtract(data.V4, Excluded(true))
	data.V6 = iprange.Subtract(data.V6, Excluded(false))
	return data, nil
}
