# Gitbox — arquitectura y diseño

Para la visión general del producto (qué hace gitbox, para quién es y por qué existe), consulta el [README](../../README.md).

---

## 1. Resumen de arquitectura

gitbox es un monorepo Go que produce un binario, la app de escritorio `GitboxApp`, sobre una librería compartida en `pkg/`:

<p align="center">
  <img src="../diagrams/architecture-overview.png" alt="Architecture Overview" width="800" />
</p>

La GUI es una app Wails v2 con frontend Svelte. El lado Go vive en `cmd/gui/` y es una capa fina de bindings Wails, locking y eventos. Las operaciones a nivel de aplicación (ciclo de vida de cuentas, cambio de credenciales, planificación de clones, discovery) viven en `pkg/ops`, y todo lo que hay por debajo — config, credenciales, proveedores, git, status, mirrors — vive en los demás paquetes de `pkg/`. La GUI soporta credenciales GCM, SSH y Token.

La app no necesita ningún binario acompañante. Solo llama a herramientas ya presentes en el sistema: `git`, Git Credential Manager, `ssh` / `ssh-keygen` / `ssh-add`, `wsl` en Windows, y las terminales y editores del usuario.

Hasta v1.x, gitbox también distribuía una CLI `gitbox` con una TUI embebida. v2 eliminó ambas; siguen vivas en la línea de mantenimiento v1, en la rama `release/v1`.

## 2. Conceptos core

### Accounts, sources y repos

<p align="center">
  <img src="../diagrams/config-model.png" alt="Config Model" width="800" />
</p>

**¿Por qué separarlos?** Una cuenta puede tener varias sources (por ejemplo, diferentes orgs de GitHub bajo el mismo login). Las sources agrupan repos lógicamente. Los repos usan nombres `org/repo` — la parte org se convierte en la estructura de carpetas.

### Modelo de credenciales

Consulta [credentials.md](credentials.md) para detalles de configuración orientados a usuarios. Esta sección cubre el diseño.

Cada tipo de credencial es **autosuficiente** para la cuenta. El aislamiento de credenciales por repo asegura que cada clone tenga configuración autocontenida en `.git/config` — una línea vacía `credential.helper =` cancela helpers heredados (globales/sistema), y luego se define el helper específico del tipo. Esto independiza los clones de `~/.gitconfig`.

**Decisiones de diseño:**

- **Token** usa almacenamiento dual: keyring del OS (para llamadas API de gitbox) + archivo `credential-store` por repo (para git). El archivo se deriva de la entrada del keyring.
- **GCM** usa `helper = manager` con config por host (`username`, `provider`, `credentialStore`) scoped por repo.
- **SSH** solo cancela helpers (auth va por `~/.ssh/config`). Discovery requiere un PAT opcional.

**Ciclo de vida de credenciales:** cambiar de tipo limpia artefactos antiguos antes de configurar el tipo nuevo. Los clones existentes se reconfiguran automáticamente (URL remota + config de credenciales). Los renombres de clave de cuenta migran todos los artefactos: claves de config, carpetas source, entradas de keyring, archivos de credenciales, claves SSH y alias SSH config.

### Config como base de datos local

El archivo JSON de config (`~/.config/gitbox/gitbox.json`) es el **estado deseado** — una base de datos local de qué cuentas, sources y repos deberían existir.

**Discovery** es una consulta hacia el norte — pregunta a la API del proveedor "¿qué repos existen?" y permite añadirlos a la config. Discovery es add-only bajo demanda; nunca elimina repos automáticamente.

### Estructura de carpetas

Los repos se clonan en una jerarquía de 3 niveles:

```text
~/00.git/                          <- global.folder
  github-personal/                 <- clave source (1er nivel)
    MyOrg/                         <- org de "MyOrg/project-a" (2º nivel)
      project-a/                   <- nombre repo (3er nivel)
      project-b/
    other-org/
      tools/                       <- repo de otra org, mismas credenciales
  forgejo-work/
    infra/
      homelab-ops/
    infra-prod/                    <- override de id_folder
      homelab/
```

Cada nivel puede sobrescribirse:

- **1er nivel**: `source.folder` sobrescribe la clave source
- **2º nivel**: `repo.id_folder` sobrescribe la parte org
- **3er nivel**: `repo.clone_folder` sobrescribe el nombre de repo (si es absoluta — `/`, `~` o `../` — reemplaza toda la ruta)

Los clones fuera de este árbol (carpetas de escaneo extra, clones anidados dentro de un contenedor multi-repo) siempre llevan un `clone_folder` absoluto, así que gitbox nunca los mueve.

---

## 3. Diseño de componentes

### cmd/gui — bindings Wails

`cmd/gui/app.go` expone los métodos que el frontend Svelte llama mediante bindings TypeScript autogenerados. Cada binding toma el lock de config, llama a `pkg/ops` o a otro paquete de `pkg/`, guarda la config y emite eventos Wails de progreso. Los archivos vecinos contienen asuntos exclusivos de la GUI: perfiles de terminal (`profiles.go`), workspaces (`workspaces.go`), contenedores multi-repo (`containers.go`), badges de PR (`pr.go`), autostart y ciclo de vida de procesos por OS. Cada `exec.Command` en `cmd/gui/` pasa por `git.HideWindow` para que no parpadee ninguna ventana de consola en Windows.

`main.go` gestiona dos flags antes de que arranque Wails: `--version` imprime la versión del build y termina, y `--test-mode` enruta la config a través de un directorio temporal construido desde el fixture `test-gitbox.json` (consulta [testing.md](testing.md)).

### pkg/ops — operaciones de aplicación

La capa de servicio entre los bindings de la GUI y los paquetes de nivel inferior. Contiene las operaciones que tocan varios paquetes a la vez: `AddAccount`, `RenameAccount`, `DeleteAccount`, `DeleteRepo`, `ChangeCredentialType`, `DeleteCredential`, `RemoveCredentialArtifacts`, `ReconfigureClones` (ejecuta `heal.Repo` en cada clone), `PlanClone` / `Clone`, `ListRemoteRepos`, `ListAccountOrgs`, `CreateRemoteRepo`, `AddDiscoveredRepos` y `ProviderClient`.

Cada operación trabaja sobre un `*config.Config` más los archivos que posee en disco (clones, claves SSH, credential stores). Las operaciones nunca guardan la config ni toman locks — quien llama serializa el acceso y persiste después. Las operaciones que tocan cada clone de una cuenta (`ReconfigureClones`) están separadas para que quien llama pueda ejecutarlas después de un save correcto.

### pkg/config — gestión de configuración

Gestiona el archivo de configuración v3 (los archivos v1 y v2 se migran de forma transparente al cargar). Tipos core: `Config`, `Account`, `Source`, `Repo`. v3 añade contenedores multi-repo (`Repo.Container`), raíces de escaneo extra (`Global.ExtraFolders`), profundidad de descubrimiento de clones anidados (`Global.NestedScanDepth`), sobrescrituras de `clone_folder` absolutas y workspaces descubiertos de solo lectura. Consulta `pkg/config/config.go` para las definiciones de structs.

**Decisiones clave de diseño:**

- **Preservación de orden JSON:** `SourceOrder` y `RepoOrder` aseguran que la iteración siga el orden del archivo de config del usuario
- **Herencia de credenciales:** los repos heredan `default_credential_type` de su cuenta salvo que lo sobrescriban
- **CRUD con integridad referencial:** `DeleteAccount` falla si alguna source la referencia; `DeleteSource` elimina en cascada sus repos

### pkg/credential — gestión de credenciales

Gestiona tokens, claves SSH, integración GCM y aislamiento de credenciales por repo. Consulta `pkg/credential/credential.go`, `pkg/credential/validate.go` y `pkg/credential/repoconfig.go`.

**Cadena de resolución de token:** Variable de entorno (`GITBOX_TOKEN_<KEY>`) -> fallback `GIT_TOKEN` -> archivo de credenciales (`~/.config/gitbox/credentials/<key>`).

**Dispatch de token API:** Enruta por tipo de credencial — `token` usa el archivo de credenciales, `gcm` ejecuta `git credential fill` (con fallback al archivo de credenciales), `ssh` prueba el archivo de credenciales (PAT opcional para discovery).

**Gestión de claves SSH:** Genera pares de claves, escribe entradas `~/.ssh/config`, prueba conexiones. Convención de nombre: alias de host `gitbox-<account-key>`, archivo de clave `gitbox-<account-key>-sshkey`.

**Config de credenciales por repo** (`repoconfig.go`): `ConfigureRepoCredential()` configura el `.git/config` de cada clone para ser autocontenido. `WriteCredentialFile()` y `DeleteToken()` gestionan los archivos git-credential-store para cuentas token. `pkg/ops` y `pkg/heal` llaman a las mismas funciones compartidas.

### pkg/provider — discovery de repositorios

Capa de abstracción para APIs de proveedores de hosting Git. Cada proveedor implementa `ListRepos()` y devuelve structs `RemoteRepo`. Consulta `pkg/provider/provider.go` para la interfaz.

| Proveedor     | API          | Auth                        | Notas                           |
| ------------- | ------------ | --------------------------- | ------------------------------- |
| GitHub        | REST v3      | Bearer token                | Soporta GitHub Enterprise       |
| GitLab        | REST v4      | header PRIVATE-TOKEN        | Compatible con self-hosted      |
| Gitea/Forgejo | REST /api/v1 | Token + fallback Basic auth | Misma API, misma implementación |
| Bitbucket     | REST v2      | HTTP Basic (app password)   | Solo cloud                      |

Los helpers incluyen `TestAuth()` para validación de credenciales y `TokenSetupGuide()` para instrucciones de creación PAT por proveedor.

**Interfaces adicionales** (opcionales, mediante type assertions):

- `RepoCreator` — crear repos (bajo namespace de usuario u org, con descripción) y comprobar existencia (todos los proveedores)
- `OrgLister` — listar organizaciones/grupos a los que pertenece el usuario, para el dropdown owner de "create repo" (todos los proveedores)
- `PushMirrorProvider` — push mirrors server-side (Gitea/Forgejo, GitLab)
- `PullMirrorProvider` — pull mirrors mediante migrate API (Gitea/Forgejo)
- `RepoInfoProvider` — obtener commit HEAD y visibilidad para comparación de sync (GitHub, GitLab, Gitea/Forgejo)

### pkg/git — operaciones Git

Wrapper fino alrededor de `os/exec` para todas las operaciones Git — sin dependencia de libgit2. Proporciona `Clone`, `CloneWithProgress`, `Pull`, `Status`, `Fetch`, `ConfigSet`, `ConfigAdd`, `ConfigUnsetAll` y más. Consulta `pkg/git/git.go`. Las claves multi-value de config git (como `credential.helper`) se gestionan con `ConfigUnsetAll` + `ConfigAdd`.

En macOS, `GitBin()` prueba rutas de Homebrew (`/opt/homebrew/bin/git`, `/usr/local/bin/git`) antes de hacer fallback a PATH, asegurando que la GUI encuentre git con GCM incluso con el PATH mínimo que heredan las apps GUI en macOS.

### pkg/status — comprobación de estado de sync

Determina el estado de sync de clones locales respecto a su upstream. Estados: Clean, Dirty, Behind, Ahead, Diverged, Conflict, NotCloned, NoUpstream, Error. Prioridad: Conflicts > Dirty > Diverged > Behind > Ahead > NoUpstream > Clean. `ComputeNesting` deriva las relaciones padre → hijo entre clones contenedor y los clones anidados dentro de ellos. Consulta `pkg/status/status.go`.

### pkg/identity — gestión de identidad Git

Gestiona identidad git por repo (`user.name`, `user.email`) con una cadena de resolución: los overrides a nivel repo hacen fallback a valores a nivel cuenta. Consulta `pkg/identity/identity.go`.

`EnsureRepoIdentity()` comprueba la git config local de cada clone y arregla la identidad si diverge de los valores esperados. `CheckGlobalIdentity()` y `RemoveGlobalIdentity()` gestionan la identidad global de `~/.gitconfig` — gitbox anima a eliminar la identidad global para que la identidad por repo (definida durante clone/reconfigure) sea siempre autoritativa.

En paralelo al check de identidad, `pkg/credential` expone `IsGlobalGCMConfigNeeded()` + `CheckGlobalGCMConfig()` + `FixGlobalGCMConfig()` para el helper de credenciales GCM global. Cuando al menos una cuenta usa GCM, gitbox verifica que `~/.gitconfig` tenga `credential.helper = manager` y `credential.credentialStore = <keychain|wincredman|secretservice>`; si falta o está mal, la GUI muestra un botón fix que escribe ambas entradas y rellena defaults del OS en `gitbox.json`. Sin esto, `git credential fill` cae a `/dev/tty` y falla con "Device not configured" en contextos GUI.

### pkg/update — auto-update

Proporciona comprobación de versión y self-update mediante GitHub Releases. `CheckLatest()` consulta la API de GitHub (limitado a una vez cada 24h). `DownloadRelease()` verifica primero que el `checksums.sha256.sig` de la release es una firma de `ssh-keygen -Y sign`, en el namespace `gitbox-release`, de la clave incrustada desde `release-signing-key.pub`, sobre `gitbox <tag>` más los checksums (un pequeño verificador sshsig sobre `golang.org/x/crypto/ssh`); solo entonces descarga el artefacto específico de la plataforma y verifica su checksum SHA256. Falla cerrada cuando la firma o `checksums.sha256` falta, no se puede leer o no es válido, o no lista el artefacto. Consulta [release-signing.md](release-signing.md). `ExtractUpdate()` e `InstallExtracted()` desempaquetan el zip y reemplazan lo que ya está instalado junto a la app en ejecución — en macOS el bundle `GitboxApp.app` completo, en Unix mediante rename atómico, en Windows renombrando primero el binario en ejecución a `.old` (`CleanupOldBinary()` elimina archivos `.old` stale en el siguiente arranque). La GUI sigue la release que GitHub marca como latest; una opción `MaxMajor` limita la versión major, y la CLI v1 de `release/v1` la usa para quedarse en 1.x.

### pkg/doctor — detección de herramientas externas

Sondea el host para cada binario externo que gitbox puede llamar — `git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, `wsl` (solo Windows) — y reporta ruta, versión y sugerencias de instalación por OS para cualquier ausente. `PrecheckForCredentialType()` alimenta los flujos de setup de la GUI para que una herramienta faltante aparezca como banner amarillo con el comando de instalación en vez de un error críptico de runtime. El informe completo está disponible en la GUI en **Settings → System check**.

### pkg/heal — self-heal de .git/config de repo

`heal.Repo(cfg, sourceKey, repoKey)` reconcilia idempotentemente el `.git/config` de un clone contra la spec de cuenta: `user.name`, `user.email`, la URL canónica de `origin` (específica del tipo de credencial, sin secretos embebidos) y el helper de credenciales. Está conectado a cada punto de disparo — clone, fetch, pull, `ops.ReconfigureClones` y el sync periódico de la GUI — para que los clones que se desvíen de la spec se reparen silenciosamente. Se introdujo junto con el fix de stdio GUI en Windows en `pkg/git.run()` para evitar que las escrituras de `.git/config` desde la GUI fallasen silenciosamente.

### pkg/move — movimiento de repo cross-account / cross-provider

Reubica un clone de una cuenta configurada a otra, incluso entre proveedores (GitHub → GitLab, Gitea → Forgejo). Orquesta un flujo por fases — preflight (probe de readiness de credenciales) → fetch → crear destino → `git push --mirror` → rewire `origin` → actualizar `gitbox.json` → borrado opcional de remoto fuente → borrado opcional de clone local → auto-clone destino cuando se borró el local. Los callbacks de fase envían progreso a la GUI. Las fases 1–6 son fatales si fallan; los borrados opcionales son best-effort y aparecen como warnings con `provider.InsufficientScopesError` humanizado a texto de remediación (por ejemplo, "add `delete_repo` scope to your GitHub PAT").

### pkg/gitignore — autorreparación del gitignore global

Instala un bloque gestionado curado de patrones de basura de OS (`.DS_Store`, `Thumbs.db`, `*~`, …) en `~/.gitignore_global` y apunta `core.excludesfile` a él. El bloque se envuelve en marcadores sentinel (`# >>> gitbox:global-gitignore >>>` / `# <<< gitbox:global-gitignore <<<`) para que las reinstalaciones reescriban solo la región gestionada; las entradas añadidas por el usuario y los patrones de negación (`!.DS_Store`) fuera de los sentinels se conservan, y los duplicados de patrones gestionados fuera de los sentinels se eliminan. Escritura atómica tmp+rename con un límite rolling de 3 backups (`.bak-YYYYMMDD-HHMMSS`). Opt-out mediante `global.check_global_gitignore` en `gitbox.json`, que gatea solo el check automático de arranque; las acciones explícitas siempre se ejecutan. Expuesto en la GUI como un banner con botón **Install**.

### pkg/workspace — workspaces de solo lectura

Descubre archivos `.code-workspace` de VS Code existentes y los expone en solo lectura — gitbox nunca crea, edita, genera ni borra archivos de workspace. `Discover` recorre `global.folder` y cada raíz de `global.extra_folders` buscando archivos `*.code-workspace` y resuelve las referencias de carpeta de cada archivo hacia clones conocidos (deepest-prefix match contra las rutas de repo resueltas); es pura y solo informa de lo que hay en disco. `RefreshCache` refleja el resultado en la sección `workspaces` de `gitbox.json` y solo persiste cuando el conjunto cambió. `BuildOpenCommand()` lanza un `.code-workspace` existente en el primer editor de `global.editors`. El soporte de tmuxinator y la generación de workspaces se eliminaron con la config v3.

### pkg/adopt — discovery de repos huérfanos

Escanea `global.folder`, cada raíz de `global.extra_folders` y los árboles de trabajo de los repos contenedor buscando clones que no están en `gitbox.json`, y luego puntúa cada uno contra cada cuenta cuyo host coincide. Todas las señales son aditivas, y gana la puntuación más alta:

| Señal                                                                      | Puntuación | Origen                                                                      |
| -------------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------- |
| Coincidencia de host (base obligatoria)                                    | 1          | hostname de la URL de la cuenta o alias SSH frente al host remoto parseado  |
| Owner igual a `account.username`                                           | +3         | segmento owner de la ruta de la URL remota                                  |
| El repo vive bajo la carpeta source de la cuenta                           | +5         | primer componente de la ruta del repo relativa a la carpeta padre de gitbox |
| La URL HTTPS embebe `user@` donde user es igual a `account.username`       | +10        | `url.User.Username()` del remoto origin                                     |
| `.git/config` tiene `credential.<url>.username` igual a `account.username` | +10        | `git config --get-regexp '^credential\..*\.username$'` en el repo           |

Si la puntuación máxima la comparten dos o más cuentas (incluido un empate solo por host) el match se marca como ambiguo: no se elige cuenta, no se mueven archivos y la GUI muestra la lista de candidatas. Los huérfanos adoptados reciben aislamiento de credenciales, identidad y una URL remota reescrita para coincidir con el tipo de credencial. Los huérfanos bajo el árbol estándar pueden reubicarse opcionalmente a su ruta canónica; los clones fuera de él y los clones anidados se incorporan en su sitio con un `clone_folder` absoluto.

### pkg/mirror — mirroring de repositorios

Gestiona setup de push y pull mirror, comprobación de estado, discovery y guías de setup manual. Los mirrors mantienen copias backup de repos en otro proveedor sin clonar localmente.

**Tipos de mirror:**

| Tipo | Dirección                           | Caso de uso                                     |
| ---- | ----------------------------------- | ----------------------------------------------- |
| Push | El servidor origen empuja al backup | Repo fuente en Forgejo/GitLab; backup en GitHub |
| Pull | El servidor backup tira del origen  | Repo fuente en GitHub; backup en Forgejo        |

**Automatización por proveedor:**

| Proveedor     | Crear repo | Push mirror             | Pull mirror          |
| ------------- | ---------- | ----------------------- | -------------------- |
| Gitea/Forgejo | Sí         | Sí                      | Sí (vía migrate API) |
| GitHub        | Sí         | No (solo guía)          | No                   |
| GitLab        | Sí         | Sí (API remote mirrors) | No                   |
| Bitbucket     | Sí         | No (solo guía)          | No                   |

**Decisiones clave de diseño:**

- **Modelo de config:** `mirrors` es una sección top-level opcional (`omitempty`), compatible hacia atrás con configs existentes. Cada grupo mirror empareja dos cuentas (`account_src`, `account_dst`) con dirección y settings de origen por repo.
- **Tokens de mirror:** Los servidores remotos necesitan PATs portables, no tokens OAuth de GCM locales a la máquina. `ResolveMirrorToken()` exige esto — las cuentas GCM deben guardar un PAT separado para mirrors.
- **Sync inmediato:** Después de crear un push mirror en Forgejo/Gitea, el código dispara `/push_mirrors-sync` para que el primer sync ocurra inmediatamente en vez de esperar al intervalo configurado.
- **Comparación de estado:** `CheckStatus()` consulta SHAs del commit HEAD en origen y backup mediante APIs de proveedor y los compara para determinar el estado de sync.
- **Checks de visibilidad:** Status advierte si los repos backup no son privados.
- **Discovery:** Escanea todos los pares de cuentas buscando relaciones mirror existentes, con confianza decreciente: API de push mirror (confirmado), flag de pull mirror (probable), coincidencia de nombre (posible).

### pkg/terminals, pkg/launch, pkg/harness — acciones Open-in

`pkg/terminals` es dueño del catálogo compilado de terminales y shells soportados por OS, la detección del host, la composición de Profiles según el OS y la lógica de merge que mantiene `terminal_apps[]`, `shells[]` y `terminal_profiles[]` sincronizados con lo que está instalado. `pkg/launch.ResolveArgs` expande la plantilla argv de un Profile para una carpeta, fuera del runtime de Wails para que las reglas de tokens tengan unit tests. `pkg/harness` parsea la tabla embebida [`tools-directory.md`](../../pkg/harness/tools-directory.md) para decidir qué AI CLI harnesses autodetecta la GUI. Consulta [Perfiles de terminal](#perfiles-de-terminal) más abajo para las reglas visibles para el usuario.

### pkg/i18n — idioma de la UI

Resuelve y normaliza el idioma de la UI, en este orden: la variable de entorno `GITBOX_LANG`, `global.language`, el locale del OS y el inglés como fallback. Los catálogos de strings se distribuyen en el frontend Svelte; este paquete solo decide cuál usar.

---

## 4. Formato de config (v3)

La config vive en `~/.config/gitbox/gitbox.json`. Consulta el [ejemplo JSON anotado](../../json/gitbox.jsonc) para una config completa con comentarios, y el [JSON Schema](../../json/gitbox.schema.json) para validación y autocompletado en editor.

**Versionado:** `config.CurrentVersion` es 3. gitbox carga v3 de forma estricta y migra los archivos v1 y v2 de forma transparente al cargar (en memoria; se persiste en el siguiente save). La migración v2 → v3 descarta la antigua sección `workspaces`, que ahora es una caché regenerable. Cualquier otra versión es un error. Los cambios aditivos nunca suben la versión.

**Backups automáticos:** Cada save significativo crea un backup fechado en el mismo directorio (por ejemplo, `gitbox-20260401-143025.json`). Se conservan los 10 más recientes — los más antiguos se eliminan automáticamente. La pantalla de recuperación de corrupción de la GUI puede restaurar cualquiera de ellos con un clic. Los saves solo de posición de ventana (mover o redimensionar la GUI) saltan el paso de backup, para que el churn cosmético no desplace las copias reales pre-corrupción.

**Herencia de tipo de credencial:** Los repos heredan `default_credential_type` de su cuenta salvo que definan su propio `credential_type`.

**Resolución de carpeta:** `globalFolder / sourceFolder / idFolder / cloneFolder`, con overrides posibles en cada nivel. Si `clone_folder` es una ruta absoluta, reemplaza toda la jerarquía.

### Global

| Campo                             | Tipo   | Obligatorio | Descripción                                                                                                                                                                                                                                                                                                                                                                                            |
| --------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `folder`                          | string | Sí          | Directorio raíz para todos los clones. Soporta `~`.                                                                                                                                                                                                                                                                                                                                                    |
| `extra_folders`                   | array  | No          | Directorios raíz adicionales escaneados en busca de clones y archivos `.code-workspace`, además de `folder`.                                                                                                                                                                                                                                                                                           |
| `nested_scan_depth`               | int    | No          | Niveles que gitbox desciende bajo un repo contenedor para encontrar clones anidados. Por defecto `1` (hijos inmediatos).                                                                                                                                                                                                                                                                               |
| `language`                        | string | No          | Idioma de la UI. Vacío significa el locale del OS (inglés como fallback).                                                                                                                                                                                                                                                                                                                              |
| `periodic_sync`                   | string | No          | Intervalo de fetch en segundo plano: off, `5m`, `15m` o `30m`.                                                                                                                                                                                                                                                                                                                                         |
| `view_mode`                       | string | No          | `"full"` o `"compact"`.                                                                                                                                                                                                                                                                                                                                                                                |
| `window` / `compact_window`       | object | No          | Posición y tamaño guardados (`x`, `y`, `width`, `height`) por modo de vista.                                                                                                                                                                                                                                                                                                                           |
| `check_global_gitignore`          | bool   | No          | Ejecuta el check automático de `~/.gitignore_global` al arrancar. Por defecto `true`.                                                                                                                                                                                                                                                                                                                  |
| `pr_badges_enabled`               | bool   | No          | Obtiene y muestra indicadores de PR / review en las filas de clon. Por defecto `true`.                                                                                                                                                                                                                                                                                                                 |
| `pr_include_drafts`               | bool   | No          | Cuenta los PRs draft en el badge "my PRs". Por defecto `true`.                                                                                                                                                                                                                                                                                                                                         |
| `credential_ssh`                  | object | No          | Defaults de plataforma SSH. Su presencia indica que SSH está disponible.                                                                                                                                                                                                                                                                                                                               |
| `credential_ssh.ssh_folder`       | string | No          | Directorio de config SSH. Por defecto `~/.ssh`.                                                                                                                                                                                                                                                                                                                                                        |
| `credential_gcm`                  | object | No          | Defaults de plataforma GCM. Su presencia indica que GCM está disponible.                                                                                                                                                                                                                                                                                                                               |
| `credential_gcm.helper`           | string | No          | Helper de credenciales. Normalmente `"manager"`.                                                                                                                                                                                                                                                                                                                                                       |
| `credential_gcm.credential_store` | string | No          | `"wincredman"`, `"keychain"` o `"secretservice"`.                                                                                                                                                                                                                                                                                                                                                      |
| `credential_token`                | object | No          | Defaults de plataforma Token/PAT. Su presencia indica que la auth por token está disponible.                                                                                                                                                                                                                                                                                                           |
| `editors`                         | array  | No          | Editores de código para el menú "Open in". Se rellena automáticamente en el primer arranque.                                                                                                                                                                                                                                                                                                           |
| `editors[].name`                  | string | Sí          | Nombre visible (por ejemplo `"VS Code"`).                                                                                                                                                                                                                                                                                                                                                              |
| `editors[].command`               | string | Sí          | Ruta completa o nombre de comando (por ejemplo `"C:\\...\\code.cmd"`).                                                                                                                                                                                                                                                                                                                                 |
| `terminals`                       | array  | No          | Lista plana de terminales legacy de antes de Terminal Profiles. Se migra a los tres arrays de abajo en la primera carga.                                                                                                                                                                                                                                                                               |
| `terminals[].name`                | string | Sí          | Nombre visible (por ejemplo `"Windows Terminal"`).                                                                                                                                                                                                                                                                                                                                                     |
| `terminals[].command`             | string | Sí          | Ruta completa o launcher en PATH (por ejemplo `"wt.exe"`, `"gnome-terminal"`).                                                                                                                                                                                                                                                                                                                         |
| `terminals[].args`                | array  | No          | Argumentos pasados antes de la ruta. Usa `"{path}"` como placeholder de la ruta; si falta, la ruta se añade al final. Usa `"{command}"` para marcar dónde se inserta el argv de un AI harness (se expande a cero elementos en lanzamientos solo de terminal).                                                                                                                                          |
| `terminal_apps`                   | array  | No          | Emuladores de terminal detectados. Los rellena el probe del catálogo en `pkg/terminals` (Windows: Windows Terminal, WezTerm, Alacritty, Tabby, ConEmu, Hyper, Mintty, ZOC. macOS: iTerm2, Terminal.app, Warp, Kitty, Ghostty, WezTerm, Alacritty. Linux: GNOME Terminal, Konsole, Terminator, Foot, Alacritty, Kitty, Tilda, Guake, xterm). El Terminals Manager de la GUI hace el probe directamente. |
| `terminal_apps[].id`              | string | Sí          | Id estable (`"wt"`, `"wezterm"`, `"gnome-terminal"`, `"iterm"`, …). Se usa como destino de referencia cruzada desde `terminal_profiles[].terminal`.                                                                                                                                                                                                                                                    |
| `terminal_apps[].name`            | string | Sí          | Nombre visible que se muestra en el Manager y en el launcher por fila.                                                                                                                                                                                                                                                                                                                                 |
| `terminal_apps[].command`         | string | Sí          | Ruta absoluta resuelta (se rellena en tiempo de detección).                                                                                                                                                                                                                                                                                                                                            |
| `terminal_apps[].args_template`   | array  | No          | Plantilla argv con tokens `{path}`, `{shell_command}`, `{shell_args}`, `{command}`. El launcher los expande por Profile mediante `pkg/launch.ResolveArgs`. Mismas reglas de tokens que `terminals[].args` arriba, más `{shell_command}` (el binario de shell resuelto) y `{shell_args}` (un punto de inserción para los args por defecto del shell).                                                   |
| `shells`                          | array  | No          | Shells detectados. Alcance del catálogo por OS — Windows: PowerShell 7, PowerShell 5, CMD, Git Bash, más filas `wsl-<name>` por distro cuando WSL está instalado. macOS: Zsh, Bash, Fish, Dash. Linux: Bash, Zsh, Fish, Ksh, Dash.                                                                                                                                                                     |
| `shells[].id`                     | string | Sí          | Id estable (`"cmd"`, `"pwsh"`, `"git-bash"`, `"wsl-ubuntu"`, …). Referenciado desde `terminal_profiles[].shell`.                                                                                                                                                                                                                                                                                       |
| `shells[].name`                   | string | Sí          | Nombre visible.                                                                                                                                                                                                                                                                                                                                                                                        |
| `shells[].command`                | string | Sí          | Ruta absoluta resuelta.                                                                                                                                                                                                                                                                                                                                                                                |
| `shells[].args`                   | array  | No          | Args por defecto insertados donde el `args_template` del terminal referencia `{shell_args}`.                                                                                                                                                                                                                                                                                                           |
| `terminal_profiles`               | array  | No          | Profiles lanzables. El launcher por fila y el menú `Open in…` leen esta lista cuando está rellena. **Composición según el OS** (issue #71): en Windows cada Profile autoderivado empareja un Terminal × Shell; en macOS / Linux cada uno es solo Terminal, con el login shell del host como shell implícito.                                                                                           |
| `terminal_profiles[].id`          | string | Sí          | Id estable (`"wt+pwsh"`, `"wezterm+launchmenu-mybash"`, `"user-1"`, …).                                                                                                                                                                                                                                                                                                                                |
| `terminal_profiles[].name`        | string | Sí          | Nombre visible (por ejemplo `"Windows Terminal — pwsh"`).                                                                                                                                                                                                                                                                                                                                              |
| `terminal_profiles[].terminal`    | string | Sí          | `terminal_apps[].id` que se lanza. Vacío solo para los Profiles de fallback de shell directo que se emiten en Windows cuando no hay ningún Terminal moderno instalado.                                                                                                                                                                                                                                 |
| `terminal_profiles[].shell`       | string | No          | `shells[].id` que se ejecuta dentro del terminal. Vacío significa "usar el default del terminal" — en macOS / Linux es el login shell del host, que el Manager muestra como un badge atenuado junto al nombre del Terminal.                                                                                                                                                                            |
| `terminal_profiles[].args`        | array  | No          | Argv de override. Cuando está vacío, el launcher usa `terminal_apps[].args_template`. Las filas `launch_menu` de WezTerm guardan aquí la forma completa `start --cwd {path} -- <argv>`.                                                                                                                                                                                                                |
| `terminal_profiles[].default`     | bool   | No          | Marca el Profile que invoca la acción principal del launcher por fila. Mutuamente exclusivo en toda la lista.                                                                                                                                                                                                                                                                                          |
| `terminal_profiles[].preferred`   | bool   | No          | Promociona el Profile a la lista rápida del menú kebab.                                                                                                                                                                                                                                                                                                                                                |
| `terminal_profiles[].hidden`      | bool   | No          | Oculta el Profile de los menús sin borrarlo. Es la única forma de ocultar un Profile autodetectado / importado de WT / importado de WezTerm / migrado (esos reaparecen en el siguiente ciclo de detección si se eliminan).                                                                                                                                                                             |
| `terminal_profiles[].source`      | string | No          | **Campo interno** — nunca se muestra en el Manager. Etiqueta de origen que usa el motor para decidir entre borrar u ocultar: solo los Profiles `"user"` se pueden borrar; las filas `"detected"` / `"wt-profile"` / `"wezterm-launchmenu"` / `"migrated"` solo se pueden ocultar.                                                                                                                      |
| `ai_harnesses`                    | array  | No          | AI CLI harnesses para el menú "Open in". Se detectan en segundo plano (poco después del arranque, cada 10 minutos, al recuperar el foco) a partir del catálogo embebido más `~/.local/bin`, los prefijos de Homebrew y los directorios conocidos por herramienta. Se lanzan dentro del shell del Terminal Profile por defecto (consulta `terminal_profiles[].default`).                                |
| `ai_harnesses[].name`             | string | Sí          | Nombre visible (por ejemplo `"Claude Code"`).                                                                                                                                                                                                                                                                                                                                                          |
| `ai_harnesses[].command`          | string | Sí          | Ruta absoluta o binario en PATH (por ejemplo `"claude"`).                                                                                                                                                                                                                                                                                                                                              |
| `ai_harnesses[].args`             | array  | No          | Args extra opcionales para el harness. Normalmente vacío.                                                                                                                                                                                                                                                                                                                                              |
| `ai_harnesses[].source`           | string | No          | **Campo interno.** Las entradas `"detected"` las añadió el sync de harnesses y pueden ver su `command` re-resuelto tras una reinstalación; las entradas `"user"` nunca se alteran más allá de `missing`. Vacío en entradas preexistentes hasta que el primer sync las clasifica.                                                                                                                       |
| `ai_harnesses[].missing`          | bool   | No          | Lo pone el sync de harnesses cuando el binario ya no se encuentra; la entrada se oculta de los menús pero se conserva para que una reinstalación la restaure con sus `args` intactos. Se limpia automáticamente cuando el binario reaparece.                                                                                                                                                           |

### Account

| Campo                     | Tipo    | Obligatorio | Descripción                                                                               |
| ------------------------- | ------- | ----------- | ----------------------------------------------------------------------------------------- |
| `provider`                | string  | Sí          | `"github"`, `"gitlab"`, `"gitea"`, `"forgejo"`, `"bitbucket"`, `"generic"`                |
| `url`                     | string  | Sí          | URL del servidor (scheme+host, sin ruta).                                                 |
| `username`                | string  | Sí          | Username de la cuenta.                                                                    |
| `name`                    | string  | Sí          | `git user.name` por defecto.                                                              |
| `email`                   | string  | Sí          | `git user.email` por defecto.                                                             |
| `default_credential_type` | string  | No          | Auth por defecto: `"gcm"`, `"ssh"` o `"token"`.                                           |
| `ssh.host`                | string  | Condicional | Alias SSH Host (por ejemplo, `"gt-myuser"`). **Obligatorio** cuando SSH está configurado. |
| `ssh.hostname`            | string  | No          | Hostname SSH real. Se deriva automáticamente de la URL si se omite.                       |
| `ssh.key_type`            | string  | Condicional | `"ed25519"` o `"rsa"`. **Obligatorio** cuando SSH está configurado.                       |
| `gcm.provider`            | string  | No          | Pista de proveedor para GCM.                                                              |
| `gcm.useHttpPath`         | boolean | No          | Acota las credenciales por ruta HTTP.                                                     |

Una cuenta es única por `(hostname, username)`. Borrar una cuenta recorre en cascada cada source, mirror y workspace que la referencia.

### Source

| Campo     | Tipo   | Obligatorio | Descripción                                                                 |
| --------- | ------ | ----------- | --------------------------------------------------------------------------- |
| `account` | string | Sí          | Referencia una clave de cuenta.                                             |
| `folder`  | string | No          | Sobrescribe la carpeta de clone de primer nivel. Por defecto: clave source. |
| `repos`   | object | Sí          | Repos con clave `org/repo`.                                                 |

### Repo (dentro de source.repos)

| Campo             | Tipo   | Obligatorio | Descripción                                                                          |
| ----------------- | ------ | ----------- | ------------------------------------------------------------------------------------ |
| `credential_type` | string | No          | Sobrescribe el método de auth. Se hereda de la cuenta.                               |
| `name`            | string | No          | Sobrescribe `git user.name`.                                                         |
| `email`           | string | No          | Sobrescribe `git user.email`.                                                        |
| `id_folder`       | string | No          | Sobrescribe el directorio de 2º nivel (carpeta org).                                 |
| `clone_folder`    | string | No          | Sobrescribe el directorio de 3er nivel. Si es absoluto, reemplaza toda la ruta.      |
| `container`       | bool   | No          | Lo marca como contenedor multi-repo; gitbox escanea dentro buscando clones anidados. |

### Mirrors

```json
{
  "mirrors": {
    "forgejo-github": {
      "account_src": "my-forgejo",
      "account_dst": "github-personal",
      "repos": {
        "infra/homelab": {
          "direction": "push",
          "origin": "src",
          "method": "api",
          "status": "active"
        },
        "MyUser/dotfiles": {
          "direction": "pull",
          "origin": "dst",
          "method": "api",
          "status": "active"
        }
      }
    }
  }
}
```

| Campo                     | Tipo   | Obligatorio | Descripción                                                              |
| ------------------------- | ------ | ----------- | ------------------------------------------------------------------------ |
| `account_src`             | string | Sí          | Clave de la cuenta origen                                                |
| `account_dst`             | string | Sí          | Clave de la cuenta destino (debe ser distinta de src)                    |
| `repos.<key>.direction`   | string | Sí          | `"push"` o `"pull"`                                                      |
| `repos.<key>.origin`      | string | Sí          | `"src"` o `"dst"` — qué cuenta es la fuente de verdad                    |
| `repos.<key>.target_repo` | string | No          | Sobrescribe el nombre del repo destino (por defecto: igual que la clave) |
| `repos.<key>.method`      | string | No          | `"api"` o `"manual"`                                                     |
| `repos.<key>.status`      | string | No          | `"active"`, `"pending"`, `"error"`, `"paused"`                           |
| `repos.<key>.last_sync`   | string | No          | Timestamp RFC3339 del último sync conocido                               |
| `repos.<key>.error`       | string | No          | Último mensaje de error                                                  |

### Caché de workspaces

La sección `workspaces` es una caché regenerable — se puede borrar sin riesgo y volver a descubrir. Las entradas son siempre workspaces de VS Code descubiertos (sin `type`/`layout`).

```json
{
  "workspaces": {
    "sumwall": {
      "name": "sumwall",
      "file": "/home/me/00.git/.../sumwall.project/sumwall.code-workspace",
      "members": [
        { "source": "github-org", "repo": "Org/browser" },
        { "source": "github-org", "repo": "Org/services" }
      ],
      "discovered": true
    }
  }
}
```

| Campo        | Tipo   | Descripción                                                                 |
| ------------ | ------ | --------------------------------------------------------------------------- |
| `name`       | string | Nombre visible (el nombre del archivo `.code-workspace` sin extensión)      |
| `file`       | string | Ruta absoluta al archivo `.code-workspace` descubierto                      |
| `members`    | array  | Clones miembro resueltos desde las carpetas del archivo (`source` + `repo`) |
| `discovered` | bool   | Siempre `true` — las entradas se descubren, nunca se escriben a mano        |

### Perfiles de terminal

El trío `terminal_apps[]` + `shells[]` + `terminal_profiles[]` pertenece a `pkg/terminals`. El paquete incluye un catálogo compilado de Terminals + Shells soportados por OS — ese es el vocabulario que gitbox sabe detectar y lanzar. Añadir una entrada nueva de emulador de terminal es un cambio de código en `pkg/terminals/catalog.go`.

En cada arranque el catálogo sondea el host y reconcilia el resultado con lo que ya hay en `gitbox.json`:

- Las entradas del catálogo que el host tiene instaladas se añaden a `terminal_apps[]` / `shells[]` (solo si faltan — las filas existentes sobreviven a la re-detección).
- Los flags hidden sobreviven a la re-detección — ocultar Mintty en esta sesión lo mantiene oculto tras upgrades que amplían el catálogo.
- Los Profiles añadidos por el usuario (`source: "user"`) y los Profiles legacy migrados (`source: "migrated"`) se conservan tal cual, aunque no estén en el conjunto recién detectado.
- Las entradas del catálogo no instaladas se omiten en silencio — reaparecen automáticamente cuando el usuario instala el binario.

#### Composición según el OS

El conjunto de Profiles autoderivados sigue reglas distintas por plataforma:

- **Windows** — Cada Profile empareja un Terminal × Shell. Los auto-Profiles de shell directo (una fila cuyo Terminal es el propio shell) no se emiten cuando hay al menos un Terminal moderno instalado. Cuando no hay ningún Terminal moderno instalado, gitbox recurre a un Profile de shell directo por shell para que el usuario no se quede tirado — y muestra un banner en el Manager: "Install Windows Terminal for the best experience."
- **macOS / Linux** — Cada Profile es solo Terminal (`terminal_profiles[].shell == ""`). El login shell del host es implícito — `pkg/launch.ResolveArgs` reduce los tokens de shell vacíos a cero elementos, y el Manager muestra el login shell como un badge de metadatos atenuado junto al nombre del Terminal. Los usuarios avanzados pueden seguir emparejando un Terminal con un Shell que no sea el de login mediante el formulario `+ Add profile` del Manager; la fila resultante se marca con `source: "user"`.

El formulario Add-Profile refleja estas reglas: en macOS / Linux el selector de shell incluye una entrada virtual `(login shell)` como valor por defecto; en Windows elegir shell es obligatorio.

#### Cómo funciona el emparejamiento de lanzamiento

Cuando hago clic en un Profile `WezTerm — PowerShell 7` o `Windows Terminal — PowerShell 7`, gitbox NO ejecuta sin más la plantilla genérica por Terminal. Primero consulta mi propia config de terminal buscando una entrada que coincida, y solo recurre a la plantilla genérica cuando no encuentra ninguna.

La búsqueda se ejecuta en cada lanzamiento (con una caché en proceso invalidada por mtime, así que las ediciones de `wezterm.lua` / `settings.json` se recogen sin reiniciar):

- **WezTerm** — gitbox parsea `wezterm.lua` (`$WEZTERM_CONFIG_FILE`, luego `$XDG_CONFIG_HOME/wezterm/wezterm.lua`, luego `~/.config/wezterm/wezterm.lua`, luego `~/.wezterm.lua`) y busca una entrada de `config.launch_menu` cuya label coincida con el shell de gitbox. Si la encuentra, gitbox lanza `wezterm-gui.exe start --cwd <path> -- <entry args>` e inserta los `set_environment_variables` de la entrada sobre el entorno del padre. El parser se ata específicamente a la tabla documentada `config.launch_menu` — si mi config guarda las entradas en una variable propia `local profiles = { … }` que alimenta un selector con keybinding propio, gitbox no puede descubrirlas y recurre a la plantilla genérica. Para que gitbox vea las entradas de un selector propio, haz un alias con `config.launch_menu = profiles` al final de `wezterm.lua` (una línea, sin impacto en el comportamiento del keybinding existente). Lo que gitbox NO reproduce es ningún callback Lua del selector configurado en `wezterm.lua` (`color_scheme` por entrada, overrides de `mux.spawn_window`, handlers de `window-focus-changed`, etc.) — esos solo se disparan cuando se elige una entrada desde el propio menú launcher de WezTerm, nunca cuando un pane se crea desde fuera.
- **Windows Terminal** — gitbox parsea `settings.json` (instalación Store, instalación Preview y luego instalación sin empaquetar bajo `%LOCALAPPDATA%`) y busca un perfil en `profiles.list` cuyo `name` coincida con el shell de gitbox. Si lo encuentra, gitbox ejecuta `wt.exe -w 0 nt --profile "<name>" -d <path>` — el propio `wt.exe` lee el `commandline`, la fuente, los colores y los flags de inicio del perfil desde `settings.json`. El prefijo `-w 0 nt` fija la pestaña nueva a la ventana WT existente más reciente (o crea una si no existe ninguna) para que un ajuste `firstWindowPreference: persistedWindowLayout` en `settings.json` no abra una segunda ventana junto a la nuestra cuando WT se cerró con pestañas guardadas.
- **Sin coincidencia / sin config / terminal no instalado** — gitbox recurre a la plantilla argv genérica (`wezterm-gui.exe start --cwd <path> -- <shell> <args>`, `wt.exe -d <path> <shell> <args>`, etc.). Ese es el comportamiento correcto para shells que no he conectado a mi config de terminal.

Los Profiles DIRECT de shell directo (los cuatro atajos `pwsh / powershell / cmd / wsl` ocultos por defecto en Windows) se saltan la búsqueda — no tienen config de terminal que consultar, así que la plantilla genérica "ejecutar el shell directamente" es la correcta.

El matcher de nombres de shell es tolerante:

- Coincidencia directa — el nombre normalizado de la entrada es igual al nombre visible del shell de gitbox (`"PowerShell 7"` ≡ `"PowerShell 7"`, `"WSL — Ubuntu-24.04"` ≡ `"WSL — Ubuntu-24.04"`).
- Sufijo tras raya larga — para nombres de gitbox como `"WSL — Ubuntu-24.04"`, también coincide una entrada etiquetada solo como `"Ubuntu-24.04"`.
- Fallback por patrón — `pwsh` coincide con entradas que contienen `"powershell 7"`, `"powershell core"` o `"pwsh"`; `powershell` coincide con `"powershell 5"` o `"windows powershell"`; `cmd` coincide con `"command prompt"` o `"cmd exe"`; `git-bash` coincide con `"git bash"`; `wsl-<distro>` coincide con el slug pelado de la distro (`"ubuntu 24 04"`).

---

## 5. Arquitectura de credenciales

<p align="center">
  <img src="../diagrams/credential-flow.png" alt="Credential Flow" width="800" />
</p>

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Credential Types" width="800" />
</p>

### Flujo token

El usuario elige Token en los ajustes de credenciales de la cuenta -> la app muestra la URL de creación PAT específica del proveedor con los scopes necesarios -> el usuario pega el token -> la app lo valida mediante la API del proveedor -> lo almacena en el archivo de credenciales (`~/.config/gitbox/credentials/<key>`). Al clonar, el token se embebe temporalmente en la URL para autenticación y luego se elimina de la URL remota. El `.git/config` por repo se configura con `credential.helper = store --file <path>` apuntando al mismo archivo credential store gestionado por gitbox, así los `git push/pull` posteriores desde cualquier terminal funcionan sin GCM.

### Flujo GCM

El usuario elige GCM en los ajustes de credenciales de la cuenta -> la app dispara `git credential fill`, que abre OAuth en navegador -> la app ejecuta `git credential approve` para persistir -> prueba acceso API con el token de GCM. Clone usa HTTPS con username. El `.git/config` por repo se configura con `credential.helper = manager` más `username`, `provider` y `credentialStore` por host, haciendo cada clone autocontenido. El acceso API extrae el token OAuth mediante `git credential fill`.

### Flujo SSH

El usuario elige SSH en los ajustes de credenciales de la cuenta -> la app crea una entrada en `~/.ssh/config` y genera un par de claves ed25519 -> muestra la clave pública para que el usuario la registre en su proveedor -> prueba la conexión SSH. Clone usa URLs `git@<host-alias>:repo.git` enrutadas mediante SSH config. El acceso API usa opcionalmente un PAT guardado por separado para discovery. El `.git/config` por repo define un `credential.helper =` vacío para cancelar defensivamente cualquier helper de credenciales global.

### Cambio de tipo de credencial

Al cambiar el tipo de credencial de una cuenta, gitbox (`ops.ChangeCredentialType` + `ops.ReconfigureClones`):

1. **Limpia artefactos antiguos** según el tipo actual (entradas de keyring, archivos credential store, claves SSH, credenciales GCM cacheadas)
2. **Actualiza la config de cuenta** con el tipo nuevo y el subobjeto de credenciales
3. **Reconfigura todos los clones existentes** — actualiza URLs remotas y config de credenciales por repo

La matriz de limpieza asegura que no persistan credenciales fantasma entre cambios de tipo:

| De -> A      | Keyring `gitbox:<key>` | Archivo credential store | GCM `git:https://` | Claves SSH + config |
| ------------ | ---------------------- | ------------------------ | ------------------ | ------------------- |
| GCM -> Token | ---                    | ---                      | Eliminado          | ---                 |
| GCM -> SSH   | ---                    | ---                      | Eliminado          | ---                 |
| Token -> GCM | Eliminado              | Eliminado                | ---                | ---                 |
| Token -> SSH | Eliminado              | Eliminado                | ---                | ---                 |
| SSH -> Token | Eliminado (discovery)  | ---                      | ---                | Eliminado           |
| SSH -> GCM   | Eliminado (discovery)  | ---                      | ---                | Eliminado           |

---

## 6. Arquitectura GUI

La GUI es una app desktop Wails v2 con frontend Svelte. El backend Go (`cmd/gui/app.go`) expone métodos que el frontend llama mediante bindings TypeScript autogenerados. El puente del frontend está en `cmd/gui/frontend/src/lib/bridge.ts`.

Las operaciones largas (clone, refresh de status, pull, mirror discovery, movimientos de repo) se ejecutan en goroutines con progreso enviado al frontend mediante eventos Wails.

**Estructura de layout:**

- **Top bar** — logo, repo health ring, mirror health ring (cuando existen mirrors), botones de acción (Pull All, Fetch All, Delete mode, Compact view)
- **Tab bar** — cambia entre las vistas Accounts, Mirrors y Workspaces
- **Tab Accounts** — account cards (con sync rings, credential badges, botones Find projects/Create repo) + lista de detalle de repos
- **Tab Mirrors** — mirror group cards (con sync rings, status dots) + lista de detalle de mirrors con status por repo, botones Discover y Check all
- **Tab Workspaces** — archivos `.code-workspace` descubiertos con sus clones miembro resueltos
- **Summary footer** — contadores agregados de repos y mirrors, más la píldora de actualización cuando existe una release más nueva
- **Compact view** — modo sidebar estrecho con health ring, account pills y mirror summary pill

**Features adicionales:**

- **Config auto-backup:** Los saves significativos crean un backup fechado (ventana rolling de los 10 más recientes) antes de sobrescribir. Los saves solo de posición de ventana saltan el backup — el churn cosmético desplazaría fuera del ring copias reales pre-corrupción.
- **Persistencia de estado de ventana:** Posición y tamaño se guardan por modo de vista (`window` y `compact_window` en config), restaurados al lanzar.
- **Autostart:** Registro de autostart específico por plataforma (launch agent macOS, registry Windows). Configurable desde la GUI.
- **Create repo:** Los repos se pueden crear directamente en proveedores (bajo namespace de usuario u org) desde el tab Accounts, con dropdown owner poblado mediante `OrgLister`.
- **Ediciones externas:** Cuando `gitbox.json` cambia en disco, la GUI lo recarga cuando la ventana recupera el foco.

Consulta la [guía GUI](gui-guide.md) para el walkthrough orientado a usuarios.

---

## 7. Principios de diseño UX

El usuario NO debería necesitar conocer internos de Git — las acciones son verbos que hacen lo que dicen.

**Feedback:**

- El status se actualiza por repo a medida que ocurre (no batched)
- Las operaciones largas muestran progress bars y luego saltan al estado final
- Silencioso por defecto — la UI resalta errores, warnings y repos que necesitan atención; los repos clean se quedan tranquilos

**Los colores de estado** son coherentes en rings, badges y filas. La [guía GUI](gui-guide.md#comprobación-automática) enumera qué significa cada estado y color.

**Reglas de comportamiento:** Las listas siguen el orden del archivo de config. Las acciones son idempotentes. Los errores dicen al usuario qué hacer, no solo qué salió mal. Los tokens nunca se muestran.

---

## 8. Seguridad

- **Los tokens NUNCA se almacenan en el archivo JSON de config.** Los PATs viven en archivos formato git-credential-store (`~/.config/gitbox/credentials/<key>`) con permisos 0600. Los tokens OAuth de GCM viven en el credential store del OS (Windows Credential Manager, macOS Keychain, Linux Secret Service) gestionado por Git Credential Manager.
- **El archivo config no contiene secretos** — solo URLs, usernames, rutas de carpeta y flags de preferencia.
- **Las llamadas API de proveedor usan tokens de archivos de credenciales o GCM** en runtime, nunca desde config.
- **Las claves privadas SSH** son archivos estándar de `~/.ssh/` con permisos adecuados (600).
- **Las URLs de clone se sanitizan** — las URLs de clone autenticadas con token eliminan el token del remoto después de clonar. Las operaciones git posteriores autentican mediante el helper de credenciales por repo, no mediante la URL.
- **Aislamiento de credenciales por repo** — el `.git/config` de cada clone cancela helpers de credenciales globales y define el suyo, evitando fugas de credenciales entre cuentas y eliminando credenciales fantasma de refresh tokens OAuth de GCM.
- **Los archivos credential store** (`~/.config/gitbox/credentials/<key>`) son plaintext con permisos 0600 — el mismo modelo de seguridad que `~/.git-credentials` y las claves privadas SSH.
- **Sin secretos en salida** — los tokens nunca se muestran, ni siquiera en mensajes de error.
- **El repositorio es público** — no hay hostnames, usernames, emails ni tokens reales en archivos versionados.

---

## 9. Diagramas

Los diagramas de arquitectura están disponibles en `docs/diagrams/` como archivos `.drawio` editables:

- **architecture-overview.drawio** — Diagrama de componentes high-level del sistema
- **credential-flow.drawio** — Flujo de resolución de credenciales por tipo
- **credential-types.drawio** — Qué secretos alimentan Discovery, Git Operations y Mirrors por tipo de credencial
- **config-model.drawio** — Modelo de datos Accounts / Sources / Repos / Mirrors

Se pueden abrir y editar con [draw.io](https://app.diagrams.net/) o la extensión drawio de VS Code.
