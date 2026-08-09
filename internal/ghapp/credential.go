package ghapp

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// CredentialUsername is the fixed username git uses with an installation token.
const CredentialUsername = "x-access-token"

// ParseCredentialRequest reads git's credential helper key=value input, which
// terminates at the first blank line or EOF.
func ParseCredentialRequest(r io.Reader) (map[string]string, error) {
	fields := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[key] = value
	}
	return fields, scanner.Err()
}

// WriteCredentialGet emits the credential fields git expects on stdout for a
// github.com request. For any other host it writes nothing so git falls through
// to its other helpers.
func WriteCredentialGet(w io.Writer, request map[string]string, token string) error {
	if !isGitHubHost(request["host"]) {
		return nil
	}
	_, err := fmt.Fprintf(w, "protocol=%s\nhost=%s\nusername=%s\npassword=%s\n\n",
		valueOr(request["protocol"], "https"), request["host"], CredentialUsername, token)
	return err
}

func isGitHubHost(host string) bool {
	return host == "github.com"
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
