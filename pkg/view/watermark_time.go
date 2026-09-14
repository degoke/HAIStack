package view

import "time"

// WatermarkAdvanceTime returns the timestamp to persist for incremental export or
// refresh. When exported resources carry LastUpdated, that data clock is preferred
// over the process clock to avoid skew-related gaps or duplicates.
func WatermarkAdvanceTime(maxUpdated, fallback time.Time) time.Time {
	if !maxUpdated.IsZero() {
		return maxUpdated.UTC()
	}
	return fallback.UTC()
}

// LatestTimestamp returns the later of two timestamps.
func LatestTimestamp(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
