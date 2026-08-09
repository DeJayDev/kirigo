package configenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

const EnvFileOverride = "KIRIGO_ENV_FILE"

func LoadDefault() error {
	paths, err := defaultPaths()
	if err != nil {
		return err
	}
	return Load(paths...)
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
