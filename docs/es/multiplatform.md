# Desarrollo multiplataforma

Pruebo gitbox en tres plataformas: Windows, macOS y Linux. Los scripts de `scripts/` automatizan el ciclo build-deploy-test para que pueda trabajar desde cualquier OS y ejecutar gitbox en los otros dos vía SSH.

Para probar las 3 plataformas, necesitas acceso SSH a máquinas que ejecuten los otros dos OSs (máquinas físicas, VMs o instancias cloud). Si solo tienes una máquina, aun así puedes ejecutar pruebas unitarias e integración localmente — CI cubre las otras plataformas en push.

Se han validado las tres perspectivas de workstation de desarrollo: Windows (v1.0.4), macOS (v1.0.5) y Linux (v1.0.6). Cada validación ejecuta el ciclo completo — configuración de credenciales, cross-compile, deploy, smoke tests, unit tests, integration tests y verificación TUI interactiva en las 3 plataformas.

## Qué necesitas

- **Go 1.26+** en tu máquina de desarrollo (cross-compiles para todas las plataformas)
- **Autenticación SSH basada en clave** a tus máquinas remotas (sin passwords)
- **Git Bash** en Windows (viene con Git for Windows)
- **jq** y **curl** en todas las máquinas (para configuración de credenciales)

## Configuración inicial

### 1. Configurar hosts SSH

```bash
cp docs/.env.example .env
```

Edita `.env` con tus hosts SSH remotos. Los scripts autodetectan tu OS local, así que deja vacía la variable de esa plataforma. Define las demás como `user@hostname`. Windows y macOS se separan en dos targets por arquitectura — `SSH_WIN_INTEL_HOST` / `SSH_WIN_ARM_HOST` y `SSH_MAC_ARM_HOST` / `SSH_MAC_INTEL_HOST` — para que máquinas amd64 y arm64 puedan coexistir en un `.env`:

```bash
# Desarrollo en Windows amd64, remotos son Macs y Linux:
SSH_WIN_INTEL_HOST=""
SSH_WIN_ARM_HOST=""
SSH_MAC_ARM_HOST="user@mac-arm-host"
SSH_MAC_INTEL_HOST="user@mac-intel-host"
SSH_LINUX_HOST="user@linux-host"

# Desarrollo en Apple Silicon, remotos incluyen una caja Windows amd64,
# una VM Windows-on-ARM (Parallels/VMware Fusion), Intel Mac y Linux:
SSH_WIN_INTEL_HOST="user@win-amd64-host"
SSH_WIN_ARM_HOST="user@win-arm-vm"
SSH_MAC_ARM_HOST=""
SSH_MAC_INTEL_HOST="user@mac-intel-host"
SSH_LINUX_HOST="user@linux-host"

# Desarrollo en Linux, remoto es un solo Mac:
SSH_WIN_INTEL_HOST=""
SSH_WIN_ARM_HOST=""
SSH_MAC_ARM_HOST="user@mac-host"
SSH_MAC_INTEL_HOST=""
SSH_LINUX_HOST=""
```

Los archivos `.env` antiguos con un único `SSH_WIN_HOST` siguen funcionando — los scripts hacen fallback a él cuando `SSH_WIN_INTEL_HOST` no está definido.

Deja una variable vacía u omítela para saltar esa plataforma. Verifica que SSH funciona antes de continuar:

```bash
ssh -o ConnectTimeout=5 user@mac-host 'echo ok'
```

### 2. Preparar el fixture de pruebas

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

Edita `test-gitbox.json` y rellena cuentas y tokens reales. Cada cuenta con clave `_test` necesita un token API válido — créalos en la web de tu proveedor:

| Proveedor           | Dónde crearlo                                          | Scopes necesarios                                      |
| ------------------- | ------------------------------------------------------ | ------------------------------------------------------ |
| **GitHub**          | Settings → Developer settings → Personal access tokens | `repo` (full), `read:user`                             |
| **Gitea / Forgejo** | Settings → Applications → Manage Access Tokens         | Repository: Read+Write, User: Read, Organization: Read |
| **GitLab**          | Preferences → Access Tokens                            | scope `api`                                            |
| **Bitbucket**       | Personal settings → App passwords                      | Repositories: Read+Write                               |

Consulta [test-gitbox.json.example](../../json/test-gitbox.json.example) para ver la estructura completa con comentarios inline.

### 3. Configurar credenciales en todas las máquinas

```bash
./scripts/setup-credentials.sh all
```

Esto hace varias cosas en cada target:

1. Copia `test-gitbox.json` al remoto
2. Verifica tokens API contra cada proveedor
3. Genera pares de claves SSH únicos para esa máquina (nombrados `test-<hostname>-<account>-sshkey`)
4. Escribe entradas SSH config para cada cuenta
5. Prueba conexiones SSH

Después de ejecutar el script, registra las claves públicas de cada máquina en tus proveedores. El script imprime la clave exacta que debes pegar para cualquiera que falle la verificación:

```text
  FAIL  gitbox-gb-github-personal — public key not registered
        Add key at https://github.com/settings/keys: ssh-ed25519 AAAAC3... test-bolica-gb-github-personal
```

Dónde registrar claves SSH:

- **GitHub:** Settings → SSH and GPG keys → New SSH key
- **GitLab:** Settings → SSH keys → Add key
- **Gitea / Forgejo:** Settings → SSH / GPG Keys → Add Key
- **Bitbucket:** Personal settings → SSH keys → Add key

Después de registrar todas las claves, vuelve a ejecutar `./scripts/setup-credentials.sh all` para verificar que todo muestra `ok` verde.

## Workflow diario

### Build y deploy

```bash
./scripts/deploy.sh
```

Cross-compila para las 3 plataformas y copia por SCP los binarios a cada remoto configurado. También copia `test-gitbox.json` si existe. Tarda unos 10 segundos.

### Smoke test

```bash
./scripts/smoke.sh all
```

Ejecuta `version`, `help` y comandos de salida JSON en todas las plataformas. No interactivo — el script lo ejecuta todo y reporta pass/fail.

### Pruebas interactivas (test-mode)

```bash
./scripts/test-commands.sh
```

Imprime los comandos exactos que ejecutar en cada plataforma. Cópialos y pégalos en tu terminal. La salida se adapta a tu `.env` — las plataformas locales se ejecutan directamente, los remotos usan SSH:

```text
  Windows:  ssh luis@kymera  →  ~/gitbox.exe --test-mode
  macOS:    build/gitbox-darwin-arm64 --test-mode
  Linux:    ssh -t luis@luix "/tmp/gitbox --test-mode"
```

**Nota SSH en Windows:** la TUI no funciona con `ssh -t host "command"` en Git Bash de Windows — sale inmediatamente. Entra por SSH a la máquina primero, luego ejecuta el comando (el enfoque de dos pasos mostrado arriba con `→`).

**¿Qué es test-mode?** El flag `--test-mode` ejecuta gitbox en un directorio temporal aislado. Lee `test-gitbox.json` en vez de tu config real, crea todos los clones en una carpeta temporal descartable e inyecta tokens de prueba como variables de entorno. Nada toca tu `~/.config/gitbox/` real ni clones existentes. El directorio temporal se borra automáticamente cuando gitbox sale.

### Pruebas interactivas (producción)

```bash
./scripts/run-commands.sh
```

La misma idea, pero usa el `~/.config/gitbox/gitbox.json` real en la máquina target.

### Sincronizar config de producción a un remoto

```bash
./scripts/send-my-production-config.sh mac
```

Copia tu `gitbox.json` local al remoto. Muestra un diff y pide confirmación antes — esto sobrescribe la config remota.

## Referencia de scripts

| Script                                  | Qué hace                                                                    |
| --------------------------------------- | --------------------------------------------------------------------------- |
| `deploy.sh`                             | Compila los 3 binarios + despliega a remotos                                |
| `smoke.sh [target]`                     | Smoke tests no interactivos                                                 |
| `test-commands.sh [target]`             | Imprime comandos test-mode para que el usuario los ejecute                  |
| `run-commands.sh [target]`              | Imprime comandos production-mode para que el usuario los ejecute            |
| `setup-credentials.sh [target]`         | Configura claves SSH y verifica tokens en el target                         |
| `send-my-production-config.sh <target>` | Copia la config local de producción a un remoto                             |
| `test-setup-credentials.sh [path]`      | Configuración de credenciales de bajo nivel (llamada por setup-credentials) |

**Targets:** `win-intel`, `win-arm`, `mac-arm`, `mac-intel`, `linux` o `all`. Aliases back-compat: `win` → `win-intel` y `mac` → `mac-arm` (los defaults históricos de una sola máquina). La mayoría de scripts usa por defecto `all` las plataformas disponibles cuando no se pasa target.

## Cómo funciona

Los scripts autodetectan tu OS local. Para operaciones locales, los comandos se ejecutan directamente. Para operaciones remotas, usan SSH con los hosts de `.env`.

- **Binarios** van a `build/` localmente, `/tmp/gitbox` en remotos Unix y `~/gitbox.exe` en remotos Windows
- **test-gitbox.json** va a `~/test-gitbox.json` en remotos (gitbox camina hacia arriba desde cwd para encontrarlo)
- **Claves SSH** se nombran `test-<hostname>-<account>-sshkey` para que cada OS tenga claves únicas
- **Cross-compilation** ocurre en tu máquina dev — Go lo gestiona nativamente

## Pruebas solo locales

Si no tienes acceso SSH a otras máquinas, aun así puedes:

- Ejecutar pruebas unitarias: `go test -short ./...`
- Ejecutar pruebas de integración: `go test ./...` (requiere `test-gitbox.json`)
- Compilar para tu OS local: `go build -o build/gitbox ./cmd/cli`
- Configurar credenciales locales: `./scripts/setup-credentials.sh`

CI (GitHub Actions) prueba las 3 plataformas en cada push, así que las regresiones cross-platform se detectan automáticamente incluso sin remotos.

## Troubleshooting

**"Permission denied" en SSH:**
Comprueba que tu clave SSH está en `~/.ssh/authorized_keys` en el remoto. Los scripts requieren autenticación basada en clave (sin passwords). Verifica con: `ssh -o ConnectTimeout=5 user@host 'echo ok'`

**"command not found: jq" en remoto:**
Instala jq en la máquina remota (`apt install jq` en Debian/Ubuntu, `brew install jq` en macOS).

**El binario crashea en remoto:**
El script deploy gestiona GOOS/GOARCH automáticamente. Si compilaste manualmente, verifica: Windows amd64 = `windows/amd64`, Windows arm64 = `windows/arm64`, macOS Apple Silicon = `darwin/arm64`, macOS Intel = `darwin/amd64`, Linux = `linux/amd64`.

**test-mode no encuentra test-gitbox.json:**
Ejecuta `./scripts/deploy.sh` — copia el fixture a `~/test-gitbox.json` en remotos. O ejecuta `./scripts/setup-credentials.sh <target>`, que también lo copia.

**SSH timeout:**
Añade `ConnectTimeout 10` a tu `~/.ssh/config` para ese host.
