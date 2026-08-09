package gcal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	calendar "google.golang.org/api/calendar/v3"
)

type CreateParams struct {
	Calendar    string
	Title       string
	Start       string
	End         string
	Description string
	JSON        io.Reader
	DryRun      bool
	Raw         bool
}

func (a *App) Create(ctx context.Context, p CreateParams) (any, error) {
	cal := orPrimary(p.Calendar)
	loc, err := a.location(ctx, cal)
	if err != nil {
		return nil, err
	}
	e := &calendar.Event{}
	if p.JSON != nil {
		if err := json.NewDecoder(p.JSON).Decode(e); err != nil {
			return nil, &ValidationError{"decode --json: " + err.Error()}
		}
	}
	e.Attendees = nil // guardrail: this CLI never sets attendees
	if p.Title != "" {
		e.Summary = p.Title
	}
	if p.Description != "" {
		e.Description = p.Description
	}
	if p.Start != "" {
		dt, err := eventDateTime(p.Start, loc, a.now())
		if err != nil {
			return nil, wrapParse("-start", err)
		}
		e.Start = dt
	}
	if p.End != "" {
		dt, err := eventDateTime(p.End, loc, a.now())
		if err != nil {
			return nil, wrapParse("-end", err)
		}
		e.End = dt
	}
	// All-day events use an exclusive end date; default a missing end to start+1 day.
	if e.Start != nil && e.Start.Date != "" && e.End == nil {
		d, _ := time.ParseInLocation("2006-01-02", e.Start.Date, loc)
		e.End = &calendar.EventDateTime{Date: d.AddDate(0, 0, 1).Format("2006-01-02")}
	}
	if e.Start == nil || e.End == nil {
		return nil, &ValidationError{"both -start and -end are required"}
	}

	if p.DryRun {
		return dryRunResult("create", cal, nil, e, p.Raw), nil
	}
	created, err := a.svc.InsertEvent(ctx, cal, e)
	if err != nil {
		return nil, err
	}
	return a.writeResult("create", cal, nil, created, p.Raw), nil
}

type UpdateParams struct {
	Calendar    string
	EventID     string
	Title       string
	Start       string
	End         string
	Description string
	JSON        io.Reader
	Scope       string
	DryRun      bool
	Raw         bool
}

func (a *App) Update(ctx context.Context, p UpdateParams) (any, error) {
	cal := orPrimary(p.Calendar)
	targetID, err := a.scopeTarget(ctx, cal, p.EventID, p.Scope)
	if err != nil {
		return nil, err
	}
	before, err := a.svc.GetEvent(ctx, cal, targetID)
	if err != nil {
		return nil, err
	}

	patch := &calendar.Event{}
	if p.JSON != nil {
		if err := json.NewDecoder(p.JSON).Decode(patch); err != nil {
			return nil, &ValidationError{"decode --json: " + err.Error()}
		}
	}
	patch.Attendees = nil // guardrail: never add/remove attendees; PATCH leaves existing ones untouched
	if p.Title != "" {
		patch.Summary = p.Title
	}
	if p.Description != "" {
		patch.Description = p.Description
	}
	if p.Start != "" || p.End != "" {
		loc, err := a.location(ctx, cal)
		if err != nil {
			return nil, err
		}
		if p.Start != "" {
			dt, err := eventDateTime(p.Start, loc, a.now())
			if err != nil {
				return nil, wrapParse("-start", err)
			}
			patch.Start = dt
		}
		if p.End != "" {
			dt, err := eventDateTime(p.End, loc, a.now())
			if err != nil {
				return nil, wrapParse("-end", err)
			}
			patch.End = dt
		}
	}
	patch = stripForWrite(patch) // drop attendees + server-owned fields so a get --raw | update round-trips cleanly

	if p.DryRun {
		return map[string]any{
			"status":   "dry-run",
			"op":       "update",
			"calendar": cal,
			"before":   eventOut(before, cal, p.Raw),
			"patch":    eventOut(patch, cal, p.Raw),
		}, nil
	}
	updated, err := a.svc.PatchEvent(ctx, cal, targetID, patch)
	if err != nil {
		return nil, err
	}
	return a.writeResult("update", cal, before, updated, p.Raw), nil
}

type DeleteParams struct {
	Calendar string
	EventID  string
	Scope    string
	Confirm  bool
	DryRun   bool
	Raw      bool
}

func (a *App) Delete(ctx context.Context, p DeleteParams) (any, error) {
	cal := orPrimary(p.Calendar)
	targetID, err := a.scopeTarget(ctx, cal, p.EventID, p.Scope)
	if err != nil {
		return nil, err
	}
	before, err := a.svc.GetEvent(ctx, cal, targetID)
	if err != nil {
		return nil, err
	}
	if p.DryRun {
		return dryRunResult("delete", cal, before, nil, p.Raw), nil
	}
	if !p.Confirm {
		return nil, &ValidationError{"delete requires --confirm"}
	}
	if err := a.svc.DeleteEvent(ctx, cal, targetID); err != nil {
		return nil, err
	}
	return a.writeResult("delete", cal, before, nil, p.Raw), nil
}

func (a *App) QuickAdd(ctx context.Context, calendarID, text string, dryRun, raw bool) (any, error) {
	cal := orPrimary(calendarID)
	if text == "" {
		return nil, &ValidationError{"quickadd text is required"}
	}
	if dryRun {
		return map[string]any{"status": "dry-run", "op": "quickadd", "calendar": cal, "text": text}, nil
	}
	created, err := a.svc.QuickAddEvent(ctx, cal, text)
	if err != nil {
		return nil, err
	}
	return a.writeResult("quickadd", cal, nil, created, raw), nil
}

// Restore re-applies the saved `before` state of an op: it deletes a created
// event, patches an updated event back, or re-inserts a deleted event (with a
// new id — the original is tombstoned by Google). The restore is itself recorded.
func (a *App) Restore(ctx context.Context, opID string, last, dryRun, raw bool) (any, error) {
	var rec opRecord
	if last {
		recs, err := a.store.list()
		if err != nil {
			return nil, err
		}
		if len(recs) == 0 {
			return nil, &ValidationError{"no ops to restore"}
		}
		rec = recs[0]
	} else {
		if opID == "" {
			return nil, &ValidationError{"restore requires an <op-id> or --last"}
		}
		var err error
		rec, err = a.store.read(opID)
		if err != nil {
			return nil, &ValidationError{"read op " + opID + ": " + err.Error()}
		}
	}
	cal := rec.Calendar

	switch {
	case rec.Before == nil && rec.After != nil: // was create -> delete
		if dryRun {
			return dryRunResult("restore", cal, rec.After, nil, raw), nil
		}
		if err := a.svc.DeleteEvent(ctx, cal, rec.After.Id); err != nil {
			return nil, err
		}
		return a.restoreResult(rec, cal, rec.After, nil, nil, raw), nil
	case rec.Before != nil && rec.After != nil: // was update -> patch back
		if dryRun {
			return dryRunResult("restore", cal, rec.After, rec.Before, raw), nil
		}
		patch := stripForWrite(rec.Before)
		// PATCH leaves absent fields untouched, so force-send the text fields our
		// flags manage; otherwise restoring an edit that *added* a title/description
		// wouldn't clear it back to empty. Fields added via -json aren't reverted.
		patch.ForceSendFields = append(patch.ForceSendFields, "Summary", "Description")
		updated, err := a.svc.PatchEvent(ctx, cal, rec.Before.Id, patch)
		if err != nil {
			return nil, err
		}
		return a.restoreResult(rec, cal, rec.After, updated, updated, raw), nil
	case rec.Before != nil && rec.After == nil: // was delete -> re-insert (new id)
		if dryRun {
			return dryRunResult("restore", cal, nil, rec.Before, raw), nil
		}
		insert := stripForWrite(rec.Before)
		insert.Id = ""
		created, err := a.svc.InsertEvent(ctx, cal, insert)
		if err != nil {
			return nil, err
		}
		return a.restoreResult(rec, cal, nil, created, created, raw), nil
	default:
		return nil, &ValidationError{"op has no state to restore"}
	}
}

func (a *App) scopeTarget(ctx context.Context, cal, eventID, scope string) (string, error) {
	switch scope {
	case "", "instance":
		return eventID, nil
	case "all":
		e, err := a.svc.GetEvent(ctx, cal, eventID)
		if err != nil {
			return "", err
		}
		if e.RecurringEventId != "" {
			return e.RecurringEventId, nil
		}
		return eventID, nil
	case "following":
		// ponytail: no RRULE-split for 'following'; add series truncation when an agent needs it.
		return "", &ValidationError{"scope 'following' is not supported in v1; use 'instance' or 'all'"}
	default:
		return "", &ValidationError{"invalid --scope " + scope + " (want instance, following, or all)"}
	}
}

// writeResult records the op snapshot and returns the success envelope.
func (a *App) writeResult(verb, cal string, before, after *calendar.Event, raw bool) map[string]any {
	opID, berr := a.record(verb, cal, before, after)
	shown := after
	if shown == nil {
		shown = before
	}
	res := map[string]any{"status": "ok", "op_id": opID, "event": eventOut(shown, cal, raw)}
	if berr != nil {
		res["backup_error"] = berr.Error()
	}
	return res
}

func (a *App) restoreResult(orig opRecord, cal string, before, after *calendar.Event, shown *calendar.Event, raw bool) map[string]any {
	opID, berr := a.record("restore", cal, before, after)
	res := map[string]any{"status": "ok", "op": "restore", "restored_op": orig.OpID, "op_id": opID, "event": eventOut(shown, cal, raw)}
	if berr != nil {
		res["backup_error"] = berr.Error()
	}
	return res
}

func (a *App) record(verb, cal string, before, after *calendar.Event) (string, error) {
	rec := opRecord{
		OpID:     a.newOpID(),
		Time:     a.now().UTC(),
		Verb:     verb,
		Calendar: cal,
		Before:   before,
		After:    after,
	}
	if after != nil {
		rec.EventID, rec.Summary = after.Id, after.Summary
	} else if before != nil {
		rec.EventID, rec.Summary = before.Id, before.Summary
	}
	return rec.OpID, a.store.write(rec)
}

func (a *App) newOpID() string {
	// Nanosecond precision so distinct ops sort chronologically for `restore --last`.
	// ponytail: a frozen clock (tests) can't discriminate; the random suffix only
	// guards the filename then, not ordering.
	return a.now().UTC().Format("20060102T150405.000000000Z") + "-" + randHex(3)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func dryRunResult(op, cal string, before, after *calendar.Event, raw bool) map[string]any {
	return map[string]any{
		"status":   "dry-run",
		"op":       op,
		"calendar": cal,
		"before":   eventOut(before, cal, raw),
		"after":    eventOut(after, cal, raw),
	}
}

func eventDateTime(value string, loc *time.Location, now time.Time) (*calendar.EventDateTime, error) {
	pt, err := parseTime(value, loc, now)
	if err != nil {
		return nil, err
	}
	if pt.DateOnly {
		return &calendar.EventDateTime{Date: pt.Time.Format("2006-01-02")}, nil
	}
	dt := &calendar.EventDateTime{DateTime: pt.Time.Format(time.RFC3339)}
	if loc != time.Local {
		dt.TimeZone = loc.String() // the offset in DateTime already carries the instant; "Local" is not a valid IANA zone
	}
	return dt, nil
}

// stripForWrite clones an event without server-owned or attendee fields so it can
// be re-inserted or patched cleanly.
func stripForWrite(e *calendar.Event) *calendar.Event {
	c := *e
	c.Attendees = nil
	c.Etag = ""
	c.HtmlLink = ""
	c.Created = ""
	c.Updated = ""
	c.ICalUID = ""
	c.Creator = nil
	c.Organizer = nil
	c.Sequence = 0
	c.Kind = ""
	c.HangoutLink = ""
	return &c
}
