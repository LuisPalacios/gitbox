# Referencia de pruebas

Inventario detallado de pruebas y detalles internos del harness. Para ejecutar pruebas, ver checklists y preparar fixtures, consulta [testing.md](testing.md).

## Inventario de pruebas

### Pruebas unitarias de TUI — 31 pruebas (`cmd/cli/tui/`)

Ciclo de vida del modelo y routing:

- `TestInit_FirstRun` — sin config → pantalla de onboarding
- `TestInit_ExistingConfig` — existe config → dashboard
- `TestInit_ExistingConfig_NoFolder` — config sin carpeta → onboarding
- `TestInit_InvalidConfig` — JSON incorrecto → error, onboarding
- `TestQuit_CtrlC` — Ctrl+C sale
- `TestQuit_Esc` — Esc sale desde el dashboard
- `TestWindowResize` — el resize se propaga a los submodelos
- `TestNavigation_SwitchScreens` — 5 subtests: routing de pantallas mediante mensajes
- `TestView_NotEmpty` — View() devuelve una cadena no vacía

Dashboard:

- `TestDashboard_Empty` — sin cuentas renderiza el estado vacío
- `TestDashboard_WithAccounts` — las cuentas aparecen en las tarjetas
- `TestDashboard_TabSwitch` — Tab alterna Accounts/Mirrors
- `TestDashboard_CardNavigation` — las flechas mueven el cursor de tarjetas
- `TestDashboard_AddAccountShortcut` — la tecla 'a' dispara añadir cuenta
- `TestDashboard_SettingsShortcut` — la tecla 's' dispara settings

Pantallas de cuenta:

- `TestAccount_Render` — la vista de detalle muestra campos de la cuenta
- `TestAccount_BackToDashboard` — Esc vuelve al dashboard
- `TestAccountAdd_Render` — el formulario muestra todos los campos
- `TestAccountAdd_SaveAccount` — rellenar formulario → guardar → verificar config en disco
- `TestAccountAdd_DuplicateKey` — rechaza claves de cuenta duplicadas
- `TestAccountAdd_InvalidKey` — rechaza formato de clave inválido

Pantallas de credenciales:

- `TestCredential_GCM_MenuRender` — el menú muestra clave de cuenta, tipo y pista de autenticación en navegador
- `TestCredential_GCM_SetupView` — desktop muestra "authenticate", headless muestra "desktop session"
- `TestCredential_GCM_SetupViewBusy` — muestra "Opening browser" mientras espera
- `TestCredential_GCM_SetupDoneSuccess` — una autenticación correcta establece resultOK
- `TestCredential_GCM_SetupDoneNeedsPAT` — transiciona a entrada de PAT cuando la API necesita un token separado
- `TestCredential_GCM_SetupDoneError` — muestra el mensaje de error
- `TestCredential_GCM_TypeSelectTriggersSetup` — seleccionar el tipo GCM inicia autenticación en navegador en desktop
- `TestCredential_GCM_BackFromSetup` — la navegación hacia atrás funciona

Onboarding:

- `TestOnboarding_Render` — renderiza la pantalla de primer uso
- `TestOnboarding_SubmitFolder` — introducir ruta de carpeta → guarda en config

### Pruebas unitarias de helpers TUI — 7 pruebas (`cmd/cli/tui/`)

- `TestCloneURL_Token` — URL HTTPS con username
- `TestCloneURL_SSH_WithHost` — URL SSH con alias de host personalizado
- `TestCloneURL_SSH_NoHost` — URL SSH con hostname desde URL
- `TestCloneURL_GCM` — URL HTTPS (igual que token)
- `TestStripScheme` — elimina https://, http://, mantiene hostnames sin esquema
- `TestReconfigureClones` — actualiza la URL remota en un clone real de git
- `TestCountClonedRepos` — cuenta solo repos que existen en disco

### Pruebas de integración TUI — 7 pruebas (`cmd/cli/tui/`)

Estas conducen el event loop real de Bubble Tea mediante **teatest** con credenciales de `test-gitbox.json`:

- `TestIntegration_TUI_DashboardLoadsAccounts` — carga asíncrona de config → el dashboard renderiza todos los encabezados de sección de cuentas
- `TestIntegration_TUI_CredentialStatus` — la comprobación asíncrona de credenciales actualiza el badge de "···" a la etiqueta del tipo de credencial
- `TestIntegration_TUI_NavigateToAccount` — Enter → detalle de cuenta → Esc → vuelta al dashboard
- `TestIntegration_TUI_AccountCredentialVerified` — la comprobación de credencial token termina con estado OK
- `TestIntegration_TUI_Discovery` — navegar a cuenta → 'd' → la API del proveedor devuelve lista de repos
- `TestIntegration_TUI_Settings` — 's' → renderiza pantalla de settings → Esc → vuelta al dashboard
- `TestIntegration_TUI_MirrorsTab` — Tab → renderiza tab mirrors → Tab → vuelta a accounts

### Pruebas unitarias CLI — 24 pruebas (`cmd/cli/`)

Comandos CRUD mediante ejecución de subprocess contra config aislada:

- `TestCLI_Version`, `TestCLI_Help` — invocación básica del binario
- `TestCLI_GlobalShow`, `TestCLI_GlobalUpdate` — operaciones de config global
- `TestCLI_AccountAdd`, `TestCLI_AccountList`, `TestCLI_AccountShow`, `TestCLI_AccountUpdate`, `TestCLI_AccountDelete` — CRUD de cuentas
- `TestCLI_SourceAdd`, `TestCLI_SourceList`, `TestCLI_SourceDelete` — CRUD de sources
- `TestCLI_RepoAdd`, `TestCLI_RepoList`, `TestCLI_RepoDelete` — CRUD de repos
- `TestCLI_MirrorAdd`, `TestCLI_MirrorAddRepo`, `TestCLI_MirrorDeleteRepo`, `TestCLI_MirrorDelete`, `TestCLI_MirrorList` — CRUD de mirrors
- `TestCLI_Status` — salida del comando status
- `TestCLI_AccountAdd_MissingFlags`, `TestCLI_AccountShow_NotFound`, `TestCLI_SourceAdd_NoAccount` — casos de error

### Pruebas de integración CLI — 6 pruebas (`cmd/cli/`)

Llamadas reales a APIs de proveedores y operaciones git mediante subprocess:

- `TestIntegration_CLI_CredentialVerify` — el token se resuelve y la API responde
- `TestIntegration_CLI_Discover` — lista repos desde el proveedor
- `TestIntegration_CLI_Clone` — clona repo a temp dir, verifica `.git` e identidad git
- `TestIntegration_CLI_Status` — comprueba estado de sync del repo clonado
- `TestIntegration_CLI_Pull` — hace pull desde remoto
- `TestIntegration_CLI_Fetch` — hace fetch desde remoto

### Prueba de escenario CLI — 1 prueba, 22 pasos (`cmd/cli/`)

- `TestScenario_CLI_FullLifecycle` — end-to-end: añadir cuentas → añadir sources → añadir repos → clone → status → pull → configurar mirror → borrar clone → reclonar → desmontar en orden inverso

### Pruebas de paquetes — 138 pruebas (`pkg/`)

- `pkg/config/` — 58 pruebas: parseo de config, operaciones CRUD, save/load
- `pkg/credential/` — pruebas: resolución de token, validación, `CanOpenBrowser`, helpers por defecto del OS, `Check`/`FixGlobalGCMConfig` (salud de gitconfig global para GCM)
- `pkg/git/` — 9 pruebas: operaciones git mediante subprocess
- `pkg/identity/` — 7 pruebas: ResolveIdentity, EnsureRepoIdentity, CheckGlobalIdentity
- `pkg/mirror/` — 5 pruebas: descubrimiento de mirrors
- `pkg/provider/` — 35 pruebas: cliente HTTP, parseo de APIs de proveedor
- `pkg/status/` — 8 pruebas: comprobación de estado de clones
- `pkg/update/` — 6 pruebas: parseo semver, comparación de versiones, comprobación de actualización (API mock), throttle, verificación de checksum

### Total: ~214 pruebas

## Cómo funciona el harness de pruebas

La clave `_test` dentro de cada cuenta se ignora silenciosamente en `config.Parse()` — el unmarshaler JSON de Go omite campos desconocidos. El harness de pruebas:

1. Parsea el archivo como una config estándar de gitbox (accounts, sources, mirrors)
2. Extrae `_test` de cada cuenta mediante una segunda pasada JSON raw
3. Define variables de entorno `GITBOX_TOKEN_<KEY>` para las cuentas que tienen `_test.token`
4. Sobrescribe `global.folder` y `credential_ssh.ssh_folder` con directorios temporales descartables
5. Escribe la config en la ruta descartable y ejecuta comandos CLI con `--config <throwaway-path>`

Los directorios de clones se limpian automáticamente con `t.TempDir()` de Go después de cada prueba. Las pruebas de integración usan el `ssh_folder` de tu `test-gitbox.json` para poder encontrar claves SSH reales. El runner valida que esta ruta no sea `~/.ssh` y que `global.folder` no sea `~/.config/gitbox` — si cualquiera apunta a un directorio real de usuario, las pruebas fallan inmediatamente.
