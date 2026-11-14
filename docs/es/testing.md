# Pruebas

Esta guía cubre los niveles de prueba, el fixture `test-gitbox.json` y las checklists que ejecuto antes de un PR y antes de un release. El inventario de pruebas y los detalles internos del harness viven en [testing-reference.md](testing-reference.md). La comprobación de salud se describe en [developer-guide.md › Comprobación de salud](developer-guide.md#comprobación-de-salud).

## Niveles de prueba

- **Pruebas unitarias de paquete** — las pruebas Go bajo `pkg/`. No necesitan preparación, ni red, ni proveedor, y lo aíslan todo con `t.TempDir()` y `t.Setenv()`. Las pruebas unitarias de `pkg/ops` se ejecutan contra un `XDG_CONFIG_HOME` temporal, un `GIT_CONFIG_GLOBAL` temporal y una carpeta SSH temporal, así que nada cambia en el host.
- **Pruebas de GUI** — las pruebas Go bajo `cmd/gui/`. Llaman directamente a los métodos de `App`, sin ventana de Wails. Necesitan el frontend compilado en `cmd/gui/frontend/dist`, porque el paquete lo embebe.
- **Pruebas del frontend** — pruebas puras de Node en `cmd/gui/frontend/src/lib/*.test.mjs`, que se ejecutan con `npm test`. No necesitan ni Wails ni credenciales.
- **Prueba de escenario** — `TestScenario_FullLifecycle` en `pkg/ops/scenario_test.go` recorre el ciclo de vida completo de una cuenta a través de `pkg/ops` contra un proveedor real. Necesita el fixture descrito más abajo, y `go test -short` la salta.

`go test ./pkg/...` se ejecuta sin el build del frontend. Todo lo demás en Go necesita `cmd/gui/frontend/dist`; constrúyelo una vez:

```bash
(cd cmd/gui/frontend && npm ci && npm run build)
```

`wails build` también lo crea. Consulta [developer-guide.md › Compilar desde el código fuente](developer-guide.md#compilar-desde-el-código-fuente).

## Pre-push hook

`.githooks/pre-push` ejecuta una comprobación `gofmt -s -l`, `go vet ./...` y `go test -short ./...` antes de cada push, y construye primero el frontend cuando falta `cmd/gui/frontend/dist`. Los push que solo llevan tags lo saltan. Lo activo una vez por clon:

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

### Ejecuciones de regresión enfocadas

- **Instantánea de arranque** — `go test ./cmd/gui/ -run TestStartupSnapshot` comprueba la anidación antes de que existan los clones y los datos de instantánea independientes. `go test ./cmd/gui/ -run '^$' -bench BenchmarkStartupSnapshot -benchmem` mide la construcción de la instantánea, que es trabajo del backend, no tiempo de arranque visible.
- **Refresco y mantenimiento de credenciales** — `go test -race ./cmd/gui/ ./pkg/credential/ ./pkg/heal/ ./pkg/git/` cubre la coordinación del refresco, la cancelación, los resultados obsoletos, los sondeos upstream y el mantenimiento previo al fetch.

Para el tiempo de arranque visible mido en el escritorio nativo: del lanzamiento del proceso a la ventana, de la ventana a las primeras filas configuradas con la anidación de contenedores, y el tiempo hasta completar el estado. Registro la plataforma, el número de clones, la disposición de contenedores y el número de ejecuciones. Una navegación del navegador contra un backend `wails dev` en ejecución mide un camino distinto, y los tiempos por SSH no pueden mostrar el pintado de filas de WebView2, así que un objetivo de arranque en Windows sigue abierto hasta que lo mido en un escritorio Windows interactivo.

### Disposición del diálogo de descubrimiento

Compruebo **Buscar proyectos** en WebKit nativo con cuentas y repositorios sintéticos. Cubro los estados de carga, error, lista vacía, un resultado y lista larga, con resultados que llegan tanto durante como después de la transición de apertura. Reabro el diálogo, filtro la lista, alterno entre ventanas bajas y altas, y compruebo el acceso por teclado, la selección y **Añadir y Pull**. Mantengo las llamadas al proveedor y a la clonación aisladas de las credenciales y los repositorios de producción.

Con espacio suficiente en la ventana, espero que el contador, el filtro, **Seleccionar todo** y un repositorio quepan sin desplazamiento. Las listas largas deben permitir el desplazamiento mientras la cabecera y los botones de acción siguen accesibles. Cuando cambio los estilos compartidos de los modales, también compruebo los diálogos de cuenta, credenciales, confirmación y descubrimiento de mirrors.

Dejo que terminen la transición de apertura y la actualización de resultados antes de leer las dimensiones de los elementos. Las lecturas intermedias del layout pueden forzar a WebKit a recalcular las dimensiones y ocultar la regresión de #221. Comparo `clientHeight` y `scrollHeight` del cuerpo, inspecciono el resultado renderizado y compruebo los límites de la fila y del pie; buscar texto CSS no prueba el layout.

## Checklist pre-PR

Ejecuto esto antes de cada push o PR. `/test-plan` en Claude Code o `$test-plan` en Codex lo automatiza.

- [ ] `./scripts/health.sh` pasa: incluye `go vet`, `go test -short`, `svelte-check` y `npm test` (consulta [Comprobación de salud](developer-guide.md#comprobación-de-salud))
- [ ] Las pruebas enfocadas del área cambiada pasan (`go test ./pkg/<area>/ ./cmd/gui/`)
- [ ] `cd cmd/gui && wails build` termina bien después de copiar los iconos (consulta [Compilar desde el código fuente](developer-guide.md#compilar-desde-el-código-fuente))
- [ ] `cmd/gui/build/bin/GitboxApp --version` imprime `GitboxApp <version> (<sha>)`
- [ ] `./scripts/ship.sh` y `./scripts/smoke.sh` pasan en cada remoto de `.env` (se omite cuando `.env` no tiene ninguno)

Cuando el cambio toca una de estas áreas, también lo compruebo en la máquina de desarrollo:

- [ ] Config: GitboxApp carga la config, y el cambio sobrevive a un reinicio
- [ ] Credenciales: las palabras de salud de la cuenta en la barra lateral y en su tarjeta coinciden con el estado real de la credencial
- [ ] Reconcile de disco o contenedores: un clon puesto bajo una raíz se adopta o aparece en **Clones sin ubicar** la próxima vez que la ventana recupera el foco
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
- [ ] El panel de detalle del repo muestra la rama y ahead/behind en Avanzado, y los archivos cambiados en ambos modos; un clon con actualizaciones entrantes abre una lista con scroll de todos los commits con autor y mensaje, conserva los detalles de archivos locales y se refresca después de actualizar
- [ ] Vista previa entrante: una lista larga cabe en una ventana pequeña; cambiar de clon o cerrar y reabrir mientras carga nunca muestra los resultados de otro clon; abrir solo el panel no ejecuta fetch ni pull
- [ ] Raíces de clonado: en Ajustes añado una raíz, la hago predeterminada y la quito; con dos raíces, el **Clonar** de una fila pregunta qué raíz y acaba ahí, y **Pull de todo** no pregunta
- [ ] Reconcile de disco: un clon reconocido puesto bajo una raíz se adopta en su sitio la próxima vez que la ventana recupera el foco; uno no reconocido aparece en **Clones sin ubicar** bajo **Encontrados en disco**, donde **Adoptar**, **Ignorar**, **Mostrar ignorados** y **Dejar de ignorar** funcionan; un clon movido se vuelve a enlazar; los repos bajo una raíz desconectada muestran **Raíz no disponible**
- [ ] Contenedores: un clon con clones anidados muestra un chevron ▾ con contorno en lugar de su punto de estado; el chevron pliega todo lo que hay debajo, el nombre abre los detalles disponibles o, en caso contrario, pliega los hijos, y los pliegues sobreviven a un reinicio
- [ ] Workspaces: un `.code-workspace` puesto en la raíz de un clon lo convierte en contenedor la próxima vez que la ventana recupera el foco, con sus clones anidados debajo y las carpetas listadas que faltan como filas atenuadas **sin clonar**; un segundo archivo pregunta cuál usar; el botón de editor abre el archivo de workspace en VS Code
- [ ] Simple / Avanzado: el interruptor de la barra superior (Simple en gris, Avanzado en rojo) cambia de modo al instante; las filas en Simple muestran el punto de estado, el nombre, la frase de estado, **Actualizar** o **Descargar** cuando hace falta, los iconos de IA, editor y terminal y ⋯; las filas en Avanzado mantienen todo el detalle y el botón de fetch por fila; Simple oculta Mirrors, el modo de borrado y la vista compacta
- [ ] Los anillos de la barra superior muestran un icono, una insignia de atención y un tooltip al pasar el ratón, y un clic en un anillo pendiente abre Requiere atención (el anillo de mirrors abre Mirrors)
- [ ] La vista Mirrors muestra sus grupos con **Descubrir** y **Comprobar todo** en la cabecera; un chip de mirror se abre en su tarjeta y vuelve a plegarse con un clic fuera o Escape
- [ ] Ajustes: Comprobación del sistema y el gestor de terminales se abren; el tema sigue al OS, claro u oscuro, y sobrevive a un reinicio
- [ ] Vista compacta y vuelta a la vista completa

### Flujos de credenciales (interactivos, por plataforma)

- [ ] Windows: token, GCM (navegador), SSH (generación de claves)
- [ ] macOS: token, GCM (navegador), SSH
- [ ] Linux: token, GCM (navegador en una sesión de escritorio), SSH

### Actualización y upgrade (al menos una plataforma)

- [ ] Con un release firmado más nuevo publicado, la píldora de actualización aparece en el pie; un clic en ella verifica la firma y el checksum, aplica la actualización, y la app se reinicia en la versión nueva
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

Verifico los cambios limitados a las instrucciones de agentes y los scripts de mantenimiento con `./scripts/health.sh harness markdown shellcheck actionlint`. El gate del harness ejecuta las suites de regresión del renderizador y del formateador Markdown, y después compara ambos árboles generados con `ai/`. Estos cambios no necesitan pruebas de la aplicación ni compilar la GUI. Aun así, ejecuto la comprobación de salud completa antes de una PR y verifico el descubrimiento en una sesión nueva de cada herramienta.
