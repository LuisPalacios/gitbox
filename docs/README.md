# Documentation

| Doc | What's in it |
| --- | --- |
| [CLI Quick Start](cli-guide.md) | Step-by-step: init, accounts, credentials, discover, clone |
| [GUI Guide](gui-guide.md) | Desktop app walkthrough |
| [Reference](reference.md) | All commands, config format, folder structure |
| [Credentials](credentials.md) | Token, GCM, and SSH setup in detail |
| [Shell Completion](completion.md) | Tab-completion for Bash, Zsh, Fish, PowerShell |
| [Architecture](architecture.md) | Technical design, component diagram |
| [Developer Guide](developer-guide.md) | Building from source, contributing |
| [Legacy migration](migration.md) | Migrating from `git-config-repos.sh` (v1) |
| [Legacy scripts](../legacy/README.md) | Original `git-config-repos.sh` and `git-status-pull.sh` |

See also: [JSON annotated example](../gitbox.jsonc) | [JSON Schema](../gitbox.schema.json)

## End user super quick start

### Desktop (GUI)

Download `Gitbox` from [Releases](https://github.com/LuisPalacios/gitbox/releases), then:

> **macOS note:** The app is not signed. After extracting, move it to your *Applications* and from Terminal run: `xattr -cr /Applications/Gitbox.app`

1. Launch — first run shows a welcome screen, pick your clone folder (default `~/00.git`)
2. Click **+** to add an account — fill in provider, URL, username, credential type
3. Credential setup runs automatically (GCM opens browser, SSH generates key, Token asks for PAT)
4. Click **Find projects** on the account card — select repos to track
5. Click **Sync All** — repos are cloned with progress bars

More in the [GUI Guide](gui-guide.md).

### CLI

More on the [CLI Quick Start](cli-guide.md).

```bash
gitboxcmd init                                    # create config
gitboxcmd account add my-github --provider github --url https://github.com \
  --username MyUser --name "My Name" --email "me@example.com"
gitboxcmd account credential setup my-github      # set up auth
gitboxcmd account discover my-github              # find your repos
gitboxcmd clone                                   # clone everything
gitboxcmd status                                  # check sync state
```

## Developer super quick start

## Building from Source

### CLI Only

```bash
# From the repository root
go build -o build/gitboxcmd ./cmd/cli

# Cross-compile for other platforms
GOOS=linux  GOARCH=amd64 go build -o build/gitboxcmd-linux-amd64  ./cmd/cli
GOOS=darwin GOARCH=arm64 go build -o build/gitboxcmd-darwin-arm64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o build/gitboxcmd.exe         ./cmd/cli
```

### GUI (Wails)

```bash
# Copy app icons from assets/ into the Wails build directory
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # Windows only

# Development mode (hot reload)
cd cmd/gui
wails dev

# Production build
wails build
# Output: cmd/gui/build/bin/Gitbox[.exe]
```
