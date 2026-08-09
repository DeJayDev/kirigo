package ghapp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{AppID: "123456", InstallationID: "42", PrivateKeyPEM: testPrivateKeyPEM}
}

func newTestService(t *testing.T, handler http.HandlerFunc, now time.Time) (*Service, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	service := &Service{
		Client:    NewClientWithHTTP(server.Client(), server.URL),
		CachePath: filepath.Join(t.TempDir(), cacheFileName),
		Now:       func() time.Time { return now },
	}
	return service, server.Close
}

func TestInstallationTokenCacheMissThenHit(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	var calls int
	service, closeFn := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/app/installations/42/access_tokens" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"token":"ghs_fresh","expires_at":%q}`, now.Add(time.Hour).Format(time.RFC3339))
	}, now)
	defer closeFn()

	cfg := testConfig()
	token, err := service.InstallationToken(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if token != "ghs_fresh" {
		t.Fatalf("token = %q, want ghs_fresh", token)
	}

	if _, err := service.InstallationToken(context.Background(), cfg); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 1 {
		t.Fatalf("API called %d times, want 1 (second should hit cache)", calls)
	}
}

func TestInstallationTokenCacheExpiryRefetches(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	var calls int
	service, closeFn := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		// Token expires within the refresh margin, so it must never be reused.
		fmt.Fprintf(w, `{"token":"ghs_%d","expires_at":%q}`, calls, now.Add(2*time.Minute).Format(time.RFC3339))
	}, now)
	defer closeFn()

	cfg := testConfig()
	if _, err := service.InstallationToken(context.Background(), cfg); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := service.InstallationToken(context.Background(), cfg); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 2 {
		t.Fatalf("API called %d times, want 2 (near-expiry token must refetch)", calls)
	}
}

func TestInstallationTokenDiscoversSingleInstallation(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	service, closeFn := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations":
			fmt.Fprint(w, `[{"id":99,"account":{"login":"acme"}}]`)
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			if !strings.Contains(r.URL.Path, "/99/") {
				t.Fatalf("token requested for wrong installation: %s", r.URL.Path)
			}
			fmt.Fprintf(w, `{"token":"ghs_disc","expires_at":%q}`, now.Add(time.Hour).Format(time.RFC3339))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}, now)
	defer closeFn()

	cfg := Config{AppID: "123456", PrivateKeyPEM: testPrivateKeyPEM}
	token, err := service.InstallationToken(context.Background(), cfg)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	if token != "ghs_disc" {
		t.Fatalf("token = %q, want ghs_disc", token)
	}
}

func TestInstallationTokenRequiresConfig(t *testing.T) {
	service := NewService(NewClient(), filepath.Join(t.TempDir(), cacheFileName))
	if _, err := service.InstallationToken(context.Background(), Config{}); err == nil {
		t.Fatal("expected validation error for empty config")
	}
}
