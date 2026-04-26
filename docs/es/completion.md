# Completado de shell

`gitbox` puede generar scripts de autocompletado para tu shell, así tienes completado con Tab para comandos, subcomandos y flags.

Shells soportados: **Bash**, **Zsh**, **Fish**, **PowerShell**.

## Configuración

### Bash

```bash
# Solo la sesión actual
source <(gitbox completion bash)

# Permanente — añadir al perfil
echo 'source <(gitbox completion bash)' >> ~/.bashrc
```

### Zsh

```zsh
# Solo la sesión actual
source <(gitbox completion zsh)

# Permanente — añadir al perfil
echo 'source <(gitbox completion zsh)' >> ~/.zshrc
```

> Si recibes `command not found: compdef`, añade `autoload -Uz compinit && compinit` antes de la línea `source`.

### Fish

```fish
gitbox completion fish | source

# Permanente
gitbox completion fish > ~/.config/fish/completions/gitbox.fish
```

### PowerShell

```powershell
# Solo la sesión actual
gitbox completion powershell | Out-String | Invoke-Expression

# Permanente — añadir al perfil
Add-Content $PROFILE 'gitbox completion powershell | Out-String | Invoke-Expression'
```

## Uso

Una vez instalado, pulsa `Tab` para completar:

```text
gitbox st<Tab>       →  gitbox status
gitbox account --<Tab>  →  muestra los flags disponibles
```

Ejecuta `gitbox completion <shell> --help` para ver los detalles específicos de cada shell.
