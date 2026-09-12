package view

import (
	"testing"
	"time"
)

func TestWatermarkAdvanceTimePrefersDataClock(t *testing.T) {
	fallback := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	dataClock := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	got := WatermarkAdvanceTime(dataClock, fallback)
	if !got.Equal(dataClock) {
		t.Fatalf("got %v, want %v", got, dataClock)
	}
}

func TestWatermarkAdvanceTimeUsesFallbackWhenZero(t *testing.T) {
	fallback := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got := WatermarkAdvanceTime(time.Time{}, fallback)
	if !got.Equal(fallback) {
		t.Fatalf("got %v, want %v", got, fallback)
	}
}
