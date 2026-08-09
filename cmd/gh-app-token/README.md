# gh-app-token

Mints scoped, short-lived GitHub App installation tokens so agents can `git` push/pull across many repos without a personal PAT or per-repo deploy keys. It doubles as a git credential helper, so once configured `git` uses fresh tokens invisibly.

```bash
make deploy   # installs gh-app-token to ~/.local/bin
```

## Setup

`setup` runs the GitHub App Manifest flow and persists credentials under `~/.config/kirigo/` (config `0600`, key `0600`, dir `0700`). It is remote-safe: it serves the manifest form on a localhost port and captures the redirect, or accepts a pasted redirect URL when the callback can't reach the box.

```bash
# On the machine (personal account). For an org: --org my-org
gh-app-token setup --configure-git global
```

If the machine is remote, forward the callback port from your laptop first, then open the printed URL:

```bash
ssh -L 8765:localhost:8765 user@remote-host
# then browse http://localhost:8765/
```

If you can't forward a port at all, run `gh-app-token setup --paste`, create the App in your browser, and paste the full redirected URL (it carries the `code`) back into the terminal.

After the App is created, `setup` prints an install URL — open it to install the App on the target org/user. Installation tokens are only mintable once the App is installed somewhere.

## git credential helper

`--configure-git global|local` registers the helper for you. To do it by hand:

```bash
git config --global credential.https://github.com.helper "$(command -v gh-app-token) git-credential"
```

Then, with no PAT in your keychain:

```bash
git ls-remote https://github.com/<org>/<repo>.git
```

## Agent / API usage

`gh-app-token` (or `gh-app-token token`) prints a fresh, cached installation token to stdout — nothing else goes to stdout, hints and errors go to stderr:

```bash
curl -H "Authorization: token $(gh-app-token)" https://api.github.com/repos/<org>/<repo>
```

Tokens are cached on disk (`~/.config/kirigo/gh-app-token-cache.json`, `0600`) keyed by app + installation, and refreshed a few minutes before their ~1h expiry.

## Config

`setup` writes `~/.config/kirigo/gh-app-token.json`. For CI, env vars override the file: `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY_PATH` (or inline `GITHUB_APP_PRIVATE_KEY`), `GITHUB_APP_OWNER`. `installation_id` is discovered automatically when the App has exactly one installation (or set `owner` to disambiguate).
