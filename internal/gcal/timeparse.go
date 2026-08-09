package gcal

import (
	"strconv"
	"strings"
	"time"

	naturaldate "github.com/tj/go-naturaldate"
)

// parsedTime is a resolved instant plus whether the input named only a date
// (which maps to an all-day event / a midnight window bound).
type parsedTime struct {
	Time     time.Time
	DateOnly bool
}

// parseTime resolves value against now, interpreting bare/relative forms in loc.
// Machine formats (RFC3339, ISO date/datetime) are handled directly; a bare
// YYYY-MM-DD is the all-day signal. Everything else is natural language via
// go-naturaldate, biased to the future ("next friday", "in 2 hours", "tomorrow
// at 3pm", "10am").
func parseTime(value string, loc *time.Location, now time.Time) (parsedTime, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return parsedTime{}, &ValidationError{"empty time value"}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return parsedTime{Time: t}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return parsedTime{Time: t, DateOnly: true}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", v, loc); err == nil {
		return parsedTime{Time: t}, nil
	}
	// go-naturaldate returns the reference time unchanged (no error) for input it
	// doesn't recognize, so guard against silently treating gibberish as "now".
	ref := now.In(loc)
	t, err := naturaldate.Parse(v, ref, naturaldate.WithDirection(naturaldate.Future))
	if err != nil || (t.Equal(ref) && !isNow(v)) {
		return parsedTime{}, &ValidationError{"cannot parse time " + strconv.Quote(value)}
	}
	return parsedTime{Time: t}, nil
}

func isNow(v string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(v), " ")) {
	case "now", "right now":
		return true
	}
	return false
}
