package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/DeJayDev/kirigo/internal/configenv"
	"github.com/DeJayDev/kirigo/internal/ghapp"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if err := configenv.LoadDefault(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	command := "token"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "token":
		return runToken()
	case "git-credential":
		return runGitCredential(args)
	case "setup":
		return runSetup(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q; want token, git-credential, or setup\n", command)
		return 2
	}
}

func runToken() int {
	cfg, err := ghapp.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	token, err := fetchToken(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stdout, token)
	return 0
}

func runGitCredential(args []string) int {
	operation := ""
	if len(args) > 0 {
		operation = args[0]
	}
	// git only wants a credential on "get"; store/erase are no-ops we accept for
	// protocol compatibility.
	if operation != "get" {
		return 0
	}

	request, err := ghapp.ParseCredentialRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if request["host"] != "github.com" {
		return 0
	}

	cfg, err := ghapp.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	token, err := fetchToken(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := ghapp.WriteCredentialGet(os.Stdout, request, token); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runSetup(args []string) int {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	org := flags.String("org", "", "GitHub org to create the App under (default: personal account)")
	name := flags.String("name", "", "App name (default kirigo-agent-git)")
	port := flags.Int("port", 0, "localhost callback port (default 8765)")
	paste := flags.Bool("paste", false, "skip the local server; paste the redirected URL instead")
	configureGit := flags.String("configure-git", "", "register the git credential helper: global, local, or empty to skip")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if s := *configureGit; s != "" && s != "global" && s != "local" {
		fmt.Fprintf(os.Stderr, "invalid -configure-git %q (want global or local)\n", s)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err := ghapp.RunSetup(ctx, ghapp.NewClient(), ghapp.SetupOptions{
		Org:          *org,
		Name:         *name,
		Port:         *port,
		Paste:        *paste,
		ConfigureGit: *configureGit,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
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
