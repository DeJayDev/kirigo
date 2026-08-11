package googlecal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/DeJayDev/kirigo/internal/oauthlocal"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	calendar "google.golang.org/api/calendar/v3"
)

// Scopes: full event CRUD plus read-only access to the calendar list and freebusy.
var Scopes = []string{
	calendar.CalendarEventsScope,
	calendar.CalendarReadonlyScope,
}

const defaultSetupPort = 8765

func OAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       Scopes,
	}
}

// TokenSource returns a source that refreshes the stored token and persists it
// back to path whenever it changes.
func TokenSource(ctx context.Context, cfg *oauth2.Config, path string) (oauth2.TokenSource, error) {
	tok, err := loadToken(path)
	if err != nil {
		return nil, err
	}
	return oauth2.ReuseTokenSource(nil, &persistingSource{
		base: cfg.TokenSource(ctx, tok),
		path: path,
	}), nil
}

type persistingSource struct {
	base oauth2.TokenSource
	path string
	last string
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != p.last {
		_ = saveToken(p.path, tok)
		p.last = tok.AccessToken
	}
	return tok, nil
}

func loadToken(path string) (*oauth2.Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no token for this account; run: gcal setup")
		}
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parse token %s: %w", path, err)
	}
	return &tok, nil
}

func saveToken(path string, tok *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

type SetupOptions struct {
	ClientID     string
	ClientSecret string
	TokenPath    string
	Port         int  // 0 => defaultSetupPort
	Paste        bool // skip the local server; paste the redirected URL instead
	In           io.Reader
	Err          io.Writer
}

// RunSetup drives the installed-app OAuth flow (loopback redirect + PKCE, with a
// paste fallback) and persists the resulting token.
func RunSetup(ctx context.Context, opts SetupOptions) error {
	if opts.Err == nil {
		opts.Err = os.Stderr
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.ClientID == "" || opts.ClientSecret == "" {
		return errors.New("GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET are required (create a Desktop OAuth client in Google Cloud Console)")
	}
	port := opts.Port
	if port == 0 {
		port = defaultSetupPort
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)
	cfg := OAuthConfig(opts.ClientID, opts.ClientSecret, redirectURL)

	verifier := oauth2.GenerateVerifier()
	state, err := oauthlocal.RandomState()
	if err != nil {
		return err
	}
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))

	code, err := captureCode(ctx, opts, port, authURL, state)
	if err != nil {
		return err
	}
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}
	if tok.RefreshToken == "" {
		fmt.Fprintln(opts.Err, "warning: Google returned no refresh token; revoke prior access and re-run setup.")
	}
	if err := saveToken(opts.TokenPath, tok); err != nil {
		return err
	}
	fmt.Fprintf(opts.Err, "Authorized. Token saved to %s\n", opts.TokenPath)
	return nil
}

func captureCode(ctx context.Context, opts SetupOptions, port int, authURL, state string) (string, error) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	if !opts.Paste {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return "", fmt.Errorf("listen on port %d: %w", port, err)
		}
		server := serveCallback(listener, state, codeCh, errCh)
		defer server.Close()
		fmt.Fprintf(opts.Err, "Open this URL in a browser to authorize:\n  %s\n", authURL)
		fmt.Fprintf(opts.Err, "If this machine is remote, forward the port from your laptop first:\n  ssh -L %d:localhost:%d %s\n", port, port, oauthlocal.SSHTarget())
	} else {
		fmt.Fprintf(opts.Err, "Open this URL in a browser, approve, then paste the redirected URL below:\n  %s\n", authURL)
	}
	fmt.Fprintf(opts.Err, "Or paste the full redirected URL here and press enter:\n")

	go oauthlocal.ReadPastedCode(opts.In, codeCh, errCh)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-errCh:
		return "", err
	case code := <-codeCh:
		if code == "" {
			return "", errors.New("no authorization code received")
		}
		return code, nil
	}
}

func serveCallback(listener net.Listener, state string, codeCh chan<- string, errCh chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- errors.New("callback state mismatch")
			return
		}
		_, _ = io.WriteString(w, "Authorized. You can close this tab and return to the terminal.")
		codeCh <- r.URL.Query().Get("code")
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	return server
}
