package oauthlocal

import "testing"

func TestExtractCode(t *testing.T) {
	cases := map[string]string{
		"abc123": "abc123",
		"http://localhost:8765/callback?code=xyz&state=s": "xyz",
	}
	for input, want := range cases {
		got, err := ExtractCode(input)
		if err != nil {
			t.Fatalf("ExtractCode(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ExtractCode(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := ExtractCode("http://localhost/callback?state=s"); err == nil {
		t.Fatal("expected error for URL without code")
	}
}
