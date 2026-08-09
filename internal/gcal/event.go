package gcal

import (
	"time"

	calendar "google.golang.org/api/calendar/v3"
)

// Event is the trimmed, kirigo-owned shape emitted by default. Use --raw for the
// full Google resource.
type Event struct {
	ID               string     `json:"id"`
	CalendarID       string     `json:"calendar_id,omitempty"`
	Title            string     `json:"title"`
	Start            string     `json:"start"`
	End              string     `json:"end"`
	AllDay           bool       `json:"all_day"`
	TimeZone         string     `json:"timezone,omitempty"`
	Location         string     `json:"location,omitempty"`
	Description      string     `json:"description,omitempty"`
	Status           string     `json:"status,omitempty"`
	HTMLLink         string     `json:"html_link,omitempty"`
	Recurrence       []string   `json:"recurrence,omitempty"`
	RecurringEventID string     `json:"recurring_event_id,omitempty"`
	Attendees        []Attendee `json:"attendees,omitempty"`
	Organizer        string     `json:"organizer,omitempty"`
	Updated          string     `json:"updated,omitempty"`
}

// Attendee is read-only: this CLI never sets attendees on a write.
type Attendee struct {
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	ResponseStatus string `json:"response_status,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
}

// ToEvent projects a Google event onto the trimmed shape.
func ToEvent(e *calendar.Event, calendarID string) Event {
	out := Event{
		ID:               e.Id,
		CalendarID:       calendarID,
		Title:            e.Summary,
		Location:         e.Location,
		Description:      e.Description,
		Status:           e.Status,
		HTMLLink:         e.HtmlLink,
		Recurrence:       e.Recurrence,
		RecurringEventID: e.RecurringEventId,
		Updated:          e.Updated,
	}
	if e.Start != nil {
		if e.Start.Date != "" {
			out.Start, out.AllDay = e.Start.Date, true
		} else {
			out.Start = e.Start.DateTime
		}
		out.TimeZone = e.Start.TimeZone
	}
	if e.End != nil {
		if e.End.Date != "" {
			out.End = e.End.Date
		} else {
			out.End = e.End.DateTime
		}
	}
	if e.Organizer != nil {
		out.Organizer = e.Organizer.Email
	}
	for _, a := range e.Attendees {
		out.Attendees = append(out.Attendees, Attendee{
			Email:          a.Email,
			DisplayName:    a.DisplayName,
			ResponseStatus: a.ResponseStatus,
			Optional:       a.Optional,
			Organizer:      a.Organizer,
		})
	}
	return out
}

func startKey(e Event) time.Time {
	if e.AllDay {
		t, _ := time.Parse("2006-01-02", e.Start)
		return t
	}
	t, _ := time.Parse(time.RFC3339, e.Start)
	return t
}
