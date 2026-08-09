package ghapp

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeJayDev/kirigo/internal/configenv"
)

const (
	configFileName = "gh-app-token.json"
	cacheFileName  = "gh-app-token-cache.json"
	keyFileName    = "gh-app-token.pem"
)

type Config struct {
	AppID          string `json:"app_id"`
	InstallationID string `json:"installation_id,omitempty"`
	Owner          string `json:"owner,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
	PrivateKeyPEM  string `json:"private_key_pem,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
	WebhookSecret  string `json:"webhook_secret,omitempty"`
}

// LoadConfig reads the JSON config (missing file is not an error) and layers
// GITHUB_APP_* environment overrides on top so CI can run without the file.
func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnvOverrides(&cfg)
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("GITHUB_APP_ID"); v != "" {
		cfg.AppID = v
	}
	if v := os.Getenv("GITHUB_APP_INSTALLATION_ID"); v != "" {
		cfg.InstallationID = v
	}
	if v := os.Getenv("GITHUB_APP_OWNER"); v != "" {
		cfg.Owner = v
	}
	if v := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); v != "" {
		cfg.PrivateKeyPath = v
	}
	if v := os.Getenv("GITHUB_APP_PRIVATE_KEY"); v != "" {
		cfg.PrivateKeyPEM = v
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.AppID) == "" {
		return errors.New("app id is required (run setup, or set GITHUB_APP_ID)")
	}
	if strings.TrimSpace(c.PrivateKeyPEM) == "" && strings.TrimSpace(c.PrivateKeyPath) == "" {
		return errors.New("private key is required (run setup, or set GITHUB_APP_PRIVATE_KEY_PATH)")
	}
	return nil
}

func (c Config) PrivateKey() (*rsa.PrivateKey, error) {
	if pem := strings.TrimSpace(c.PrivateKeyPEM); pem != "" {
		return ParsePrivateKey([]byte(pem))
	}
	if c.PrivateKeyPath == "" {
		return nil, errors.New("no private key configured")
	}
	data, err := os.ReadFile(c.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", c.PrivateKeyPath, err)
	}
	return ParsePrivateKey(data)
}

// Save writes the config as 0600 inside a 0700 config dir.
func (c Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

func ConfigPath() (string, error) { return kirigoPath(configFileName) }
func CachePath() (string, error)  { return kirigoPath(cacheFileName) }
func KeyPath() (string, error)    { return kirigoPath(keyFileName) }

func kirigoPath(name string) (string, error) {
	dir, err := configenv.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}
