// Package verify opens the built databases with maxminddb-golang and checks
// their structure and a list of known addresses.
package verify

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/jinkela1145/enhanced-geoip/internal/config"
)

// KnownIP is a row of testdata/known_ips.csv.
type KnownIP struct {
	Line     int
	IP       netip.Addr
	Country  string
	Lat, Lon float64
	HasLoc   bool
	MaxKM    float64
	Anycast  *bool
	CDN      *bool
	Evidence string
}

// KnownHeader is the header of known_ips.csv.
var KnownHeader = []string{"ip", "country", "latitude", "longitude", "max_km", "anycast", "cdn", "evidence"}

// ReadKnownIPs reads the known address list.
func ReadKnownIPs(path string) ([]KnownIP, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.Comment = '#'
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !slices.Equal(header, KnownHeader) {
		return nil, fmt.Errorf("%s: header must be %s", path, strings.Join(KnownHeader, ","))
	}
	var out []KnownIP
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		line, _ := cr.FieldPos(0)
		k := KnownIP{Line: line, Country: strings.ToUpper(strings.TrimSpace(rec[1])), Evidence: strings.TrimSpace(rec[7])}
		if k.IP, err = netip.ParseAddr(strings.TrimSpace(rec[0])); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		k.IP = k.IP.Unmap()
		if k.Evidence == "" {
			return nil, fmt.Errorf("%s line %d: evidence is required", path, line)
		}
		if rec[2] != "" || rec[3] != "" {
			if k.Lat, err = strconv.ParseFloat(rec[2], 64); err != nil {
				return nil, fmt.Errorf("%s line %d: latitude: %w", path, line, err)
			}
			if k.Lon, err = strconv.ParseFloat(rec[3], 64); err != nil {
				return nil, fmt.Errorf("%s line %d: longitude: %w", path, line, err)
			}
			if k.MaxKM, err = strconv.ParseFloat(rec[4], 64); err != nil || k.MaxKM <= 0 {
				return nil, fmt.Errorf("%s line %d: max_km is required with coordinates", path, line)
			}
			k.HasLoc = true
		}
		for i, dst := range []**bool{&k.Anycast, &k.CDN} {
			s := strings.ToLower(strings.TrimSpace(rec[5+i]))
			switch s {
			case "":
			case "true", "false":
				v := s == "true"
				*dst = &v
			default:
				return nil, fmt.Errorf("%s line %d: %s must be true, false or empty", path, line, KnownHeader[5+i])
			}
		}
		out = append(out, k)
	}
	return out, nil
}

// Report is the outcome of Run.
type Report struct {
	FullNetworks int
	LiteNetworks int
	KnownChecked int
	Failures     []string
}

func (r *Report) failf(format string, args ...any) {
	if len(r.Failures) < 200 {
		r.Failures = append(r.Failures, fmt.Sprintf(format, args...))
	}
}

type fullRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location *struct {
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
	ASN     uint32         `maxminddb:"autonomous_system_number"`
	Network map[string]any `maxminddb:"network"`
	Source  string         `maxminddb:"source"`
}

var validSources = []string{"dbip", "bgp-asn", "override"}

func checkNetworkMap(rep *Report, where string, m map[string]any, allowRegion bool) {
	for k, v := range m {
		switch k {
		case "anycast", "cdn":
			if b, ok := v.(bool); !ok || !b {
				rep.failf("%s: network.%s must be true when present, got %v", where, k, v)
			}
		case "cloud", "cloud_region":
			if k == "cloud_region" && !allowRegion {
				rep.failf("%s: network.cloud_region is not allowed here", where)
			}
			if s, ok := v.(string); !ok || s == "" || s != strings.ToLower(s) && k == "cloud" {
				rep.failf("%s: network.%s must be a non-empty lower-case string, got %v", where, k, v)
			}
		default:
			rep.failf("%s: unexpected key network.%s", where, k)
		}
	}
}

// Run checks the databases in dir. knownPath may be empty.
func Run(cfg *config.Config, dir, knownPath string) (*Report, error) {
	rep := &Report{}
	full, err := maxminddb.Open(filepath.Join(dir, cfg.FullFile()))
	if err != nil {
		return nil, err
	}
	defer full.Close()
	lite, err := maxminddb.Open(filepath.Join(dir, cfg.LiteFile()))
	if err != nil {
		return nil, err
	}
	defer lite.Close()

	for _, c := range []struct {
		r    *maxminddb.Reader
		want string
	}{{full, cfg.FullType()}, {lite, cfg.LiteType()}} {
		md := c.r.Metadata
		if md.DatabaseType != c.want || !strings.Contains(md.DatabaseType, "City") {
			rep.failf("metadata: database_type %q, want %q", md.DatabaseType, c.want)
		}
		if md.IPVersion != 6 {
			rep.failf("metadata %s: ip_version %d, want 6", c.want, md.IPVersion)
		}
		if !slices.Contains(md.Languages, "en") || !slices.Contains(md.Languages, "zh-CN") {
			rep.failf("metadata %s: languages %v must include en and zh-CN", c.want, md.Languages)
		}
		if md.Description["en"] == "" {
			rep.failf("metadata %s: description.en is empty", c.want)
		}
		if md.BuildEpoch == 0 {
			rep.failf("metadata %s: build_epoch is 0", c.want)
		}
	}

	// Full database: structure of every distinct record.
	seen := map[uintptr]bool{}
	for res := range full.Networks() {
		if err := res.Err(); err != nil {
			return nil, err
		}
		rep.FullNetworks++
		if seen[res.Offset()] {
			continue
		}
		seen[res.Offset()] = true
		var rec fullRecord
		if err := res.Decode(&rec); err != nil {
			rep.failf("full %s: decode: %v", res.Prefix(), err)
			continue
		}
		where := "full " + res.Prefix().String()
		if !slices.Contains(validSources, rec.Source) {
			rep.failf("%s: source %q", where, rec.Source)
		}
		if c := rec.Country.ISOCode; c != "" && (len(c) != 2 || strings.ToUpper(c) != c) {
			rep.failf("%s: bad country code %q", where, c)
		}
		if l := rec.Location; l != nil {
			if math.Abs(l.Latitude) > 90 || math.Abs(l.Longitude) > 180 || l.AccuracyRadius == 0 {
				rep.failf("%s: bad location %+v", where, *l)
			}
		}
		for _, s := range rec.Subdivisions {
			if s.Names["en"] == "" {
				rep.failf("%s: subdivision without an English name", where)
			}
		}
		if rec.Source == "bgp-asn" {
			if rec.Country.ISOCode != "CN" || len(rec.Subdivisions) == 0 || rec.Subdivisions[0].Names["zh-CN"] == "" {
				rep.failf("%s: China overlay record must be CN with a zh-CN province name", where)
			}
		}
		checkNetworkMap(rep, where, rec.Network, true)
	}

	// Lite database: exact schema.
	step, tiers := cfg.Lite.CoordStep, cfg.Lite.RadiusTiers
	seen = map[uintptr]bool{}
	for res := range lite.Networks() {
		if err := res.Err(); err != nil {
			return nil, err
		}
		rep.LiteNetworks++
		if seen[res.Offset()] {
			continue
		}
		seen[res.Offset()] = true
		where := "lite " + res.Prefix().String()
		var rec map[string]any
		if err := res.Decode(&rec); err != nil {
			rep.failf("%s: decode: %v", where, err)
			continue
		}
		for k, v := range rec {
			m, ok := v.(map[string]any)
			if !ok {
				rep.failf("%s: %s is not a map", where, k)
				continue
			}
			switch k {
			case "country":
				if len(m) != 1 || m["iso_code"] == nil {
					rep.failf("%s: country must only contain iso_code: %v", where, m)
				}
			case "location":
				lat, ok1 := m["latitude"].(float64)
				lon, ok2 := m["longitude"].(float64)
				rad, ok3 := m["accuracy_radius"].(uint64)
				if len(m) != 3 || !ok1 || !ok2 || !ok3 {
					rep.failf("%s: location must be {latitude, longitude, accuracy_radius}: %v", where, m)
					continue
				}
				if !onGrid(lat, step) || !onGrid(lon, step) {
					rep.failf("%s: coordinates %v,%v not rounded to %g", where, lat, lon, step)
				}
				if !slices.Contains(tiers, uint16(rad)) {
					rep.failf("%s: accuracy_radius %d is not a tier", where, rad)
				}
			case "network":
				checkNetworkMap(rep, where, m, false)
			default:
				rep.failf("%s: unexpected top-level key %q", where, k)
			}
		}
	}

	if knownPath != "" {
		known, err := ReadKnownIPs(knownPath)
		if err != nil {
			return nil, err
		}
		for _, k := range known {
			rep.KnownChecked++
			checkKnown(rep, full, lite, k)
		}
	}
	if len(rep.Failures) > 0 {
		return rep, errors.New(strings.Join(rep.Failures, "\n"))
	}
	return rep, nil
}

func onGrid(v, step float64) bool {
	q := v / step
	return math.Abs(q-math.Round(q)) < 1e-9
}

func checkKnown(rep *Report, full, lite *maxminddb.Reader, k KnownIP) {
	type rec struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
		Location *struct {
			Latitude  float64 `maxminddb:"latitude"`
			Longitude float64 `maxminddb:"longitude"`
		} `maxminddb:"location"`
		Network struct {
			Anycast bool `maxminddb:"anycast"`
			CDN     bool `maxminddb:"cdn"`
		} `maxminddb:"network"`
	}
	for _, db := range []struct {
		name string
		r    *maxminddb.Reader
	}{{"full", full}, {"lite", lite}} {
		var got rec
		res := db.r.Lookup(k.IP)
		if err := res.Err(); err != nil {
			rep.failf("known_ips line %d (%s): %s lookup: %v", k.Line, k.IP, db.name, err)
			continue
		}
		if !res.Found() {
			rep.failf("known_ips line %d (%s): not found in %s", k.Line, k.IP, db.name)
			continue
		}
		if err := res.Decode(&got); err != nil {
			rep.failf("known_ips line %d (%s): %s decode: %v", k.Line, k.IP, db.name, err)
			continue
		}
		if k.Country != "" && got.Country.ISOCode != k.Country {
			rep.failf("known_ips line %d (%s): %s country %q, want %q", k.Line, k.IP, db.name, got.Country.ISOCode, k.Country)
		}
		if k.HasLoc {
			if got.Location == nil {
				rep.failf("known_ips line %d (%s): %s has no location", k.Line, k.IP, db.name)
			} else if d := Haversine(k.Lat, k.Lon, got.Location.Latitude, got.Location.Longitude); d > k.MaxKM {
				rep.failf("known_ips line %d (%s): %s location is %.0f km away, limit %.0f km", k.Line, k.IP, db.name, d, k.MaxKM)
			}
		}
		if k.Anycast != nil && got.Network.Anycast != *k.Anycast {
			rep.failf("known_ips line %d (%s): %s anycast=%v, want %v", k.Line, k.IP, db.name, got.Network.Anycast, *k.Anycast)
		}
		if k.CDN != nil && got.Network.CDN != *k.CDN {
			rep.failf("known_ips line %d (%s): %s cdn=%v, want %v", k.Line, k.IP, db.name, got.Network.CDN, *k.CDN)
		}
	}
}

// Haversine returns the great-circle distance in kilometres.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}
