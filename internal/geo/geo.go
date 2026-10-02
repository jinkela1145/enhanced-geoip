// Package geo holds small geographic helpers shared by the builder and the
// verifier.
package geo

import "math"

// EarthRadiusKM is the mean Earth radius used for distances.
const EarthRadiusKM = 6371.0

// Haversine returns the great-circle distance between two points in
// kilometres.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * EarthRadiusKM * math.Asin(math.Min(1, math.Sqrt(a)))
}
