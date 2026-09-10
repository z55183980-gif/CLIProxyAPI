package usage

import "math"

// NormalizeCacheCreationBreakdown partitions an authoritative cache-write total.
// Missing TTL details use 5m pricing; contradictory details are capped proportionally.
func NormalizeCacheCreationBreakdown(total, fiveMinutes, oneHour int64) (int64, int64) {
	if total <= 0 {
		return 0, 0
	}
	fiveMinutes = max(fiveMinutes, 0)
	oneHour = max(oneHour, 0)
	if fiveMinutes <= total && oneHour <= total-fiveMinutes {
		return total - oneHour, oneHour
	}
	normalized := math.Round(float64(total) * (float64(fiveMinutes) / (float64(fiveMinutes) + float64(oneHour))))
	if normalized >= float64(total) {
		return total, 0
	}
	fiveMinutes = int64(normalized)
	return fiveMinutes, total - fiveMinutes
}
