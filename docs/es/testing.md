# Pruebas

Esta guía cubre los niveles de prueba, el fixture `test-gitbox.json` y las checklists que ejecuto antes de un PR y antes de un release. El inventario de pruebas y los detalles internos del harness viven en [testing-reference.md](testing-reference.md). La comprobación de salud se describe en [developer-guide.md › Comprobación de salud](developer-guide.md#comprobación-de-salud).

## Niveles de prueba

- **Pruebas unitarias de paquete** — las pruebas Go bajo `pkg/`. No necesitan preparación, ni red, ni proveedor, y lo aíslan todo con `t.TempDir()` y `t.Setenv()`. Las pruebas unitarias de `pkg/ops` se ejecutan contra un `XDG_CONFIG_HOME` temporal, un `GIT_CONFIG_GLOBAL` temporal y una carpeta SSH temporal, así que nada cambia en el host.
- **Pruebas de GUI** — las pruebas Go bajo `cmd/gui/`. Llaman directamente a los métodos de `App`, sin ventana de Wails. Necesitan el frontend compilado en `cmd/gui/frontend/dist`, porque el paquete lo embebe.
- **Pruebas del frontend** — `npm test` ejecuta pruebas puras de Node y monta componentes de Svelte en jsdom. Ambas suites usan `node --test` y funcionan sin Wails ni credenciales. La prueba de catálogos usa Go para leer los códigos declarados en el repositorio.
- **Pruebas de integración del modo remoto** — `go test -tags sshd_integration ./pkg/remote/` contra un `sshd` en contenedor, que ejecuta `scripts/remote-it.sh` en un host Linux con Docker, y `go test -tags remote_host ./pkg/remote/` contra un host real, que ejecuta `scripts/remote-host-it.sh`. Consulta [Pruebas del modo remoto](#pruebas-del-modo-remoto).
- **Prueba de escenario** — `TestScenario_FullLifecycle` en `pkg/ops/scenario_test.go` recorre el ciclo de vida completo de una cuenta a través de `pkg/ops` contra un proveedor real. Necesita el fixture descrito más abajo, y `go test -short` la salta.

`go test ./pkg/...` se ejecuta sin el build del frontend. Todo lo demás en Go necesita `cmd/gui/frontend/dist`; constrúyelo una vez:

```bash
(cd cmd/gui/frontend && npm ci && npm run build)
```

`wails build` también lo crea. Consulta [developer-guide.md › Compilar desde el código fuente](developer-guide.md#compilar-desde-el-código-fuente).

## Comprobaciones del flujo de work units

Compruebo los cambios en las skills de work units con el gate del harness:

```bash
./scripts/health.sh harness markdown shellcheck actionlint
```

El gate del harness incluye `ai/scripts/test_coordinate_issues.py`, `ai/scripts/test_coordinate_roadmap.py` y `ai/scripts/test_work_issue_access.py`. Usan fixtures locales para el registro compartido, la cola de workers con capacidades simuladas de 15 y cuatro, el helper opcional de checklist de work unit, y la raíz de worktrees y los permisos de acceso de `work-issue`. Describo su cobertura en [testing-reference.md](testing-reference.md#pruebas-de-mantenimiento-del-harness-de-agentes).

También reviso el flujo en una sesión nueva de Codex y otra de Claude Code: `wu-container` redacta HLR y criterios de aceptación completos, `wu-tasks` vincula las tareas a esos criterios y `wu-coordinate-tasks` pregunta el número de contenedor si falta. Compruebo que la coordinación excluya las issues independientes y las tareas de otro padre, que las tareas dependientes esperen hasta la integración, y que la reanudación conserve la work unit indicada. Uso escenarios de solo lectura para estas comprobaciones; probar el descubrimiento no autoriza la publicación. Los cambios limitados al harness omiten las pruebas Go y las compilaciones de GUI; aun así ejecuto la comprobación de salud completa antes de una PR.

## Medición del arranque

Compilo la interfaz nativa y asigno a `binary` la ruta absoluta de su ejecutable. Creo flotas limpias de 50, 200 y 500 clones locales en directorios vacíos separados. Su cuenta y sus remotos usan la interfaz de bucle local, su vista inicial es **Todos los repos** y su configuración y ajustes de Git se guardan en el directorio de prueba. Para el primer tamaño ejecuto:

```bash
fleet=$(mktemp -d)
scripts/demo-fleet/benchmark.sh 50 "$fleet"
HOME="$fleet/home" XDG_CONFIG_HOME="$fleet/config" GIT_CONFIG_GLOBAL="$fleet/gitconfig" \
  GIT_CONFIG_NOSYSTEM=1 HTTP_PROXY=http://127.0.0.1:1 HTTPS_PROXY=http://127.0.0.1:1 \
  NO_PROXY=127.0.0.1 "$binary" --print-timing
```

Cierro la ventana cuando aparece la única línea JSON `startup-timing`. Para cada tamaño y compilación repito el arranque tres veces en la misma máquina con copias nuevas de la flota y registro la mediana de cada etapa. Comparo con las mismas opciones de compilación nativa y condiciones de caché. Para una revisión anterior a la opción, instrumento los mismos puntos de observación antes de compilarla.

Los valores son milisegundos de un reloj monotónico desde que se crea el objeto de aplicación Go de la ventana. `decode` registra la carga de la configuración, `mount` el montaje de la interfaz, `snapshot` la instantánea independiente, `firstRow` la primera fila de repositorio insertada en la vista actual, `firstStatus` el primer estado aplicado, `fullStatus` la pasada local completa y `firstRemote` la primera tarea de red de la flota programada. Mantengo los campos ausentes como observaciones sin registrar. El informe solo contiene tiempos y permanece en la máquina.

## Pruebas fuzz

Mantengo pequeños corpus de semillas de Go en `testdata/fuzz/<target>/` dentro de cada paquete. `go test` y `go test -short` normales ejecutan estas entradas sin generar mutaciones. Ejecuto las semillas directamente con:

```bash
go test ./pkg/config/ ./pkg/workspace/ ./pkg/status/ ./pkg/rpc/
```

Activo las pasadas de mutaciones con `HEALTH_FUZZ=1`; la comprobación de salud predeterminada sigue ejecutando solo las semillas. Cada target dispone de 10 segundos y dos workers. El job de salud de PR en Linux activa esta variable. Puedo ejecutar solo estas pasadas o incluirlas en la comprobación completa:

```bash
HEALTH_FUZZ=1 ./scripts/health.sh fuzz
HEALTH_FUZZ=1 ./scripts/health.sh
```

Para investigar durante más tiempo selecciono un solo target explícitamente:

```bash
go test ./pkg/config/ -run='^$' -fuzz='^FuzzDecodeRoundTrip$' -fuzztime=1m -parallel=2
```

`FuzzDecodeRoundTrip` comprueba la intención de la config migrada y que la serialización de los campos de extensión conservados sea estable, `FuzzStripJSONC` comprueba las propiedades de la eliminación de comentarios, `FuzzRepoPath` comprueba la resolución de rutas dentro de su dominio de entrada seguro documentado y `FuzzDecodeFrame` comprueba que el lector de tramas del protocolo del servidor nunca entra en pánico ni reserva más que su tope; [testing-reference.md](testing-reference.md#propiedades-fuzz) describe las aserciones. Go guarda una entrada que falla en el directorio `testdata/fuzz/` del target e imprime un comando para reproducirla. Conservo la entrada minimizada como semilla de regresión después de corregir su causa; no copio la caché de fuzz al repositorio.

## Pruebas del modo remoto

El arranque y la ventana de solo lectura del [modo remoto](remote-mode.md) tienen cuatro capas de pruebas.

- **Pruebas unitarias** en `pkg/remote`, que ejecuta cualquier `go test -short`. Incluyen `TestNoConfigOverrideInProduction` y un `ssh` falso que ejecuta los scripts remotos reales con el `sh` local en una carpeta home propia de la prueba: instalación, comprobaciones de hash, `noexec`, banners, desinstalación y las raíces rechazadas. `TestProbeDarwin` da al sondeo la respuesta que dio un Mac, y `TestHashToolSelection` ejecuta la función de hash con solo algunas de `shasum`, `sha256sum` y `openssl` en el `PATH`. Windows salta las pruebas del ssh falso, que necesitan un `sh` POSIX. `cmd/gui` añade `TestRemoteLifecycle`, que maneja una ventana remota con un conector que no necesita ssh, y las comprobaciones de solo lectura: `TestBackendContract` sobre cada método servido, `TestHostileServer`, `TestOpenExternal`, `TestRemoteWindowReadOnly`, `TestServerSavesThroughTheGuard` y `TestBackendContractMutations`, las comprobaciones del token de #242, `TestTokenCanary` y `TestHostRefusesGCM`, y la comprobación de las claves SSH de #244, `TestPrivateKeyNeverOnWire`. El `TestMain` del paquete da a estas pruebas una carpeta personal propia, así que una configuración SSH nunca escribe en el `~/.ssh` real:

  ```bash
  go test -race -short ./cmd/gui/ -run 'TestBackendContract|TestHostileServer|TestOpenExternal|TestRemoteWindowReadOnly|TestServerSavesThroughTheGuard|TestBackendContractMutations|TestTokenCanary|TestHostRefusesGCM|TestPrivateKeyNeverOnWire'
  ```

- **El build de servidor de `cmd/gui`.** `go test -race -short -tags gitboxserver ./cmd/gui/` ejecuta las pruebas del marcador de uso y del registro rotativo del servidor. Las pruebas que necesitan el shell de la ventana llevan `//go:build !gitboxserver`, así que esta ejecución compila.
- **Pruebas de integración con sshd** tras el tag `sshd_integration`, en `pkg/remote/integration_test.go` y los demás archivos `*_integration_test.go`, con el arnés de contenedores de `itdocker_test.go`:

  ```bash
  scripts/remote-it.sh                           # every scenario
  GITBOX_IT_RUN=TestFishLoginShell scripts/remote-it.sh
  GITBOX_IT_RUN='TestReadsLeaveTheDomainUntouched|TestMutationsOverSSH|TestCutSSHMidSave' scripts/remote-it.sh
  GITBOX_IT_RUN='TestCutSSHMidClone|TestLatency300ms' scripts/remote-it.sh
  GITBOX_IT_RUN='TestKillSSHDuringFetchAll|TestResumeInFlightClone|TestPeriodicSyncWhileDisconnected|TestNoZombieDaemons|TestSocketPermissions|TestHeartbeatDeadLink|TestConfigChangedWithTheDaemon' scripts/remote-it.sh
  GITBOX_IT_RUN=TestTokenCanary scripts/remote-it.sh
  GITBOX_IT_RUN='TestSSHAccountOnRemote|TestPrivateKeyNeverOnWire|TestAgentReport' scripts/remote-it.sh
  ```

  El script necesita Docker en Linux. Construye la imagen `gitbox-sshd` a partir de `test/sshd` (Debian slim con OpenSSH, fish, git, `iproute2` y `procps`, un usuario `test` cuya clave vive fuera del home, y un repositorio de prueba de 60 MB que `git daemon` sirve en `127.0.0.1:9418` cuando el contenedor arranca con `GIT_DAEMON=1`, y un usuario `git` con `git-shell` dueño de `org/small.git` cuando arranca con `GIT_HOST=1`, el proveedor git de las pruebas de cuentas SSH), un servidor `linux/amd64` con el tag `e2e_sentinel`, que añade una línea a `$HOME/.gitbox-e2e/ran.log` en cada arranque, y la forja falsa de `test/fakeforge`, que `TestTokenCanary` copia en su contenedor: la API de Gitea y git por HTTP tras los tokens de un archivo. Cada prueba arranca su propio contenedor en una red `--internal` de Docker, sin ruta de salida, y se conecta mediante el `ssh` real con un archivo de config y un `known_hosts` propios, rellenado con la clave del host del contenedor. Las pruebas de fallos arrancan el suyo con `--cap-add NET_ADMIN` y modelan su red con `tc netem`. "Nunca se ejecutó" significa que el número de líneas de `ran.log` no cambió. El gate local lo ejecuta en su carril `remote-it`. Las pruebas de `daemon_integration_test.go` inician cada sesión a través del daemon de #247 (`remote.Options.Daemon`), mientras que el resto mantiene `serve --stdio`, el camino de una sesión sin el daemon; `TestPeriodicSyncWhileDisconnected` fija un intervalo de 15 segundos y consulta hasta ver dos ejecuciones de fetch-all en lugar de dormir. El script construye el binario de pruebas una vez y ejecuta las pruebas en `GITBOX_SHARDS` procesos a la vez (3 por defecto, `1` para un solo proceso), cada uno con su propia red de Docker y un tiempo límite de 30 minutos; falla cuando los fragmentos no cubren la lista completa de pruebas.
- **Pruebas en host real** tras el tag `remote_host`, con el arnés de `pkg/remote/ithost_test.go` (#245). Ejecutan las pruebas que no necesitan un contenedor propio contra un host real, como un Mac con Sesión remota: la batería del arranque, `TestTokenCanary` y `TestRemoteTerminalOverSSH` en el shell de inicio de sesión del host. Las demás pruebas se saltan.

  ```bash
  scripts/remote-host-it.sh                      # against SSH_REMOTE_MAC_HOST from .env
  GITBOX_IT_RUN=TestTokenCanary scripts/remote-host-it.sh my-mac
  ```

  El script pregunta al host su plataforma con `uname -sm`, construye para ella el servidor centinela y la forja falsa, y ejecuta `go test -race -tags remote_host ./pkg/remote/`. Las pruebas escriben y borran archivos en el home de la cuenta remota, así que debe ser una cuenta desechable propia. El arnés se niega a arrancar sin su marcador `~/.gitbox-it-host`, o cuando el alias llega a la cuenta desde la que se ejecutan las pruebas. Resuelve el alias con `ssh -G`, después lee solo su clave y un archivo `known_hosts` (`GITBOX_REMOTE_TEST_KNOWN_HOSTS`, a partir de `SSH_REMOTE_MAC_KNOWN_HOSTS` en `.env`), y escribe un ssh config propio con `IdentityAgent none`. Cada prueba recibe una carpeta propia bajo `~/.gitbox-it/` como su `server_dir`, y la limpieza la borra junto con la config de gitbox, la raíz de clonado y el archivo rc que escribió la prueba. Un host real conserva su red, así que `TestInstallNoInternet` solo comprueba la ruta en el carril de sshd. En un Mac preparo la cuenta una vez:

  ```bash
  sudo sysadminctl -addUser gitbox-it -fullName "Gitbox IT" -shell /bin/zsh -home /Users/gitbox-it -password -
  sudo createhomedir -c -u gitbox-it
  sudo dscl . -create /Users/gitbox-it IsHidden 1
  sudo dseditgroup -o edit -a gitbox-it -t user com.apple.access_ssh
  mkdir -p ~/.cache/gitbox-it && ssh-keygen -q -t ed25519 -N "" -f ~/.cache/gitbox-it/id_ed25519
  printf 'localhost %s\n' "$(cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)" > ~/.cache/gitbox-it/known_hosts
  sudo -H -u gitbox-it sh -c 'umask 077; mkdir -p ~/.ssh; cat > ~/.ssh/authorized_keys; touch ~/.gitbox-it-host' < ~/.cache/gitbox-it/id_ed25519.pub
  ```

  Después, un alias `Host` para ella en `~/.ssh/config`, antes de cualquier bloque `Host *`, y el alias en `.env`:

  ```text
  Host gitbox-it-mac
    HostName localhost
    User gitbox-it
    IdentityFile ~/.cache/gitbox-it/id_ed25519
    IdentityAgent none
  ```

  El gate local no ejecuta este carril: consulta [Gate local](local-gate.md#qué-ejecuta-cada-carril).

Un smoke remoto maneja los métodos del cliente contra un host real desde mi máquina, con una config temporal: añadir, conectar, registro, diagnóstico, desconectar y desinstalar. Necesita un build que lleve su servidor y un `server_dir` de prueba. `GITBOX_SMOKE_OS` nombra el sistema del host (`linux` por defecto), y `./scripts/smoke.sh remote-mac` lo ejecuta contra `SSH_REMOTE_MAC_HOST`:

```bash
eval "$(scripts/stamp.sh)" && scripts/build-server.sh
pkg=github.com/LuisPalacios/gitbox/cmd/gui
GITBOX_SMOKE_ALIAS=my-server GITBOX_SMOKE_SERVER_DIR='~/.cache/gitbox-smoke' \
  go test -tags serverbin,remote_smoke -run TestRemoteSmoke -v \
  -ldflags "-X $pkg.version=$VERSION -X $pkg.commit=$COMMIT" ./cmd/gui/
```

## Git hooks

`.githooks/pre-push` se niega a actualizar `main` salvo que el commit que se sube haya pasado el [gate local](local-gate.md). En otras ramas ejecuta una comprobación `gofmt -s -l`, `go vet ./...` y `go test -short ./...` salvo que el commit ya haya pasado el gate, y construye primero el frontend cuando falta `cmd/gui/frontend/dist`. Los push que solo llevan tags saltan esas comprobaciones.

Ningún commit lleva atribución a una IA: ni un trailer de coautoría que nombre una herramienta de IA, ni un trailer de sesión de un harness, ni un pie "generated with". Un squash merge copia en `main` los trailers de todos los commits de la rama, y GitHub muestra entonces la herramienta como contributor. `.githooks/commit-msg` rechaza un mensaje así, y `.githooks/pre-push` rechaza un push cuyos commits nuevos o mensaje de tag anotado lleven uno, sea cual sea la ref de destino. Ambos usan `.githooks/ai-attribution.sh`, que `/merge-pr` también ejecuta sobre el texto del PR antes de un squash. Los coautores humanos y las simples menciones de una herramienta siguen permitidos. Activo los hooks una vez por clon:

```bash
git config core.hooksPath .githooks
```

## El fixture de pruebas

La prueba de escenario y `GitboxApp --test-mode` leen `test-gitbox.json` en la raíz del repo: un `gitbox.json` normal cuyas cuentas pueden llevar una clave `_test` con un token. Las pruebas unitarias no lo necesitan. Sin él, la prueba de escenario falla con instrucciones para crear el archivo o ejecutar `go test -short`.

### Prepararlo

Copio la plantilla y la relleno con cuentas reales:

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

Para probar el primer arranque, uso un fixture sin cuentas, fuentes ni mirrors y añado `"_test": {"firstRun": true}` en el nivel superior. Conservo una entrada válida en `global.roots` y fijo `global.language` en `en` o `es`. El asistente sugiere la raíz temporal de la prueba.

El archivo está en el gitignore porque contiene secretos reales. No lo commitees nunca.

Cada cuenta a la que las pruebas pueden llamar recibe una clave `_test` con un personal access token:

```json
"gb-github-personal": {
  "provider": "github",
  "_test": {
    "token": "ghp_xxxxxxxxxxxxxxxxxxxx"
  }
}
```

Las cuentas sin `_test` son solo de visualización: aparecen en `--test-mode`, pero no se les hace ninguna llamada API. La prueba de escenario usa la primera source, en orden de clave, que tiene repos y cuya cuenta tiene un token. El token necesita los mismos permisos que una credencial token de gitbox; consulta [credentials.md › Cómo crear un PAT](credentials.md#cómo-crear-un-pat).

### Cuentas GCM

Las credenciales GCM salen de un inicio de sesión interactivo en el navegador, así que no hay token que guardar. No pongo `_test` en las cuentas GCM: la prueba de escenario las salta y siguen disponibles para comprobaciones manuales en `--test-mode`.

### Mirrors (opcional)

Para comprobar mirrors a mano en `--test-mode`, añado una sección `mirrors` con un par de cuentas real:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "gb-github-personal",
    "account_dst": "gb-home-personal",
    "repos": {}
  }
}
```

Ambas cuentas necesitan un token `_test`, y el token de destino necesita acceso de escritura, porque configurar un mirror crea el repo de destino. La prueba de escenario no necesita esta sección: su paso de mirror solo crea y borra un grupo de mirror en la config.

### Verificar los tokens y las claves SSH

Antes de la primera ejecución del escenario, compruebo el fixture:

```bash
./scripts/setup-credentials.sh
```

Verifica cada token `_test` contra su proveedor, genera pares de claves SSH por host, añade sus entradas `Host` a la config SSH del fixture y prueba las conexiones SSH. Es idempotente. Sin argumentos se ejecuta en esta máquina y en cada remoto configurado en `.env`; un destino como `linux` lo limita. Cuando cada línea muestra un `ok` verde, el fixture está listo. Consulta [multiplatform.md](multiplatform.md) para la preparación de los remotos.

### Seguridad

- El harness y `--test-mode` sustituyen `global.roots` por un único directorio temporal, así que cada clon y archivo de config acaba ahí y se borra después.
- `credential_ssh.ssh_folder` sale del fixture, para que las pruebas encuentren las claves SSH generadas. Apúntalo a una carpeta aislada como `~/.gitbox-test/ssh`.
- La ejecución se detiene de inmediato cuando `ssh_folder` es `~/.ssh` o una raíz de `global.roots` es el directorio de config de gitbox (`~/.config/gitbox`).

## Ejecutar las pruebas

Las ejecuto en este orden, de la más rápida a la más lenta.

1. Pruebas unitarias, sin fixture:

   ```bash
   go test -short ./...
   ```

2. Comprobación de tipos y pruebas del frontend:

   ```bash
   cd cmd/gui/frontend
   npm run check
   npm test
   ```

3. La prueba de escenario, contra la primera cuenta del fixture con token:

   ```bash
   go test -v -run TestScenario ./pkg/ops/
   ```

4. Todo, en verbose y sin caché:

   ```bash
   go test -v -p 1 -count=1 ./...
   ```

5. La app contra el fixture en lugar de mi config real:

   ```bash
   GitboxApp --test-mode
   ```

   `--test-mode` busca `test-gitbox.json` subiendo desde el directorio actual, escribe una config descartable en un directorio temporal, sobrescribe `global.roots`, aplica las mismas comprobaciones de seguridad e inyecta los tokens del fixture como variables de entorno. Mi `~/.config/gitbox/` real y mis clones no se tocan, y el directorio temporal desaparece cuando la app sale. `./scripts/test-commands.sh` imprime el comando de lanzamiento para cada plataforma.

### Pruebas de componentes del frontend

Ejecuto `npm run test:dom` desde `cmd/gui/frontend` para lanzar solo las pruebas de componentes. El harness monta el panel real con Svelte Testing Library y usa un puente de Wails y un bus de eventos programables. Los fixtures usan repositorios, cuentas y tokens sintéticos.

La suite comprueba Requiere atención, el orden de los eventos de arranque, una finalización tardía después de dejar de seguir un proyecto, el borrado del token del campo de entrada y la restauración del foco, y la conservación de la vista, la selección de detalles y el desplazamiento al cambiar de experiencia. A 600 px de ancho de contenido, estima el ancho íntegro de las etiquetas de las filas de Simple en inglés y español con una fuente monoespaciada de referencia, usando los espacios del CSS y los controles de cada fila. Mantengo la prueba smoke de Wails empaquetado en cada OS y uso su webview nativa para comprobar el renderizado real y el foco.

### Ejecuciones de regresión enfocadas

- **API del renderer** — `go test ./cmd/gui/ -run TestBoundSurface` comprueba el conjunto de métodos documentado frente a `*App` y los bindings JavaScript y TypeScript generados.
- **Instantánea de arranque** — `go test ./cmd/gui/ -run TestStartupSnapshot` comprueba la anidación antes de que existan los clones y los datos de instantánea independientes. `go test ./cmd/gui/ -run '^$' -bench BenchmarkStartupSnapshot -benchmem` mide la construcción de la instantánea, que es trabajo del backend, no tiempo de arranque visible.
- **Refresco y mantenimiento de credenciales** — `go test -race ./cmd/gui/ ./pkg/credential/ ./pkg/heal/ ./pkg/git/` cubre la coordinación del refresco, la cancelación, los resultados obsoletos, los sondeos upstream y el mantenimiento previo al fetch.
- **Secuencias de fallos** — `go test -race -short ./cmd/gui/ ./pkg/config/ ./pkg/git/...` comprueba cambios simultáneos de configuración, fallos parciales al guardar y bloqueos de helpers de credenciales de Git con fixtures aislados. La comprobación de salud ejecuta esta pasada con el detector de carreras en macOS y Linux.
- **Exportación de diagnóstico** — `go test -short ./pkg/diag/ ./cmd/gui/ -run "Test(Collect|Activity|Redact|ExportDiagnostics|Diagnostics_)"` comprueba la lista permitida, los valores de control de privacidad, la retención y el guardado de los bytes revisados. En el escritorio nativo uso un fixture sintético, abro **Ajustes → Comprobación del sistema → Ejecutar → Exportar diagnóstico**, guardo el archivo en una carpeta temporal y lo comparo byte a byte con la vista previa.

Para el tiempo de arranque visible mido en el escritorio nativo: del lanzamiento del proceso a la ventana, de la ventana a las primeras filas configuradas con la anidación de contenedores, y el tiempo hasta completar el estado. Registro la plataforma, el número de clones, la disposición de contenedores y el número de ejecuciones. Una navegación del navegador contra un backend `wails dev` en ejecución mide un camino distinto, y los tiempos por SSH no pueden mostrar el pintado de filas de WebView2, así que un objetivo de arranque en Windows sigue abierto hasta que lo mido en un escritorio Windows interactivo.

### Disposición del diálogo de descubrimiento

Compruebo **Buscar proyectos** en WebKit nativo con cuentas y repositorios sintéticos. Cubro los estados de carga, error, lista vacía, un resultado y lista larga, con resultados que llegan tanto durante como después de la transición de apertura. Reabro el diálogo, filtro la lista, alterno entre ventanas bajas y altas, y compruebo el acceso por teclado, la selección y **Añadir y Pull**. Mantengo las llamadas al proveedor y a la clonación aisladas de las credenciales y los repositorios de producción.

Con espacio suficiente en la ventana, espero que el contador, el filtro, **Seleccionar todo** y un repositorio quepan sin desplazamiento. Las listas largas deben permitir el desplazamiento mientras la cabecera y los botones de acción siguen accesibles. Cuando cambio los estilos compartidos de los modales, también compruebo los diálogos de cuenta, credenciales, confirmación y descubrimiento de mirrors.

Dejo que terminen la transición de apertura y la actualización de resultados antes de leer las dimensiones de los elementos. Las lecturas intermedias del layout pueden forzar a WebKit a recalcular las dimensiones y ocultar la regresión de #221. Comparo `clientHeight` y `scrollHeight` del cuerpo, inspecciono el resultado renderizado y compruebo los límites de la fila y del pie; buscar texto CSS no prueba el layout.

### Almacenamiento de credenciales y auditoría de permisos

Ejecuto las comprobaciones de permisos contra un `XDG_CONFIG_HOME` temporal con cuentas sintéticas y archivos de credenciales ficticios. Nunca relajo los permisos de credenciales de producción ni copio archivos de credenciales reales en un fixture de exposición.

- Abro **Ajustes → Comprobación del sistema → Ejecutar** y confirmo que los hallazgos de credenciales muestran la ruta afectada, el problema y la acción sugerida sin el contenido del token. Un fixture correcto pasa; un estado de permisos ilegible o no compatible informa de un hallazgo en lugar de un resultado correcto.
- Solo en el fixture, doy a un archivo ficticio el modo `0644` en Unix o una concesión de ACL Windows fuera de propietario/SYSTEM/Administrators. Tras reiniciar, el aviso de exposición aparece sobre el panel y abre Comprobación del sistema. La auditoría deja los permisos intactos. Después de reparar los permisos del fixture, **Volver a comprobar** elimina el hallazgo y el aviso.
- Guardo y renombro una cuenta dedicada de pruebas y después confirmo que su credencial sigue funcionando tras reiniciar. Las comprobaciones automatizadas aisladas verifican que ambas acciones escriben directamente el formato git-credential-store, incluso en cuentas sin clones, y siguen leyendo archivos antiguos con tokens en bruto. Nunca imprimo un token real ni el contenido de archivos de credenciales en la salida de las pruebas.

### Prueba de identidad del clon

En macOS o Linux ejecuto `scripts/demo-fleet/identity-proof.sh` desde la raíz del repositorio para generar diez clones aislados. Ejecuto el binario compilado desde el directorio del fixture que imprime el script, con su propio directorio personal y ajustes de Git:

```bash
binary=/absolute/path/to/GitboxApp
fixture=$(scripts/demo-fleet/identity-proof.sh)
(cd "$fixture" && env HOME="$fixture/home" XDG_CONFIG_HOME="$fixture/xdg"   GIT_CONFIG_GLOBAL="$fixture/global.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_COUNT=0   "$binary" --test-mode)
```

Abro **¿Por qué esta cuenta?** para un clon de referencia y espero un aviso porque los servidores del fixture están desconectados. Compruebo que **wrong-host**, **include-helper** y **scoped-helper** muestran **Inseguro**, y que **other-host-helper** y **scoped-reset** muestran un aviso sin conflicto entre gestores. Comparo los bytes de la configuración del clon antes y después de abrir el panel y compruebo que los secretos sintéticos siguen ocultos. Uso **Reparar** en **wrong-host** y compruebo que la prueba muestra la URL de descarga prevista. En Simple espero una frase sobre la cuenta.

## Pruebas nativas

El [gate local](local-gate.md) ejecuta las pruebas nativas en cada cambio: su carril de macOS ejecuta la comprobación de salud completa y las suites de coordinación, su carril de Linux ejecuta `go vet` y las pruebas cortas, y su carril de Windows ejecuta las suites de coordinación y las pruebas cortas. Cada carril también construye la GUI y ejecuta el smoke con `--version`. El carril de Windows fija `core.autocrlf=false` y `core.longpaths=true` solo para sus propios procesos, así que mi configuración global de Git no cambia.

El workflow de release repite vet, las pruebas cortas y las dos suites de coordinación en runners alojados de Linux, macOS y Windows antes de construir la release. `pr.yml` mantiene solo el job informativo `check` de Linux.

Ni el gate ni los jobs de release verifican el renderizado del escritorio ni el arranque del paquete. Mantengo el smoke test de arranque del paquete en `scripts/smoke.sh` y pruebo el comportamiento interactivo de la GUI en una sesión de escritorio nativa.

## Checklist pre-PR

Ejecuto esto antes de cada push o PR. `/test-plan` en Claude Code o `$test-plan` en Codex lo automatiza.

- [ ] `./scripts/gate.sh` pasa sobre el commit final en macOS, Linux y Windows (consulta [Gate local](local-gate.md)); cubre los cuatro puntos siguientes en todas las plataformas
- [ ] `./scripts/health.sh` pasa: incluye `go vet`, `go test -short`, `svelte-check` y `npm test` (consulta [Comprobación de salud](developer-guide.md#comprobación-de-salud))
- [ ] Las pruebas enfocadas del área cambiada pasan (`go test ./pkg/<area>/ ./cmd/gui/`)
- [ ] `cd cmd/gui && wails build` termina bien después de copiar los iconos (consulta [Compilar desde el código fuente](developer-guide.md#compilar-desde-el-código-fuente))
- [ ] `cmd/gui/build/bin/GitboxApp --version` imprime `GitboxApp <version> (<sha>)`
- [ ] `./scripts/ship.sh` y `./scripts/smoke.sh` pasan en cada remoto de `.env` (se omite cuando `.env` no tiene ninguno)

Cuando cambio la documentación, también ejecuto `./scripts/health.sh docs-parity` por separado para comprobar la estructura en inglés y español. Cuando cambio el propio comprobador, ejecuto `uv run scripts/test_docs_parity.py` para verificar sus diagnósticos con fixtures aislados.

Cuando el cambio toca una de estas áreas, también lo compruebo en la máquina de desarrollo:

- [ ] Config: GitboxApp carga la config, y el cambio sobrevive a un reinicio
- [ ] Credenciales: las palabras de salud de la cuenta en la barra lateral y en su tarjeta coinciden con el estado real de la credencial
- [ ] Reconcile de disco o contenedores: un clon puesto bajo una raíz se adopta o aparece en **Clones sin ubicar** la próxima vez que la ventana recupera el foco
- [ ] Editar otro repositorio durante un escaneo. Tanto su cambio como la nueva adopción se conservan al reiniciar
- [ ] GUI: la pantalla cambiada se renderiza en Simple y en Avanzado, en los temas claro y oscuro
- [ ] Windows: no aparece ninguna ventana de consola al ejecutar la acción cambiada

## Checklist completa de release

Ejecuto esto antes de crear el tag de un release. Combina pasos automatizados e interactivos en todas las plataformas.

### Automatizado

- [ ] `./scripts/health.sh` pasa
- [ ] `go test ./...` pasa con `test-gitbox.json` en su sitio, incluida `TestScenario_FullLifecycle`
- [ ] `./scripts/ship.sh` construye y prepara la GUI en cada remoto
- [ ] `./scripts/smoke.sh all` imprime una versión en cada plataforma
- [ ] `./scripts/sign-release.sh --check` pasa, así que la clave de releases está en el agente SSH
- [ ] `create-dmg` está instalado en el Mac que ejecuta `./scripts/release-macos.sh`

### Verificación GUI (interactiva, todas las plataformas)

Lanzo GitboxApp en cada plataforma con los comandos que imprimen `./scripts/run-commands.sh` (config real) o `./scripts/test-commands.sh` (fixture).

- [ ] La app se abre; en Windows sin console flash
- [ ] Primer arranque: con `XDG_CONFIG_HOME` apuntando a una carpeta vacía, la configuración guiada va de carpeta → conectar (inicio de sesión en el navegador, **Usar un token**, **Cambiar los datos** conserva la carpeta elegida) → elegir proyectos → **Descargar**; la pantalla principal se abre en Simple y los repos elegidos se clonan
- [ ] Añadir cuentas: en Simple, **Conectar una cuenta** ejecuta los mismos pasos de conectar y elegir; en Avanzado, **Añadir cuenta** abre el formulario completo
- [ ] Barra lateral: Requiere atención, Todos los repos, cada cuenta y Mirrors abren su vista; el logo de GitBox y ⌘B / Ctrl+B la pliegan al riel de iconos y la despliegan; por debajo de 780 px se muestra el riel y el logo abre la barra lateral sobre el contenido; la vista y el riel sobreviven a un reinicio
- [ ] Grupos de cuenta: en Todos los repos y Requiere atención cada cuenta está en una tarjeta clara con un triángulo ▾ relleno y su nombre en mayúsculas en el color del proveedor; un clic en la cabecera la pliega, y sus conteos siguen visibles mientras está plegada
- [ ] Tarjeta de cuenta: la tarjeta de la vista de cuenta muestra la marca de salud y el nombre, las palabras de salud con su botón de arreglo, y los conteos con **Buscar** y **+ Crear** en los dos modos; una credencial rota la tiñe de rojo y pone su arreglo al principio de Requiere atención
- [ ] Editar y renombrar la cuenta conserva los clones, la vista y la credencial
- [ ] La configuración de credenciales funciona para token, GCM y SSH
- [ ] Discovery: **Buscar** → seleccionar repos → **Añadir y Pull** los clona en la raíz de la cuenta
- [ ] Clonar, **Pull de todo** y **Fetch de todo** actualizan los puntos de estado; la tira de actividad muestra el alcance mientras se ejecutan, y una tarjeta de resultado los resume
- [ ] En un repo de prueba cuyo checkout deja un archivo sin seguimiento, descargo el repo en Simple y compruebo que la fila pasa de **Comprobando…** a **Cambios en este ordenador**. Repito en inglés.
- [ ] El panel de detalle del repo muestra la rama y ahead/behind en Avanzado, y los archivos cambiados en ambos modos; un clon con actualizaciones entrantes abre una lista con scroll de todos los commits con autor y mensaje, conserva los detalles de archivos locales y se refresca después de actualizar
- [ ] Vista previa entrante: una lista larga cabe en una ventana pequeña; cambiar de clon o cerrar y reabrir mientras carga nunca muestra los resultados de otro clon; abrir solo el panel no ejecuta fetch ni pull
- [ ] En Simple abro la vista previa de **Actualizar todo**, cancelo, vuelvo a abrir el plan, edito un clon que figura para actualizar y confirmo. El resultado cuenta el proyecto omitido y conserva mis cambios. En Avanzado activo el plan y compruebo que lista los repositorios y sus acciones.
- [ ] Raíces de clonado: en Ajustes añado una raíz, la hago predeterminada y la quito; con dos raíces, el **Clonar** de una fila pregunta qué raíz y acaba ahí, y **Pull de todo** no pregunta
- [ ] Reconcile de disco: un clon reconocido puesto bajo una raíz se adopta en su sitio la próxima vez que la ventana recupera el foco; uno no reconocido aparece en **Clones sin ubicar** bajo **Encontrados en disco**, donde **Adoptar**, **Ignorar**, **Mostrar ignorados** y **Dejar de ignorar** funcionan; un clon movido se vuelve a enlazar; los repos bajo una raíz desconectada muestran **Raíz no disponible**
- [ ] En un clon de prueba pendiente de adopción que necesita reparar su identidad, crea un archivo vacío `.git/config.lock`. Tras la adopción, verifica que **Requiere atención** muestra el aviso y que el clon sigue en la cuenta. Retira el archivo de bloqueo de prueba y pulsa **Reintentar reparación**. Comprueba que desaparece el aviso y que tus archivos de trabajo conservan su contenido.
- [ ] Contenedores: un clon con clones anidados muestra un chevron ▾ con contorno en lugar de su punto de estado; el chevron pliega todo lo que hay debajo, el nombre abre los detalles disponibles o, en caso contrario, pliega los hijos, y los pliegues sobreviven a un reinicio
- [ ] Workspaces: un `.code-workspace` puesto en la raíz de un clon lo convierte en contenedor la próxima vez que la ventana recupera el foco, con sus clones anidados debajo y las carpetas listadas que faltan como filas atenuadas **sin clonar**; un segundo archivo pregunta cuál usar; el botón de editor abre el archivo de workspace en VS Code
- [ ] Simple / Avanzado: el interruptor de la barra superior (Simple en gris, Avanzado en rojo) cambia de modo al instante; las filas en Simple muestran el punto de estado, el nombre, la frase de estado, **Actualizar** o **Descargar** cuando hace falta, los iconos de IA, editor y terminal y ⋯; las filas en Avanzado mantienen todo el detalle y el botón de fetch por fila; Simple oculta Mirrors, el modo de borrado y el botón de tema. Comprueba el renderizado nativo con contenido estrecho y ancho, en los temas claro y oscuro.
- [ ] Los anillos de la barra superior muestran un icono, una insignia de atención y un tooltip al pasar el ratón, y un clic en un anillo pendiente abre Requiere atención (el anillo de mirrors abre Mirrors)
- [ ] La vista Mirrors muestra sus grupos con **Descubrir** y **Comprobar todo** en la cabecera; un chip de mirror se abre en su tarjeta y vuelve a plegarse con un clic fuera o Escape
- [ ] Ajustes: Comprobación del sistema y el gestor de terminales se abren; el tema sigue al OS, claro u oscuro, y sobrevive a un reinicio

### Flujos de credenciales (interactivos, por plataforma)

- [ ] Windows: token, GCM (navegador), SSH (generación de claves)
- [ ] macOS: token, GCM (navegador), SSH
- [ ] Linux: token, GCM (navegador en una sesión de escritorio), SSH

### Actualización y upgrade (al menos una plataforma)

- [ ] Con un release firmado más nuevo publicado, la píldora de actualización aparece en el pie; un clic en ella verifica la firma y el checksum, aplica la actualización, y la app se reinicia en la versión nueva
- [ ] En Windows, actualizo una instalación de prueba en `Program Files` desde un release de prueba firmado. El aviso de control de cuentas de usuario (UAC) identifica a `GitboxApp.exe`, se abre la nueva versión con los permisos de mi usuario y la actualización no crea archivos `.cmd`
- [ ] La píldora del AppImage de Linux abre la página del release en lugar de actualizar
- [ ] Una instalación v1 se actualiza a v2 y conserva su `gitbox.json`

Los caminos que fallan cerrados (firma ausente o inválida, checksum ausente) los cubren las pruebas de `pkg/update`, no se comprueban a mano.

### Notas específicas por plataforma

| Área | Windows | macOS | Linux |
| --- | --- | --- | --- |
| Store GCM | Windows Credential Manager | macOS Keychain | `secretservice` (predeterminado) o `gpg` |
| SSH agent | OpenSSH agent o Pageant | System ssh-agent | System ssh-agent |
| Binario Git | `git.exe` (Git for Windows) | `/usr/bin/git` o Homebrew | `git` del sistema |
| Abrir navegador (GCM) | Funciona siempre | Funciona incluso vía SSH | Necesita `DISPLAY` o `WAYLAND_DISPLAY` |
| Framework GUI | Wails + WebView2 | Wails + WebKit | Wails + WebKitGTK |
| Ruta de config | `~/.config/gitbox/` | `~/.config/gitbox/` | `~/.config/gitbox/` |

`XDG_CONFIG_HOME` sobrescribe la ruta de config en todas las plataformas.

## Añadir checks nuevos

Cuando añado una feature, actualizo los docs de pruebas:

1. Añadir la prueba automatizada y listarla en el [inventario de pruebas](testing-reference.md#inventario-de-pruebas).
2. Añadir un paso manual a la sección de checklist correspondiente de arriba.
3. Si la feature se comporta distinto según la plataforma, añadir una fila o una nota a la tabla de plataformas.

## Comprobaciones del harness de agentes

Verifico los cambios limitados a las instrucciones de agentes y los scripts de mantenimiento con `./scripts/health.sh harness markdown shellcheck actionlint`. El gate del harness ejecuta las suites de regresión del renderizador, del formateador Markdown, del registro de coordinación y de la edición del roadmap, y después compara ambos árboles generados con `ai/`. Estos cambios no necesitan pruebas de la aplicación ni compilar la GUI. Aun así, ejecuto la comprobación de salud completa antes de una PR y verifico el descubrimiento en una sesión nueva de cada herramienta.
