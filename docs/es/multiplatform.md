# Desarrollo multiplataforma

Pruebo gitbox en tres plataformas: Windows, macOS y Linux. Los scripts de `scripts/` automatizan el ciclo build-ship-test para que pueda trabajar desde cualquier OS y ejecutar `GitboxApp` en los otros dos vía SSH.

Para probar las 3 plataformas, necesitas acceso SSH a máquinas que ejecuten los otros dos OSs (máquinas físicas, VMs o instancias cloud). Si solo tienes una máquina, aun así puedes ejecutar las pruebas unitarias y la prueba de escenario localmente — CI cubre las otras plataformas.

La GUI no se puede cross-compilar: cada plataforma necesita su propio webview nativo (WebView2 en Windows, WebKit en macOS, WebKitGTK en Linux). Por eso los scripts envían el código fuente a cada remoto y ejecutan `wails build` allí.

## Qué necesitas

- **Go 1.26+**, **Node.js 20.19+ o 22.12+** y la **Wails CLI v2** en tu máquina de desarrollo y en cada remoto que compile la GUI (consulta [developer-guide.md](developer-guide.md) para ver las librerías de cada OS)
- **Autenticación SSH basada en clave** a tus máquinas remotas (sin passwords)
- **Git Bash** en Windows (viene con Git for Windows)
- **jq** y **curl** en todas las máquinas (para configuración de credenciales)
- **Una sesión de escritorio** en cada máquina donde lances la GUI de forma interactiva

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

### Build local

Siempre empiezo en la máquina donde trabajo: compilo la GUI con `wails build` (consulta [developer-guide.md](developer-guide.md)) y la lanzo allí primero. La salida queda en `cmd/gui/build/bin/`.

### Enviar a remotos

```bash
./scripts/ship.sh            # todos los remotos configurados, en paralelo
./scripts/ship.sh myhost     # solo el host cuyo nombre corto coincide
```

Envía el código fuente a cada remoto con `tar | ssh`, ejecuta `wails build` allí y deja el resultado preparado: `/tmp/GitboxApp.app` en macOS, `/tmp/GitboxApp` en Linux y `~/GitboxApp.exe` en Windows. Cuando existe `test-gitbox.json` en la raíz del repo, se copia a `~/test-gitbox.json` en cada remoto. Los logs de cada host van a `/tmp/gitbox-ship-<platform>.log`.

### Smoke test

```bash
./scripts/smoke.sh all
```

Ejecuta `GitboxApp --version` en todas las plataformas — la salida local de `wails build` y las copias que `ship.sh` dejó preparadas en los remotos. El flag imprime la versión y sale sin abrir ninguna ventana, así que funciona por SSH normal. No interactivo — el script lo ejecuta todo y reporta pass/fail.

### Pruebas interactivas (test-mode)

```bash
./scripts/test-commands.sh
```

Imprime el comando exacto para lanzar `GitboxApp --test-mode` en cada plataforma. La GUI necesita la sesión de escritorio del target, así que los comandos se imprimen, no se ejecutan — ejecuta cada uno en un terminal de ese host:

```text
  Windows (me@win-host):  cd ~ && ~/GitboxApp.exe --test-mode
  macOS:  cd "/path/to/gitbox" && "/path/to/gitbox/cmd/gui/build/bin/GitboxApp.app/Contents/MacOS/GitboxApp" --test-mode
  Linux (me@linux-host):  cd ~ && /tmp/GitboxApp --test-mode
```

**¿Qué es test-mode?** El flag `--test-mode` ejecuta GitboxApp en un directorio temporal aislado. Lee `test-gitbox.json` (buscándolo hacia arriba desde el directorio actual) en vez de tu config real, crea todos los clones en una carpeta temporal descartable e inyecta tokens de prueba como variables de entorno. Nada toca tu `~/.config/gitbox/` real ni clones existentes. El directorio temporal se borra automáticamente cuando la app sale.

### Pruebas interactivas (producción)

```bash
./scripts/run-commands.sh
```

La misma idea, pero los comandos impresos lanzan GitboxApp contra el `~/.config/gitbox/gitbox.json` real en la máquina target.

### Sincronizar config de producción a un remoto

```bash
./scripts/send-my-production-config.sh mac
```

Copia tu `gitbox.json` local al remoto. Muestra un diff y pide confirmación antes — esto sobrescribe la config remota.

## Referencia de scripts

| Script                                  | Qué hace                                                                        |
| --------------------------------------- | ------------------------------------------------------------------------------- |
| `ship.sh [short-name]`                  | Compila la GUI en cada remoto y la deja preparada para pruebas                  |
| `smoke.sh [target]`                     | Smoke test no interactivo (`GitboxApp --version`)                               |
| `test-commands.sh [target]`             | Imprime comandos de lanzamiento test-mode para que el usuario los ejecute       |
| `run-commands.sh [target]`              | Imprime comandos de lanzamiento production-mode para que el usuario los ejecute |
| `setup-credentials.sh [target]`         | Configura claves SSH y verifica tokens en el target                             |
| `send-my-production-config.sh <target>` | Copia la config local de producción a un remoto                                 |
| `test-setup-credentials.sh [path]`      | Configuración de credenciales de bajo nivel (llamada por setup-credentials)     |

**Targets:** `win-intel`, `win-arm`, `mac-arm`, `mac-intel`, `linux` o `all`. Aliases back-compat: `win` → `win-intel` y `mac` → `mac-arm` (los defaults históricos de una sola máquina). La mayoría de scripts usa por defecto `all` las plataformas disponibles cuando no se pasa target. `ship.sh` recibe en su lugar el nombre corto de un host (p. ej. `myhost` para `user@myhost`).

## Cómo funciona

Los scripts autodetectan tu OS local. Para operaciones locales, los comandos se ejecutan directamente. Para operaciones remotas, usan SSH con los hosts de `.env`.

- **La GUI** se compila localmente en `cmd/gui/build/bin/`, y en remotos en un directorio scratch antes de quedar preparada en `/tmp/GitboxApp[.app]` en Unix y `~/GitboxApp.exe` en Windows
- **test-gitbox.json** va a `~/test-gitbox.json` en remotos (GitboxApp lo busca hacia arriba desde el directorio actual)
- **Claves SSH** se nombran `test-<hostname>-<account>-sshkey` para que cada OS tenga claves únicas
- **Las shells SSH sin login** no cargan tu perfil de shell, así que los scripts amplían `PATH` con las ubicaciones habituales de Go, Wails y Homebrew antes de compilar en un remoto

## Pruebas solo locales

Si no tienes acceso SSH a otras máquinas, aun así puedes:

- Ejecutar pruebas unitarias: `go test -short ./...`
- Ejecutar la prueba de escenario: `go test ./...` (requiere `test-gitbox.json`)
- Compilar para tu OS local: `cd cmd/gui && wails build`
- Configurar credenciales locales: `./scripts/setup-credentials.sh`

CI (GitHub Actions) ejecuta vet, pruebas unitarias, la comprobación del frontend y un build de la GUI en Linux en cada pull request, y compila todas las plataformas en cada tag de release, así que las regresiones cross-platform se detectan incluso sin remotos.

## Troubleshooting

**"Permission denied" en SSH:**
Comprueba que tu clave SSH está en `~/.ssh/authorized_keys` en el remoto. Los scripts requieren autenticación basada en clave (sin passwords). Verifica con: `ssh -o ConnectTimeout=5 user@host 'echo ok'`

**"error in libcrypto" seguido de "Permission denied" desde Git Bash en Windows:**
Tus claves viven en un agente SSH (1Password o el servicio ssh-agent de Windows) y en disco solo existen ficheros `.pub`. El `ssh` MSYS propio de Git Bash no puede alcanzar el agente de named pipe de Windows, así que intenta cargar el `.pub` como clave privada y falla. Los scripts ahora prefieren automáticamente el OpenSSH nativo de Windows en `C:\Windows\System32\OpenSSH` cuando existe. Si ejecutas `ssh` a mano desde Git Bash, antepón el PATH de la misma forma: `PATH="/c/Windows/System32/OpenSSH:$PATH" ssh host`.

**"command not found: jq" en remoto:**
Instala jq en la máquina remota (`apt install jq` en Debian/Ubuntu, `brew install jq` en macOS).

**"wails: command not found" durante ship:**
El build remoto se ejecuta en una shell sin login. Instala la Wails CLI en el remoto con `go install github.com/wailsapp/wails/v2/cmd/wails@latest` y comprueba que `$HOME/go/bin` existe. Lee el log del host en `/tmp/gitbox-ship-<platform>.log` para ver el error exacto.

**test-mode no encuentra test-gitbox.json:**
Ejecuta `./scripts/ship.sh` — copia el fixture a `~/test-gitbox.json` en remotos. O ejecuta `./scripts/setup-credentials.sh <target>`, que también lo copia. Lanza la app desde el directorio home (`cd ~`) para que la búsqueda hacia arriba encuentre el archivo.

**SSH timeout:**
Añade `ConnectTimeout 10` a tu `~/.ssh/config` para ese host.
