package main

import (
	"os"
	"testing"
)

// runSilent runs run() with the binary's stdout/stderr envelopes discarded.
func runSilent(args []string) int {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return run(args)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		devnull.Close()
	}()
	return run(args)
}

func TestRunExitCodes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KIRIGO_ENV_FILE", "")
	t.Setenv("KIRIGO_GCAL_ACCOUNT", "")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no subcommand", nil, 2},
		{"get without event-id", []string{"get"}, 2},
		{"unknown flag", []string{"list", "--nope"}, 2},
		// ValidationError (no configured account) must survive kong's Run() error
		// wrapping and still map to exit 2, not 1.
		{"validation error through kong wrap", []string{"get", "abc123"}, 2},
		{"flag after positional parses", []string{"get", "abc123", "--raw"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSilent(tc.args); got != tc.want {
				t.Errorf("run(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
