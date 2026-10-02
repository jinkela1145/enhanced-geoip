package build

import (
	"math"
	"testing"

	"github.com/jinkela1145/enhanced-geoip/internal/sources"
)

func TestRadiusTier(t *testing.T) {
	tiers := []uint16{10, 25, 50, 100, 250, 500, 1000}
	for in, want := range map[uint16]uint16{1: 10, 10: 10, 11: 25, 50: 50, 51: 100, 250: 250, 300: 500, 1000: 1000, 5000: 1000} {
		if got := radiusTier(in, tiers); got != want {
			t.Errorf("radiusTier(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestRounding(t *testing.T) {
	cases := []struct {
		v    float64
		want float64
	}{{35.69, 35.5}, {35.76, 36}, {-0.2, 0}, {-33.87, -34}, {139.69, 139.5}, {-122.42, -122.5}, {180, 180}}
	for _, c := range cases {
		got := cleanZero(float64(roundTo(c.v, 0.5)) * 0.5)
		if got != c.want || math.Signbit(got) != math.Signbit(c.want) {
			t.Errorf("round(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestFindProvince(t *testing.T) {
	r := &resolver{provinces: []sources.CNProvince{
		{ISO: "NM", NameZH: "内蒙古自治区", NameEN: "Inner Mongolia", DBIPNames: []string{"Nei Mongol", "Inner Mongolia Autonomous Region"}},
		{ISO: "GD", NameZH: "广东省", NameEN: "Guangdong"},
		{ISO: "BJ", NameZH: "北京市", NameEN: "Beijing"},
	}}
	for in, want := range map[string]int16{
		"inner mongolia": 0, "Nei Mongol": 0, "内蒙古": 0, "内蒙古自治区": 0, "NM": 0,
		"广东": 1, "广东省": 1, "GUANGDONG": 1, "北京": 2, "Beijing": 2, "": -1, "Hong Kong": -1, "香港特别行政区": -1,
	} {
		if got := r.findProvince(in); got != want {
			t.Errorf("findProvince(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestNetFlagsMerge(t *testing.T) {
	a := sources.NetFlags{Cloud: "aws", Region: "us-east-1"}
	b := sources.NetFlags{CDN: true}
	if got := a.Merge(b); got != (sources.NetFlags{CDN: true, Cloud: "aws", Region: "us-east-1"}) {
		t.Errorf("merge = %+v", got)
	}
	if got := b.Merge(sources.NetFlags{Anycast: true, Region: "eu-west-1"}); !got.Anycast || !got.CDN || got.Region != "eu-west-1" {
		t.Errorf("merge = %+v", got)
	}
}
