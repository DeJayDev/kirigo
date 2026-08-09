package ghapp

import (
	"strings"
	"testing"
)

func TestParseCredentialRequestStopsAtBlankLine(t *testing.T) {
	input := "protocol=https\nhost=github.com\npath=DeJayDev/kirigo.git\n\nignored=yes\n"
	fields, err := ParseCredentialRequest(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCredentialRequest: %v", err)
	}
	if fields["host"] != "github.com" || fields["protocol"] != "https" {
		t.Fatalf("fields = %#v", fields)
	}
	if _, ok := fields["ignored"]; ok {
		t.Fatal("read past the blank-line terminator")
	}
}

func TestWriteCredentialGetEmitsToken(t *testing.T) {
	var out strings.Builder
	request := map[string]string{"protocol": "https", "host": "github.com"}
	if err := WriteCredentialGet(&out, request, "ghs_tok"); err != nil {
		t.Fatalf("WriteCredentialGet: %v", err)
	}
	got := out.String()
	for _, want := range []string{"username=x-access-token", "password=ghs_tok", "host=github.com"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q missing %q", got, want)
		}
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("output must end with a blank line, got %q", got)
	}
}

func TestWriteCredentialGetSkipsNonGitHub(t *testing.T) {
	var out strings.Builder
	request := map[string]string{"protocol": "https", "host": "gitlab.com"}
	if err := WriteCredentialGet(&out, request, "ghs_tok"); err != nil {
		t.Fatalf("WriteCredentialGet: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("expected no output for non-github host, got %q", out.String())
	}
}
