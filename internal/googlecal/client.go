package googlecal

import (
	"context"
	"fmt"
	"time"

	calendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const sendUpdatesNone = "none" // this CLI never notifies attendees

type Client struct {
	svc *calendar.Service
}

func NewClient(ctx context.Context, opts ...option.ClientOption) (*Client, error) {
	svc, err := calendar.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("build calendar service: %w", err)
	}
	return &Client{svc: svc}, nil
}

func (c *Client) ListEvents(ctx context.Context, calendarID, q string, timeMin, timeMax time.Time, max int64) ([]*calendar.Event, error) {
	var out []*calendar.Event
	pageToken := ""
	for {
		call := c.svc.Events.List(calendarID).Context(ctx).
			SingleEvents(true).OrderBy("startTime").ShowDeleted(false).
			TimeMin(timeMin.Format(time.RFC3339)).TimeMax(timeMax.Format(time.RFC3339))
		if q != "" {
			call = call.Q(q)
		}
		if max > 0 {
			call = call.MaxResults(max)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		page, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list events: %w", err)
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" || (max > 0 && int64(len(out)) >= max) {
			break
		}
		pageToken = page.NextPageToken
	}
	if max > 0 && int64(len(out)) > max {
		out = out[:max]
	}
	return out, nil
}

func (c *Client) GetEvent(ctx context.Context, calendarID, eventID string) (*calendar.Event, error) {
	e, err := c.svc.Events.Get(calendarID, eventID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}
	return e, nil
}

func (c *Client) InsertEvent(ctx context.Context, calendarID string, e *calendar.Event) (*calendar.Event, error) {
	out, err := c.svc.Events.Insert(calendarID, e).SendUpdates(sendUpdatesNone).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}
	return out, nil
}

func (c *Client) PatchEvent(ctx context.Context, calendarID, eventID string, patch *calendar.Event) (*calendar.Event, error) {
	out, err := c.svc.Events.Patch(calendarID, eventID, patch).SendUpdates(sendUpdatesNone).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("patch event: %w", err)
	}
	return out, nil
}

func (c *Client) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	if err := c.svc.Events.Delete(calendarID, eventID).SendUpdates(sendUpdatesNone).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	return nil
}

func (c *Client) QuickAddEvent(ctx context.Context, calendarID, text string) (*calendar.Event, error) {
	out, err := c.svc.Events.QuickAdd(calendarID, text).SendUpdates(sendUpdatesNone).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("quickadd event: %w", err)
	}
	return out, nil
}

func (c *Client) FreeBusy(ctx context.Context, calendarIDs []string, timeMin, timeMax time.Time) (*calendar.FreeBusyResponse, error) {
	items := make([]*calendar.FreeBusyRequestItem, 0, len(calendarIDs))
	for _, id := range calendarIDs {
		items = append(items, &calendar.FreeBusyRequestItem{Id: id})
	}
	resp, err := c.svc.Freebusy.Query(&calendar.FreeBusyRequest{
		TimeMin: timeMin.Format(time.RFC3339),
		TimeMax: timeMax.Format(time.RFC3339),
		Items:   items,
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("freebusy query: %w", err)
	}
	return resp, nil
}

func (c *Client) ListCalendars(ctx context.Context) ([]*calendar.CalendarListEntry, error) {
	var out []*calendar.CalendarListEntry
	pageToken := ""
	for {
		call := c.svc.CalendarList.List().Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		page, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list calendars: %w", err)
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

func (c *Client) CalendarTimeZone(ctx context.Context, calendarID string) (string, error) {
	got, err := c.svc.Calendars.Get(calendarID).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("get calendar %s: %w", calendarID, err)
	}
	return got.TimeZone, nil
}
