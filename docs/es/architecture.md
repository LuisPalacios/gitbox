# Gitbox — arquitectura y diseño

Para la visión general del producto (qué hace gitbox, para quién es y por qué existe), consulta el [README](../../README.es.md).

---

## 1. Resumen de arquitectura

gitbox es un monorepo Go que produce dos binarios desde una librería compartida, con tres modos de runtime:

<p align="center">
  <img src="../diagrams/architecture-overview.png" alt="Architecture Overview" width="800" />
</p>

| Binario         | Modo | Propósito                             | Tecnología                  | Auth            |
| --------------- | ---- | ------------------------------------- | --------------------------- | --------------- |
| **`gitbox`**    | CLI  | Power users, servidores headless, CI  | Go + Cobra                  | GCM, SSH, Token |
| **`gitbox`**    | TUI  | Terminal interactiva (sin args + tty) | Go + Bubble Tea + Lip Gloss | GCM, SSH, Token |
| **`GitboxApp`** | GUI  | Usuarios desktop                      | Go + Wails v2 + Svelte      | GCM (guiado)    |

La TUI se lanza automáticamente cuando `gitbox` se ejecuta sin argumentos y stdin es una terminal. Si no, se ejecutan los comandos CLI de Cobra. Ambos binarios comparten exactamente la misma librería `pkg/` — ni la TUI ni la GUI reimplementan lógica que la CLI ya tiene.

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

- **Token** usa almacenamiento dual: keyring del OS (para llamadas API de gitbox) + archivo `credential-store` por repo (para la CLI git). El archivo se deriva de la entrada del keyring.
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
      tools/
  forgejo-work/
    infra/
      homelab-ops/
```

Cada nivel puede sobrescribirse:

- **1er nivel**: `source.folder` sobrescribe la clave source
- **2º nivel**: `repo.id_folder` sobrescribe la parte org
- **3er nivel**: `repo.clone_folder` sobrescribe el nombre de repo (si es ruta absoluta, reemplaza todo)

---

## 3. Diseño de componentes

### pkg/config — gestión de configuración

Gestiona el archivo de configuración v2. Tipos core: `Config`, `Account`, `Source`, `Repo`. Consulta `pkg/config/config.go` para las definiciones de structs.

**Decisiones clave de diseño:**

- **Preservación de orden JSON:** `SourceOrder` y `RepoOrder` aseguran que la iteración siga el orden del archivo de config del usuario
- **Herencia de credenciales:** los repos heredan `default_credential_type` de su cuenta salvo que lo sobrescriban
- **CRUD con integridad referencial:** `DeleteAccount` falla si alguna source la referencia; `DeleteSource` elimina en cascada sus repos

### pkg/credential — gestión de credenciales

Gestiona tokens, claves SSH, integración GCM y aislamiento de credenciales por repo. Consulta `pkg/credential/credential.go`, `pkg/credential/validate.go` y `pkg/credential/repoconfig.go`.

**Cadena de resolución de token:** Variable de entorno (`GITBOX_TOKEN_<KEY>`) -> fallback `GIT_TOKEN` -> archivo de credenciales (`~/.config/gitbox/credentials/<key>`).

**Dispatch de token API:** Enruta por tipo de credencial — `token` usa el archivo de credenciales, `gcm` ejecuta `git credential fill` (con fallback al archivo de credenciales), `ssh` prueba el archivo de credenciales (PAT opcional para discovery).

**Gestión de claves SSH:** Genera pares de claves, escribe entradas `~/.ssh/config`, prueba conexiones. Convención de nombre: alias de host `gitbox-<account-key>`, archivo de clave `gitbox-<account-key>-sshkey`.

**Config de credenciales por repo** (`repoconfig.go`): `ConfigureRepoCredential()` configura el `.git/config` de cada clone para ser autocontenido. `WriteCredentialFile()` y `RemoveCredentialFile()` gestionan los archivos git-credential-store administrados por gitbox para cuentas token. Tanto CLI como GUI llaman a las mismas funciones compartidas.

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

En macOS, `GitBin()` prueba rutas de Homebrew (`/opt/homebrew/bin/git`, `/usr/local/bin/git`) antes de hacer fallback a PATH, asegurando que las apps GUI encuentren git con GCM incluso con el PATH mínimo que heredan las apps GUI en macOS.

### pkg/status — comprobación de estado de sync

Determina el estado de sync de clones locales respecto a su upstream. Estados: Clean, Dirty, Behind, Ahead, Diverged, Conflict, NotCloned, NoUpstream, Error. Prioridad: Conflicts > Dirty > Diverged > Behind > Ahead > NoUpstream > Clean. Consulta `pkg/status/status.go`.

### pkg/identity — gestión de identidad Git

Gestiona identidad git por repo (`user.name`, `user.email`) con una cadena de resolución: los overrides a nivel repo hacen fallback a valores a nivel cuenta. Consulta `pkg/identity/identity.go`.

`EnsureRepoIdentity()` comprueba la git config local de cada clone y arregla la identidad si diverge de los valores esperados. `CheckGlobalIdentity()` y `RemoveGlobalIdentity()` gestionan la identidad global de `~/.gitconfig` — gitbox anima a eliminar la identidad global para que la identidad por repo (definida durante clone/reconfigure) sea siempre autoritativa.

En paralelo al check de identidad, `pkg/credential` expone `IsGlobalGCMConfigNeeded()` + `CheckGlobalGCMConfig()` + `FixGlobalGCMConfig()` para el helper de credenciales GCM global. Cuando al menos una cuenta usa GCM, gitbox verifica que `~/.gitconfig` tenga `credential.helper = manager` y `credential.credentialStore = <keychain|wincredman|secretservice>`; si falta o está mal, la GUI / TUI muestran un botón fix que escribe ambas entradas y rellena defaults del OS en `gitbox.json`. Sin esto, `git credential fill` cae a `/dev/tty` y falla con "Device not configured" en contextos GUI.

### pkg/update — auto-update

Proporciona comprobación de versión y self-update mediante GitHub Releases. `CheckLatest()` consulta la API de GitHub (limitado a una vez cada 24h). `DownloadRelease()` descarga el artefacto específico de la plataforma y verifica su checksum SHA256. `Apply()` extrae el zip y reemplaza binarios in place — en Unix mediante rename atómico, en Windows renombrando primero el binario en ejecución a `.old` (`CleanupOldBinary()` elimina archivos `.old` stale en el siguiente arranque).

### pkg/doctor — detección de herramientas externas

Sondea el host para cada binario externo que gitbox puede llamar — `git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, `tmux`, `tmuxinator`, `wsl` (solo Windows) — y reporta ruta, versión y sugerencias de instalación por OS para cualquier ausente. `PrecheckForCredentialType()` alimenta los flujos de setup GUI y TUI para que una herramienta faltante aparezca como banner amarillo con el comando de instalación en vez de un error críptico de runtime. La salida también está disponible como `gitbox doctor` (humana o `--json`) con exit code `1` cuando falta cualquier herramienta requerida.

### pkg/heal — self-heal de .git/config de repo

`heal.Repo(cfg, sourceKey, repoKey)` reconcilia idempotentemente el `.git/config` de un clone contra la spec de cuenta: `user.name`, `user.email`, la URL canónica de `origin` (específica del tipo de credencial, sin secretos embebidos) y el helper de credenciales. Está conectado a cada punto de disparo — clone, fetch y pull en CLI / TUI / GUI, y el sync periódico de la GUI — para que los clones que se desvíen de la spec se reparen silenciosamente. Se introdujo junto con el fix de stdio GUI en Windows en `pkg/git.run()` para evitar que las escrituras de `.git/config` desde GUI fallasen silenciosamente.

### pkg/move — movimiento de repo cross-account / cross-provider

Reubica un clone de una cuenta configurada a otra, incluso entre proveedores (GitHub → GitLab, Gitea → Forgejo). Orquesta un flujo por fases — preflight (probe de readiness de credenciales) → fetch → crear destino → `git push --mirror` → rewire `origin` → actualizar `gitbox.json` → borrado opcional de remoto fuente → borrado opcional de clone local → auto-clone destino cuando se borró el local. Los callbacks de fase envían progreso a GUI y TUI. Las fases 1–6 son fatales si fallan; los borrados opcionales son best-effort y aparecen como warnings con `provider.InsufficientScopesError` humanizado a texto de remediación (por ejemplo, "add `delete_repo` scope to your GitHub PAT").

### pkg/gitignore — self-heal de gitignore global

Instala un bloque gestionado curado de patrones de basura de OS (`.DS_Store`, `Thumbs.db`, `*~`, …) en `~/.gitignore_global` y apunta `core.excludesfile` a él. El bloque se envuelve en marcadores sentinel para que las reinstalaciones reescriban solo la región gestionada; las entradas añadidas por el usuario y patrones de negación fuera de los sentinels se conservan. Escritura atómica tmp+rename con un límite rolling de 3 backups (`.bak-YYYYMMDD-HHMMSS`). Opt-out mediante `global.check_global_gitignore` en `gitbox.json`, que gatea solo el check automático de arranque; las acciones explícitas siempre se ejecutan. Expuesto como `gitbox gitignore check|install`, el shortcut `G` de TUI y un banner GUI con botón **Install**.

### pkg/workspace — workspaces multi-repo basados en tareas

Genera archivos multi-root `.code-workspace` de VS Code (con colocación del archivo en el ancestro común más cercano y un bloque mínimo de settings que deja a VS Code detectar repos anidados) y perfiles YAML de tmuxinator (`windowsPerRepo` por defecto, `splitPanes` alternativo). `BuildOpenCommand()` devuelve la invocación para el primer editor en `global.editors` o la primera terminal que ejecuta `tmuxinator start <key>`. En Windows, tmuxinator se enruta a través de WSL: YAML se escribe en el `~/.tmuxinator/<key>.yml` del lado WSL mediante su ruta UNC `\\wsl.localhost\…`, y los lanzamientos ejecutan `wsl.exe -- tmuxinator start <key>` independientemente de la terminal host. `pkg/workspace/discover.go` recorre la carpeta gestionada por gitbox y `~/.tmuxinator/` (más el lado WSL en Windows), compara rutas parseadas contra clones conocidos mediante deepest-prefix match y auto-adopta matches no ambiguos con `discovered: true`; los matches ambiguos se muestran para revisión humana y nunca se auto-adoptan.

### pkg/adopt — discovery de repos orphan

Escanea la carpeta gestionada por gitbox en busca de clones que no están en `gitbox.json` y puntúa cada uno contra cada cuenta cuyo host coincide. Señales de puntuación (aditivas): owner igual a `account.username` (+3), repo vive bajo la carpeta source de la cuenta (+5), URL HTTPS embebe `user@` coincidente con el username (+10), `.git/config` tiene `credential.<url>.username` igual al username (+10). Los empates aparecen como ambiguos y nunca se auto-adoptan; los orphans adoptados reciben aislamiento de credenciales, identidad y una reubicación opcional a la estructura estándar de carpetas.

### pkg/mirror — mirroring de repositorios

Gestiona setup de push y pull mirror, comprobación de estado y guías de setup manual. Los mirrors mantienen copias backup de repos en otro proveedor sin clonar localmente.

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
- **Tokens de mirror:** Los servidores remotos necesitan PATs portables, no tokens OAuth de GCM locales a la máquina. `ResolveMirrorToken()` exige esto — las cuentas GCM deben guardar un PAT separado mediante `credential setup --token`.
- **Sync inmediato:** Después de crear un push mirror en Forgejo/Gitea, el código dispara `/push_mirrors-sync` para que el primer sync ocurra inmediatamente en vez de esperar al intervalo configurado.
- **Comparación de estado:** `CheckStatus()` consulta SHAs del commit HEAD en origen y backup mediante APIs de proveedor y los compara para determinar el estado de sync.
- **Checks de visibilidad:** Status advierte si los repos backup no son privados.

---

## 4. Formato de config (v2)

Consulta el [ejemplo JSON anotado](../../json/gitbox.jsonc) para una config completa con comentarios, y el [JSON Schema](../../json/gitbox.schema.json) para validación y autocompletado en editor.

**Herencia de tipo de credencial:** Los repos heredan `default_credential_type` de su cuenta salvo que definan su propio `credential_type`.

**Resolución de carpeta:** `globalFolder / sourceFolder / idFolder / cloneFolder`, con overrides posibles en cada nivel. Si `clone_folder` es una ruta absoluta, reemplaza toda la jerarquía.

---

## 5. Arquitectura de credenciales

<p align="center">
  <img src="../diagrams/credential-flow.png" alt="Credential Flow" width="800" />
</p>

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Credential Types" width="800" />
</p>

### Flujo token

El usuario ejecuta `credential setup` -> la app muestra la URL de creación PAT específica del proveedor con los scopes necesarios -> el usuario pega el token -> la app lo valida mediante la API del proveedor -> lo almacena en el archivo de credenciales (`~/.config/gitbox/credentials/<key>`). Al clonar, el token se embebe temporalmente en la URL para autenticación y luego se elimina de la URL remota. El `.git/config` por repo se configura con `credential.helper = store --file <path>` apuntando al mismo archivo credential store gestionado por gitbox, así los `git push/pull` posteriores desde cualquier terminal funcionan sin GCM.

### Flujo GCM

El usuario ejecuta `credential setup` -> la app dispara `git credential fill`, que abre OAuth en navegador -> la app ejecuta `git credential approve` para persistir -> prueba acceso API con el token de GCM. Clone usa HTTPS con username. El `.git/config` por repo se configura con `credential.helper = manager` más `username`, `provider` y `credentialStore` por host, haciendo cada clone autocontenido. El acceso API extrae el token OAuth mediante `git credential fill`.

### Flujo SSH

El usuario ejecuta `credential setup` -> la app crea una entrada en `~/.ssh/config` y genera un par de claves ed25519 -> muestra la clave pública para que el usuario la registre en su proveedor -> prueba la conexión SSH. Clone usa URLs `git@<host-alias>:repo.git` enrutadas mediante SSH config. El acceso API usa opcionalmente un PAT guardado por separado para discovery. El `.git/config` por repo define un `credential.helper =` vacío para cancelar defensivamente cualquier helper de credenciales global.

### Cambio de tipo de credencial

Al cambiar el tipo de credencial de una cuenta, gitbox:

1. **Limpia artefactos antiguos** según el tipo actual (entradas de keyring, archivos credential store, credenciales GCM cacheadas)
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

## 6. Estructura de comandos CLI

<p align="center">
  <img src="../diagrams/cli-workflow.png" alt="CLI Workflow" width="800" />
</p>

### Árbol de comandos

```text
gitbox
|- init                              Inicializar archivo de config
|- global [--folder] [--periodic-sync]  Definir settings globales
|- account
|  |- list                           Listar todas las cuentas
|  |- add <key> --provider ...       Añadir una cuenta
|  |- update <key> --name ...        Actualizar campos de cuenta
|  |- delete <key>                   Borrar una cuenta
|  |- show <key>                     Mostrar cuenta como JSON
|  |- orgs <key>                     Listar organizaciones del proveedor
|  |- credential
|  |  |- setup <key>                 Configurar credenciales (idempotente)
|  |  |- verify <key>                Verificar que las credenciales funcionan
|  |  +- del <key>                   Eliminar credenciales
|  +- discover <key>                 Descubrir repos desde API del proveedor
|- source
|  |- list                           Listar todas las sources
|  |- add <key> --account ...        Añadir una source
|  |- update <key> --folder ...      Actualizar campos de source
|  |- delete <key>                   Borrar source + sus repos
|  +- show <key>                     Mostrar source como JSON
|- repo
|  |- list [--source <key>]          Listar repos (filtro opcional)
|  |- add <source> <repo>            Añadir un repo a una source
|  |- update <source> <repo> ...     Actualizar campos de repo
|  |- delete <source> <repo>         Eliminar un repo
|  +- show <source> <repo>           Mostrar repo como JSON
|- clone [--source] [--repo]         Clonar repos configurados
|- pull [--source] [--repo]          Hacer pull de repos behind
|- fetch [--source] [--repo]         Hacer fetch sin pull
|- status [--source] [--repo]        Mostrar estado de sync de todos los repos
|- browse --repo <repo>              Abrir la web de un repo en el navegador
|- sweep [--dry-run] [--source]      Eliminar ramas locales stale
|- mirror
|  |- list                           Listar todos los grupos mirror
|  |- show <key>                     Mostrar detalles de mirror como JSON
|  |- add <key> --account-src/dst    Crear un grupo mirror
|  |- delete <key>                   Borrar un grupo mirror
|  |- add-repo <key> <repo> ...      Añadir un repo a un mirror
|  |- delete-repo <key> <repo>       Eliminar un repo de un mirror
|  |- setup [<key>] [--repo]         Ejecutar setup API para mirrors pendientes
|  |- status [<key>]                 Comprobar estado live de sync mirror
|  +- discover [<key>]               Descubrir repos mirrorable entre cuentas
|- workspace
|  |- list                           Listar workspaces configurados
|  |- show <key>                     Mostrar detalles de workspace
|  |- add <key> --type ...           Crear un workspace (codeWorkspace / tmuxinator)
|  |- delete <key>                   Eliminar workspace de config
|  |- add-member <key> <src>/<repo>  Añadir un clone a un workspace
|  |- delete-member <key> <src>/<repo>  Eliminar un clone de un workspace
|  |- generate <key>                 (Re)escribir el archivo workspace en disco
|  |- open <key>                     Regenerar + lanzar el workspace
|  +- discover [--apply]             Escanear disco, adoptar opcionalmente
|- identity [--remove]               Comprobar/eliminar identidad git global
|- gitignore
|  |- check                          Estado de ~/.gitignore_global + core.excludesfile
|  +- install                        Instalar / refrescar el bloque gestionado
|- reconfigure [--account]           Actualizar aislamiento de credenciales por repo
|- scan [--dir] [--pull]             Recorrido de filesystem para estado de repos
|- adopt [--all] [--dry-run]         Adoptar repos orphan en gitbox.json
|- doctor [--json]                   Sondear herramientas externas; exit 1 si falta alguna requerida
|- update [--check]                  Comprobar e instalar updates desde GitHub releases
|- completion <shell>                Generar completado de shell
+- version                           Mostrar info de versión
```

### Flags globales

```text
--config <path>    Ruta personalizada de archivo config
--json             Formato de salida JSON
--verbose          Mostrar todos los elementos (incluidos clean/skipped)
```

---

## 7. Principios de diseño UX

Aplican tanto a CLI como a GUI. El usuario NO debería necesitar conocer internos de Git — los comandos son verbos que hacen lo que dicen.

**Salida:**

- Una línea coloreada por elemento, emitida a medida que ocurre (no batched)
- Las operaciones largas muestran progress bars y luego saltan al estado final
- Silencioso por defecto — solo errores, warnings y cambios reales. `--verbose` muestra todo

**Sistema de color** (ANSI true-color, respeta `NO_COLOR`):

| Color  | Símbolo | Significado                 |
| ------ | ------- | --------------------------- |
| Green  | `+`     | Success, clean, ok          |
| Orange | `!` `~` | Warning, dirty, no upstream |
| Red    | `!` `x` | Diverged, conflict, error   |
| Purple | `<` `o` | Behind upstream, not cloned |
| Blue   | `>`     | Ahead of upstream           |
| Cyan   |         | Info, skip, section header  |

**Reglas de comportamiento:** El orden JSON sigue el orden del archivo de config. Los comandos son idempotentes. Los errores dicen al usuario qué hacer, no solo qué salió mal. Los tokens nunca se muestran.

---

## 8. Arquitectura GUI

La GUI es una app desktop Wails v2 con frontend Svelte. El backend Go (`cmd/gui/app.go`) expone métodos que el frontend llama mediante bindings TypeScript autogenerados. El puente del frontend está en `cmd/gui/frontend/src/lib/bridge.ts`.

Las operaciones largas (clone, refresh de status, pull, mirror discovery) se ejecutan en goroutines con progreso enviado al frontend mediante eventos Wails.

**Estructura de layout:**

- **Top bar** — logo, repo health ring, mirror health ring (cuando existen mirrors), botones de acción (Pull All, Fetch All, Delete mode, Compact view)
- **Tab bar** — cambia entre vistas Accounts y Mirrors
- **Tab Accounts** — account cards (con sync rings, credential badges, botones Find projects/Create repo) + lista de detalle de repos
- **Tab Mirrors** — mirror group cards (con sync rings, status dots) + lista de detalle de mirrors con status por repo, botones Discover y Check all
- **Summary footer** — contadores agregados de repos y mirrors
- **Compact view** — modo sidebar estrecho con health ring, account pills y mirror summary pill

**Features adicionales:**

- **Config auto-backup:** Los saves significativos crean un backup fechado (ventana rolling de los 10 más recientes) antes de sobrescribir. Los saves solo de posición de ventana saltan el backup — el churn cosmético desplazaría fuera del ring copias reales pre-corrupción.
- **Persistencia de estado de ventana:** Posición y tamaño se guardan por modo de vista (`window` y `compact_window` en config), restaurados al lanzar.
- **Autostart:** Registro de autostart específico por plataforma (launch agent macOS, registry Windows). Configurable desde la GUI.
- **Create repo:** Los repos se pueden crear directamente en proveedores (bajo namespace de usuario u org) desde el tab Accounts, con dropdown owner poblado mediante `OrgLister`.

Consulta la [guía GUI](gui-guide.md) para el walkthrough orientado a usuarios.

---

## 9. Seguridad

- **Los tokens NUNCA se almacenan en el archivo JSON de config.** Los PATs viven en archivos formato git-credential-store (`~/.config/gitbox/credentials/<key>`) con permisos 0600. Los tokens OAuth de GCM viven en el credential store del OS (Windows Credential Manager, macOS Keychain, Linux Secret Service) gestionado por Git Credential Manager.
- **El archivo config no contiene secretos** — solo URLs, usernames, rutas de carpeta y flags de preferencia.
- **Las llamadas API de proveedor usan tokens de archivos de credenciales o GCM** en runtime, nunca desde config.
- **Las claves privadas SSH** son archivos estándar de `~/.ssh/` con permisos adecuados (600).
- **Las URLs de clone se sanitizan** — las URLs de clone autenticadas con token eliminan el token del remoto después de clonar. Las operaciones git posteriores autentican mediante el helper de credenciales por repo, no mediante la URL.
- **Aislamiento de credenciales por repo** — el `.git/config` de cada clone cancela helpers de credenciales globales y define el suyo, evitando fugas de credenciales entre cuentas y eliminando credenciales fantasma de refresh tokens OAuth de GCM.
- **Los archivos credential store** (`~/.config/gitbox/credentials/<key>`) son plaintext con permisos 0600 — el mismo modelo de seguridad que `~/.git-credentials` y las claves privadas SSH.
- **Sin secretos en salida** — los tokens nunca se muestran, ni siquiera en verbose mode.
- **El repositorio es público** — no hay hostnames, usernames, emails ni tokens reales en archivos versionados.

---

## 10. Diagramas

Los diagramas de arquitectura están disponibles en `docs/diagrams/` como archivos `.drawio` editables:

- **architecture-overview.drawio** — Diagrama de componentes high-level del sistema
- **credential-flow.drawio** — Flujo de resolución de credenciales por tipo
- **credential-types.drawio** — Qué secretos alimentan Discovery, Git Operations y Mirrors por tipo de credencial
- **config-model.drawio** — Modelo de datos Accounts / Sources / Repos / Mirrors
- **cli-workflow.drawio** — Workflow de usuario desde init hasta uso diario

Se pueden abrir y editar con [draw.io](https://app.diagrams.net/) o la extensión drawio de VS Code.
