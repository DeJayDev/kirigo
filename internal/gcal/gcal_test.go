package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	calendar "google.golang.org/api/calendar/v3"
)

// fakeSvc is an in-memory calendarService for domain tests.
type fakeSvc struct {
	byID     map[string]*calendar.Event
	deleted  []string
	inserted []*calendar.Event
	seq      int
	freebusy *calendar.FreeBusyResponse
}

func newFake() *fakeSvc { return &fakeSvc{byID: map[string]*calendar.Event{}} }

func (f *fakeSvc) CalendarTimeZone(context.Context, string) (string, error) {
	return "America/Chicago", nil
}
func (f *fakeSvc) GetEvent(_ context.Context, _, id string) (*calendar.Event, error) {
	if e, ok := f.byID[id]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}
func (f *fakeSvc) InsertEvent(_ context.Context, _ string, e *calendar.Event) (*calendar.Event, error) {
	f.seq++
	out := *e
	if out.Id == "" {
		out.Id = fmt.Sprintf("gen%d", f.seq)
	}
	f.byID[out.Id] = &out
	f.inserted = append(f.inserted, &out)
	return &out, nil
}
func (f *fakeSvc) PatchEvent(_ context.Context, _, id string, patch *calendar.Event) (*calendar.Event, error) {
	e, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	merged := *e
	if patch.Summary != "" {
		merged.Summary = patch.Summary
	}
	if patch.Start != nil {
		merged.Start = patch.Start
	}
	f.byID[id] = &merged
	return &merged, nil
}
func (f *fakeSvc) DeleteEvent(_ context.Context, _, id string) error {
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}
func (f *fakeSvc) QuickAddEvent(ctx context.Context, cal, text string) (*calendar.Event, error) {
	return f.InsertEvent(ctx, cal, &calendar.Event{Summary: text})
}
func (f *fakeSvc) ListEvents(context.Context, string, string, time.Time, time.Time, int64) ([]*calendar.Event, error) {
	return nil, nil
}
func (f *fakeSvc) FreeBusy(context.Context, []string, time.Time, time.Time) (*calendar.FreeBusyResponse, error) {
	if f.freebusy != nil {
		return f.freebusy, nil
	}
	return &calendar.FreeBusyResponse{}, nil
}
func (f *fakeSvc) ListCalendars(context.Context) ([]*calendar.CalendarListEntry, error) {
	return nil, nil
}

func testApp(t *testing.T, svc calendarService) *App {
	t.Helper()
	return NewApp(svc, &Store{dir: t.TempDir()}, refNow)
}

func opIDs(t *testing.T, a *App) []LogEntry {
	t.Helper()
	res, err := a.Log(0)
	if err != nil {
		t.Fatal(err)
	}
	return res.(map[string]any)["ops"].([]LogEntry)
}

func TestCreateStripsAttendeesAndDefaultsAllDayEnd(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	_, err := a.Create(context.Background(), CreateParams{
		Title: "Party",
		Start: "2026-08-06",
		JSON:  strings.NewReader(`{"attendees":[{"email":"x@y.com"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.inserted) != 1 {
		t.Fatalf("inserted %d events, want 1", len(f.inserted))
	}
	got := f.inserted[0]
	if got.Attendees != nil {
		t.Errorf("attendees were not stripped: %+v", got.Attendees)
	}
	if got.Start.Date != "2026-08-06" {
		t.Errorf("start = %q, want 2026-08-06", got.Start.Date)
	}
	if got.End.Date != "2026-08-07" {
		t.Errorf("end = %q, want exclusive 2026-08-07", got.End.Date)
	}
	if ops := opIDs(t, a); len(ops) != 1 || ops[0].Verb != "create" {
		t.Errorf("expected one create op, got %+v", ops)
	}
}

func TestDeleteRequiresConfirm(t *testing.T) {
	f := newFake()
	f.byID["evt1"] = &calendar.Event{Id: "evt1", Summary: "X"}
	a := testApp(t, f)

	_, err := a.Delete(context.Background(), DeleteParams{EventID: "evt1"})
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("want ValidationError without --confirm, got %v", err)
	}
	if _, ok := f.byID["evt1"]; !ok {
		t.Error("event was deleted without --confirm")
	}

	if _, err := a.Delete(context.Background(), DeleteParams{EventID: "evt1", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.byID["evt1"]; ok {
		t.Error("event not deleted with --confirm")
	}
}

func TestDeleteDryRunNeedsNoConfirm(t *testing.T) {
	f := newFake()
	f.byID["evt1"] = &calendar.Event{Id: "evt1", Summary: "X"}
	a := testApp(t, f)

	res, err := a.Delete(context.Background(), DeleteParams{EventID: "evt1", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["status"] != "dry-run" {
		t.Errorf("status = %v, want dry-run", res.(map[string]any)["status"])
	}
	if _, ok := f.byID["evt1"]; !ok {
		t.Error("dry-run deleted the event")
	}
	if len(opIDs(t, a)) != 0 {
		t.Error("dry-run recorded an op")
	}
}

func TestScopeFollowingUnsupportedAndAllTargetsMaster(t *testing.T) {
	f := newFake()
	f.byID["inst1"] = &calendar.Event{Id: "inst1", RecurringEventId: "master1"}
	f.byID["master1"] = &calendar.Event{Id: "master1", Summary: "Standup"}
	a := testApp(t, f)

	_, err := a.Delete(context.Background(), DeleteParams{EventID: "inst1", Scope: "following", Confirm: true})
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("scope following should be a ValidationError, got %v", err)
	}

	if _, err := a.Delete(context.Background(), DeleteParams{EventID: "inst1", Scope: "all", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "master1" {
		t.Errorf("scope all deleted %v, want [master1]", f.deleted)
	}
}

func TestRestoreDeletedReinsertsWithNewID(t *testing.T) {
	f := newFake()
	f.byID["evt1"] = &calendar.Event{Id: "evt1", Summary: "Lunch"}
	a := testApp(t, f)

	if _, err := a.Delete(context.Background(), DeleteParams{EventID: "evt1", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	ops := opIDs(t, a)
	if len(ops) != 1 {
		t.Fatalf("want 1 op after delete, got %d", len(ops))
	}
	if _, err := a.Restore(context.Background(), ops[0].OpID, false, false, false); err != nil {
		t.Fatal(err)
	}
	if len(f.inserted) != 1 {
		t.Fatalf("restore inserted %d events, want 1", len(f.inserted))
	}
	got := f.inserted[0]
	if got.Summary != "Lunch" {
		t.Errorf("restored summary = %q, want Lunch", got.Summary)
	}
	if got.Id == "evt1" {
		t.Error("restore reused the original id; it must get a fresh one")
	}
}

func TestListEmptyEventsMarshalsAsArray(t *testing.T) {
	a := testApp(t, newFake()) // fake ListEvents returns nil
	res, err := a.List(context.Background(), nil, "now", "in 7 days", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res.(map[string]any)["events"])
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Errorf("empty events marshaled as %s, want []", b)
	}
}

func TestFreeBusyOmitsFreeWhenCalendarErrors(t *testing.T) {
	f := newFake()
	f.freebusy = &calendar.FreeBusyResponse{
		Calendars: map[string]calendar.FreeBusyCalendar{
			"primary": {Errors: []*calendar.Error{{Reason: "notFound"}}},
		},
	}
	a := testApp(t, f)

	res, err := a.FreeBusy(context.Background(), nil, "now", "in 7 days")
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if _, ok := m["free"]; ok {
		t.Error("free must be omitted when a calendar could not be read")
	}
	if s, _ := m["free_error"].(string); s == "" {
		t.Error("expected free_error explaining the omission")
	}
	cals := m["calendars"].([]map[string]any)
	if len(cals) != 1 {
		t.Fatalf("want 1 calendar entry, got %d", len(cals))
	}
	if _, ok := cals[0]["errors"]; !ok {
		t.Error("per-calendar errors not surfaced")
	}
}

func TestFreeGaps(t *testing.T) {
	parse := func(s string) time.Time {
		tt, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tt
	}
	from := parse("2026-08-06T09:00:00Z")
	to := parse("2026-08-06T17:00:00Z")
	busy := []interval{
		{parse("2026-08-06T10:00:00Z"), parse("2026-08-06T11:00:00Z")},
		{parse("2026-08-06T13:00:00Z"), parse("2026-08-06T14:00:00Z")},
	}
	free := freeGaps(busy, from, to)
	want := [][2]string{
		{"2026-08-06T09:00:00Z", "2026-08-06T10:00:00Z"},
		{"2026-08-06T11:00:00Z", "2026-08-06T13:00:00Z"},
		{"2026-08-06T14:00:00Z", "2026-08-06T17:00:00Z"},
	}
	if len(free) != len(want) {
		t.Fatalf("got %d gaps, want %d: %+v", len(free), len(want), free)
	}
	for i, w := range want {
		if free[i].Start != w[0] || free[i].End != w[1] {
			t.Errorf("gap %d = %v, want %v", i, free[i], w)
		}
	}
}
