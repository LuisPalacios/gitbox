# git-status-pull.sh

Script Bash que verifica el estado de múltiples repositorios Git a partir del directorio actual. Informa qué repos necesitan pull y puede hacerlo automáticamente si es seguro.

## Ejecución

```bash
./git-status-pull.sh          # Muestra estado de todos los repos
./git-status-pull.sh pull     # Hace pull automático donde sea seguro
./git-status-pull.sh -v       # Salida detallada
./git-status-pull.sh -v pull  # Detallado + pull automático
```

## Compatibilidad de plataformas

| Plataforma | Detección | Comando git |
| ---------- | --------- | ----------- |
| Linux nativo | por defecto | `git` |
| macOS | `$OSTYPE == darwin*` | `git` |
| WSL2 | `/proc/version` contiene "Microsoft"/"WSL" | `git.exe` |
| Git Bash (Windows) | `$MSYSTEM` definida | `git` |

## Cómo funciona

1. Busca todos los directorios `.git` desde el directorio actual
2. Para cada repositorio:
   - Hace `fetch` para comprobar el estado remoto
   - Detecta: commits adelantados/atrasados, divergencia, stash, staged, untracked, modificados, renombrados
   - Determina si es seguro hacer pull automático
3. Muestra el resultado con colores:
   - **LIMPIO**: sincronizado, no hay nada que hacer
   - **NECESITA PULL**: atrasado pero limpio, se puede hacer pull
   - **HACIENDO EL PULL**: (con argumento `pull`) ejecuta el pull automáticamente
   - **DEBES REVISARLO**: hay cambios locales que impiden un pull seguro

## Modo verbose (-v)

Muestra información detallada por repositorio:

- Rama actual
- Commits adelantados / atrasados
- Divergencia
- Elementos en stash
- Archivos en stage, no rastreados, modificados, movidos
- Push pendiente
