# Shell Completion

`gitboxcmd` can generate autocompletion scripts for your shell so you get tab-completion for commands, subcommands, and flags.

Supported shells: **Bash**, **Zsh**, **Fish**, **PowerShell**.

## Setup

### Bash

```bash
# Current session only
source <(gitboxcmd completion bash)

# Permanent — add to your profile
echo 'source <(gitboxcmd completion bash)' >> ~/.bashrc
```

### Zsh

```zsh
# Current session only
source <(gitboxcmd completion zsh)

# Permanent — add to your profile
echo 'source <(gitboxcmd completion zsh)' >> ~/.zshrc
```

> If you get `command not found: compdef`, add `autoload -Uz compinit && compinit` before the source line.

### Fish

```fish
gitboxcmd completion fish | source

# Permanent
gitboxcmd completion fish > ~/.config/fish/completions/gitboxcmd.fish
```

### PowerShell

```powershell
# Current session only
gitboxcmd completion powershell | Out-String | Invoke-Expression

# Permanent — add to your profile
Add-Content $PROFILE 'gitboxcmd completion powershell | Out-String | Invoke-Expression'
```

## Usage

Once installed, press `Tab` to complete:

```text
gitboxcmd st<Tab>       →  gitboxcmd status
gitboxcmd account --<Tab>  →  shows available flags
```

Run `gitboxcmd completion <shell> --help` for shell-specific details.
