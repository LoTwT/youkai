package main

import (
	"testing"
	"time"
)

func TestReadinessSchedule(t *testing.T) {
	cases := []struct {
		value        string
		wantInterval time.Duration
		wantTotal    time.Duration
	}{
		{"1", 0, 0},
		{"once", 0, 0},
		{"60s", time.Second, 60 * time.Second},
		{"0s", 0, 0},
	}
	for _, tc := range cases {
		interval, total := readinessSchedule(tc.value)
		if interval != tc.wantInterval || total != tc.wantTotal {
			t.Errorf("readinessSchedule(%q) = %s, %s; want %s, %s",
				tc.value, interval, total, tc.wantInterval, tc.wantTotal)
		}
	}
}
