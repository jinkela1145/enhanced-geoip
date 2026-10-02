package build

import (
	"math"

	"github.com/maxmind/mmdbwriter/mmdbtype"

	"github.com/jinkela1145/enhanced-geoip/internal/sources"
)

// valueCache builds mmdbtype values and shares identical sub-values so that
// millions of records do not each allocate their own copies.
type valueCache struct {
	continents map[*sources.Continent]mmdbtype.Map
	countries  map[*sources.Country]mmdbtype.Map
	places     map[*sources.Place]mmdbtype.Map
	subdivs    map[*sources.Place]mmdbtype.Slice // keyed by first element; multi-level lists are built on demand
	locations  map[locKey]mmdbtype.Map
	networks   map[sources.NetFlags]mmdbtype.Map
	asns       map[*sources.ASNInfo][2]mmdbtype.DataType
	sources    map[string]mmdbtype.String
	lite       map[liteKey]mmdbtype.Map
}

type locKey struct {
	lat, lon float64
	radius   uint16
	tz       string
}

type liteKey struct {
	country      string
	lat, lon     int32
	hasLoc       bool
	radius       uint16
	anycast, cdn bool
	cloud        string
}

func newValueCache() *valueCache {
	return &valueCache{
		continents: map[*sources.Continent]mmdbtype.Map{},
		countries:  map[*sources.Country]mmdbtype.Map{},
		places:     map[*sources.Place]mmdbtype.Map{},
		subdivs:    map[*sources.Place]mmdbtype.Slice{},
		locations:  map[locKey]mmdbtype.Map{},
		networks:   map[sources.NetFlags]mmdbtype.Map{},
		asns:       map[*sources.ASNInfo][2]mmdbtype.DataType{},
		sources:    map[string]mmdbtype.String{},
		lite:       map[liteKey]mmdbtype.Map{},
	}
}

func namesMap(n sources.Names) mmdbtype.Map {
	m := make(mmdbtype.Map, len(n))
	for k, v := range n {
		if v != "" {
			m[mmdbtype.String(k)] = mmdbtype.String(v)
		}
	}
	return m
}

func (c *valueCache) continent(p *sources.Continent) mmdbtype.Map {
	if v, ok := c.continents[p]; ok {
		return v
	}
	m := mmdbtype.Map{}
	if p.Code != "" {
		m["code"] = mmdbtype.String(p.Code)
	}
	if p.GeonameID != 0 {
		m["geoname_id"] = mmdbtype.Uint32(p.GeonameID)
	}
	if len(p.Names) > 0 {
		m["names"] = namesMap(p.Names)
	}
	c.continents[p] = m
	return m
}

func (c *valueCache) country(p *sources.Country) mmdbtype.Map {
	if v, ok := c.countries[p]; ok {
		return v
	}
	m := mmdbtype.Map{"iso_code": mmdbtype.String(p.ISOCode)}
	if p.GeonameID != 0 {
		m["geoname_id"] = mmdbtype.Uint32(p.GeonameID)
	}
	if p.IsInEU {
		m["is_in_european_union"] = mmdbtype.Bool(true)
	}
	if len(p.Names) > 0 {
		m["names"] = namesMap(p.Names)
	}
	c.countries[p] = m
	return m
}

func (c *valueCache) place(p *sources.Place) mmdbtype.Map {
	if v, ok := c.places[p]; ok {
		return v
	}
	m := mmdbtype.Map{}
	if p.GeonameID != 0 {
		m["geoname_id"] = mmdbtype.Uint32(p.GeonameID)
	}
	if p.ISOCode != "" {
		m["iso_code"] = mmdbtype.String(p.ISOCode)
	}
	if len(p.Names) > 0 {
		m["names"] = namesMap(p.Names)
	}
	c.places[p] = m
	return m
}

func (c *valueCache) subdivisions(ps []*sources.Place) mmdbtype.Slice {
	if len(ps) == 1 {
		if v, ok := c.subdivs[ps[0]]; ok {
			return v
		}
	}
	s := make(mmdbtype.Slice, 0, len(ps))
	for _, p := range ps {
		s = append(s, c.place(p))
	}
	if len(ps) == 1 {
		c.subdivs[ps[0]] = s
	}
	return s
}

func (c *valueCache) location(lat, lon float64, radius uint16, tz string) mmdbtype.Map {
	k := locKey{lat, lon, radius, tz}
	if v, ok := c.locations[k]; ok {
		return v
	}
	m := mmdbtype.Map{
		"latitude":        mmdbtype.Float64(lat),
		"longitude":       mmdbtype.Float64(lon),
		"accuracy_radius": mmdbtype.Uint16(radius),
	}
	if tz != "" {
		m["time_zone"] = mmdbtype.String(tz)
	}
	c.locations[k] = m
	return m
}

// network returns the network map with false / empty keys left out, or nil.
func (c *valueCache) network(f sources.NetFlags, withRegion bool) mmdbtype.Map {
	if !withRegion {
		f.Region = ""
	}
	if f.IsZero() {
		return nil
	}
	if v, ok := c.networks[f]; ok {
		return v
	}
	m := mmdbtype.Map{}
	if f.Anycast {
		m["anycast"] = mmdbtype.Bool(true)
	}
	if f.CDN {
		m["cdn"] = mmdbtype.Bool(true)
	}
	if f.Cloud != "" {
		m["cloud"] = mmdbtype.String(f.Cloud)
	}
	if f.Region != "" {
		m["cloud_region"] = mmdbtype.String(f.Region)
	}
	c.networks[f] = m
	return m
}

func (c *valueCache) asn(a *sources.ASNInfo) (mmdbtype.DataType, mmdbtype.DataType) {
	if v, ok := c.asns[a]; ok {
		return v[0], v[1]
	}
	v := [2]mmdbtype.DataType{mmdbtype.Uint32(a.ASN), mmdbtype.String(a.Org)}
	c.asns[a] = v
	return v[0], v[1]
}

// full builds the GeoLite2-City compatible record.
func (c *valueCache) full(res *resolved) mmdbtype.Map {
	m := make(mmdbtype.Map, 10)
	if res.continent != nil {
		m["continent"] = c.continent(res.continent)
	}
	if res.country != nil {
		m["country"] = c.country(res.country)
	}
	if len(res.subdivs) > 0 {
		m["subdivisions"] = c.subdivisions(res.subdivs)
	}
	if res.city != nil {
		m["city"] = c.place(res.city)
	}
	if res.hasLoc {
		m["location"] = c.location(res.lat, res.lon, res.radius, res.timeZone)
	}
	if res.asn != nil {
		num, org := c.asn(res.asn)
		m["autonomous_system_number"] = num
		if res.asn.Org != "" {
			m["autonomous_system_organization"] = org
		}
	}
	if n := c.network(res.flags, true); n != nil {
		m["network"] = n
	}
	src, ok := c.sources[res.source]
	if !ok {
		src = mmdbtype.String(res.source)
		c.sources[res.source] = src
	}
	m["source"] = src
	return m
}

func (c *valueCache) liteKeyOf(res *resolved, step float64, tiers []uint16) liteKey {
	k := liteKey{
		country: countryISO(res.country),
		anycast: res.flags.Anycast,
		cdn:     res.flags.CDN,
		cloud:   res.flags.Cloud,
	}
	if res.hasLoc {
		k.hasLoc = true
		k.lat = roundTo(res.lat, step)
		k.lon = roundTo(res.lon, step)
		k.radius = radiusTier(res.radius, tiers)
	}
	return k
}

// liteValue builds the map-oriented record:
// {"country":{"iso_code"},"location":{"latitude","longitude","accuracy_radius"},"network":{...}}
func (c *valueCache) liteValue(k liteKey, step float64) mmdbtype.Map {
	if v, ok := c.lite[k]; ok {
		return v
	}
	m := mmdbtype.Map{}
	if k.country != "" {
		m["country"] = mmdbtype.Map{"iso_code": mmdbtype.String(k.country)}
	}
	if k.hasLoc {
		m["location"] = mmdbtype.Map{
			"latitude":        mmdbtype.Float64(cleanZero(float64(k.lat) * step)),
			"longitude":       mmdbtype.Float64(cleanZero(float64(k.lon) * step)),
			"accuracy_radius": mmdbtype.Uint16(k.radius),
		}
	}
	if n := c.network(sources.NetFlags{Anycast: k.anycast, CDN: k.cdn, Cloud: k.cloud}, false); n != nil {
		m["network"] = n
	}
	c.lite[k] = m
	return m
}

// cleanZero turns -0 into 0 so that equal coordinates serialize identically.
func cleanZero(v float64) float64 {
	if v == 0 || math.IsNaN(v) {
		return 0
	}
	return v
}
