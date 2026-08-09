package gcal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	calendar "google.golang.org/api/calendar/v3"
)

// Store holds per-account before/after snapshots, one file per mutation.
type Store struct {
	dir string
}

func NewStore(account string) (*Store, error) {
	dir, err := AccountDir(account)
	if err != nil {
		return nil, err
	}
	return &Store{dir: filepath.Join(dir, "backups")}, nil
}

// opRecord is the full snapshot of one mutation. before==nil means the entity
// did not exist (a create); after==nil means it no longer exists (a delete).
type opRecord struct {
	OpID     string          `json:"op_id"`
	Time     time.Time       `json:"time"`
	Verb     string          `json:"verb"`
	Calendar string          `json:"calendar"`
	EventID  string          `json:"event_id,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Before   *calendar.Event `json:"before"`
	After    *calendar.Event `json:"after"`
}

func (s *Store) write(rec opRecord) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, rec.OpID+".json"), data, 0o600)
}

func (s *Store) read(opID string) (opRecord, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, opID+".json"))
	if err != nil {
		return opRecord{}, err
	}
	var rec opRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return opRecord{}, fmt.Errorf("parse op %s: %w", opID, err)
	}
	return rec, nil
}

// list returns records newest-first (op ids sort chronologically by construction).
func (s *Store) list() ([]opRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var recs []opRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := s.read(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].OpID > recs[j].OpID })
	return recs, nil
}

func (s *Store) remove(opID string) error {
	return os.Remove(filepath.Join(s.dir, opID+".json"))
}

// LogEntry is the trimmed header shown by `gcal log`.
type LogEntry struct {
	OpID     string    `json:"op_id"`
	Time     time.Time `json:"time"`
	Verb     string    `json:"verb"`
	Calendar string    `json:"calendar"`
	EventID  string    `json:"event_id,omitempty"`
	Summary  string    `json:"summary,omitempty"`
}

func (a *App) Log(max int) (any, error) {
	recs, err := a.store.list()
	if err != nil {
		return nil, err
	}
	if max > 0 && len(recs) > max {
		recs = recs[:max]
	}
	entries := make([]LogEntry, 0, len(recs))
	for _, r := range recs {
		entries = append(entries, LogEntry{r.OpID, r.Time, r.Verb, r.Calendar, r.EventID, r.Summary})
	}
	return map[string]any{"status": "ok", "ops": entries}, nil
}

func (a *App) Prune(before string, all bool) (any, error) {
	if !all && strings.TrimSpace(before) == "" {
		return nil, &ValidationError{"prune requires -before <time> or --all"}
	}
	var cutoff time.Time
	if !all {
		pt, err := parseTime(before, time.Local, a.now())
		if err != nil {
			return nil, err
		}
		cutoff = pt.Time
	}
	recs, err := a.store.list()
	if err != nil {
		return nil, err
	}
	pruned := 0
	for _, r := range recs {
		if all || r.Time.Before(cutoff) {
			if err := a.store.remove(r.OpID); err == nil {
				pruned++
			}
		}
	}
	return map[string]any{"status": "ok", "pruned": pruned}, nil
}
