package ghapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultAPIBaseURL = "https://api.github.com"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{baseURL: defaultAPIBaseURL, httpClient: http.DefaultClient}
}

func NewClientWithHTTP(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

type InstallationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
	} `json:"account"`
}

// ManifestApp is the credential set GitHub returns when converting a manifest code.
type ManifestApp struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
	HTMLURL       string `json:"html_url"`
}

func (c *Client) CreateInstallationToken(ctx context.Context, installationID, appJWT string) (InstallationToken, error) {
	path := fmt.Sprintf("/app/installations/%s/access_tokens", installationID)
	var token InstallationToken
	if err := c.do(ctx, http.MethodPost, path, appJWT, nil, &token); err != nil {
		return InstallationToken{}, err
	}
	return token, nil
}

func (c *Client) ListInstallations(ctx context.Context, appJWT string) ([]Installation, error) {
	var installations []Installation
	if err := c.do(ctx, http.MethodGet, "/app/installations", appJWT, nil, &installations); err != nil {
		return nil, err
	}
	return installations, nil
}

func (c *Client) ConvertManifest(ctx context.Context, code string) (ManifestApp, error) {
	path := fmt.Sprintf("/app-manifests/%s/conversions", code)
	var app ManifestApp
	if err := c.do(ctx, http.MethodPost, path, "", nil, &app); err != nil {
		return ManifestApp{}, err
	}
	return app, nil
}

func (c *Client) do(ctx context.Context, method, path, bearer string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GitHub request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create GitHub request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call GitHub: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("GitHub returned HTTP %s: %s", response.Status, readError(response.Body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func readError(body io.Reader) string {
	var parsed struct {
		Message string `json:"message"`
	}
	raw, _ := io.ReadAll(body)
	if err := json.Unmarshal(raw, &parsed); err == nil && parsed.Message != "" {
		return parsed.Message
	}
	return string(bytes.TrimSpace(raw))
}
