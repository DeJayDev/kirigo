package configenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadUsesFirstExistingEnvFileWithoutOverridingShell(t *testing.T) {
	t.Setenv("GOOGLE_MAPS_API_KEY", "from-shell")

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.env")
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("GOOGLE_MAPS_API_KEY=from-file\nKIRIGO_TEST_VALUE=loaded\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := Load(missing, envFile); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := os.Getenv("GOOGLE_MAPS_API_KEY"); got != "from-shell" {
		t.Fatalf("GOOGLE_MAPS_API_KEY = %q, want shell value", got)
	}
	if got := os.Getenv("KIRIGO_TEST_VALUE"); got != "loaded" {
		t.Fatalf("KIRIGO_TEST_VALUE = %q, want loaded", got)
	}
}

func TestLoadDefaultUsesKirigoConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.Unsetenv("GOOGLE_MAPS_API_KEY"); err != nil {
		t.Fatalf("unset GOOGLE_MAPS_API_KEY: %v", err)
	}
	if err := os.Unsetenv(EnvFileOverride); err != nil {
		t.Fatalf("unset %s: %v", EnvFileOverride, err)
	}

	kirigoDir := filepath.Join(dir, "kirigo")
	if err := os.MkdirAll(kirigoDir, 0o700); err != nil {
		t.Fatalf("make config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kirigoDir, ".env"), []byte("GOOGLE_MAPS_API_KEY=from-config\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := LoadDefault(); err != nil {
		t.Fatalf("LoadDefault returned error: %v", err)
	}
	if got := os.Getenv("GOOGLE_MAPS_API_KEY"); got != "from-config" {
		t.Fatalf("GOOGLE_MAPS_API_KEY = %q, want from-config", got)
	}
}

func TestLoadPlaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	places, err := LoadPlaces()
	if err != nil || places != nil {
		t.Fatalf("missing file: places = %v, err = %v", places, err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "kirigo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kirigo", "places.json"), []byte(`{"home": "123 Main St, SF"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	places, err = LoadPlaces()
	if err != nil || places["home"] != "123 Main St, SF" {
		t.Fatalf("places = %v, err = %v", places, err)
	}
}

func TestLoadDefaultAppliesTZFromEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "kirigo.env")
	if err := os.WriteFile(envFile, []byte("TZ=America/Los_Angeles\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvFileOverride, envFile)
	t.Setenv("TZ", "")
	if err := os.Unsetenv("TZ"); err != nil {
		t.Fatal(err)
	}
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	if err := LoadDefault(); err != nil {
		t.Fatal(err)
	}
	if time.Local.String() != "America/Los_Angeles" {
		t.Fatalf("time.Local = %s", time.Local)
	}
}
