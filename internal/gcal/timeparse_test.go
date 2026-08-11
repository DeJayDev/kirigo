package gcal

import (
	"errors"
	"testing"
	"time"
)

var chicago = mustLoad("America/Chicago")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Wednesday 2026-08-05 12:00 America/Chicago.
func refNow() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, chicago) }

func TestParseTime(t *testing.T) {
	now := refNow()
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, chicago)
	}
	cases := []struct {
		in       string
		wantTime time.Time
		dateOnly bool
	}{
		{"now", now, false},
		{"today", at(2026, 8, 5, 0, 0), false},
		{"tomorrow", at(2026, 8, 6, 0, 0), false},
		{"yesterday", at(2026, 8, 4, 0, 0), false},
		{"in 2 hours", at(2026, 8, 5, 14, 0), false},
		{"10am", at(2026, 8, 5, 10, 0), false},
		{"5pm", at(2026, 8, 5, 17, 0), false},
		{"tomorrow at 3pm", at(2026, 8, 6, 15, 0), false},
		{"next monday", at(2026, 8, 10, 0, 0), false},
		{"2026-08-10", at(2026, 8, 10, 0, 0), true},
		{"2026-08-10 15:30", at(2026, 8, 10, 15, 30), false},
	}
	for _, c := range cases {
		got, err := parseTime(c.in, chicago, now)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if !got.Time.Equal(c.wantTime) {
			t.Errorf("%q: time = %v, want %v", c.in, got.Time, c.wantTime)
		}
		if got.DateOnly != c.dateOnly {
			t.Errorf("%q: dateOnly = %v, want %v", c.in, got.DateOnly, c.dateOnly)
		}
	}
}

func TestParseTimeRFC3339(t *testing.T) {
	got, err := parseTime("2026-08-10T15:30:00-05:00", chicago, refNow())
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 10, 15, 30, 0, 0, chicago)
	if !got.Time.Equal(want) {
		t.Errorf("time = %v, want %v", got.Time, want)
	}
	if got.DateOnly {
		t.Error("RFC3339 should not be date-only")
	}
}

func TestParseTimeRelativeDays(t *testing.T) {
	got, err := parseTime("in 7 days", chicago, refNow())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Time.After(refNow().AddDate(0, 0, 6)) || got.Time.After(refNow().AddDate(0, 0, 8)) {
		t.Errorf("in 7 days = %v, want ~7 days out", got.Time)
	}
}

func TestParseTimeInvalid(t *testing.T) {
	_, err := parseTime("wat", chicago, refNow())
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Errorf("want ValidationError, got %v", err)
	}
}
