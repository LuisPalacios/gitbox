# Referencia de pruebas

El inventario de pruebas y cómo funciona el harness de pruebas. Para los niveles de prueba, la preparación del fixture y las checklists, consulta [testing.md](testing.md).

## Inventario de pruebas

Los recuentos son funciones de prueba Go de nivel superior (no se cuentan los subtests de `t.Run` ni `TestMain`) y las llamadas `test(...)` de nivel superior en las suites del frontend. Actualizo los recuentos de Go con:

```bash
rg -c '^func Test\w*\(t \*testing\.T\)' -g '*_test.go' pkg cmd
```

El total actual es de 593 pruebas Go (516 en `pkg/`, 77 en `cmd/gui/`) y 42 pruebas del frontend. Los archivos solo de Windows (`*_windows_test.go`) se incluyen en los recuentos, pero solo se ejecutan en Windows.

### Biblioteca compartida (`pkg/`)

- **`pkg/adopt`** (16) — descubrimiento de huérfanos y puntuación de cuentas.
  - `adopt_test.go`: alias de host SSH, descubrimiento en su sitio, asociación de cuentas por el usuario embebido en la URL, el username de la credencial y la carpeta padre, empates ambiguos.
  - `nested_test.go`: clones anidados bajo un contenedor, raíces anidadas extra y explícitas, adopción en su sitio bajo una segunda raíz de clonado, confianza que necesita algo más que el host.
- **`pkg/config`** (112) — el archivo de config.
  - `config_test.go`, `load_test.go`: parseo, validación, versiones incorrectas, mirrors, idas y vueltas, `LoadWithRepair` para mirrors y sources colgantes, valores por defecto del gitignore global y de los indicadores de PR.
  - `migrate_v3_test.go`, `roots_test.go`, `migrate_v5_test.go`, `migrate_terminals_test.go`: migración v2 → v3 → v4 → v5 (v4 convierte `global.folder` en `global.roots`, v5 descarta la sección `workspaces`), el valor por defecto de la profundidad de escaneo anidado, listas de terminales legacy.
  - `roots_test.go`: raíces de clonado (añadir, hacer predeterminada, quitar y olvidar, ubicar un repo) con cada ruta de clon fijada.
  - `crud_test.go`, `clones_test.go`, `ai_harness_test.go`: CRUD de cuentas, sources, repos y mirrors, clones ignorados, la copia profunda que usan los escaneos sin bloqueo, entradas de AI harness.
  - `collapsed_test.go`, `save_test.go`: poda del estado de layout (pliegues, barra lateral, la vista de una cuenta borrada, la clave de vista `workspaces` retirada), el ring de backups, ningún backup en los guardados solo de layout y solo de tema, y un backup que siempre se conserva para un archivo escrito por una versión anterior de la config.
- **`pkg/credential`** (36) — resolución de tokens desde `GITBOX_TOKEN_<KEY>` y `GIT_TOKEN`, comprobaciones de token con caducidad y tokens rechazados, helper y store de GCM por defecto del OS, salud del gitconfig global para GCM (`CheckGlobalGCMConfig`, `FixGlobalGCMConfig`), búsqueda de claves SSH y de config, cancelación con un helper bloqueado, config de credenciales por repo con comparación previa, y escrituras atómicas de archivos de credenciales con lectores concurrentes.
- **`pkg/doctor`** (34) — la tabla de herramientas y las pistas de instalación, búsqueda de herramientas (carpetas extra, `~/.local/bin`, rutas guardadas, shims de Windows), el sondeo de GCM (preguntar a git, carpetas de instalación, ausente), comprobaciones previas por tipo de credencial, decodificación de la salida UTF-16 de las herramientas. `wsl_windows_test.go` (solo Windows) lee las distros de WSL del registro y demuestra que el stub de instalación de WSL nunca se ejecuta.
- **`pkg/git`** (34) — operaciones git mediante subprocess (status, clone, ramas, barridos de ramas), URLs de remoto y de perfil, descubrimiento de repos y de repos anidados, conexiones rechazadas en milisegundos, instantáneas de config con includes, claves multivalor y worktrees enlazados.
- **`pkg/gitignore`** (23) — el bloque gestionado del gitignore global: ida y vuelta, fusión con contenido del usuario, instalación idempotente, un `core.excludesfile` existente, limpieza de duplicados, backups.
- **`pkg/harness`** (28) — el directorio de AI harness embebido (parseo, herramientas retiradas), la sincronización de harnesses (detección, herramientas ausentes y reinstaladas, entradas del usuario, orden), parseo del `launch_menu` de WezTerm.
- **`pkg/heal`** (11) — URL de origin esperada por tipo de credencial, ocultación de URLs, reparación de identidad, eliminación de tokens embebidos, dos lecturas y ninguna escritura para un clon sin cambios, identidad sobrescrita, worktrees enlazados.
- **`pkg/i18n`** (1) — normalización de idioma.
- **`pkg/identity`** (4) — `ResolveIdentity` con fallback a la cuenta y overrides por repo, `CheckGlobalIdentity`.
- **`pkg/launch`** (13) — expansión de argv, quoting de shell POSIX, envoltura de AI harness por shell (PowerShell, cmd, POSIX), AppleScript de macOS.
- **`pkg/mirror`** (6) — parseo de URLs remotas, descubrimiento de mirrors, clasificación de errores de estado.
- **`pkg/move`** (5) — parseo de claves de repo, URLs de clone HTTPS, validación previa.
- **`pkg/ops`** (18) — 17 pruebas unitarias aisladas (añadir, descartar, renombrar y borrar cuentas, también entre dos raíces de clonado; sugerir una clave de cuenta; cambiar y borrar credenciales; borrar un repo; planificar un clone; reconfigurar clones; añadir repos descubiertos) más `TestScenario_FullLifecycle`.
- **`pkg/provider`** (52) — el cliente HTTP, el parseo de la API de cada proveedor (repos, paginación, crear, borrar, mirrors, listas de PR) y la caducidad del token (formatos de cabecera de GitHub sin petición extra, `personal_access_tokens/self` de GitLab, una caducidad desconocida sigue pasando).
- **`pkg/reconcile`** (8) — clones seguros adoptados en su sitio, los indecisos dejados con su motivo, un clon movido vuelto a enlazar, un clon borrado conservado, una raíz no disponible dejada en paz, clones ignorados omitidos, adopción en una cuenta elegida, worktrees enlazados nunca adoptados.
- **`pkg/status`** (18) — estado del clon (limpio, con cambios, por delante, por detrás, sin clonar, detached), detección de rama, raíz no disponible, rutas de repo a través de las raíces de source y de repo, anidamiento (incluido un contenedor dentro de otro).
- **`pkg/terminals`** (57) — forma del catálogo, composición de perfiles según el OS, búsquedas de WezTerm y Windows Terminal, resolución del lanzamiento, reglas de fusión y sincronización, distros de WSL detectadas sin ejecutar `wsl.exe`.
- **`pkg/update`** (28) — parseo y comparación semver, la comprobación de actualización (una actualización pendiente se informa en cada comprobación, el límite de versión major), nombres de artefactos, detección del AppImage solo-aviso, destinos de instalación, relanzamiento y espera al proceso antiguo (más el script elevado de Windows, solo en Windows), verificación de checksum, firmas de release comprobadas contra fixtures hechos con `ssh-keygen` real (otro tag, checksums manipulados, namespace incorrecto, clave ajena, malformada), la clave embebida que coincide con `allowed_signers`, y descargas que fallan cerradas.
- **`pkg/workspace`** (12) — archivos `.code-workspace` en la raíz de un clon, un archivo que crea un contenedor, varios que preguntan una vez y conservan la elección, un archivo preferido que vuelve a ganar cuando regresa, un archivo desaparecido que borra la elección, placeholders para las carpetas listadas que faltan dentro de un contenedor (nunca dentro de otro clon), los clones que lista un workspace, JSON con comentarios.

### Bindings de la GUI (`cmd/gui/`)

- `account_actions_test.go`, `browser_actions_test.go` (4) — resolución de la carpeta de cuenta, URLs de proveedor, rutas de error para cuentas y repos desconocidos.
- `roots_test.go` (2) — añadir, hacer predeterminada y quitar raíces de clonado; carpetas de cuenta a través de la raíz de la source.
- `reconcile_test.go` (3) — un clon seguro adoptado y guardado, un reintento cuando la config cambia durante el escaneo, adopción en una cuenta elegida, ignorar un clon y adoptarlo desde la lista de ignorados.
- `containers_test.go` (8) — persistencia del flag de contenedor, profundidad de escaneo anidado, `clone_folder` absoluto para los clones incorporados, persistencia y poda del estado de layout (pliegues, barra lateral, vista), un archivo de workspace que crea un contenedor que adopta su clon anidado y lista sus carpetas que faltan en una sola pasada, elegir entre varios archivos de workspace.
- `uimode_test.go` (5) — `ui_mode` es Advanced por defecto, validación de set y get, el onboarding escribe Simple solo en una instalación nueva, `SetTheme` valida y persiste el tema.
- `harness_test.go` (15) — detección de AI harness, orden, deduplicación, poda de harnesses retirados, herramientas ausentes y reinstaladas, el fallback a `~/.local/bin`, el perfil por defecto del lanzador, rutas de error.
- `terminal_test.go`, `startup_sync_test.go` (3) — saneado de rutas MSYS y del entorno para las terminales lanzadas, un primer arranque que nunca escribe la lista legacy `global.terminals`.
- `gcm_setup_test.go` (4) — el inicio de sesión de GCM sobrevive a un stderr inutilizable, fuerza las ventanas propias de GCM por encima de los ajustes por usuario `GCM_GUI_PROMPT` y `GCM_INTERACTIVE`, agota el tiempo con un mensaje que nombra esas variables, e informa del stderr del helper.
- `refresh_test.go` (20) — ciclos de refresco compartidos, límites de workers, rotación de tokens, config obsoleta, apagado, fallos de mantenimiento, reintentos con back-off de los rechazos de conexión instantáneos, la caducidad del token que se pasa al frontend.
- `probe_test.go` (10) — sondeos upstream: concurrencia limitada, vuelos compartidos y throttling, cancelación, un plazo compartido para la búsqueda de credenciales, config obsoleta y tokens rotados, apagado.
- `startup_test.go` (2 + `BenchmarkStartupSnapshot`) — anidamiento antes de que existan los clones, una instantánea independiente de la config viva.
- `update_test.go` (1) — el build AppImage rechaza `ApplyUpdate` antes de cualquier descarga.

### Frontend (`cmd/gui/frontend/src/lib/`)

`npm test` en `cmd/gui/frontend` las ejecuta con `node --test`; `./scripts/health.sh frontend-test` ejecuta la misma suite.

- `repo-tree.test.mjs` (8) — el árbol de clones: propiedad entre sources, ninguna fila duplicada, plegado de anidados, recuperación ante padres obsoletos y ciclos, orden alfabético, grupos de atención separados por cuenta, las filas placeholder de un contenedor.
- `status.test.mjs` (12) — la forma, el color, el spinner, el tipo Simple, los indicadores de conteo, la regla del panel de detalle, las reglas de credenciales, el rango de atención y la etiqueta del glosario de cada estado; la salud de la cuenta por modo; el aviso de caducidad del token.
- `views.test.mjs` (5) — resolución de la vista guardada, vistas de cuenta, la lista de Requiere atención y sus conteos.
- `activity.test.mjs` (7) — alcances y resúmenes de Fetch de todo y Pull de todo, fallos de conexión agrupados por host.
- `notices.test.mjs` (4) — tarjetas de resultado: mostrarlas, descartarlas, cerrarlas automáticamente y limitarlas.
- `theme.test.mjs` (2) — colores de proveedor de los nombres de cuenta en los dos temas, con el color de texto para un proveedor desconocido.
- `i18n.test.mjs` (4) — claves y placeholders coincidentes en inglés y español, ninguna clave inexistente, ningún texto fijo en las plantillas, el marcado en línea de `src/lib/rich.ts`.

### Escenario (`pkg/ops/scenario_test.go`)

`TestScenario_FullLifecycle` ejecuta 12 subtests en orden:

1. `01_add_accounts`
2. `02_credential_check`
3. `03_discover`
4. `04_add_repo`
5. `05_clone`
6. `06_status`
7. `07_pull_and_fetch`
8. `08_update_account_reconfigures_clones`
9. `09_mirror_crud`
10. `10_reclone`
11. `11_rename_account`
12. `12_delete_everything`

## Cómo funciona el harness de pruebas

### Helpers de pruebas unitarias en `pkg/ops`

- `isolate` (`ops_test.go`) apunta `XDG_CONFIG_HOME`, `GIT_CONFIG_GLOBAL` y la carpeta SSH a un directorio temporal, define `GIT_CONFIG_NOSYSTEM`, vacía `GIT_TOKEN` y devuelve una config nueva cuya única raíz está en ese directorio.
- `makeClone` crea un repo git en la ruta de clon planificada de un repo de una source, con un origin dado.
- `gitRun` ejecuta git en una carpeta y hace fallar la prueba si hay error.

### Carga del fixture

`requireIntegration` en `pkg/ops/fixture_test.go` carga `test-gitbox.json` para la prueba de escenario:

1. Se salta en modo `-short` y falla con instrucciones de preparación cuando falta el archivo.
2. Parsea el archivo con `config.Parse`, que ignora las claves `_test` desconocidas.
3. Lee `_test.token` de cada cuenta en una pasada JSON raw aparte.
4. Define `GITBOX_TOKEN_<KEY>` para cada cuenta con token.
5. `rejectRealPaths` rechaza un `credential_ssh.ssh_folder` igual a `~/.ssh` y cualquier raíz de `global.roots` que sea el directorio de config de gitbox.
6. Apunta el SSH de git a la config SSH del fixture mediante `GIT_SSH_COMMAND`.

Después, el escenario define un `XDG_CONFIG_HOME` temporal, construye una config nueva cuya única raíz es un directorio temporal, y guarda y recarga la config tras cada paso, igual que la GUI persiste tras cada acción. `firstAccountWithRepos` elige la cuenta: la primera source, en orden de clave, con repos y un token. `t.TempDir()` borra los clones después.

### Modo de prueba

`GitboxApp --test-mode` pasa por `config.SetupTestMode()` en `pkg/config/testmode.go`. Busca `test-gitbox.json` subiendo desde el directorio actual, aplica las mismas comprobaciones de rutas, escribe una config descartable con `global.roots` sustituido por un único directorio temporal (vaciando cada `source.root` y `repo.root`), define `GIT_SSH_COMMAND` e inyecta los tokens del fixture como variables de entorno. La GUI borra el directorio temporal al salir.

### Pruebas de concurrencia y de procesos

- Las pruebas de refresco y de sondeo en `cmd/gui` bloquean trabajo inyectado en canales para solapar disparadores sin un runtime de Wails, y usan servidores HTTP locales para comprobar la concurrencia limitada, los sondeos compartidos, los plazos y el estado upstream conservado tras errores o rotación de credenciales.
- Las pruebas de contexto de credenciales usan servidores de proveedor locales y un script de helper bloqueado para demostrar la cancelación.
- Las pruebas de mantenimiento en `pkg/git`, `pkg/heal` y `pkg/credential` usan config de git aislada, archivos include y worktrees enlazados, y cuentan los procesos git con `GIT_TRACE2_EVENT`: un clon SSH, token o GCM sin cambios lanza dos comandos git de solo lectura. Las pruebas de archivos de credenciales comprueban fechas de modificación sin cambios, lectores concurrentes, rotación, permisos y separación de cuentas.
- `TestMain` en `pkg/terminals/detect_test.go` y `pkg/doctor/wsl_windows_test.go` convierte el binario de prueba en un `wsl.exe` falso que escribe un archivo centinela (`GITBOX_TEST_FAKE_WSL_SENTINEL`) cuando se ejecuta, así las pruebas demuestran que la detección de WSL nunca lo ejecuta.
- `.githooks/pre-push` elimina `GIT_DIR`, `GIT_WORK_TREE` y las demás variables que git exporta a los hooks, para que las propias llamadas a git de las pruebas apunten a sus repos temporales en lugar de a este repositorio.

## Referencia del mantenimiento del refresco

Medí el mantenimiento previo al fetch antes (`da83922`) y después de la reescritura con comparación previa sobre el mismo fixture aislado: 24 clones locales, una cuenta, ningún fetch de red, una pasada secuencial en caliente por plataforma, tipo de credencial y estado. En los casos con diferencias cambié ambos campos de identidad, origin y helper antes de cada pasada. El harness se ejecutó con Go 1.26.1; Windows usó Git 2.53.0.windows.1.

Los tiempos transcurridos cubren el bucle de mantenimiento completo en el proceso padre, incluida la creación de procesos, la ejecución de git, las operaciones de archivos y la sobrecarga del trazado. Los recuentos de procesos salen de los eventos de inicio de Trace2. Estas ejecuciones no separan el tiempo interno de git, no miden el pintado de la GUI ni estiman un refresco de red completo con ocho workers.

| Plataforma | Estado del clon | Credencial | Procesos git antes → después | Segundos transcurridos antes → después |
| --- | --- | --- | --- | --- |
| macOS arm64 | sin cambios | SSH | 216 → 48 | 0.902 → 0.191 |
| macOS arm64 | sin cambios | Token | 240 → 48 | 1.013 → 0.202 |
| macOS arm64 | sin cambios | GCM | 192 → 48 | 0.923 → 0.278 |
| Windows amd64 | sin cambios | SSH | 216 → 48 | 9.831 → 2.037 |
| Windows amd64 | sin cambios | Token | 240 → 48 | 12.396 → 2.382 |
| Windows amd64 | sin cambios | GCM | 192 → 48 | 9.386 → 2.202 |
| macOS arm64 | con diferencias | SSH | 288 → 264 | 1.311 → 1.216 |
| macOS arm64 | con diferencias | Token | 312 → 288 | 1.457 → 1.319 |
| macOS arm64 | con diferencias | GCM | 264 → 288 | 1.222 → 1.402 |
| Windows amd64 | con diferencias | SSH | 288 → 264 | 12.774 → 11.085 |
| Windows amd64 | con diferencias | Token | 312 → 288 | 13.576 → 12.089 |
| Windows amd64 | con diferencias | GCM | 264 → 288 | 11.105 → 13.073 |

La reparación de diferencias de GCM valida la config efectiva después de escribir, así que su caso con diferencias cuesta más que antes. Conservo esa comprobación para que un fetch nunca se ejecute con ajustes que sobrescribe un include o la config de un worktree.

- **Tiempo interno de git** — una pasada aparte en Windows sobre 24 clones SSH sin cambios tardó 8.918 → 1.963 s en el proceso padre y 5.421 → 1.193 s de tiempo interno de git (sumado de los `exit.t_abs` de Trace2 de nivel superior). El residuo de 3.497 → 0.770 s cubre el arranque de procesos, IPC y las operaciones de archivos del llamante; no lo atribuyo solo al antivirus ni a `HideWindow`.
- **Planificación de sondeos** — ejecuciones nativas en Windows amd64 con Go 1.27.0 y servidores HTTP de loopback: doce upstreams compartidos por dos sources hicieron doce peticiones, una búsqueda de credenciales y como mucho cuatro peticiones concurrentes. Con un timeout de 20 ms por petición, doce peticiones bloqueadas terminaron en 61.34 ms; con un presupuesto total de 20 ms, la pasada se detuvo tras cuatro peticiones en 20.57 ms. Los límites de producción siguen en diez segundos por petición y treinta por pasada, y estas cifras no predicen la latencia real del proveedor.
