package geo

import (
	"math"
	"testing"
)

func TestDistanceKm(t *testing.T) {
	lat1, lng1 := 40.7128, -74.0060  // New York
	lat2, lng2 := 34.0522, -118.2437 // Los Angeles

	dist := DistanceKm(lat1, lng1, lat2, lng2)
	expected := 3935.0 // Approximately 3935 to 3940 km

	if math.Abs(dist-expected) > 10.0 {
		t.Errorf("Expected approximately %.1f km, got %.1f km", expected, dist)
	}
}
