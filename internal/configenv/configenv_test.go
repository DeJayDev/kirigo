package configenv

import (
	"os"
	"path/filepath"
	"testing"
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
