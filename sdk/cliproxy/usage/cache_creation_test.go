package usage

import (
	"math"
	"testing"
)

func TestNormalizeCacheCreationBreakdown(t *testing.T) {
	for _, tc := range []struct{ total, five, hour, wantFive, wantHour int64 }{
		{100, 30, 70, 30, 70}, {100, 0, 0, 100, 0}, {100, 10, 40, 60, 40},
		{100, 60, 140, 30, 70}, {100, -1, 40, 60, 40}, {0, 0, 100, 0, 0},
		{100, math.MaxInt64, math.MaxInt64, 50, 50},
	} {
		five, hour := NormalizeCacheCreationBreakdown(tc.total, tc.five, tc.hour)
		if five != tc.wantFive || hour != tc.wantHour {
			t.Fatalf("%+v: got %d/%d", tc, five, hour)
		}
	}
}
