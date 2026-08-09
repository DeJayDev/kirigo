package output

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

type ev struct {
	ID     string `json:"id"`
	AllDay bool   `json:"all_day"`
	Loc    string `json:"location,omitempty"`
}

func TestWriteTOONMirrorsJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, map[string]any{"events": []ev{{ID: "a", AllDay: true}}}, "toon"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "all_day") {
		t.Errorf("want json tag key all_day, got:\n%s", s)
	}
	if strings.Contains(s, "AllDay") {
		t.Errorf("leaked Go field name AllDay:\n%s", s)
	}
	if strings.Contains(s, "location") {
		t.Errorf("omitempty not honored, empty location present:\n%s", s)
	}
	if !strings.HasSuffix(s, "\n") {
		t.Error("output should end with a newline")
	}
}

func TestWriteJSONDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, map[string]any{"status": "ok"}, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\"status\": \"ok\"") {
		t.Errorf("unexpected json: %s", buf.String())
	}
}

func TestWriteUnknownFormat(t *testing.T) {
	if err := Write(io.Discard, map[string]any{}, "xml"); err == nil {
		t.Error("want an error for an unknown format")
	}
}

func TestCompactUniformKeysEnablesTabular(t *testing.T) {
	var buf bytes.Buffer
	payload := map[string]any{"items": []any{
		map[string]any{"a": "1", "b": "2"},
		map[string]any{"a": "3"}, // b missing -> filled with null so rows are uniform
	}}
	if err := Write(&buf, payload, "toon"); err != nil {
		t.Fatal(err)
	}
	if s := buf.String(); !strings.Contains(s, "items[2]{") {
		t.Errorf("expected dense tabular header, got:\n%s", s)
	}
}

func TestCompactScalarArrayJoined(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, map[string]any{"tags": []any{"x", "y", "z"}}, "toon"); err != nil {
		t.Fatal(err)
	}
	if s := buf.String(); !strings.Contains(s, "x|y|z") {
		t.Errorf("expected pipe-joined scalar array, got:\n%s", s)
	}
}

func TestCompactSkipsArraysWithNestedObjects(t *testing.T) {
	var buf bytes.Buffer
	// one row has a nested object array -> not tabularizable -> leave as-is
	payload := map[string]any{"items": []any{
		map[string]any{"a": "1", "kids": []any{map[string]any{"x": "1"}}},
		map[string]any{"a": "2"},
	}}
	if err := Write(&buf, payload, "toon"); err != nil {
		t.Fatal(err)
	}
	if s := buf.String(); strings.Contains(s, "items[2]{") {
		t.Errorf("should not tabularize rows with nested objects, got:\n%s", s)
	}
}

func TestResolveFormat(t *testing.T) {
	// clear ambient agent env (CLAUDECODE may be set in this session)
	for _, v := range []string{"KIRIGO_FORMAT", "CLAUDECODE", "CODEX_THREAD_ID", "OPENCODE", "KIRI"} {
		t.Setenv(v, "")
	}
	check := func(flagVal, want string) {
		t.Helper()
		got, err := ResolveFormat(flagVal)
		if err != nil {
			t.Fatalf("ResolveFormat(%q) error: %v", flagVal, err)
		}
		if got != want {
			t.Errorf("ResolveFormat(%q) = %q, want %q", flagVal, got, want)
		}
	}
	check("", "json")
	check("toon", "toon")
	t.Setenv("CLAUDECODE", "1")
	check("", "toon") // agent autodetect
	t.Setenv("KIRIGO_FORMAT", "json")
	check("", "json")     // env overrides agent autodetect
	check("toon", "toon") // flag overrides env
}

func TestResolveFormatRejectsUnknown(t *testing.T) {
	for _, v := range []string{"KIRIGO_FORMAT", "CLAUDECODE", "CODEX_THREAD_ID", "OPENCODE", "KIRI"} {
		t.Setenv(v, "")
	}
	if _, err := ResolveFormat("xml"); err == nil {
		t.Error("want an error for an unknown format")
	}
}
