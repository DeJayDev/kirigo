package gcal

import (
	"context"
	"sort"
	"strings"
	"sync"
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

// window resolves the calendars (defaulting to primary), the target timezone,
// and the parsed [from,to) bounds — the shared preamble of List and FreeBusy.
func (a *App) window(ctx context.Context, calendars []string, from, to string) ([]string, parsedTime, parsedTime, error) {
	if len(calendars) == 0 {
		calendars = []string{PrimaryCalendar}
	}
	loc, err := a.location(ctx, calendars[0])
	if err != nil {
		return nil, parsedTime{}, parsedTime{}, err
	}
	now := a.now()
	tmin, err := parseTime(from, loc, now)
	if err != nil {
		return nil, parsedTime{}, parsedTime{}, wrapParse("-from", err)
	}
	tmax, err := parseTime(to, loc, now)
	if err != nil {
		return nil, parsedTime{}, parsedTime{}, wrapParse("-to", err)
	}
	return calendars, tmin, tmax, nil
}

func (a *App) List(ctx context.Context, calendars []string, from, to, query string, max int64) (any, error) {
	calendars, tmin, tmax, err := a.window(ctx, calendars, from, to)
	if err != nil {
		return nil, err
	}
	// Fetch calendars concurrently; the merge+sort below makes per-calendar order irrelevant.
	perCal := make([][]Event, len(calendars))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i, cal := range calendars {
		wg.Go(func() {
			raw, err := a.svc.ListEvents(ctx, cal, query, tmin.Time, tmax.Time, max)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			evs := make([]Event, len(raw))
			for j, e := range raw {
				evs[j] = ToEvent(e, cal)
			}
			perCal[i] = evs
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	events := []Event{} // never nil, so an empty result marshals as [] not null
	for _, evs := range perCal {
		events = append(events, evs...)
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

type CalendarInfo struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	TimeZone   string `json:"timezone"`
	AccessRole string `json:"access_role"`
	Primary    bool   `json:"primary"`
}

func (a *App) Calendars(ctx context.Context) (any, error) {
	entries, err := a.svc.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CalendarInfo, 0, len(entries))
	for _, c := range entries {
		out = append(out, CalendarInfo{
			ID:         c.Id,
			Summary:    c.Summary,
			TimeZone:   c.TimeZone,
			AccessRole: c.AccessRole,
			Primary:    c.Primary,
		})
	}
	return map[string]any{"status": "ok", "calendars": out}, nil
}

// Window is a {start,end} RFC3339 span: a busy block or a free gap.
type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type interval struct{ start, end time.Time }

func (a *App) FreeBusy(ctx context.Context, calendars []string, from, to string) (any, error) {
	calendars, tmin, tmax, err := a.window(ctx, calendars, from, to)
	if err != nil {
		return nil, err
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
		busy := make([]Window, 0, len(fb.Busy))
		for _, p := range fb.Busy {
			busy = append(busy, Window{Start: p.Start, End: p.End})
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
func freeGaps(busy []interval, from, to time.Time) []Window {
	sort.Slice(busy, func(i, j int) bool { return busy[i].start.Before(busy[j].start) })
	free := []Window{}
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

func gap(a, b time.Time) Window {
	return Window{Start: a.Format(time.RFC3339), End: b.Format(time.RFC3339)}
}

func wrapParse(flag string, err error) error {
	return &ValidationError{"parse " + flag + ": " + err.Error()}
}
