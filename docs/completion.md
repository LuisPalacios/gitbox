# Shell Completion

`gitbox` can generate autocompletion scripts for your shell so you get tab-completion for commands, subcommands, and flags.

Supported shells: **Bash**, **Zsh**, **Fish**, **PowerShell**.

## Setup

### Bash

```bash
# Current session only
source <(gitbox completion bash)

# Permanent — add to your profile
echo 'source <(gitbox completion bash)' >> ~/.bashrc
```

### Zsh

```zsh
# Current session only
source <(gitbox completion zsh)

# Permanent — add to your profile
echo 'source <(gitbox completion zsh)' >> ~/.zshrc
```

> If you get `command not found: compdef`, add `autoload -Uz compinit && compinit` before the source line.

### Fish

```fish
gitbox completion fish | source

# Permanent
gitbox completion fish > ~/.config/fish/completions/gitbox.fish
```

### PowerShell

```powershell
# Current session only
gitbox completion powershell | Out-String | Invoke-Expression

# Permanent — add to your profile
Add-Content $PROFILE 'gitbox completion powershell | Out-String | Invoke-Expression'
```

## Usage

Once installed, press `Tab` to complete:

```text
gitbox st<Tab>       →  gitbox status
gitbox account --<Tab>  →  shows available flags
```

Run `gitbox completion <shell> --help` for shell-specific details.
