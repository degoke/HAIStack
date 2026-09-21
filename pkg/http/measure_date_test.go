package http

import (
	"testing"
	"time"
)

func TestParseMeasureDateKeepsOffset(t *testing.T) {
	got, err := parseMeasureDate("2021-01-01T00:00:00-05:00", "periodEnd")
	if err != nil {
		t.Fatal(err)
	}
	if got.Location() == time.UTC {
		t.Fatalf("offset datetime must keep its zone, got %v", got)
	}
	if got.Hour() != 0 || got.Day() != 1 {
		t.Fatalf("local midnight was converted away: %v", got)
	}
	dateOnly, err := parseMeasureDate("2021-01-01", "periodEnd")
	if err != nil {
		t.Fatal(err)
	}
	if dateOnly.Hour() != 0 || dateOnly.Location() != time.UTC {
		t.Fatalf("date-only should stay UTC midnight: %v", dateOnly)
	}
}
