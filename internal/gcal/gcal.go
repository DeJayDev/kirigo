package gcal

import (
	"context"
	"sort"
	"strings"
	"time"

	calendar "google.golang.org/api/calendar/v3"
)

const PrimaryCalendar = "primary"

// ValidationError marks a bad-input error so main can exit 2 (vs 1 for runtime).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// calendarService is the narrow slice of the Calendar API the domain needs;
// googlecal.Client implements it, and tests supply a fake.
type calendarService interface {
	ListEvents(ctx context.Context, calendarID, q string, timeMin, timeMax time.Time, max int64) ([]*calendar.Event, error)
	GetEvent(ctx context.Context, calendarID, eventID string) (*calendar.Event, error)
	InsertEvent(ctx context.Context, calendarID string, e *calendar.Event) (*calendar.Event, error)
	PatchEvent(ctx context.Context, calendarID, eventID string, patch *calendar.Event) (*calendar.Event, error)
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
	QuickAddEvent(ctx context.Context, calendarID, text string) (*calendar.Event, error)
	FreeBusy(ctx context.Context, calendarIDs []string, timeMin, timeMax time.Time) (*calendar.FreeBusyResponse, error)
	ListCalendars(ctx context.Context) ([]*calendar.CalendarListEntry, error)
	CalendarTimeZone(ctx context.Context, calendarID string) (string, error)
}

type App struct {
	svc   calendarService
	store *Store
	now   func() time.Time
}

func NewApp(svc calendarService, store *Store, now func() time.Time) *App {
	if now == nil {
		now = time.Now
	}
	return &App{svc: svc, store: store, now: now}
}

func orPrimary(cal string) string {
	if cal == "" {
		return PrimaryCalendar
	}
	return cal
}

// location resolves the target calendar's IANA timezone, falling back to local.
func (a *App) location(ctx context.Context, calendarID string) (*time.Location, error) {
	tz, err := a.svc.CalendarTimeZone(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	if tz == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local, nil // ponytail: unknown tz name -> local, good enough
	}
	return loc, nil
}

func eventOut(e *calendar.Event, cal string, raw bool) any {
	if e == nil {
		return nil
	}
	if raw {
		return e
	}
	return ToEvent(e, cal)
}

func (a *App) List(ctx context.Context, calendars []string, from, to, query string, max int64) (any, error) {
	if len(calendars) == 0 {
		calendars = []string{PrimaryCalendar}
	}
	loc, err := a.location(ctx, calendars[0])
	if err != nil {
		return nil, err
	}
	now := a.now()
	tmin, err := parseTime(from, loc, now)
	if err != nil {
		return nil, wrapParse("-from", err)
	}
	tmax, err := parseTime(to, loc, now)
	if err != nil {
		return nil, wrapParse("-to", err)
	}
	events := []Event{} // never nil, so an empty result marshals as [] not null
	for _, cal := range calendars {
		raw, err := a.svc.ListEvents(ctx, cal, query, tmin.Time, tmax.Time, max)
		if err != nil {
			return nil, err
		}
		for _, e := range raw {
			events = append(events, ToEvent(e, cal))
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return startKey(events[i]).Before(startKey(events[j])) })
	if max > 0 && int64(len(events)) > max {
		events = events[:max] // the per-calendar cap is a page hint; the real cap is the merged set
	}
	return map[string]any{
		"status": "ok",
		"from":   tmin.Time.Format(time.RFC3339),
		"to":     tmax.Time.Format(time.RFC3339),
		"events": events,
	}, nil
}

func (a *App) Get(ctx context.Context, calendarID, eventID string, raw bool) (any, error) {
	cal := orPrimary(calendarID)
	e, err := a.svc.GetEvent(ctx, cal, eventID)
	if err != nil {
		return nil, err
	}
	return eventOut(e, cal, raw), nil
}

func (a *App) Calendars(ctx context.Context) (any, error) {
	entries, err := a.svc.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(entries))
	for _, c := range entries {
		out = append(out, map[string]any{
			"id":          c.Id,
			"summary":     c.Summary,
			"timezone":    c.TimeZone,
			"access_role": c.AccessRole,
			"primary":     c.Primary,
		})
	}
	return map[string]any{"status": "ok", "calendars": out}, nil
}

type interval struct{ start, end time.Time }

func (a *App) FreeBusy(ctx context.Context, calendars []string, from, to string) (any, error) {
	if len(calendars) == 0 {
		calendars = []string{PrimaryCalendar}
	}
	loc, err := a.location(ctx, calendars[0])
	if err != nil {
		return nil, err
	}
	now := a.now()
	tmin, err := parseTime(from, loc, now)
	if err != nil {
		return nil, wrapParse("-from", err)
	}
	tmax, err := parseTime(to, loc, now)
	if err != nil {
		return nil, wrapParse("-to", err)
	}
	resp, err := a.svc.FreeBusy(ctx, calendars, tmin.Time, tmax.Time)
	if err != nil {
		return nil, err
	}

	perCal := make([]map[string]any, 0, len(calendars))
	var allBusy []interval
	var failed []string
	for _, cal := range calendars {
		fb := resp.Calendars[cal]
		busy := make([]map[string]string, 0, len(fb.Busy))
		for _, p := range fb.Busy {
			busy = append(busy, map[string]string{"start": p.Start, "end": p.End})
			s, err1 := time.Parse(time.RFC3339, p.Start)
			e, err2 := time.Parse(time.RFC3339, p.End)
			if err1 == nil && err2 == nil {
				allBusy = append(allBusy, interval{s, e})
			}
		}
		entry := map[string]any{"id": cal, "busy": busy}
		if len(fb.Errors) > 0 {
			reasons := make([]string, 0, len(fb.Errors))
			for _, e := range fb.Errors {
				reasons = append(reasons, e.Reason)
			}
			entry["errors"] = reasons
			failed = append(failed, cal)
		}
		perCal = append(perCal, entry)
	}
	res := map[string]any{
		"status":    "ok",
		"from":      tmin.Time.Format(time.RFC3339),
		"to":        tmax.Time.Format(time.RFC3339),
		"calendars": perCal,
	}
	if len(failed) > 0 {
		// A calendar we couldn't read might be busy; reporting free windows would assert an availability we don't have.
		res["free_error"] = "free windows omitted; could not read: " + strings.Join(failed, ", ")
	} else {
		res["free"] = freeGaps(allBusy, tmin.Time, tmax.Time)
	}
	return res, nil
}

// freeGaps merges busy intervals and returns the open windows within [from,to).
func freeGaps(busy []interval, from, to time.Time) []map[string]string {
	sort.Slice(busy, func(i, j int) bool { return busy[i].start.Before(busy[j].start) })
	free := []map[string]string{}
	cursor := from
	for _, b := range busy {
		s, e := b.start, b.end
		if s.Before(from) {
			s = from
		}
		if s.After(to) {
			s = to
		}
		if e.After(to) {
			e = to
		}
		if !e.After(cursor) {
			continue
		}
		if s.After(cursor) {
			free = append(free, gap(cursor, s))
		}
		cursor = e
	}
	if cursor.Before(to) {
		free = append(free, gap(cursor, to))
	}
	return free
}

func gap(a, b time.Time) map[string]string {
	return map[string]string{"start": a.Format(time.RFC3339), "end": b.Format(time.RFC3339)}
}

func wrapParse(flag string, err error) error {
	return &ValidationError{"parse " + flag + ": " + err.Error()}
}
