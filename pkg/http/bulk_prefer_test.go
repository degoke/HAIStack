package http

import "testing"

func TestPrefersRespondAsync(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"respond-async", true},
		{"Respond-Async", true},
		{"respond-async, wait=10", true},
		{"wait=10, respond-async", true},
		{"respond-async; wait=10", true},
		{"", false},
		{"return=minimal", false},
		{"respond-async-please", false},
	}
	for _, tc := range cases {
		if got := prefersRespondAsync(tc.header); got != tc.want {
			t.Errorf("prefersRespondAsync(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
