package gcal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DeJayDev/kirigo/internal/configenv"
)

const (
	accountEnv     = "KIRIGO_GCAL_ACCOUNT"
	DefaultAccount = "default"
)

func baseDir() (string, error) {
	dir, err := configenv.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gcal"), nil
}

// AccountDir is ~/.config/kirigo/gcal/<account>.
func AccountDir(account string) (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, account), nil
}

func TokenPath(account string) (string, error) {
	dir, err := AccountDir(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

// ResolveAccount picks the account: flag > env > the sole account > "default".
func ResolveAccount(flagValue string) (string, error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(os.Getenv(accountEnv)); v != "" {
		return v, nil
	}
	accounts, err := listAccounts()
	if err != nil {
		return "", err
	}
	switch {
	case len(accounts) == 1:
		return accounts[0], nil
	case slices.Contains(accounts, DefaultAccount):
		return DefaultAccount, nil
	case len(accounts) == 0:
		return "", &ValidationError{"no gcal accounts; run: gcal setup --account <name>"}
	default:
		return "", &ValidationError{fmt.Sprintf("multiple gcal accounts (%s); pass --account or set %s", strings.Join(accounts, ", "), accountEnv)}
	}
}

func listAccounts() ([]string, error) {
	base, err := baseDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}
