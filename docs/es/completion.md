# Completado de shell

[Read in English](../completion.md)

gitbox usa el completado generado por Cobra. Los comandos y flags permanecen en ingles para mantener scripts y documentacion tecnica estables.

## Bash

```bash
gitbox completion bash > ~/.local/share/bash-completion/completions/gitbox
```

## Zsh

```bash
gitbox completion zsh > "${fpath[1]}/_gitbox"
```

## Fish

```bash
gitbox completion fish > ~/.config/fish/completions/gitbox.fish
```

## PowerShell

```powershell
gitbox completion powershell | Out-String | Invoke-Expression
```
