package main

import (
	"os"
	"testing"
)

// runSilent runs run() with the binary's stdout/stderr envelopes discarded, so
// exit-code tests don't spray JSON into the test log.
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
	t.Setenv("GOOGLE_MAPS_API_KEY", "")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no args is a validation error", nil, 2},
		{"missing api key", []string{"--origin", "a", "--destination", "b"}, 2},
		{"unknown flag", []string{"--nope"}, 2},
		{"repeatable + interspersed flags parse", []string{"--origin", "a", "--waypoint", "x", "--destination", "b", "--waypoint", "y"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSilent(tc.args); got != tc.want {
				t.Errorf("run(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
