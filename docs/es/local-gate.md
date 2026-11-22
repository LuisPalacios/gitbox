# Gate local

`scripts/gate.sh` verifica un commit concreto en macOS, Linux y Windows en paralelo, además de las pruebas de integración con sshd del modo remoto, en mis propias máquinas, y sustituye a las comprobaciones de PR de GitHub como barrera de merge. Esta página explica cómo funciona y cómo reproducirlo en otro repositorio.

## Por qué verifico en local

Las comprobaciones de PR esperaban a los runners alojados de GitHub. El job de macOS se ejecutaba en cerca de un minuto, pero a menudo pasaba 5–15 minutos en cola, y un ruleset de comprobaciones obligatorias bloqueaba cada cambio en él. Los runners self-hosted eliminarían la cola, pero en un repositorio público un pull request desde un fork podría ejecutar código en ellos.

El gate local mantiene la verificación en hardware que controlo y nunca ejecuta código que no he revisado. GitHub mantiene una comprobación informativa de Linux en los pull requests, y el workflow de release sigue construyendo y probando todas las plataformas antes de una release.

## Cómo funciona una ejecución

1. El gate se niega a ejecutarse cuando hay cambios sin commitear en archivos rastreados, porque prueba el `HEAD` commiteado, no el árbol de trabajo.
2. Empaqueta `HEAD` y sus tags con `git bundle`. Los tags permiten que `git describe` funcione, como hace el `fetch-depth: 0` del checkout de CI.
3. Para cada carril copia el bundle y `scripts/gate-lane.sh` al host del carril, bajo `~/.cache/gitbox-gate/in/`. El carril borra los dos al terminar, tanto si pasa como si falla, y elimina los archivos que una ejecución interrumpida dejó allí hace más de un día.
4. Todos los carriles se ejecutan a la vez. Cada carril trae su propio bundle a su propio repositorio de caché, para que los carriles que comparten host no compitan por los mismos archivos, clona el commit en un directorio limpio bajo `~/.cache/gitbox-gate/` y ejecuta sus comprobaciones.
5. El gate imprime una línea de resumen por carril y guarda el log completo de cada carril en `${TMPDIR:-/tmp}/gitbox-gate/<sha>/<lane>.log`.
6. Cuando todos los carriles pasan, el gate registra un marcador para el SHA.

Un carril borra su clon tras pasar (`GATE_KEEP=1` lo conserva) y lo conserva tras un fallo para inspeccionarlo. Los clones de más de un día se eliminan en la siguiente ejecución.

## Qué ejecuta cada carril

Cada carril salvo remote-it construye el frontend embebido con `npm ci && npm run build`, construye la GUI con la versión de la CLI de Wails que fija CI (`go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build`) comprueba que los bindings que ese build regeneró coinciden con los del commit, y comprueba el ejecutable construido.

El build es el que hace una release. Cada uno de esos carriles compila los seis binarios de `gitbox-server` con `scripts/build-server.sh`, ejecuta la prueba que vuelve a leer cada uno contra su SHA-256 fijado, y construye la GUI con `-tags serverbin` para que los lleve. Después `scripts/check-gui-artifact.sh` hace fallar el carril salvo que `GitboxApp --version` y `GitboxApp --list-server-targets` informen exactamente de la versión y el commit del sello del carril con los seis targets, y salvo que el ejecutable principal esté dentro de su techo en `scripts/size-ceilings.txt`.

- **mac:** `HEALTH_FUZZ=1 ./scripts/health.sh` (todos los comprobadores, incluidos vet y staticcheck para linux, darwin y windows, las pruebas unitarias, los fixtures del bootstrap, el lint de Markdown y la comprobación del espejo del arnés), más las dos suites de coordinación. El build apunta a macOS 13 como la release.
- **linux:** `go vet ./...`, `go test -short ./...` y `bash scripts/test-bootstrap.sh`, y después el build con `-tags webkit2_41`.
- **win:** las dos suites de coordinación, `go test -short ./...` y `bash scripts/test-bootstrap.sh`, y después el build. El carril fija `core.autocrlf=false` y `core.longpaths=true` mediante `GIT_CONFIG_COUNT` para sus propios procesos, así que la configuración global de Git del usuario del host nunca cambia.
- **remote-it:** `bash scripts/remote-it.sh` en el host del carril linux, a la vez que el carril linux. Construye la imagen `gitbox-sshd` a partir de `test/sshd`, construye un servidor `linux/amd64` con el tag `e2e_sentinel` y ejecuta `go test -race -tags sshd_integration ./pkg/remote/`: los escenarios del arranque del modo remoto, cada uno contra un contenedor nuevo en una red interna de Docker. No construye ninguna GUI. Consulta [Pruebas › Pruebas del modo remoto](testing.md#pruebas-del-modo-remoto).

## Hosts y requisitos previos

El gate lee los hosts de `.env`, el mismo archivo que usan `ship.sh` y `smoke.sh` (consulta [Multiplataforma](multiplatform.md)). Un carril se ejecuta en local cuando esta máquina coincide con su plataforma, y por SSH en caso contrario:

```bash
SSH_WIN_HOST="user@windows-host"     # win lane (or SSH_WIN_INTEL_HOST)
SSH_LINUX_HOST="user@linux-host"     # linux and remote-it lanes
SSH_MAC_ARM_HOST=""                  # mac lane; empty when the gate runs on that Mac
```

Un worktree enlazado no tiene `.env` propio, así que los scripts recurren al `.env` del clon principal.

Cada host necesita SSH por clave sin preguntas (`BatchMode`), `git`, Go, Node con npm, `ssh-keygen` de OpenSSH 8.2 o posterior para los fixtures del bootstrap y `bash` (Git Bash en Windows). Los carriles mac y win también necesitan `uv`. El carril mac ejecuta la comprobación de salud, así que necesita las herramientas de salud de la [guía de desarrollo](developer-guide.md#comprobación-de-salud) (`shellcheck`, `actionlint`, `pnpm`). El host Linux necesita los paquetes de desarrollo de GTK 3 y WebKitGTK 4.1 (`libgtk-3-dev libwebkit2gtk-4.1-dev` en Debian o Ubuntu). Debian funciona igual que Ubuntu; solo el AppImage de la release depende de una base Ubuntu, y la release sigue construyéndose en GitHub. El carril remote-it necesita Docker en el host Linux, con el usuario de SSH en el grupo `docker`; Docker Desktop en macOS no llega a las IP de los contenedores desde el host, así que ese carril solo se ejecuta en Linux. Su primera ejecución descarga `debian:bookworm-slim` y los paquetes de OpenSSH y fish.

La primera ejecución en un host descarga los módulos de Go, los paquetes npm y la CLI de Wails. Las siguientes reutilizan esas cachés, y de ahí sale la mayor parte de la velocidad.

## Ejecutar el gate

```bash
./scripts/gate.sh                  # every lane
./scripts/gate.sh --only linux     # one lane (repeatable)
./scripts/gate.sh --skip win       # every lane but one (repeatable)
./scripts/gate.sh --only remote-it # the sshd integration tests only
./scripts/gate.sh --report         # publish the HEAD marker as a commit status
```

`--only` y `--skip` registran un marcador **parcial**. Los uso cuando un host está caído, y lo indico en el PR.

`--report` publica el marcador como un estado de commit `local-gate` en GitHub: `success` para un paso completo, `pending` para uno parcial. El commit tiene que estar ya subido. Selecciona la cuenta de `gh` a partir del propietario de `remote.origin.url`.

## Marcadores, el pre-push hook y los merges

El marcador es un pequeño archivo de texto en `$(git rev-parse --git-common-dir)/gitbox-gate/<sha>` con el estado, los carriles y sus duraciones. Todos los worktrees del clon comparten el git common dir, así que un gate ejecutado en un worktree cuenta en los demás. Un carril fallido elimina el marcador del commit, porque manda la evidencia más reciente; una re-ejecución con `--only` o `--skip` que pasa conserva un paso completo anterior.

`.githooks/pre-push` hace cumplir el gate:

- Un push que actualiza `main` necesita un marcador `pass` para el commit que se sube. Un marcador `partial` pasa solo con `GATE_ALLOW_PARTIAL=1`, y la falta de marcador bloquea el push.
- Un push a otra rama se salta las comprobaciones ligeras del hook (`gofmt`, `go vet`, `go test -short`) cuando el commit ya tiene marcador, y las ejecuta en caso contrario.
- Los push de tags y los borrados de ramas se saltan todas las comprobaciones.

`/merge-pr` solo fusiona cuando el commit head del PR tiene un marcador `pass` o un estado `local-gate` correcto, y cuando la comprobación informativa de GitHub no ha fallado. Un rebase da al head un SHA nuevo, así que el gate vuelve a ejecutarse después. Un squash merge lleva a `main` el árbol del commit head, y `/merge-pr` comprueba antes que el head contiene el último `main`.

Nada en GitHub exige ya una comprobación. El ruleset que protege `main` de borrados y force pushes se mantiene, y el gate depende de que el hook esté activo (`git config core.hooksPath .githooks`). `git push --no-verify` lo evita, así que nunca lo uso en `main`.

## Qué queda en GitHub

- `.github/workflows/pr.yml` ejecuta el job `check` de Linux en los pull requests y en los push a `main`: la comprobación de salud, un build de la GUI de Linux y un smoke con `--version` en un runner `ubuntu-22.04` limpio. Es informativo, así que no bloquea ni retrasa nada.
- `.github/workflows/ci.yml` se ejecuta con los tags de versión. Su job `test` ejecuta vet, las pruebas cortas y las dos suites de coordinación en Linux y Windows antes de construir y empaquetar sus assets de la release. `scripts/release-macos.sh` construye los assets de macOS en mi Mac.

## Replicar el gate en otro repositorio

1. Copio `scripts/gate.sh`, `scripts/gate-lane.sh` y los helpers que usan de `scripts/_common.sh` (`load_env`, `ssh_host_for`, `run_on`, `platform_label` y las funciones de salida).
2. Edito los pasos de los carriles en `gate-lane.sh` para el proyecto: su build, sus comandos de prueba y su smoke check. Mantengo el clon limpio, las rutas por SHA y el subshell de `step()`, porque bash ignora `set -e` dentro de una función llamada desde un `if`.
3. Creo un `.env` con un host SSH por plataforma remota, y compruebo `ssh -o BatchMode=yes <host> true` contra cada uno.
4. Instalo el toolchain en cada host y ejecuto `./scripts/gate.sh` una vez para calentar las cachés.
5. Añado la comprobación del marcador al hook `pre-push` del repositorio y lo activo con `git config core.hooksPath .githooks`.
6. Hago que el procedimiento de merge exija el marcador o el estado `local-gate`, como hace `/merge-pr`.
7. Quito las comprobaciones de estado obligatorias de GitHub (rulesets o protección de ramas), mantengo la protección contra borrados y force pushes, y dejo los jobs nativos lentos solo donde una release los necesita.

## Resolución de problemas

- **Un carril falla enseguida con "host unreachable":** el gate comprueba SSH con un timeout de 10 segundos y `BatchMode`. Enciende el host o arregla su clave y vuelve a ejecutarlo, o usa `--skip <lane>` y acepta un marcador parcial.
- **"uncommitted changes to tracked files":** commitea o guarda en stash los cambios; el gate nunca prueba el árbol de trabajo.
- **Un carril falla:** el resumen muestra las últimas líneas de su log y la ruta del log completo. El clon fallido queda en `~/.cache/gitbox-gate/work-<lane>-<sha>` en ese host.
- **Las pruebas de Windows fallan por archivos bloqueados:** un handle que queda abierto (por ejemplo, una conexión SQLite que solo hace commit mediante su gestor de contexto) mantiene el archivo bloqueado, y la limpieza del directorio temporal falla. Cierra el handle explícitamente.
