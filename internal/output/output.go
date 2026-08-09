// Package output writes a value as JSON (default) or TOON, so binaries can offer
// a token-compact format for LLM/agent consumption without changing call sites.
package output

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DeJayDev/kirigo/internal/jsonout"
	toon "github.com/toon-format/toon-go"
)

// RegisterFlag adds the standard -format flag to fs and returns its value pointer,
// so every binary exposes output selection identically.
func RegisterFlag(fs *flag.FlagSet) *string {
	return fs.String("format", "", "output format: json (default) or toon; overrides KIRIGO_FORMAT")
}

// WriteError writes the standard {status, error} envelope to w in the given format.
func WriteError(w io.Writer, msg, format string) error {
	return Write(w, map[string]string{"status": "error", "error": msg}, format)
}

// ResolveFormat picks the output format: an explicit flag wins, then the
// KIRIGO_FORMAT env var, then agent-context autodetect (toon), then json. These
// binaries exist for agent consumption, so when a known agent harness is detected
// toon (far fewer tokens) is the better default; KIRIGO_FORMAT=json escapes it.
func ResolveFormat(flagValue string) (string, error) {
	f := pickFormat(flagValue)
	if f != "json" && f != "toon" {
		return "", fmt.Errorf("unknown output format %q (want json or toon)", f)
	}
	return f, nil
}

func pickFormat(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("KIRIGO_FORMAT"); v != "" {
		return v
	}
	if agentContext() {
		return "toon"
	}
	return "json"
}

func agentContext() bool {
	return os.Getenv("CLAUDECODE") == "1" ||
		os.Getenv("CODEX_THREAD_ID") != "" ||
		os.Getenv("OPENCODE") == "1" ||
		os.Getenv("KIRI") == "1"
}

// Write encodes v to w in the given format ("json"/"" or "toon").
func Write(w io.Writer, v any, format string) error {
	switch format {
	case "", "json":
		return jsonout.Write(w, v)
	case "toon":
		return writeTOON(w, v)
	default:
		return fmt.Errorf("unknown output format %q (want json or toon)", format)
	}
}

// writeTOON normalizes v through encoding/json first so TOON mirrors the JSON
// shape exactly (json tags + omitempty; toon-go otherwise reads only `toon` tags
// and Go field names), then compacts it into TOON's dense tabular form.
func writeTOON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var normalized any
	if err := json.Unmarshal(b, &normalized); err != nil {
		return err
	}
	out, err := toon.Marshal(compact(normalized))
	if err != nil {
		return err
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	_, err = w.Write(out)
	return err
}

// compact reshapes a JSON-normalized value so TOON can use its dense tabular form
// (header once, rows as CSV). An array of objects with all-scalar values gets a
// uniform key set — keys missing from a row become explicit null — and a scalar
// array is pipe-joined so it stays a scalar column instead of blocking tabular
// encoding. Arrays whose objects hold nested objects/arrays are left untouched
// (they can't tabularize, so filling would only add noise).
func compact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k := range t {
			t[k] = compact(t[k])
		}
		return t
	case []any:
		for i := range t {
			t[i] = compact(t[i])
		}
		switch {
		case len(t) >= 1 && allScalar(t):
			return strings.Join(scalarStrings(t), "|")
		case len(t) > 1 && allObjects(t) && allScalarValues(t):
			fillUniformKeys(t)
		}
		return t
	default:
		return v
	}
}

func isComposite(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func allScalar(t []any) bool {
	for _, el := range t {
		if isComposite(el) {
			return false
		}
	}
	return true
}

func allObjects(t []any) bool {
	for _, el := range t {
		if _, ok := el.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func allScalarValues(t []any) bool {
	for _, el := range t {
		for _, val := range el.(map[string]any) {
			if isComposite(val) {
				return false
			}
		}
	}
	return true
}

func fillUniformKeys(t []any) {
	keys := map[string]struct{}{}
	for _, el := range t {
		for k := range el.(map[string]any) {
			keys[k] = struct{}{}
		}
	}
	for _, el := range t {
		m := el.(map[string]any)
		for k := range keys {
			if _, ok := m[k]; !ok {
				m[k] = nil
			}
		}
	}
}

func scalarStrings(t []any) []string {
	out := make([]string, len(t))
	for i, el := range t {
		if el == nil {
			out[i] = ""
		} else {
			out[i] = fmt.Sprintf("%v", el)
		}
	}
	return out
}
