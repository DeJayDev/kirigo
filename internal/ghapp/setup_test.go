package ghapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConvertManifestExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app-manifests/abc123/conversions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id": 777,
			"slug": "kirigo-agent-git",
			"client_id": "Iv1.abc",
			"client_secret": "secret",
			"webhook_secret": "hook",
			"pem": "-----BEGIN RSA PRIVATE KEY-----\nMII\n-----END RSA PRIVATE KEY-----",
			"html_url": "https://github.com/apps/kirigo-agent-git"
		}`))
	}))
	defer server.Close()

	app, err := NewClientWithHTTP(server.Client(), server.URL).ConvertManifest(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("ConvertManifest: %v", err)
	}
	if app.ID != 777 || app.ClientID != "Iv1.abc" || app.PEM == "" {
		t.Fatalf("app = %#v", app)
	}
}

func TestExtractCode(t *testing.T) {
	cases := map[string]string{
		"abc123": "abc123",
		"http://localhost:8765/callback?code=xyz&state=s": "xyz",
	}
	for input, want := range cases {
		got, err := extractCode(input)
		if err != nil {
			t.Fatalf("extractCode(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("extractCode(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := extractCode("http://localhost/callback?state=s"); err == nil {
		t.Fatal("expected error for URL without code")
	}
}

func TestManifestFormHTMLEscapesManifest(t *testing.T) {
	html, err := manifestFormHTML("https://github.com/settings/apps/new?state=s", manifest{
		Name:               "kirigo-agent-git",
		RedirectURL:        "http://localhost:8765/callback",
		DefaultPermissions: manifestPermissions,
	})
	if err != nil {
		t.Fatalf("manifestFormHTML: %v", err)
	}
	body := string(html)
	if !strings.Contains(body, `name="manifest"`) {
		t.Fatal("form missing manifest input")
	}
	// The JSON is embedded in an HTML attribute, so its quotes must be escaped.
	if strings.Contains(body, `value=""kirigo`) {
		t.Fatal("manifest JSON was not attribute-escaped")
	}
}
