package googlecal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestTokenSourceRefreshesAndPersistsToken(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		got = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "token.json")
	if err := saveToken(path, &oauth2.Token{
		AccessToken:  "expired-access",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &oauth2.Config{ClientID: "client-id", ClientSecret: "client-secret", Endpoint: oauth2.Endpoint{TokenURL: server.URL}}
	source, err := TokenSource(context.Background(), cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := source.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "fresh-access" {
		t.Fatalf("access token = %q, want fresh-access", tok.AccessToken)
	}
	if got.Get("grant_type") != "refresh_token" || got.Get("refresh_token") != "refresh-token" {
		t.Fatalf("refresh request = %v", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved oauth2.Token
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.AccessToken != "fresh-access" {
		t.Fatalf("saved access token = %q, want fresh-access", saved.AccessToken)
	}
	if saved.RefreshToken != "refresh-token" {
		t.Fatalf("saved refresh token = %q, want refresh-token", saved.RefreshToken)
	}
}

func TestPersistingSourceReportsSaveFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	source := &persistingSource{
		base: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "fresh-access"}),
		path: filepath.Join(parent, "token.json"),
	}
	_, err := source.Token()
	if err == nil || !strings.Contains(err.Error(), "persist refreshed token") {
		t.Fatalf("Token() error = %v, want token persistence error", err)
	}
}
