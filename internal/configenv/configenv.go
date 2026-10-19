package configenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
)

const EnvFileOverride = "KIRIGO_ENV_FILE"

func LoadDefault() error {
	paths, err := defaultPaths()
	if err != nil {
		return err
	}
	if err := Load(paths...); err != nil {
		return err
	}
	// Go reads TZ once, on first use of time.Local, which may predate the env file.
	if tz := os.Getenv("TZ"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return fmt.Errorf("load TZ %q: %w", tz, err)
		}
		time.Local = loc
	}
	return nil
}

// LoadPlaces reads named places ({"home": "123 Main St"}) from places.json in the
// kirigo config directory. A missing file means no places.
func LoadPlaces() (map[string]string, error) {
	kirigoDir, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(kirigoDir, "places.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read places %s: %w", path, err)
	}
	var places map[string]string
	if err := json.Unmarshal(data, &places); err != nil {
		return nil, fmt.Errorf("parse places %s: %w", path, err)
	}
	return places, nil
}

func Load(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read env file %s: %w", path, err)
		}
		if err := godotenv.Load(path); err != nil {
			return fmt.Errorf("load env file %s: %w", path, err)
		}
		return nil
	}
	return nil
}

func defaultPaths() ([]string, error) {
	if override := os.Getenv(EnvFileOverride); override != "" {
		return []string{override}, nil
	}

	kirigoDir, err := Dir()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(kirigoDir, ".env"),
		filepath.Join(kirigoDir, "env"),
		filepath.Join(kirigoDir, "kirigo.env"),
	}, nil
}

// Dir returns the kirigo config directory. It honors XDG_CONFIG_HOME on every
// platform (unlike os.UserConfigDir, which ignores it on macOS), falling back to
// $HOME/.config so config lives at ~/.config/kirigo everywhere.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "kirigo"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "kirigo"), nil
}
