# Referencia de pruebas

Inventario de pruebas y detalles internos del harness. Para ejecutar pruebas, ver checklists y preparar fixtures, consulta [testing.md](testing.md).

## Inventario de pruebas

Los recuentos son funciones `func Test…` de nivel superior por paquete, obtenidas con `grep -rc "^func Test" --include=*_test.go pkg cmd`. Los subtests (`t.Run`) no se cuentan.

### Pruebas de paquetes — 431 pruebas (`pkg/`)

- `pkg/adopt/` — 13 pruebas: descubrimiento de huérfanos, puntuación de cuentas (usuario embebido en la URL, username de la credencial, carpeta padre, empates ambiguos), clones anidados bajo contenedores
- `pkg/config/` — 98 pruebas: parseo de config, migración v1/v2 → v3, operaciones CRUD, save/load, backups, preparación de test-mode
- `pkg/credential/` — 21 pruebas: resolución de token, validación, helpers por defecto del OS, `Check`/`FixGlobalGCMConfig` (salud de gitconfig global para GCM)
- `pkg/doctor/` — 24 pruebas: forma de la tabla de herramientas, pistas de instalación, búsquedas, comprobaciones previas por tipo de credencial, decodificación de la salida de herramientas
- `pkg/git/` — 30 pruebas: operaciones git mediante subprocess, URLs de repo y de perfil, descubrimiento de repos anidados
- `pkg/gitignore/` — 23 pruebas: ida y vuelta del bloque gestionado, fusión con contenido del usuario, instalación idempotente, backups, saneado de duplicados
- `pkg/harness/` — 28 pruebas: parseo del directorio de herramientas embebido, herramientas retiradas, parseo del `launch_menu` de WezTerm
- `pkg/heal/` — 7 pruebas: URL de origin esperada por tipo de credencial, reparación de identidad, eliminación de tokens embebidos
- `pkg/i18n/` — 1 prueba: normalización de idioma
- `pkg/identity/` — 7 pruebas: `ResolveIdentity`, `EnsureRepoIdentity`, `CheckGlobalIdentity`
- `pkg/launch/` — 13 pruebas: expansión de argv, quoting de shell, envoltura de AI harness por shell, AppleScript de macOS
- `pkg/mirror/` — 6 pruebas: parseo de URLs remotas, descubrimiento de mirrors, clasificación de errores de estado
- `pkg/move/` — 5 pruebas: parseo de claves de repo, URLs de clone, validación previa
- `pkg/ops/` — 15 pruebas: 14 pruebas unitarias aisladas (añadir, renombrar y borrar cuenta; cambio y borrado de tipo de credencial; borrar repo; planificación de clones; reconfigurar clones; añadir repos descubiertos) más el escenario `TestScenario_FullLifecycle`
- `pkg/provider/` — 43 pruebas: cliente HTTP, parseo de APIs de proveedor
- `pkg/status/` — 15 pruebas: comprobación de estado de clones, detección de rama, cálculo de anidamiento
- `pkg/terminals/` — 51 pruebas: forma del catálogo, composición de Profile según el OS, búsquedas de WezTerm y Windows Terminal, reglas de fusión
- `pkg/update/` — 26 pruebas: parseo semver, comparación de versiones, comprobación de actualización (API mock), límite de versión mayor, nombres de artefactos, detección del AppImage solo-aviso, destinos de instalación, verificación de checksum, verificación de la firma de release contra firmas hechas con `ssh-keygen` real (otro tag, checksums manipulados, namespace incorrecto, clave ajena, malformada), `allowed_signers` sincronizado con la clave incrustada, descarga que falla cerrada (checksums o firma ausentes, ilegibles o inválidos rechazan la actualización, y una firma mala la detiene antes de descargar el artefacto)
- `pkg/workspace/` — 5 pruebas: descubrimiento de workspaces, refresco de caché, carpetas extra, contenedores tentativos

### Pruebas de la GUI — 28 pruebas (`cmd/gui/`)

Lógica del lado Go de la app Wails que se ejecuta sin ventana:

- Acciones de cuenta y navegador — resolución de la carpeta de cuenta, URLs de proveedor, rutas de error para cuentas y repos desconocidos
- Acciones de AI harness — detección, orden, deduplicación, poda de harnesses retirados, fallback a `~/.local/bin`, Profile por defecto del lanzador
- Auto-actualización — el build AppImage rechaza `ApplyUpdate` antes de cualquier descarga
- Terminales — saneado de rutas MSYS y del entorno para las terminales lanzadas, y un primer arranque que nunca escribe la lista legacy `global.terminals`
- Workspaces y contenedores — refresco de caché, persistencia del flag de contenedor, carpetas extra, profundidad de escaneo anidado, `clone_folder` absoluto para clones incorporados

### Prueba de escenario — 1 prueba, 12 pasos (`pkg/ops/`)

- `TestScenario_FullLifecycle` — end-to-end a través de `pkg/ops`: añadir cuenta → comprobar credencial → descubrir → añadir repo → clone → status → pull y fetch → editar cuenta + reconfigurar clones → CRUD de mirrors → reclonar → renombrar cuenta → borrarlo todo

### Total: 459 pruebas

## Cómo funciona el harness de pruebas

La clave `_test` dentro de cada cuenta se ignora silenciosamente en `config.Parse()` — el unmarshaler JSON de Go omite campos desconocidos. El harness del escenario (`requireIntegration` en `pkg/ops/fixture_test.go`):

1. Se salta en modo `-short` y falla con instrucciones de preparación cuando falta `test-gitbox.json`
2. Parsea el archivo como una config estándar de gitbox (accounts, sources, mirrors)
3. Extrae `_test` de cada cuenta mediante una segunda pasada JSON raw
4. Define variables de entorno `GITBOX_TOKEN_<KEY>` para las cuentas que tienen `_test.token`
5. Rechaza los fixtures cuyo `credential_ssh.ssh_folder` es `~/.ssh` o cuyo `global.folder` es el directorio real de config de gitbox
6. Apunta el SSH de git a la config SSH aislada del fixture mediante `GIT_SSH_COMMAND`

Después, el propio escenario define un `XDG_CONFIG_HOME` temporal, construye una config nueva con `global.folder` en un directorio temporal, y guarda y recarga esa config tras cada paso, igual que la GUI persiste tras cada acción. Los directorios de clones se limpian automáticamente con `t.TempDir()` de Go después de la prueba.

`GitboxApp --test-mode` usa el mismo fixture mediante `config.SetupTestMode()`: busca `test-gitbox.json` subiendo desde el directorio actual, escribe una config descartable con `global.folder` sobrescrito, aplica las mismas comprobaciones de seguridad de rutas e inyecta los tokens del fixture como variables de entorno.
