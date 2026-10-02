package geo

import (
	"math"
	"testing"
)

func TestHaversine(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		want, tol              float64
	}{
		{"same point", 39.9, 116.4, 39.9, 116.4, 0, 1e-9},
		{"Beijing-Shanghai", 39.9042, 116.4074, 31.2304, 121.4737, 1067, 5},
		{"one degree of latitude", 0, 0, 1, 0, 111.19, 0.05},
		{"across the antimeridian", 0, 179.5, 0, -179.5, 111.19, 0.05},
		{"antipodes", 0, 0, 0, 180, math.Pi * EarthRadiusKM, 1e-6},
	}
	for _, c := range cases {
		if got := Haversine(c.lat1, c.lon1, c.lat2, c.lon2); math.Abs(got-c.want) > c.tol {
			t.Errorf("%s: got %.3f km, want %.3f ± %g", c.name, got, c.want, c.tol)
		}
	}
}
