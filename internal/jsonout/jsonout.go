package jsonout

import (
	"encoding/json"
	"io"
)

func Write(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
