package main

import (
	"os"
	"testing"
)

// runSilent runs run() with the binary's stdout/stderr discarded.
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
	t.Setenv("GITHUB_APP_ID", "")

	cases := []struct {
		name string
		args []string
		want int
	}{
		// bare invocation runs the default "token" command; missing config -> 2.
		{"default token, missing config", nil, 2},
		{"git-credential store is a no-op", []string{"git-credential", "store"}, 0},
		{"setup rejects bad configure-git", []string{"setup", "--configure-git", "bad"}, 2},
		{"unknown flag", []string{"--nope"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSilent(tc.args); got != tc.want {
				t.Errorf("run(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
