package build

import (
	"math"
	"sort"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// familyAcc accumulates address counts for one address family.
type familyAcc struct {
	is4      bool
	ranges   int
	total    iprange.U128
	bySource map[string]iprange.U128
	anycast  iprange.U128
	cdn      iprange.U128
	cloud    map[string]iprange.U128
	withASN  iprange.U128
	cnLayer  [4]iprange.U128 // by cnOutcome

	// China coverage: address space delegated to CN by APNIC.
	delegated   iprange.U128
	delInDB     iprange.U128
	delCountry  map[string]iprange.U128
	delCNLevel  [4]iprange.U128 // by level, only records with country CN
	delCNSource map[string]iprange.U128
	delCNLayer  [4]iprange.U128
}

func newFamilyAcc(is4 bool, delegated []iprange.Range) *familyAcc {
	a := &familyAcc{
		is4:         is4,
		bySource:    map[string]iprange.U128{},
		cloud:       map[string]iprange.U128{},
		delCountry:  map[string]iprange.U128{},
		delCNSource: map[string]iprange.U128{},
	}
	for _, r := range delegated {
		a.delegated = a.delegated.Add(r.Size())
	}
	return a
}

func (a *familyAcc) add(r *resolver, k key, outcome cnOutcome, size iprange.U128, inDelegated bool) {
	country, lv, source := r.summary(k)
	a.total = a.total.Add(size)
	a.bySource[source] = a.bySource[source].Add(size)
	if k.flags >= 0 {
		f := r.flagTable[k.flags]
		if f.Anycast {
			a.anycast = a.anycast.Add(size)
		}
		if f.CDN {
			a.cdn = a.cdn.Add(size)
		}
		if f.Cloud != "" {
			a.cloud[f.Cloud] = a.cloud[f.Cloud].Add(size)
		}
	}
	if k.asn >= 0 {
		a.withASN = a.withASN.Add(size)
	}
	a.cnLayer[outcome] = a.cnLayer[outcome].Add(size)
	if !inDelegated {
		return
	}
	a.delInDB = a.delInDB.Add(size)
	a.delCountry[country] = a.delCountry[country].Add(size)
	if country == "CN" {
		a.delCNLevel[lv] = a.delCNLevel[lv].Add(size)
		a.delCNSource[source] = a.delCNSource[source].Add(size)
		a.delCNLayer[outcome] = a.delCNLayer[outcome].Add(size)
	}
}

// FamilyReport summarises one family of the full database.
type FamilyReport struct {
	Unit       string             `json:"unit"`
	Ranges     int                `json:"ranges"`
	Total      float64            `json:"total"`
	BySource   map[string]float64 `json:"by_source"`
	Anycast    float64            `json:"anycast"`
	CDN        float64            `json:"cdn"`
	Cloud      map[string]float64 `json:"cloud"`
	WithASN    float64            `json:"with_asn"`
	CNASNLayer map[string]float64 `json:"cn_asn_layer"`
}

// CoverageReport describes how the address space APNIC delegated to China is
// located in the database. Percentages are of the delegated space.
type CoverageReport struct {
	Unit       string             `json:"unit"`
	Delegated  float64            `json:"delegated"`
	Percent    map[string]float64 `json:"percent"`
	TopCountry map[string]float64 `json:"top_countries_percent"`
}

// Stats is written to manifest.json.
type Stats struct {
	IPv4                    FamilyReport   `json:"ipv4"`
	IPv6                    FamilyReport   `json:"ipv6"`
	CNCoverageIPv4          CoverageReport `json:"cn_coverage_ipv4"`
	CNCoverageIPv6          CoverageReport `json:"cn_coverage_ipv6"`
	UnmatchedCNSubdivisions map[string]int `json:"unmatched_cn_subdivisions,omitempty"`
	// LiteAggregation reports the Lite block aggregation per family
	// ("ipv4", "ipv6").
	LiteAggregation map[string]LiteAggStats `json:"lite_aggregation,omitempty"`
}

func unitOf(is4 bool) (string, float64) {
	if is4 {
		return "addresses", 1
	}
	return "/48", math.Ldexp(1, 80)
}

func (a *familyAcc) report() FamilyReport {
	unit, div := unitOf(a.is4)
	conv := func(u iprange.U128) float64 { return round3(u.Float64() / div) }
	fr := FamilyReport{
		Unit: unit, Ranges: a.ranges, Total: conv(a.total),
		BySource: map[string]float64{}, Cloud: map[string]float64{},
		Anycast: conv(a.anycast), CDN: conv(a.cdn), WithASN: conv(a.withASN),
		CNASNLayer: map[string]float64{
			"agree":     conv(a.cnLayer[cnAgree]),
			"corrected": conv(a.cnLayer[cnCorrected]),
			"filled":    conv(a.cnLayer[cnFilled]),
		},
	}
	for k, v := range a.bySource {
		fr.BySource[k] = conv(v)
	}
	for k, v := range a.cloud {
		fr.Cloud[k] = conv(v)
	}
	return fr
}

func (a *familyAcc) coverage() CoverageReport {
	unit, div := unitOf(a.is4)
	cr := CoverageReport{Unit: unit, Delegated: round3(a.delegated.Float64() / div), Percent: map[string]float64{}, TopCountry: map[string]float64{}}
	if a.delegated.IsZero() {
		return cr
	}
	d := a.delegated.Float64()
	pct := func(u iprange.U128) float64 { return round3(100 * u.Float64() / d) }
	cr.Percent["in_database"] = pct(a.delInDB)
	cr.Percent["country_cn"] = pct(a.delCountry["CN"])
	cr.Percent["cn_city"] = pct(a.delCNLevel[levelCity])
	cr.Percent["cn_subdivision_only"] = pct(a.delCNLevel[levelSubdivision])
	cr.Percent["cn_country_only"] = pct(a.delCNLevel[levelCountry].Add(a.delCNLevel[levelNone]))
	cr.Percent["cn_source_dbip"] = pct(a.delCNSource[SourceDBIP])
	cr.Percent["cn_source_bgp_asn"] = pct(a.delCNSource[SourceBGPASN])
	cr.Percent["cn_source_override"] = pct(a.delCNSource[SourceOverride])
	cr.Percent["cn_asn_agree"] = pct(a.delCNLayer[cnAgree])
	cr.Percent["cn_asn_corrected"] = pct(a.delCNLayer[cnCorrected])
	cr.Percent["cn_asn_filled"] = pct(a.delCNLayer[cnFilled])
	type kv struct {
		k string
		v iprange.U128
	}
	var list []kv
	for k, v := range a.delCountry {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v.Float64() != list[j].v.Float64() {
			return list[i].v.Float64() > list[j].v.Float64()
		}
		return list[i].k < list[j].k
	})
	for i, e := range list {
		if i == 5 {
			break
		}
		name := e.k
		if name == "" {
			name = "(none)"
		}
		cr.TopCountry[name] = pct(e.v)
	}
	return cr
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
