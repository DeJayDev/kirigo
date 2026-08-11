package ghapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// refreshMargin refreshes installation tokens a few minutes before GitHub's ~1h
// expiry so a cached token never expires mid-request.
const refreshMargin = 5 * time.Minute

type Service struct {
	Client    *Client
	CachePath string
	Now       func() time.Time
}

func NewService(client *Client, cachePath string) *Service {
	return &Service{Client: client, CachePath: cachePath, Now: time.Now}
}

type cacheEntry struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// InstallationToken returns a valid installation access token, reusing the disk
// cache when the stored token has more than refreshMargin left.
func (s *Service) InstallationToken(ctx context.Context, cfg Config) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	now := s.now()

	installationID := strings.TrimSpace(cfg.InstallationID)
	if installationID != "" {
		if token, ok := s.readCache(cfg.AppID, installationID, now); ok {
			return token, nil
		}
	}

	key, err := cfg.PrivateKey()
	if err != nil {
		return "", err
	}
	appJWT, err := SignAppJWT(cfg.AppID, key, now)
	if err != nil {
		return "", err
	}

	if installationID == "" {
		installationID, err = s.discoverInstallation(ctx, cfg, appJWT)
		if err != nil {
			return "", err
		}
		if token, ok := s.readCache(cfg.AppID, installationID, now); ok {
			return token, nil
		}
	}

	token, err := s.Client.CreateInstallationToken(ctx, installationID, appJWT)
	if err != nil {
		return "", err
	}
	s.writeCache(cfg.AppID, installationID, cacheEntry(token))
	return token.Token, nil
}

func (s *Service) discoverInstallation(ctx context.Context, cfg Config, appJWT string) (string, error) {
	installations, err := s.Client.ListInstallations(ctx, appJWT)
	if err != nil {
		return "", err
	}
	if owner := strings.TrimSpace(cfg.Owner); owner != "" {
		for _, install := range installations {
			if strings.EqualFold(install.Account.Login, owner) {
				return strconv.FormatInt(install.ID, 10), nil
			}
		}
		return "", fmt.Errorf("app is not installed on %q", owner)
	}
	if len(installations) == 1 {
		return strconv.FormatInt(installations[0].ID, 10), nil
	}
	if len(installations) == 0 {
		return "", errors.New("app has no installations; install it first")
	}
	return "", fmt.Errorf("app has %d installations; set installation_id or owner to disambiguate", len(installations))
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func cacheKey(appID, installationID string) string { return appID + "/" + installationID }

func (s *Service) readCache(appID, installationID string, now time.Time) (string, bool) {
	entries := s.loadCache()
	entry, ok := entries[cacheKey(appID, installationID)]
	if !ok {
		return "", false
	}
	if now.After(entry.ExpiresAt.Add(-refreshMargin)) {
		return "", false
	}
	return entry.Token, true
}

func (s *Service) loadCache() map[string]cacheEntry {
	data, err := os.ReadFile(s.CachePath)
	if err != nil {
		return map[string]cacheEntry{}
	}
	entries := map[string]cacheEntry{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return map[string]cacheEntry{}
	}
	return entries
}

func (s *Service) writeCache(appID, installationID string, entry cacheEntry) {
	entries := s.loadCache()
	entries[cacheKey(appID, installationID)] = entry
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.CachePath), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(s.CachePath, data, 0o600)
}
