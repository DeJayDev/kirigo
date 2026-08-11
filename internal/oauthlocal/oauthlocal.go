// Package oauthlocal holds the loopback-redirect plumbing shared by the two
// setup flows (Google Calendar OAuth and the GitHub App manifest): reading a
// pasted code, extracting a code from a bare string or redirect URL, a CSRF
// state token, and the "ssh -L" hint target.
package oauthlocal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// ReadPastedCode reads one chunk from in, extracts a code (bare or from a
// redirect URL), and delivers it on codeCh (or an error on errCh).
func ReadPastedCode(in io.Reader, codeCh chan<- string, errCh chan<- error) {
	buf := make([]byte, 4096)
	n, err := in.Read(buf)
	if n > 0 {
		code, perr := ExtractCode(strings.TrimSpace(string(buf[:n])))
		if perr != nil {
			errCh <- perr
			return
		}
		codeCh <- code
		return
	}
	if err != nil && err != io.EOF {
		errCh <- err
	}
}

// ExtractCode accepts a bare code or a full redirected URL.
func ExtractCode(input string) (string, error) {
	if input == "" {
		return "", errors.New("empty input")
	}
	if !strings.Contains(input, "://") {
		return input, nil
	}
	parsed, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse pasted URL: %w", err)
	}
	code := parsed.Query().Get("code")
	if code == "" {
		return "", errors.New("pasted URL has no code parameter")
	}
	return code, nil
}

// SSHTarget is the user@host hint for the "ssh -L" port-forward instruction.
func SSHTarget() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "<user>"
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "<this-host>"
	}
	return user + "@" + host
}

// RandomState returns a random hex CSRF state token.
func RandomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
