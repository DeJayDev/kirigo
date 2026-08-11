package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"

	"github.com/DeJayDev/kirigo/internal/configenv"
	"github.com/DeJayDev/kirigo/internal/ghapp"
)

type CLI struct {
	Token         TokenCmd         `cmd:"" default:"1" help:"print an installation token (default)"`
	GitCredential GitCredentialCmd `cmd:"" name:"git-credential" help:"act as a git credential helper"`
	Setup         SetupCmd         `cmd:"" help:"create the GitHub App and store its credentials"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if err := configenv.LoadDefault(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var cli CLI
	parser, err := kong.New(&cli, kong.Name("gh-app-token"),
		kong.Description("Scoped, short-lived GitHub App installation tokens; also a git credential helper."))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := kctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if _, ok := errors.AsType[configErr](err); ok {
			return 2
		}
		return 1
	}
	return 0
}

// configErr marks a config/validation failure so it exits 2 (vs 1 for runtime).
type configErr struct{ error }

type TokenCmd struct{}

func (c *TokenCmd) Run() error {
	cfg, err := ghapp.LoadConfig()
	if err != nil {
		return configErr{err}
	}
	if err := cfg.Validate(); err != nil {
		return configErr{err}
	}
	token, err := fetchToken(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, token)
	return nil
}

type GitCredentialCmd struct {
	Operation string `arg:"" optional:"" help:"git credential operation (get/store/erase)"`
}

func (c *GitCredentialCmd) Run() error {
	// git only wants a credential on "get"; store/erase are no-ops we accept for
	// protocol compatibility.
	if c.Operation != "get" {
		return nil
	}
	request, err := ghapp.ParseCredentialRequest(os.Stdin)
	if err != nil {
		return err
	}
	if request["host"] != "github.com" {
		return nil
	}
	cfg, err := ghapp.LoadConfig()
	if err != nil {
		return configErr{err}
	}
	if err := cfg.Validate(); err != nil {
		return configErr{err}
	}
	token, err := fetchToken(cfg)
	if err != nil {
		return err
	}
	return ghapp.WriteCredentialGet(os.Stdout, request, token)
}

type SetupCmd struct {
	Org          string `help:"GitHub org to create the App under (default: personal account)"`
	Name         string `help:"App name (default kirigo-agent-git)"`
	Port         int    `help:"localhost callback port (default 8765)"`
	Paste        bool   `help:"skip the local server; paste the redirected URL instead"`
	ConfigureGit string `name:"configure-git" help:"register the git credential helper: global, local, or empty to skip"`
}

func (c *SetupCmd) Run() error {
	if s := c.ConfigureGit; s != "" && s != "global" && s != "local" {
		return configErr{fmt.Errorf("invalid --configure-git %q (want global or local)", s)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, err := ghapp.RunSetup(ctx, ghapp.NewClient(), ghapp.SetupOptions{
		Org:          c.Org,
		Name:         c.Name,
		Port:         c.Port,
		Paste:        c.Paste,
		ConfigureGit: c.ConfigureGit,
	})
	return err
}

func fetchToken(cfg ghapp.Config) (string, error) {
	cachePath, err := ghapp.CachePath()
	if err != nil {
		return "", err
	}
	service := ghapp.NewService(ghapp.NewClient(), cachePath)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return service.InstallationToken(ctx, cfg)
}
