# Referencia de pruebas

Inventario de pruebas y detalles internos del harness. Para ejecutar pruebas, ver checklists y preparar fixtures, consulta [testing.md](testing.md).

## Inventario de pruebas

Los recuentos son funciones `func Test…` de nivel superior por paquete, obtenidas con `grep -rc "^func Test" --include=*_test.go pkg cmd`. Los subtests (`t.Run`) no se cuentan.

### Pruebas de paquetes — 489 pruebas (`pkg/`)

- `pkg/adopt/` — 15 pruebas: descubrimiento de huérfanos, puntuación de cuentas (usuario embebido en la URL, username de la credencial, carpeta padre, empates ambiguos), clones anidados bajo contenedores, adopción en el lugar bajo una segunda raíz de clonado
- `pkg/config/` — 113 pruebas: parseo de config, migración v2 → v3 → v4, raíces de clonado (añadir, predeterminada, quitar y olvidar, ubicar un clon, fijaciones que conservan cada ruta de clon), operaciones CRUD, save/load, backups, preparación de test-mode, poda del estado de layout (pliegues, barra lateral, la vista de una cuenta borrada), un renombrado que conserva su vista, y omisión del backup en los guardados de layout y de tema
- `pkg/credential/` — 35 pruebas: resolución de token, validación, caducidad del token y tokens rechazados, helpers por defecto del OS, `Check`/`FixGlobalGCMConfig` (salud de gitconfig global para GCM), cancelación, mantenimiento con comparación previa, actualizaciones atómicas de archivos de tokens
- `pkg/doctor/` — 24 pruebas: forma de la tabla de herramientas, pistas de instalación, búsquedas, comprobaciones previas por tipo de credencial, decodificación de la salida de herramientas
- `pkg/git/` — 34 pruebas: operaciones git mediante subprocess, URLs de repo y de perfil, descubrimiento de repos anidados, fetches cancelables, conexiones rechazadas en milisegundos, instantáneas de config con includes y worktrees enlazados
- `pkg/gitignore/` — 23 pruebas: ida y vuelta del bloque gestionado, fusión con contenido del usuario, instalación idempotente, backups, saneado de duplicados
- `pkg/harness/` — 28 pruebas: parseo del directorio de herramientas embebido, herramientas retiradas, parseo del `launch_menu` de WezTerm
- `pkg/heal/` — 11 pruebas: URL de origin esperada por tipo de credencial, reparación de identidad, eliminación de tokens embebidos, mantenimiento sin cambios con dos procesos, reparaciones sobrescritas, worktrees enlazados
- `pkg/i18n/` — 1 prueba: normalización de idioma
- `pkg/identity/` — 4 pruebas: `ResolveIdentity`, `CheckGlobalIdentity`
- `pkg/launch/` — 13 pruebas: expansión de argv, quoting de shell, envoltura de AI harness por shell, AppleScript de macOS
- `pkg/mirror/` — 6 pruebas: parseo de URLs remotas, descubrimiento de mirrors, clasificación de errores de estado
- `pkg/move/` — 5 pruebas: parseo de claves de repo, URLs de clone, validación previa
- `pkg/ops/` — 18 pruebas: 17 pruebas unitarias aisladas (añadir, renombrar y borrar cuenta, también entre dos raíces de clonado; sugerir un account key; descartar una cuenta recién añadida; cambio y borrado de tipo de credencial; borrar repo; planificación de clones; reconfigurar clones; añadir repos descubiertos) más el escenario `TestScenario_FullLifecycle`
- `pkg/provider/` — 52 pruebas: cliente HTTP, parseo de APIs de proveedor, caducidad del token (formatos de la cabecera de GitHub, `personal_access_tokens/self` de GitLab, ninguna petición extra en GitHub, una caducidad desconocida sigue pasando)
- `pkg/status/` — 17 pruebas: comprobación de estado de clones, detección de rama, rutas de repo a través de las raíces de source y de repo, cálculo de anidamiento (incluido un contenedor dentro de otro)
- `pkg/terminals/` — 55 pruebas: forma del catálogo, composición de Profile según el OS, búsquedas de WezTerm y Windows Terminal, reglas de fusión
- `pkg/update/` — 28 pruebas: parseo semver, comparación de versiones, comprobación de actualización (API mock, una actualización pendiente se informa en cada comprobación), límite de versión mayor, nombres de artefactos, detección del AppImage solo-aviso, destinos de instalación, comando de relanzamiento por plataforma y espera a que termine el proceso antiguo (más el script elevado de Windows, en Windows), verificación de checksum, verificación de la firma de release contra firmas hechas con `ssh-keygen` real (otro tag, checksums manipulados, namespace incorrecto, clave ajena, malformada), `allowed_signers` sincronizado con la clave incrustada, descarga que falla cerrada (checksums o firma ausentes, ilegibles o inválidos rechazan la actualización, y una firma mala la detiene antes de descargar el artefacto)
- `pkg/workspace/` — 7 pruebas: descubrimiento de workspaces, refresco de caché, escaneo de cada raíz de clonado, contenedores tentativos

### Pruebas de la GUI — 72 pruebas (`cmd/gui/`)

Lógica del lado Go de la app Wails que se ejecuta sin ventana:

- Acciones de cuenta y navegador — resolución de la carpeta de cuenta a través de la raíz de clonado de la cuenta, URLs de proveedor, rutas de error para cuentas y repos desconocidos
- Acciones de AI harness — detección, orden, deduplicación, poda de harnesses retirados, fallback a `~/.local/bin`, Profile por defecto del lanzador
- Modo de experiencia y tema — `ui_mode` es Advanced por defecto, validación de set/get, y el onboarding escribe Simple solo en una instalación nueva; `SetTheme` valida y persiste el tema
- Auto-actualización — el build AppImage rechaza `ApplyUpdate` antes de cualquier descarga
- Terminales — saneado de rutas MSYS y del entorno para las terminales lanzadas, y un primer arranque que nunca escribe la lista legacy `global.terminals`
- Raíces de clonado — añadir, hacer predeterminada, quitar con y sin olvidar
- Workspaces y contenedores — refresco de caché, persistencia del flag de contenedor, profundidad de escaneo anidado, `clone_folder` absoluto para clones incorporados, persistencia y poda del estado de layout (pliegues, barra lateral y vista)

Cubro las instantáneas de arranque y el descubrimiento de workspaces sin bloqueo en `cmd/gui/startup_test.go` y `cmd/gui/workspaces_test.go`, incluidas las entradas de escaneo obsoletas y las ediciones concurrentes de config. `BenchmarkStartupSnapshot` mide la construcción de la instantánea por separado del renderizado nativo. Ejecuto las regresiones puras del frontend con `npm test` en `cmd/gui/frontend`: el árbol de clones y su orden alfabético (`src/lib/repo-tree.test.mjs`), la traducción de estados, las etiquetas del glosario, los rangos de atención y la salud de la cuenta, incluido el aviso de caducidad del token (`src/lib/status.test.mjs`), la resolución de la vista y la lista y los conteos de Requiere atención (`src/lib/views.test.mjs`), las ejecuciones de Fetch de todo y Pull de todo y sus resúmenes (`src/lib/activity.test.mjs`), las tarjetas de resultado (`src/lib/notices.test.mjs`) y las reglas del texto de la UI (`src/lib/i18n.test.mjs`: mismas claves y placeholders en inglés y español, ninguna clave inexistente, ningún texto fijo en las plantillas, y el marcado en línea de `src/lib/rich.ts`); `scripts/health.sh frontend-test` ejecuta la misma suite.

Cubro la coordinación del refresco y los sondeos upstream sin una ventana Wails, incluidos los disparadores solapados, los workers limitados, la cancelación, los resultados de config o credenciales obsoletos y la caducidad del token que llega al frontend. Las pruebas de contexto de credenciales usan servidores de proveedores locales y un helper bloqueado. Las regresiones de instantáneas de config y mantenimiento usan config Git aislada, archivos include, worktrees enlazados y recuentos de procesos Trace2; las comprobaciones de archivos de tokens cubren fechas de modificación sin cambios, lectores concurrentes, rotación, permisos y separación de cuentas.

### Prueba de escenario — 1 prueba, 12 pasos (`pkg/ops/`)

- `TestScenario_FullLifecycle` — end-to-end a través de `pkg/ops`: añadir cuenta → comprobar credencial → descubrir → añadir repo → clone → status → pull y fetch → editar cuenta + reconfigurar clones → CRUD de mirrors → reclonar → renombrar cuenta → borrarlo todo

### Total: 561 pruebas

## Cómo funciona el harness de pruebas

La clave `_test` dentro de cada cuenta se ignora silenciosamente en `config.Parse()` — el unmarshaler JSON de Go omite campos desconocidos. El harness del escenario (`requireIntegration` en `pkg/ops/fixture_test.go`):

1. Se salta en modo `-short` y falla con instrucciones de preparación cuando falta `test-gitbox.json`
2. Parsea el archivo como una config estándar de gitbox (accounts, sources, mirrors)
3. Extrae `_test` de cada cuenta mediante una segunda pasada JSON raw
4. Define variables de entorno `GITBOX_TOKEN_<KEY>` para las cuentas que tienen `_test.token`
5. Rechaza los fixtures cuyo `credential_ssh.ssh_folder` es `~/.ssh` o con una raíz de `global.roots` que es el directorio real de config de gitbox
6. Apunta el SSH de git a la config SSH aislada del fixture mediante `GIT_SSH_COMMAND`

Después, el propio escenario define un `XDG_CONFIG_HOME` temporal, construye una config nueva cuya única raíz es un directorio temporal, y guarda y recarga esa config tras cada paso, igual que la GUI persiste tras cada acción. Los directorios de clones se limpian automáticamente con `t.TempDir()` de Go después de la prueba.

`GitboxApp --test-mode` usa el mismo fixture mediante `config.SetupTestMode()`: busca `test-gitbox.json` subiendo desde el directorio actual, escribe una config descartable con `global.roots` sobrescrito, aplica las mismas comprobaciones de seguridad de rutas e inyecta los tokens del fixture como variables de entorno.
