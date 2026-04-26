# Completado de shell

[Read in English](../completion.md)

`gitbox completion` genera scripts de autocompletado para los shells compatibles con Cobra. El autocompletado cubre comandos, subcomandos y flags, pero no inventa valores de configuración como nombres de cuentas o repositorios.

## Bash

Para probarlo en la sesión actual:

```bash
source <(gitbox completion bash)
```

Para instalarlo de forma permanente en Linux:

```bash
mkdir -p ~/.local/share/bash-completion/completions
gitbox completion bash > ~/.local/share/bash-completion/completions/gitbox
```

En macOS con Homebrew:

```bash
gitbox completion bash > "$(brew --prefix)/etc/bash_completion.d/gitbox"
```

Abre una terminal nueva después de instalarlo.

## Zsh

Para probarlo en la sesión actual:

```bash
source <(gitbox completion zsh)
```

Para instalarlo de forma permanente en un directorio propio:

```bash
mkdir -p ~/.zsh/completions
gitbox completion zsh > ~/.zsh/completions/_gitbox
```

Asegúrate de que tu `.zshrc` carga ese directorio:

```bash
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit
compinit
```

## Fish

Para probarlo en la sesión actual:

```fish
gitbox completion fish | source
```

Para instalarlo de forma permanente:

```fish
mkdir -p ~/.config/fish/completions
gitbox completion fish > ~/.config/fish/completions/gitbox.fish
```

## PowerShell

Para probarlo en la sesión actual:

```powershell
gitbox completion powershell | Out-String | Invoke-Expression
```

Para hacerlo permanente, añade el comando al perfil de PowerShell:

```powershell
notepad $PROFILE
```

Luego añade:

```powershell
gitbox completion powershell | Out-String | Invoke-Expression
```

Abre una terminal nueva para comprobarlo. Si PowerShell bloquea el perfil por la política de ejecución, revisa la política antes de cambiarla; no hace falta bajar la seguridad global del sistema para usar `gitbox`.
