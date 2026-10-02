package sources

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// Curated table headers. Lines starting with # are comments.
var (
	HeaderCNAdmin = []string{"iso_code", "name_zh", "name_en", "dbip_names", "geoname_id",
		"capital_zh", "capital_en", "capital_geoname_id", "latitude", "longitude", "radius_km"}
	HeaderCNCities     = []string{"province_iso", "name_zh", "name_en", "geoname_id", "latitude", "longitude"}
	HeaderCNASN        = []string{"asn", "province_iso", "as_name", "evidence", "note"}
	HeaderAnycastASNs  = []string{"asn", "anycast", "cdn", "operator", "evidence"}
	HeaderAnycastNets  = []string{"network", "anycast", "cdn", "operator", "evidence"}
	HeaderOverrides    = []string{"network", "country_iso", "province", "city", "latitude", "longitude", "accuracy_km", "evidence"}
	errMissingEvidence = errors.New("evidence is required")
)

type csvRow struct {
	line   int
	fields map[string]string
}

func (r csvRow) get(k string) string { return strings.TrimSpace(r.fields[k]) }

func (r csvRow) errf(path, format string, args ...any) error {
	return fmt.Errorf("%s line %d: %s", path, r.line, fmt.Sprintf(format, args...))
}

// readCSV reads a UTF-8 CSV file whose header must equal want. A missing file
// yields no rows.
func readCSV(path string, want []string) ([]csvRow, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.Comment = '#'
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	if !slices.Equal(header, want) {
		return nil, fmt.Errorf("%s: header must be %q, got %q", path, strings.Join(want, ","), strings.Join(header, ","))
	}
	var rows []csvRow
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		line, _ := cr.FieldPos(0)
		if len(rec) != len(want) {
			return nil, fmt.Errorf("%s line %d: expected %d fields, got %d", path, line, len(want), len(rec))
		}
		m := make(map[string]string, len(want))
		for i, k := range want {
			m[k] = rec[i]
		}
		rows = append(rows, csvRow{line: line, fields: m})
	}
	return rows, nil
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no", "n":
		return false, nil
	case "1", "true", "yes", "y":
		return true, nil
	}
	return false, fmt.Errorf("not a boolean: %q", s)
}

func parseCoord(s string, limit float64) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.Abs(v) > limit {
		return 0, fmt.Errorf("bad coordinate %q", s)
	}
	return v, nil
}

// CNProvince is a row of data/cn_admin.csv.
type CNProvince struct {
	ISO              string
	NameZH, NameEN   string
	DBIPNames        []string
	GeonameID        uint32
	CapitalZH        string
	CapitalEN        string
	CapitalGeonameID uint32
	Lat, Lon         float64
	RadiusKM         uint16
}

// ReadCNAdmin reads data/cn_admin.csv.
func ReadCNAdmin(path string) ([]CNProvince, error) {
	rows, err := readCSV(path, HeaderCNAdmin)
	if err != nil {
		return nil, err
	}
	var out []CNProvince
	seen := map[string]bool{}
	for _, r := range rows {
		p := CNProvince{
			ISO: strings.ToUpper(r.get("iso_code")), NameZH: r.get("name_zh"), NameEN: r.get("name_en"),
			CapitalZH: r.get("capital_zh"), CapitalEN: r.get("capital_en"),
		}
		if p.ISO == "" || p.NameZH == "" || p.NameEN == "" {
			return nil, r.errf(path, "iso_code, name_zh and name_en are required")
		}
		if seen[p.ISO] {
			return nil, r.errf(path, "duplicate iso_code %s", p.ISO)
		}
		seen[p.ISO] = true
		for _, n := range strings.Split(r.get("dbip_names"), ";") {
			if n = strings.TrimSpace(n); n != "" {
				p.DBIPNames = append(p.DBIPNames, n)
			}
		}
		if p.GeonameID, err = parseUint32(r.get("geoname_id"), true); err != nil {
			return nil, r.errf(path, "geoname_id: %v", err)
		}
		if p.CapitalGeonameID, err = parseUint32(r.get("capital_geoname_id"), true); err != nil {
			return nil, r.errf(path, "capital_geoname_id: %v", err)
		}
		if p.Lat, err = parseCoord(r.get("latitude"), 90); err != nil {
			return nil, r.errf(path, "%v", err)
		}
		if p.Lon, err = parseCoord(r.get("longitude"), 180); err != nil {
			return nil, r.errf(path, "%v", err)
		}
		radius, err := strconv.ParseUint(r.get("radius_km"), 10, 16)
		if err != nil || radius == 0 {
			return nil, r.errf(path, "radius_km must be a positive integer")
		}
		p.RadiusKM = uint16(radius)
		out = append(out, p)
	}
	return out, nil
}

// CNCity is a row of data/cn_cities.csv.
type CNCity struct {
	ProvinceISO    string
	NameZH, NameEN string
	GeonameID      uint32
	Lat, Lon       float64
}

// ReadCNCities reads data/cn_cities.csv.
func ReadCNCities(path string) ([]CNCity, error) {
	rows, err := readCSV(path, HeaderCNCities)
	if err != nil {
		return nil, err
	}
	var out []CNCity
	for _, r := range rows {
		c := CNCity{ProvinceISO: strings.ToUpper(r.get("province_iso")), NameZH: r.get("name_zh"), NameEN: r.get("name_en")}
		if c.ProvinceISO == "" || c.NameZH == "" || c.NameEN == "" {
			return nil, r.errf(path, "province_iso, name_zh and name_en are required")
		}
		if c.GeonameID, err = parseUint32(r.get("geoname_id"), true); err != nil {
			return nil, r.errf(path, "geoname_id: %v", err)
		}
		if c.Lat, err = parseCoord(r.get("latitude"), 90); err != nil {
			return nil, r.errf(path, "%v", err)
		}
		if c.Lon, err = parseCoord(r.get("longitude"), 180); err != nil {
			return nil, r.errf(path, "%v", err)
		}
		out = append(out, c)
	}
	return out, nil
}

// CNASN is a row of data/cn_asn_province.csv.
type CNASN struct {
	ASN         uint32
	ProvinceISO string
	ASName      string
	Evidence    string
}

// ReadCNASN reads data/cn_asn_province.csv.
func ReadCNASN(path string) ([]CNASN, error) {
	rows, err := readCSV(path, HeaderCNASN)
	if err != nil {
		return nil, err
	}
	var out []CNASN
	seen := map[uint32]bool{}
	for _, r := range rows {
		asn, err := parseUint32(r.get("asn"), false)
		if err != nil {
			return nil, r.errf(path, "asn: %v", err)
		}
		if seen[asn] {
			return nil, r.errf(path, "duplicate asn %d", asn)
		}
		seen[asn] = true
		row := CNASN{ASN: asn, ProvinceISO: strings.ToUpper(r.get("province_iso")), ASName: r.get("as_name"), Evidence: r.get("evidence")}
		if row.ProvinceISO == "" {
			return nil, r.errf(path, "province_iso is required")
		}
		if row.Evidence == "" {
			return nil, r.errf(path, "%v", errMissingEvidence)
		}
		out = append(out, row)
	}
	return out, nil
}

// ReadAnycastASNs reads data/anycast_asns.csv: flags applied to every range
// announced by the AS (via iptoasn).
func ReadAnycastASNs(path string) (map[uint32]NetFlags, error) {
	rows, err := readCSV(path, HeaderAnycastASNs)
	if err != nil {
		return nil, err
	}
	out := map[uint32]NetFlags{}
	for _, r := range rows {
		asn, err := parseUint32(r.get("asn"), false)
		if err != nil {
			return nil, r.errf(path, "asn: %v", err)
		}
		if _, dup := out[asn]; dup {
			return nil, r.errf(path, "duplicate asn %d", asn)
		}
		var f NetFlags
		if f.Anycast, err = parseBool(r.get("anycast")); err != nil {
			return nil, r.errf(path, "anycast: %v", err)
		}
		if f.CDN, err = parseBool(r.get("cdn")); err != nil {
			return nil, r.errf(path, "cdn: %v", err)
		}
		if r.get("evidence") == "" {
			return nil, r.errf(path, "%v", errMissingEvidence)
		}
		if f.IsZero() {
			return nil, r.errf(path, "at least one of anycast / cdn must be true")
		}
		out[asn] = f
	}
	return out, nil
}

// ReadAnycastNets reads data/anycast_prefixes.csv.
func ReadAnycastNets(path string) ([]FlagEntry, error) {
	rows, err := readCSV(path, HeaderAnycastNets)
	if err != nil {
		return nil, err
	}
	var out []FlagEntry
	for _, r := range rows {
		rg, err := iprange.Parse(r.get("network"))
		if err != nil {
			return nil, r.errf(path, "network: %v", err)
		}
		var f NetFlags
		if f.Anycast, err = parseBool(r.get("anycast")); err != nil {
			return nil, r.errf(path, "anycast: %v", err)
		}
		if f.CDN, err = parseBool(r.get("cdn")); err != nil {
			return nil, r.errf(path, "cdn: %v", err)
		}
		if r.get("evidence") == "" {
			return nil, r.errf(path, "%v", errMissingEvidence)
		}
		if f.IsZero() {
			return nil, r.errf(path, "at least one of anycast / cdn must be true")
		}
		out = append(out, FlagEntry{R: rg, Bits: 129, Flags: f, Source: "curated:" + r.get("operator")})
	}
	return out, nil
}

// Override is a row of data/overrides.csv.
type Override struct {
	Line     int
	R        iprange.Range
	Country  string
	Province string
	City     string
	Lat, Lon float64
	HasLoc   bool
	RadiusKM uint16
	Evidence string
}

// ReadOverrides reads data/overrides.csv.
func ReadOverrides(path string) ([]Override, error) {
	rows, err := readCSV(path, HeaderOverrides)
	if err != nil {
		return nil, err
	}
	var out []Override
	for _, r := range rows {
		o := Override{Line: r.line, Country: strings.ToUpper(r.get("country_iso")),
			Province: r.get("province"), City: r.get("city"), Evidence: r.get("evidence")}
		if o.R, err = iprange.Parse(r.get("network")); err != nil {
			return nil, r.errf(path, "network: %v", err)
		}
		if IsExcluded(o.R.Start) || IsExcluded(o.R.End) {
			return nil, r.errf(path, "network %s is reserved / private", o.R)
		}
		if len(o.Country) != 2 {
			return nil, r.errf(path, "country_iso must be a two-letter ISO 3166-1 code")
		}
		lat, lon := r.get("latitude"), r.get("longitude")
		if (lat == "") != (lon == "") {
			return nil, r.errf(path, "latitude and longitude must be given together")
		}
		if lat != "" {
			if o.Lat, err = parseCoord(lat, 90); err != nil {
				return nil, r.errf(path, "%v", err)
			}
			if o.Lon, err = parseCoord(lon, 180); err != nil {
				return nil, r.errf(path, "%v", err)
			}
			o.HasLoc = true
		}
		if s := r.get("accuracy_km"); s != "" {
			v, err := strconv.ParseUint(s, 10, 16)
			if err != nil || v == 0 {
				return nil, r.errf(path, "accuracy_km must be a positive integer")
			}
			o.RadiusKM = uint16(v)
		}
		if o.Evidence == "" {
			return nil, r.errf(path, "%v", errMissingEvidence)
		}
		if o.City != "" && o.Province == "" && o.Country == "CN" {
			return nil, r.errf(path, "a CN city needs its province")
		}
		out = append(out, o)
	}
	return out, nil
}

func parseUint32(s string, allowEmpty bool) (uint32, error) {
	if s == "" && allowEmpty {
		return 0, nil
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("not an unsigned integer: %q", s)
	}
	return uint32(v), nil
}
