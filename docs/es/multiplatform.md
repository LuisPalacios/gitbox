# Desarrollo multiplataforma

Gitbox funciona en Windows, macOS y Linux. Los scripts de `scripts/` automatizan el ciclo de build, envío y smoke test, así que el trabajo se hace en una máquina y `GitboxApp` se ejecuta en las demás por SSH.

La GUI no se puede cross-compilar: cada plataforma necesita su propio webview nativo (WebView2 en Windows, WebKit en macOS, WebKitGTK en Linux). Por eso los scripts envían el código fuente a cada remoto y ejecutan `wails build` allí.

Sin máquinas remotas, las pruebas unitarias y la prueba de escenario siguen ejecutándose en local, y CI cubre las demás plataformas. Consulta [Pruebas solo locales](#pruebas-solo-locales).

## Qué necesitas

- Los requisitos de build de [developer-guide.md](developer-guide.md#requisitos-previos) (Go, Node.js, la Wails CLI y las librerías de cada OS) en la máquina de desarrollo y en cada remoto que compile la GUI.
- Autenticación SSH basada en clave en todos los remotos (sin contraseñas).
- Git Bash en Windows (viene con Git for Windows).
- `jq` y `curl` en todas las máquinas, para la configuración de credenciales.
- Una sesión de escritorio en cada máquina donde se lance la GUI de forma interactiva.

## Configuración inicial

### 1. Configurar hosts SSH

```bash
cp docs/.env.example .env
```

Edita `.env` con tus hosts SSH remotos. Los scripts detectan el OS local, así que deja vacía la variable de esa plataforma y define las demás como `user@hostname`. Windows y macOS se dividen cada uno en dos targets por arquitectura, para que máquinas amd64 y arm64 puedan coexistir en un mismo `.env`:

| Variable             | Target      | Plataforma                  |
| -------------------- | ----------- | --------------------------- |
| `SSH_WIN_INTEL_HOST` | `win-intel` | Windows amd64               |
| `SSH_WIN_ARM_HOST`   | `win-arm`   | Windows arm64               |
| `SSH_MAC_ARM_HOST`   | `mac-arm`   | macOS Apple Silicon (arm64) |
| `SSH_MAC_INTEL_HOST` | `mac-intel` | macOS Intel (amd64)         |
| `SSH_LINUX_HOST`     | `linux`     | Linux amd64                 |

Por ejemplo, en una máquina Windows amd64 con dos Macs y un equipo Linux como remotos:

```bash
SSH_WIN_INTEL_HOST=""
SSH_WIN_ARM_HOST=""
SSH_MAC_ARM_HOST="user@mac-arm-host"
SSH_MAC_INTEL_HOST="user@mac-intel-host"
SSH_LINUX_HOST="user@linux-host"
```

`docs/.env.example` incluye más escenarios. Deja una variable vacía u omítela para saltar esa plataforma. Un `.env` antiguo con un único `SSH_WIN_HOST` sigue funcionando: los scripts recurren a él cuando `SSH_WIN_INTEL_HOST` no está definido.

Comprueba SSH antes de continuar:

```bash
ssh -o ConnectTimeout=5 user@mac-arm-host 'echo ok'
```

### 2. Preparar el fixture de pruebas

Copia `json/test-gitbox.json.example` a `test-gitbox.json` en la raíz del repo y rellena cuentas y tokens reales. [testing.md](testing.md#el-fixture-de-pruebas) explica el fixture y su clave `_test`, y [credentials.md](credentials.md#cómo-crear-un-pat) muestra cómo crear el token de cada proveedor.

### 3. Configurar credenciales en todas las máquinas

```bash
./scripts/setup-credentials.sh all
```

Para cada target, el script copia `test-gitbox.json` y `scripts/test-setup-credentials.sh` al remoto y lo ejecuta allí. En cada máquina:

1. Verifica el token API de cada cuenta que tiene clave `_test`.
2. Genera un par de claves SSH único para esa máquina, llamado `test-<hostname>-<account>-sshkey` (conserva las claves existentes).
3. Añade una entrada `Host` por cuenta a `~/.ssh/config` (conserva las entradas existentes).
4. Prueba cada conexión SSH.

Después registra las claves públicas de cada máquina en tus proveedores. Para cada clave que falla la verificación, el script imprime la página que debes abrir y la clave exacta que debes pegar:

```text
  FAIL  gitbox-github-myaccount — public key not registered
        Add key at https://github.com/settings/keys: ssh-ed25519 AAAAC3... test-<hostname>-github-MyAccount
```

Vuelve a ejecutar `./scripts/setup-credentials.sh all` después de registrar las claves. Todas las líneas deberían mostrar un `ok` verde. El script es idempotente.

## Flujo diario

### Build local

Empieza en la máquina donde trabajas: compila la GUI como se describe en [developer-guide.md](developer-guide.md#compilar-desde-el-código-fuente) y lánzala allí primero. La salida queda en `cmd/gui/build/bin/`.

### Enviar a remotos

```bash
./scripts/ship.sh            # todos los remotos configurados, en paralelo
./scripts/ship.sh myhost     # solo el host cuyo nombre corto coincide
```

El nombre corto es la parte de host de `user@host` sin su dominio; el valor completo `user@host` también funciona. Para cada remoto, `ship.sh`:

1. Envía el código fuente con `tar | ssh` a `~/gitbox-remote-build`, sin `.git`, `.env`, `node_modules` ni la salida de build.
2. Compila el frontend y después ejecuta `wails build -s` con la versión y el commit del checkout local estampados mediante `-ldflags`, para que el binario remoto no muestre `dev-none`.
3. Deja el resultado preparado en `/tmp/GitboxApp.app` en macOS, `/tmp/GitboxApp` en Linux y `~/GitboxApp.exe` en Windows.
4. Copia `test-gitbox.json` a `~/test-gitbox.json` cuando existe en la raíz del repo.

Los logs de cada host van a `/tmp/gitbox-ship-<platform>.log`, y el resumen muestra las últimas líneas de cada log fallido. Después de enviar a un Mac, `ship.sh` imprime los pasos únicos que permiten a una app preparada en `/tmp` alcanzar servidores de la red local; [credentials.md](credentials.md#macos-permiso-de-red-local) explica por qué.

### Smoke test

```bash
./scripts/smoke.sh all
```

Ejecuta `GitboxApp --version` en todas las plataformas: la salida local de `wails build` y las copias que `ship.sh` dejó preparadas en los remotos. El flag imprime `GitboxApp <version> (<sha>)` y sale sin abrir ninguna ventana, así que funciona por SSH normal. El script informa de éxito o fallo por plataforma y sale con código distinto de cero ante cualquier fallo.

### Pruebas interactivas (modo de prueba)

```bash
./scripts/test-commands.sh
```

Imprime el comando que lanza `GitboxApp --test-mode` en cada plataforma. La GUI necesita la sesión de escritorio del target, así que el script imprime los comandos en vez de ejecutarlos. Ejecuta cada uno en un terminal de ese host:

```text
  Windows (amd64) (user@win-host):  cd ~ && ~/GitboxApp.exe --test-mode
  macOS (arm64):  cd "<repo>" && "<repo>/cmd/gui/build/bin/GitboxApp.app/Contents/MacOS/GitboxApp" --test-mode
  Linux (user@linux-host):  cd ~ && /tmp/GitboxApp --test-mode
```

El modo de prueba ejecuta la app contra `test-gitbox.json` en un directorio desechable y nunca toca la config ni los clones reales; [testing.md](testing.md#ejecutar-las-pruebas) tiene los detalles. La app encuentra el fixture subiendo desde su directorio de trabajo, por eso los comandos remotos empiezan con `cd ~`. El script avisa cuando un remoto no tiene `~/test-gitbox.json`.

### Pruebas interactivas (producción)

```bash
./scripts/run-commands.sh
```

La misma idea, pero los comandos impresos lanzan `GitboxApp` contra el `~/.config/gitbox/gitbox.json` real de cada máquina. En macOS usan `open` sobre el bundle.

[testing.md](testing.md#ejecuciones-de-regresión-enfocadas) describe cómo medir el arranque en un escritorio nativo.

### Sincronizar la config de producción a un remoto

```bash
./scripts/send-my-production-config.sh mac-arm
```

Copia el `~/.config/gitbox/gitbox.json` local a un remoto. Primero muestra un diff y pide confirmación, porque sobrescribe la config del remoto. Acepta un único target y rechaza `all`.

## Referencia de scripts

| Script                                  | Qué hace                                                                       |
| --------------------------------------- | ------------------------------------------------------------------------------ |
| `ship.sh [short-name]`                  | Compila la GUI en cada remoto y la deja preparada para pruebas                 |
| `smoke.sh [target]`                     | Smoke test no interactivo (`GitboxApp --version`)                              |
| `test-commands.sh [target]`             | Imprime los comandos de lanzamiento en modo de prueba                          |
| `run-commands.sh [target]`              | Imprime los comandos de lanzamiento en modo producción                         |
| `setup-credentials.sh [target]`         | Verifica tokens y configura claves SSH en cada target                          |
| `send-my-production-config.sh <target>` | Copia la config local de producción a un remoto                                |
| `test-setup-credentials.sh [path]`      | Configuración de credenciales de bajo nivel que ejecuta `setup-credentials.sh` |

**Targets:** `win-intel`, `win-arm`, `mac-arm`, `mac-intel`, `linux` o `all`. Los alias `win` y `mac` equivalen a `win-intel` y `mac-arm`. Los scripts que aceptan `[target]` usan `all` por defecto y saltan las plataformas sin host en `.env`. `ship.sh` acepta en su lugar el nombre corto de un host y por defecto usa todos los remotos configurados. `ship.sh -h` imprime su ayuda.

## Cómo funciona

Los scripts cargan `scripts/_common.sh`, que detecta el OS local y carga `.env`. Los comandos para la plataforma local se ejecutan directamente; los comandos para las demás se ejecutan por SSH con los hosts de `.env`.

- La GUI se compila localmente en `cmd/gui/build/bin/`. En los remotos se compila en `~/gitbox-remote-build` y queda preparada en `/tmp/GitboxApp[.app]` en Unix y en `~/GitboxApp.exe` en Windows, porque `scp` y Git Bash no coinciden en dónde está `/tmp`.
- `test-gitbox.json` va a `~/test-gitbox.json` en los remotos.
- Las claves SSH se llaman `test-<hostname>-<account>-sshkey`, así que cada máquina tiene sus propias claves y todas se pueden registrar a la vez en un proveedor.
- Las shells SSH sin login no cargan el perfil de la shell, así que los scripts añaden a `PATH` las ubicaciones habituales de Go, Wails y Homebrew antes de compilar en un remoto. En remotos Windows también ponen las carpetas de apps de Scoop por delante de sus shims, que fallan en sesiones SSH.
- En una máquina de desarrollo Windows, los scripts prefieren el OpenSSH nativo de `C:\Windows\System32\OpenSSH`, que alcanza los agentes SSH detrás de la named pipe de Windows.

## Pruebas solo locales

Sin acceso SSH a otras máquinas:

- Ejecuta las pruebas unitarias: `go test -short ./...`
- Ejecuta la prueba de escenario: `go test ./pkg/ops/` (requiere `test-gitbox.json`)
- Compila para el OS local: consulta [developer-guide.md](developer-guide.md#compilar-desde-el-código-fuente)
- Configura las credenciales locales: `./scripts/setup-credentials.sh`

CI detecta las regresiones multiplataforma incluso sin remotos: `pr.yml` ejecuta la comprobación de salud y un build de la GUI en Linux en cada pull request, y `ci.yml` compila todas las plataformas en cada tag de release. Consulta [developer-guide.md](developer-guide.md#integración-continua).

## Resolución de problemas

**"Permission denied" en SSH.** Comprueba que tu clave pública está en `~/.ssh/authorized_keys` en el remoto. Los scripts requieren autenticación basada en clave. Prueba con `ssh -o ConnectTimeout=5 user@host 'echo ok'`.

**"error in libcrypto" seguido de "Permission denied" desde Git Bash en Windows.** Las claves viven en un agente SSH (un gestor de contraseñas como 1Password o Bitwarden, o el servicio ssh-agent de Windows) y en disco solo existen ficheros `.pub`. El `ssh` MSYS propio de Git Bash no puede alcanzar el agente de la named pipe de Windows, así que intenta cargar el `.pub` como clave privada y falla. Los scripts ya usan el OpenSSH nativo de Windows cuando existe. Si ejecutas `ssh` a mano desde Git Bash, ponlo primero en `PATH`: `PATH="/c/Windows/System32/OpenSSH:$PATH" ssh host`.

**"command not found: jq" en un remoto.** Instala `jq` allí (`apt install jq` en Debian o Ubuntu, `brew install jq` en macOS).

**"wails: command not found" durante el envío.** El build remoto se ejecuta en una shell sin login. Instala la Wails CLI en el remoto con `go install github.com/wailsapp/wails/v2/cmd/wails@latest` y comprueba que `$HOME/go/bin` existe. El log del host en `/tmp/gitbox-ship-<platform>.log` muestra el error exacto.

**El modo de prueba no encuentra `test-gitbox.json`.** Ejecuta `./scripts/ship.sh` o `./scripts/setup-credentials.sh <target>`; ambos copian el fixture a `~/test-gitbox.json` en los remotos. Lanza la app desde el directorio home (`cd ~`) para que la búsqueda hacia arriba lo encuentre.

**Timeout de SSH.** Añade `ConnectTimeout 10` a la entrada de ese host en `~/.ssh/config`.
