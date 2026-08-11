package ghapp

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DeJayDev/kirigo/internal/oauthlocal"
)

type SetupOptions struct {
	Org   string // empty => personal account
	Name  string
	Port  int  // 0 => defaultSetupPort
	Paste bool // skip the local server, only accept a pasted redirect URL

	// ConfigureGit registers the credential helper in git config: "global",
	// "local", or "" to skip.
	ConfigureGit string

	In  io.Reader
	Err io.Writer
}

const defaultSetupPort = 8765

// manifestPermissions is the minimal set for cloning and pushing across an
// installation's repos. metadata:read is mandatory for any App.
var manifestPermissions = map[string]string{
	"contents": "write",
	"metadata": "read",
}

type manifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	RedirectURL        string            `json:"redirect_url"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
}

// RunSetup drives the GitHub App Manifest flow end to end: serve the manifest
// form, capture the redirect code (via localhost callback or a pasted URL),
// exchange it for credentials, persist them, and optionally register the git
// credential helper.
func RunSetup(ctx context.Context, client *Client, opts SetupOptions) (Config, error) {
	if opts.Err == nil {
		opts.Err = os.Stderr
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	port := opts.Port
	if port == 0 {
		port = defaultSetupPort
	}
	name := opts.Name
	if name == "" {
		name = "kirigo-agent-git"
	}

	state, err := oauthlocal.RandomState()
	if err != nil {
		return Config{}, err
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)
	man := manifest{
		Name:               name,
		URL:                "https://github.com/DeJayDev/kirigo",
		RedirectURL:        redirectURL,
		Public:             false,
		DefaultPermissions: manifestPermissions,
	}

	code, err := captureManifestCode(ctx, opts, port, man, state)
	if err != nil {
		return Config{}, err
	}

	app, err := client.ConvertManifest(ctx, code)
	if err != nil {
		return Config{}, fmt.Errorf("exchange manifest code: %w", err)
	}

	cfg, err := persistApp(app, opts.Org)
	if err != nil {
		return Config{}, err
	}

	fmt.Fprintf(opts.Err, "App created: %s (id %d)\n", app.Slug, app.ID)
	if app.HTMLURL != "" {
		fmt.Fprintf(opts.Err, "Install it on your org/user here, then it is ready:\n  %s/installations/new\n", app.HTMLURL)
	}

	if opts.ConfigureGit != "" {
		if err := configureGitHelper(opts.ConfigureGit); err != nil {
			return cfg, err
		}
		fmt.Fprintf(opts.Err, "Registered git credential helper (%s scope) for https://github.com\n", opts.ConfigureGit)
	}
	return cfg, nil
}

func captureManifestCode(ctx context.Context, opts SetupOptions, port int, man manifest, state string) (string, error) {
	newAppURL := manifestNewAppURL(opts.Org, state)
	formHTML, err := manifestFormHTML(newAppURL, man)
	if err != nil {
		return "", err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	var server *http.Server
	if !opts.Paste {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return "", fmt.Errorf("listen on port %d: %w", port, err)
		}
		server = serveManifestForm(listener, formHTML, state, codeCh, errCh)
		defer server.Close()

		fmt.Fprintf(opts.Err, "Open this in a browser to create the App:\n  http://localhost:%d/\n", port)
		fmt.Fprintf(opts.Err, "If this machine is remote, forward the port from your laptop first:\n  ssh -L %d:localhost:%d %s\n", port, port, oauthlocal.SSHTarget())
	} else {
		fmt.Fprintf(opts.Err, "Submit this manifest form in a browser, then paste the redirected URL below.\n")
		fmt.Fprintf(opts.Err, "Manifest new-app URL: %s\n", newAppURL)
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
			return "", fmt.Errorf("no code received")
		}
		return code, nil
	}
}

func serveManifestForm(listener net.Listener, formHTML []byte, state string, codeCh chan<- string, errCh chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(formHTML)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("callback state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		_, _ = io.WriteString(w, "App created. You can close this tab and return to the terminal.")
		codeCh <- code
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	return server
}

func manifestNewAppURL(org, state string) string {
	base := "https://github.com/settings/apps/new"
	if org != "" {
		base = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new", org)
	}
	return base + "?state=" + url.QueryEscape(state)
}

var manifestFormTemplate = template.Must(template.New("form").Parse(
	`<!doctype html><html><body>
<form id="f" action="{{.Action}}" method="post">
<input type="hidden" name="manifest" value="{{.Manifest}}">
</form>
<script>document.getElementById("f").submit()</script>
</body></html>`))

func manifestFormHTML(action string, man manifest) ([]byte, error) {
	encoded, err := json.Marshal(man)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := manifestFormTemplate.Execute(&buf, struct {
		Action   template.URL
		Manifest string
	}{Action: template.URL(action), Manifest: string(encoded)}); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func persistApp(app ManifestApp, org string) (Config, error) {
	keyPath, err := KeyPath()
	if err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return Config{}, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(app.PEM), 0o600); err != nil {
		return Config{}, fmt.Errorf("write private key: %w", err)
	}

	cfg := Config{
		AppID:          fmt.Sprintf("%d", app.ID),
		Owner:          org,
		PrivateKeyPath: keyPath,
		ClientID:       app.ClientID,
		ClientSecret:   app.ClientSecret,
		WebhookSecret:  app.WebhookSecret,
	}
	if err := cfg.Save(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func configureGitHelper(scope string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own path: %w", err)
	}
	absExe, err := filepath.Abs(exe)
	if err != nil {
		return err
	}

	args := []string{"config"}
	switch scope {
	case "global":
		args = append(args, "--global")
	case "local":
		// git config default target is the local repo
	default:
		return fmt.Errorf("unknown git config scope %q (want global or local)", scope)
	}
	args = append(args, "credential.https://github.com.helper", absExe+" git-credential")

	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
