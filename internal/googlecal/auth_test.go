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
	"sync/atomic"
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
	if saved.Expiry.IsZero() {
		t.Fatal("saved expiry is zero; next run would never refresh")
	}
}

func TestTokenSourceRefreshesWhenExpiryMissing(t *testing.T) {
	// oauth2.Token.Valid treats zero Expiry as never-expiring. A token.json that
	// only has access+refresh (or lost expiry) must still force a refresh.
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "token.json")
	// Write raw JSON without expiry — the real-world failure shape.
	if err := os.WriteFile(path, []byte(`{
  "access_token": "stale-access",
  "token_type": "Bearer",
  "refresh_token": "refresh-token"
}`), 0o600); err != nil {
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
	if refreshes.Load() != 1 {
		t.Fatalf("refresh count = %d, want 1", refreshes.Load())
	}

	var saved oauth2.Token
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "refresh-token" {
		t.Fatalf("saved refresh token = %q, want refresh-token", saved.RefreshToken)
	}
	if saved.Expiry.IsZero() {
		t.Fatal("saved expiry is still zero after refresh")
	}
}

func TestTokenSourceUsesDetachedContextForRefresh(t *testing.T) {
	// Command contexts are short-lived (30s). Refresh must not inherit cancellation
	// from the caller's cancelled parent after TokenSource construction.
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		// Simulate a slow token endpoint; the parent ctx is already cancelled.
		time.Sleep(50 * time.Millisecond)
		if err := r.Context().Err(); err != nil {
			http.Error(w, "cancelled: "+err.Error(), http.StatusInternalServerError)
			return
		}
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

	parent, cancel := context.WithCancel(context.Background())
	cfg := &oauth2.Config{ClientID: "client-id", ClientSecret: "client-secret", Endpoint: oauth2.Endpoint{TokenURL: server.URL}}
	source, err := TokenSource(parent, cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // command deadline fires before the API call obtains a token

	tok, err := source.Token()
	if err != nil {
		t.Fatalf("Token() after parent cancel: %v", err)
	}
	if tok.AccessToken != "fresh-access" {
		t.Fatalf("access token = %q, want fresh-access", tok.AccessToken)
	}
	select {
	case <-started:
	default:
		t.Fatal("token endpoint was never hit")
	}
}

func TestTokenSourceRejectsExpiredTokenWithoutRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	if err := saveToken(path, &oauth2.Token{
		AccessToken: "expired-access",
		Expiry:      time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &oauth2.Config{ClientID: "client-id", ClientSecret: "client-secret", Endpoint: oauth2.Endpoint{TokenURL: "http://127.0.0.1:0"}}
	_, err := TokenSource(context.Background(), cfg, path)
	if err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("TokenSource() error = %v, want no refresh token", err)
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

func TestSaveTokenPreservesRefreshToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	if err := saveToken(path, &oauth2.Token{
		AccessToken:  "old",
		RefreshToken: "keep-me",
		Expiry:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Google refresh responses usually omit refresh_token.
	if err := saveToken(path, &oauth2.Token{
		AccessToken: "new",
		Expiry:      time.Now().Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved oauth2.Token
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.AccessToken != "new" || saved.RefreshToken != "keep-me" {
		t.Fatalf("saved = %+v, want access=new refresh=keep-me", saved)
	}
}
