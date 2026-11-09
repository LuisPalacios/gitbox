# Gitbox — arquitectura y diseño

Para la visión general del producto (qué hace Gitbox, para quién es y por qué existe), consulta el [README](../../readme.md).

---

## 1. Resumen de arquitectura

Gitbox es un monorepo Go que produce un único binario, la app de escritorio `GitboxApp`, sobre una librería compartida en `pkg/`:

<p align="center">
  <img src="../diagrams/architecture-overview.png" alt="Architecture Overview" width="800" />
</p>

La GUI es una app Wails v2 con frontend Svelte. El lado Go vive en `cmd/gui/` y es una capa fina de bindings Wails, bloqueos y eventos. Las operaciones a nivel de aplicación (ciclo de vida de cuentas, cambio de credenciales, planificación de clones, discovery) viven en `pkg/ops`. Todo lo que hay por debajo (config, credenciales, proveedores, git, estado, mirrors) vive en los demás paquetes de `pkg/`. La GUI soporta credenciales GCM, SSH y Token.

La app no necesita ningún binario acompañante. Solo llama a herramientas ya presentes en el sistema: `git`, Git Credential Manager, `ssh` / `ssh-keygen` / `ssh-add`, `wsl` en Windows, y los terminales y editores del usuario.

La CLI v1 y su TUI embebida se eliminaron en v2. Siguen vivas en la línea de mantenimiento v1, en la rama `release/v1`.

## 2. Conceptos principales

### Accounts, sources y repos

<p align="center">
  <img src="../diagrams/config-model.png" alt="Config Model" width="800" />
</p>

Accounts y sources están separados porque una cuenta puede tener varias sources (por ejemplo, distintas orgs de GitHub bajo el mismo login). Las sources agrupan repos de forma lógica. Los repos usan nombres `org/repo`, y la parte org se convierte en la estructura de carpetas.

### Modelo de credenciales

[credentials.md](credentials.md) cubre la configuración orientada al usuario. Esta sección cubre el diseño.

Cada tipo de credencial es **autosuficiente** para la cuenta. El aislamiento de credenciales por repo da a cada clon un `.git/config` autocontenido: una línea vacía `credential.helper =` cancela los helpers heredados (globales y del sistema), y después se define el helper específico del tipo. Los clones no dependen de `~/.gitconfig`.

**Decisiones de diseño:**

- **Token** guarda el PAT en un archivo de credenciales de gitbox por cuenta, `~/.config/gitbox/credentials/<key>` (0600, formato git-credential-store). Gitbox lo lee para las llamadas a la API, y cada clon de la cuenta apunta `credential.helper = store --file <path>` a ese archivo para git.
- **GCM** usa `helper = manager` con `username` y `provider` por host, acotados a cada repo, más `credential.credentialStore`. GCM guarda sus secretos en el almacén de credenciales del OS.
- **SSH** solo cancela helpers (la autenticación va por `~/.ssh/config`). Discovery necesita un PAT opcional, guardado en el mismo archivo de credenciales que el de una cuenta token.

**Ciclo de vida de las credenciales:** cambiar de tipo elimina los artefactos del tipo anterior antes de configurar el nuevo, y después reconfigura los clones existentes (URL remota y config de credenciales). Renombrar la clave de una cuenta migra todos los artefactos: claves de config, la carpeta del source, el archivo de credenciales, las claves SSH y el alias de la config SSH.

### Config como base de datos local

El archivo JSON de config (`~/.config/gitbox/gitbox.json`) es el **estado deseado**: una base de datos local de las cuentas, sources y repos que deberían existir, y de las raíces de clonado en las que viven.

**Discovery** es una consulta hacia el norte. Pregunta a la API del proveedor qué repos existen y deja que el usuario los añada a la config. Discovery solo añade, bajo demanda, y nunca elimina repos.

**Reconcile** es su contrapartida en disco: Gitbox lee qué clones existen bajo las raíces de clonado. La config sigue siendo el registro de la intención (cuentas, raíces, mirrors, sobrescrituras, repos que se quieren pero aún no se han clonado), y `pkg/reconcile` mantiene el inventario al día. Adopta en su sitio los clones que reconoce, vuelve a enlazar los que se han movido y deja el resto al usuario. Nunca toca archivos y nunca olvida un repo cuyo clon ha desaparecido.

### Estructura de carpetas

Los repos se clonan en una jerarquía de 3 niveles bajo una raíz de clonado. `global.roots` lista las raíces, la predeterminada primero. Cada source clona en una de ellas (`source.root`, vacío para la predeterminada), y un repo concreto puede vivir en otra (`repo.root`):

```text
~/00.git/                          <- una raíz de global.roots
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

- **1er nivel**: `source.folder` sobrescribe la clave source.
- **2º nivel**: `repo.id_folder` sobrescribe la parte org.
- **3er nivel**: `repo.clone_folder` sobrescribe el nombre del repo. Un valor absoluto (`/` o `~`) reemplaza toda la ruta, y también uno que empieza por `../`, resuelto respecto a la raíz.

Cambiar la raíz de un source o la raíz predeterminada fija la raíz por la que ya se resuelve cada clon existente, así que ninguna ruta de clon se mueve. Los clones fuera de este árbol (clones anidados dentro de un contenedor multi-repo, clones encontrados fuera de la estructura estándar) siempre llevan un `clone_folder` absoluto, así que Gitbox nunca los mueve.

---

## 3. Diseño de componentes

### cmd/gui — bindings Wails

`cmd/gui/app.go` expone los métodos que el frontend Svelte llama mediante bindings TypeScript autogenerados. Los métodos enlazados toman el bloqueo de config para el estado compartido, llaman a `pkg/ops` o a otro paquete de `pkg/` para hacer el trabajo, guardan los cambios y emiten eventos Wails de progreso. Los archivos vecinos contienen asuntos exclusivos de la GUI: perfiles de terminal (`profiles.go`), contenedores multi-repo y sus archivos de workspace (`containers.go`), raíces de clonado (`roots.go`), reconcile de disco (`reconcile.go`), badges de PR (`pr.go`), autostart y ciclo de vida de procesos por OS. Cada `exec.Command` de `cmd/gui/` pasa por `git.HideWindow`, así que no parpadea ninguna ventana de consola en Windows.

`main.go` gestiona dos flags antes de que arranque Wails: `--version` imprime la versión del build y termina, y `--test-mode` enruta la config a través de un directorio temporal construido desde el fixture `test-gitbox.json` (consulta [testing.md](testing.md)).

El arranque devuelve la config, las preferencias, el estado de plegado y la anidación derivada de las rutas en una única instantánea independiente. El frontend la renderiza antes de que llegue el estado local y muestra indicadores de espera hasta que termina la comprobación o el fetch. Los hijos de otros sources se quedan bajo sus contenedores, pero conservan su propio source para el estado, las acciones y la propiedad. La detección de editores y perfiles de terminal se ejecuta fuera del bloqueo de config y se combina con los ajustes actuales, así que las ediciones concurrentes se conservan.

`cmd/gui/refresh.go` coordina el trabajo de refresco:

- Las llamadas solapadas comparten una única ejecución activa del estado local, Fetch de todo, el fetch por clon y la comprobación de credenciales de cada cuenta.
- Se conserva una invalidación pendiente del estado local, se descartan los resultados calculados a partir de una config que ha cambiado desde entonces, y cerrar la app cancela el trabajo de refresco.
- Cuando la cuenta cambia o se pide una recomprobación durante una comprobación de credenciales, la comprobación se vuelve a ejecutar contra la config actual. Los resultados offline se reintentan con backoff; los errores de autenticación, no.
- Un ciclo de Fetch de todo publica el estado local antes de esperar al trabajo del proveedor. `fetch:alldone` llega después de su estado final y de las comprobaciones remotas.

`cmd/gui/probe.go` comparte y limita los sondeos que comprueban si cada upstream sigue existiendo:

- Las credenciales se resuelven una vez por cuenta, y cada upstream se consulta una vez por sondeo.
- Límites: cuatro peticiones a la vez, 10 segundos por petición, 30 segundos por sondeo (incluida la resolución de credenciales) y al menos 60 segundos tras un sondeo completado antes del siguiente.
- Los errores y la cancelación conservan el estado upstream conocido. Los resultados superados por fetches o cambios de credenciales más recientes se descartan.

### pkg/ops — operaciones de aplicación

La capa de servicio entre los bindings de la GUI y los paquetes de nivel inferior. Contiene las operaciones que tocan varios paquetes a la vez: `AddAccount`, `RenameAccount`, `DeleteAccount`, `DeleteRepo`, `ChangeCredentialType`, `DeleteCredential`, `RemoveCredentialArtifacts`, `ReconfigureClones` (ejecuta `heal.Repo` en cada clon), `PlanClone` / `Clone`, `ListRemoteRepos`, `ListAccountOrgs`, `CreateRemoteRepo`, `AddDiscoveredRepos` y `ProviderClient`.

Cada operación trabaja sobre un `*config.Config` más los archivos que posee en disco (clones, claves SSH, archivos de credenciales). Las operaciones nunca guardan la config ni toman bloqueos: quien llama serializa el acceso y persiste después. Las operaciones que tocan cada clon de una cuenta (`ReconfigureClones`) están separadas, así que quien llama puede ejecutarlas después de un guardado correcto.

### pkg/config — gestión de configuración

Gestiona el archivo de configuración v5 y migra los archivos v2, v3 y v4 al cargar. Tipos principales: `Config`, `Account`, `Source`, `Repo`, `Mirror`. Las raíces de clonado (`Global.Roots`, `Source.Root`, `Repo.Root`) y sus mutadores viven en `roots.go`, y `status.RepoPath` resuelve cada ruta de clon a través de ellas. Los contenedores multi-repo (`Repo.Container`, `Repo.Workspace`), la profundidad de escaneo de anidados (`Global.NestedScanDepth`) y el estado de plegado de la GUI (`Global.Collapsed`, podado de claves obsoletas en cada `Save`) completan el modelo. `pkg/config/config.go` contiene las definiciones de structs; la [sección 4](#4-formato-de-config-v5) es la referencia de campos.

**Decisiones clave de diseño:**

- **Preservación del orden JSON:** `SourceOrder`, `RepoOrder` y `MirrorOrder` hacen que la iteración siga el orden del archivo de config.
- **Herencia de credenciales:** los repos heredan `default_credential_type` de su cuenta salvo que lo sobrescriban.
- **CRUD con integridad referencial:** `DeleteAccount` falla si algún source referencia la cuenta, y `DeleteSource` elimina en cascada sus repos.

### pkg/credential — gestión de credenciales

Gestiona tokens, claves SSH, la integración con GCM y el aislamiento de credenciales por repo. Consulta `pkg/credential/credential.go`, `pkg/credential/validate.go` y `pkg/credential/repoconfig.go`.

**Almacenamiento de tokens:** `StoreToken`, `GetToken` y `DeleteToken` escriben, leen y eliminan el archivo de credenciales `~/.config/gitbox/credentials/<key>` (`CredentialFilePath`). `GetToken` acepta tanto la línea de git-credential-store (`https://user:TOKEN@host`) como un token en bruto. No hay ningún otro almacén de tokens.

**Cadena de resolución de tokens (`ResolveToken`):** la variable de entorno `GITBOX_TOKEN_<KEY>`, después `GIT_TOKEN` y después el archivo de credenciales.

**Dispatch de tokens para la API:** enruta por tipo de credencial. `token` usa `ResolveToken`, `gcm` ejecuta `git credential fill` y recurre a `ResolveToken`, y `ssh` prueba `ResolveToken` (el PAT opcional de discovery). `ResolveMirrorToken` exige un PAT portable para los mirrors (consulta [pkg/mirror](#pkgmirror--mirroring-de-repositorios)).

**Gestión de claves SSH:** genera pares de claves, escribe entradas en `~/.ssh/config` y prueba conexiones. Convención de nombres: alias de host `gitbox-<account-key>`, archivo de clave `gitbox-<account-key>-sshkey`.

**Config de credenciales por repo** (`repoconfig.go`): `ConfigureRepoCredential()` hace autocontenido el `.git/config` de cada clon, y `WriteCredentialFile()` escribe la línea de git-credential-store para las cuentas token. `pkg/ops` y `pkg/heal` llaman a las mismas funciones.

`credential.CheckContext` verifica una cuenta y respeta la cancelación, que se informa como offline. Los subprocesos de verificación de GCM y SSH se detienen tras 15 segundos o en el plazo anterior de quien llama.

### pkg/provider — discovery de repositorios

Capa de abstracción para las APIs de los proveedores de hosting Git. Cada proveedor implementa `ListRepos()`, que devuelve structs `RemoteRepo`. Consulta `pkg/provider/provider.go` para la interfaz.

| Proveedor     | API          | Auth                          | Notas                           |
| ------------- | ------------ | ----------------------------- | ------------------------------- |
| GitHub        | REST v3      | Bearer token                  | Soporta GitHub Enterprise       |
| GitLab        | REST v4      | Cabecera PRIVATE-TOKEN        | Compatible con self-hosted      |
| Gitea/Forgejo | REST /api/v1 | Token + fallback a Basic auth | Misma API, misma implementación |
| Bitbucket     | REST v2      | HTTP Basic (app password)     | Solo cloud                      |

`CheckAuth()` valida una credencial y `TokenSetupGuide()` devuelve las instrucciones de creación de PAT por proveedor. `CheckAuth()` también devuelve la caducidad del token: GitHub lee la cabecera `GitHub-Authentication-Token-Expiration` de la llamada que ya lista los repos, y GitLab añade un `GET /personal_access_tokens/self` tras una comprobación correcta. Los demás proveedores no devuelven caducidad. `credential.StatusResult.ExpiresAt` la lleva hasta `CredentialStatus.expiresAt` en la GUI, y `accountHealth` en `src/lib/status.ts` convierte en aviso un token que funciona y caduca en 14 días o menos.

**Interfaces opcionales** (mediante type assertions):

- `RepoCreator`: crear repos (bajo el usuario o una org, con descripción) y comprobar su existencia (todos los proveedores).
- `OrgLister`: listar las organizaciones o grupos a los que pertenece el usuario, para el desplegable de propietario al crear un repo (todos los proveedores).
- `PushMirrorProvider`: push mirrors del lado del servidor (Gitea/Forgejo, GitLab).
- `PullMirrorProvider`: pull mirrors mediante la API migrate (Gitea/Forgejo).
- `RepoInfoProvider`: obtener el commit HEAD y la visibilidad para la comparación de sync (GitHub, GitLab, Gitea/Forgejo).

### pkg/git — operaciones Git

Wrapper fino alrededor de `os/exec` para todas las operaciones Git, sin dependencia de libgit2. Proporciona `Clone`, `CloneWithProgress`, `Pull`, `Status`, `Fetch`, `ConfigSet`, `ConfigAdd`, `ConfigUnsetAll` y más (consulta `pkg/git/git.go`). Las claves de config git con varios valores, como `credential.helper`, se gestionan con `ConfigUnsetAll` + `ConfigAdd`.

En macOS, `GitBin()` prueba las rutas de Homebrew (`/opt/homebrew/bin/git`, `/usr/local/bin/git`) antes de recurrir a `PATH`, así que la GUI encuentra un git con GCM incluso con el `PATH` mínimo que heredan las apps GUI de macOS.

### pkg/status — comprobación de estado de sync

Determina el estado de sync de los clones locales respecto a su upstream. Estados: Clean, Dirty, Behind, Ahead, Diverged, Conflict, NotCloned, NoUpstream, Error, UpstreamGone, RootOffline. Un repo cuya raíz de clonado falta se lee como RootOffline en lugar de NotCloned (`RepoRootOffline`), así que un disco desconectado no parece clones borrados. Prioridad: Conflict > Dirty > Diverged > Behind > Ahead > NoUpstream > Clean. `ComputeNesting` deriva las relaciones padre → hijo entre los clones contenedor y los clones anidados dentro de ellos. Consulta `pkg/status/status.go`.

### pkg/identity — gestión de identidad Git

Gestiona la identidad git por repo (`user.name`, `user.email`) con una cadena de resolución: los overrides a nivel de repo recurren a los valores a nivel de cuenta. Consulta `pkg/identity/identity.go`. `heal.Repo` comprueba la identidad de cada clon y repara las desviaciones.

`CheckGlobalIdentity()` y `RemoveGlobalIdentity()` gestionan la identidad global de `~/.gitconfig`. Gitbox anima a eliminarla, para que la identidad por repo definida al clonar o reconfigurar sea siempre la que manda.

`pkg/credential` hace lo mismo con el helper GCM global (`gcm_config.go`). Sin un helper GCM en `~/.gitconfig`, `git credential fill` cae a `/dev/tty` y falla con "Device not configured" en contextos GUI. El diseño:

- **Cuándo:** `IsGlobalGCMConfigNeeded()` condiciona la comprobación a que al menos una cuenta use GCM, así que las configuraciones solo con SSH o token nunca ven el aviso.
- **Comprobación:** `CheckGlobalGCMConfig()` espera que `credential.helper` se resuelva a GCM y que `credential.credentialStore` coincida (`keychain`, `wincredman` o `secretservice`), y toma los valores esperados de `global.credential_gcm` o de los valores por defecto del OS. Lee la lista completa de valores de `credential.helper` y aplica la regla de reinicio de git (un valor vacío borra los helpers anteriores). Un helper se resuelve a GCM cuando es `manager`, el antiguo `manager-core` o una ruta (entre comillas o no) cuyo binario es `git-credential-manager`. Por eso una cadena estándar de `git-credential-manager configure` (un reinicio más una ruta absoluta) pasa sin necesidad de arreglo.
- **Arreglo:** `FixGlobalGCMConfig()`, detrás del botón de arreglo de la GUI, primero rellena `DefaultCredentialHelper()` y `DefaultCredentialStore()` en `gitbox.json` cuando a `global.credential_gcm` le faltan. Después escribe solo lo que la comprobación señala: una cadena de helpers que ya se resuelve a GCM no se toca, y una que no lo hace se reemplaza entera con `--replace-all`, igual que un `credentialStore` incorrecto.

Las secciones `[credential "https://…"]` por host, incluida la config por repo que escribe Gitbox, siguen teniendo prioridad sobre estos valores globales.

### pkg/update — auto-update

Proporciona comprobación de versión y autoactualización mediante GitHub Releases. El diseño:

- **Comprobación:** `CheckLatest()` consulta la API de GitHub en cada llamada. La GUI la llama al arrancar y cada seis horas, y sigue la release que GitHub marca como latest. Una opción `MaxMajor` limita la versión major; la CLI v1 de `release/v1` la usa para quedarse en 1.x.
- **Verificación:** `DownloadRelease()` verifica primero que el `checksums.sha256.sig` de la release es una firma de `ssh-keygen -Y sign` en el namespace `gitbox-release`, hecha con la clave incrustada desde `release-signing-key.pub`, sobre `gitbox <tag>` más los checksums (un pequeño verificador sshsig sobre `golang.org/x/crypto/ssh`). Solo entonces descarga el artefacto específico de la plataforma y verifica su checksum SHA256. Falla cerrada cuando la firma o `checksums.sha256` falta, no se puede leer o no es válido, o no lista el artefacto. Consulta [release-signing.md](release-signing.md).
- **Instalación:** `ExtractUpdate()` e `InstallExtracted()` desempaquetan el zip y reemplazan lo que está instalado junto a la app en ejecución: el bundle `GitboxApp.app` completo en macOS, un rename atómico en los demás sistemas Unix y, en Windows, un rename previo del binario en ejecución a `.old` (`CleanupOldBinary()` elimina los archivos `.old` obsoletos en el siguiente arranque).
- **Relanzamiento:** `Relaunch()` arranca la nueva versión desacoplada con `--wait-pid=<PID antiguo>` (en macOS mediante `open -n` sobre el bundle), y la GUI se cierra. El nuevo proceso espera en `WaitForExit()` hasta que el antiguo ha terminado, así el bloqueo de instancia única queda libre. Una actualización elevada en Windows relanza desde su script auxiliar mediante `explorer.exe`, así la nueva app no se ejecuta elevada.

### pkg/doctor — detección de herramientas externas

Sondea el host en busca de cada binario externo que Gitbox puede llamar (`git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add` y `wsl` en Windows) e informa de la ruta, la versión y las indicaciones de instalación por OS de cualquiera que falte. `PrecheckForCredentialType()` alimenta los flujos de configuración de la GUI, así que una herramienta que falta aparece como un banner amarillo con el comando de instalación en vez de como un error críptico en tiempo de ejecución. Para GCM, el sondeo pregunta primero a `git credential-manager --version`, porque git lo resuelve desde sus propios directorios, que no tienen por qué estar en `PATH`. Solo recurre a las carpetas de instalación conocidas cuando git no puede ejecutarlo. La GUI muestra el informe completo en **Ajustes → Comprobación del sistema**.

El sondeo nunca ejecuta `C:\Windows\System32\wsl.exe`: Windows trae ese archivo como un stub que, cuando falta el paquete, pide instalar WSL en una ventana propia. `doctor.WSLDistros()` lee las distros del registro, y la versión sale del binario del propio paquete en `Program Files\WSL`. La detección de shells usa la misma lista, así que el shell `wsl` solo aparece cuando hay una distro instalada.

### pkg/heal — autorreparación del .git/config de un repo

`heal.Repo(cfg, sourceKey, repoKey)` reconcilia de forma idempotente el `.git/config` de un clon con la especificación de la cuenta: `user.name`, `user.email`, la URL canónica de `origin` (específica del tipo de credencial, sin secretos embebidos) y el helper de credenciales. Cada punto de disparo lo ejecuta (clonado, fetch, pull, movimiento de repo, `ops.ReconfigureClones` y el sync periódico de la GUI), así que los clones que se desvían de la especificación se reparan en silencio.

Las comprobaciones de identidad, origin y credenciales comparten un único `git.ReadConfigSnapshot`: dos procesos Git leen la config efectiva (con includes) y la config local directa. Un clon sin cambios no provoca escrituras en la config de credenciales ni en el archivo de credenciales. Un archivo de token modificado se sustituye de forma atómica, las actualizaciones de una cuenta compartida se serializan y los permisos de archivo siguen siendo privados. Si un include posterior o la config de un worktree sigue sobrescribiendo una reparación, heal informa de un aviso de mantenimiento y la GUI omite el fetch en lugar de usar una identidad, un origin o una cadena de helpers incorrectos.

### pkg/move — movimiento de repos entre cuentas y proveedores

Reubica un clon de una cuenta configurada a otra, incluso entre proveedores (GitHub → GitLab, Gitea → Forgejo). El flujo se ejecuta por fases: preflight (sondeo de preparación de credenciales) → fetch → crear el destino → `git push --mirror` → reconectar `origin` → actualizar `gitbox.json` → borrado opcional del remoto de origen → borrado opcional del clon local → clonado automático del destino cuando se borró el clon local. Los callbacks de fase envían el progreso a la GUI. Las fases 1–6 son fatales si fallan. Los borrados opcionales son best-effort y aparecen como avisos, con `provider.InsufficientScopesError` convertido en texto de solución (por ejemplo, "add `delete_repo` scope to your GitHub PAT").

### pkg/gitignore — autorreparación del gitignore global

Instala un bloque gestionado y curado de patrones de basura del OS (`.DS_Store`, `Thumbs.db`, `*~`, …) en `~/.gitignore_global` y apunta `core.excludesfile` a él. Unos marcadores centinela (`# >>> gitbox:global-gitignore >>>` / `# <<< gitbox:global-gitignore <<<`) envuelven el bloque, así que las reinstalaciones reescriben solo la región gestionada. Las entradas del usuario y los patrones de negación (`!.DS_Store`) fuera de los centinelas se conservan, y los duplicados de patrones gestionados fuera de ellos se eliminan. La escritura es atómica (tmp+rename), con un límite rotativo de 3 backups (`.bak-YYYYMMDD-HHMMSS`). `global.check_global_gitignore` en `gitbox.json` condiciona solo la comprobación automática al arrancar; las acciones explícitas siempre se ejecutan. La GUI lo muestra como un banner con un botón **Instalar**.

### pkg/workspace — archivos de workspace de los contenedores

Vincula los archivos `.code-workspace` de VS Code a los clones que los contienen. Gitbox nunca crea, edita, genera ni borra archivos de workspace.

- `Sync` lee la raíz de cada clon en disco (no recorre subcarpetas). Un archivo de workspace convierte el clon en un contenedor con ese archivo en `repo.workspace`. Varios lo convierten en un contenedor que abre el archivo preferido (`repo.workspace_preferred`, que se fija cuando el usuario elige uno) si está, si no conserva su archivo actual, y si no informa de una `Choice` para que la GUI pregunte. Ninguno borra una elección obsoleta y deja el flag de contenedor como está.
- `Apply` escribe esos cambios en la config.
- `Members` lee las carpetas que un archivo de workspace lista dentro de su contenedor. `Placeholders` se queda con las que no están en disco, a las que no resuelve ningún repo configurado y que no están dentro de otro clon (un submódulo, por ejemplo). `MemberRepos` lista los clones configurados que nombra, que la GUI muestra en el color de workspace.

Los archivos de workspace son JSON con comentarios, así que los comentarios y las comas finales se eliminan antes de parsearlos. El binding del reconcile de disco ejecuta `Sync` sobre una instantánea sin el bloqueo de config, aplica los cambios bajo él y después ejecuta el escaneo de reconcile, así que un contenedor que acaba de ganar un archivo de workspace adopta sus clones anidados en la misma pasada.

### pkg/adopt — discovery de repos huérfanos

Escanea cada raíz de `global.roots` y los árboles de trabajo de los repos contenedor en busca de clones que no están en `gitbox.json`, y después puntúa cada uno contra cada cuenta cuyo host coincide. Todas las señales son aditivas, y gana la puntuación más alta:

| Señal                                                                      | Puntuación | Origen                                                                     |
| -------------------------------------------------------------------------- | ---------- | -------------------------------------------------------------------------- |
| Coincidencia de host (base obligatoria)                                    | 1          | hostname de la URL de la cuenta o alias SSH frente al host remoto parseado |
| Owner igual a `account.username`                                           | +3         | segmento owner de la ruta de la URL remota                                 |
| El repo vive bajo la carpeta source de la cuenta                           | +5         | primer componente de la ruta del repo relativa a la raíz que lo contiene   |
| La URL HTTPS embebe `user@` donde user es igual a `account.username`       | +10        | `url.User.Username()` del remoto origin                                    |
| `.git/config` tiene `credential.<url>.username` igual a `account.username` | +10        | `git config --get-regexp '^credential\..*\.username$'` en el repo          |

Si dos o más cuentas comparten la puntuación máxima (incluido un empate solo por host), el match es ambiguo y no se elige ninguna cuenta. Un match único es **seguro** cuando su puntuación supera la base de solo host, de modo que algo más allá del servidor vincula el clon a la cuenta. La adopción nunca mueve archivos: un clon en su sitio estándar bajo una raíz registra esa raíz, y cualquier otro clon recibe un `clone_folder` absoluto. Los clones adoptados reciben aislamiento de credenciales, identidad y una URL remota reescrita para coincidir con el tipo de credencial.

El discovery ordinario de huérfanos se detiene en el límite de cada clon, incluido un archivo `.git` usado por un worktree. Las raíces explícitas se escanean de forma independiente. El escaneo de contenedores usa `nested_scan_depth` para encontrar clones anidados, y también escanea, a cualquier profundidad, cada carpeta que el archivo de workspace de un contenedor lista dentro de él.

### pkg/reconcile — reconcile de disco

`Plan` ejecuta el escaneo de huérfanos y decide, para cada clon que la config no sigue, en este orden:

- Un worktree enlazado, o un clon sin un remoto utilizable, espera al usuario.
- Un clon cuya clave de repo y servidor coinciden exactamente con un repo seguido cuyo clon ha desaparecido, con su raíz disponible, **vuelve a enlazar** esa entrada a la nueva ruta.
- Un match seguro se **adopta** en su sitio.
- Todo lo demás espera en la lista Clones sin ubicar con su motivo: solo host, ambiguo, host desconocido, sin remoto, ya seguido o worktree.

Las rutas de `global.ignored_clones` se informan aparte. `Apply` escribe las decisiones en la config; ningún paso toca archivos. La GUI ejecuta una pasada al arrancar, al recuperar el foco la ventana, tras un Fetch de todo manual, tras cambios en las raíces de clonado y al reescanear un contenedor. Cada pasada sincroniza primero los archivos de workspace de los contenedores (`pkg/workspace`), después toma una instantánea de la config, escanea sin el bloqueo, vuelve a comprobar la config (reintentando cuando ha cambiado) y aplica bajo el bloqueo. Los repos bajo una raíz que falta nunca se vuelven a enlazar ni se olvidan.

### pkg/mirror — mirroring de repositorios

Gestiona la configuración de push y pull mirrors, la comprobación de estado, el discovery y las guías de configuración manual. Los mirrors mantienen copias de respaldo de los repos en otro proveedor sin clonarlos en local.

**Tipos de mirror:**

| Tipo | Dirección                              | Caso de uso                                     |
| ---- | -------------------------------------- | ----------------------------------------------- |
| Push | El servidor de origen empuja al backup | Repo fuente en Forgejo/GitLab; backup en GitHub |
| Pull | El servidor de backup tira del origen  | Repo fuente en GitHub; backup en Forgejo        |

**Automatización por proveedor:**

| Proveedor     | Crear repo | Push mirror             | Pull mirror          |
| ------------- | ---------- | ----------------------- | -------------------- |
| Gitea/Forgejo | Sí         | Sí                      | Sí (vía API migrate) |
| GitHub        | Sí         | No (solo guía)          | No                   |
| GitLab        | Sí         | Sí (API remote mirrors) | No                   |
| Bitbucket     | Sí         | No (solo guía)          | No                   |

**Decisiones clave de diseño:**

- **Modelo de config:** `mirrors` es una sección opcional de nivel superior (`omitempty`), así que las configs sin mirrors siguen siendo válidas. Cada grupo de mirror empareja dos cuentas (`account_src`, `account_dst`) con dirección y origen por repo.
- **Tokens de mirror:** los servidores remotos necesitan PATs portables, no tokens OAuth de GCM locales a la máquina. `ResolveMirrorToken()` lo exige, así que las cuentas GCM deben guardar un PAT aparte para los mirrors.
- **Sync inmediato:** después de crear un push mirror en Forgejo/Gitea, el código llama a `/push_mirrors-sync`, así que el primer sync se ejecuta de inmediato en vez de esperar al intervalo configurado.
- **Comparación de estado:** `CheckStatus()` consulta los SHAs del commit HEAD de origen y backup mediante las APIs de los proveedores y los compara. El estado de los mirrors se deriva de estas comprobaciones en vivo y nunca se guarda.
- **Comprobaciones de visibilidad:** el estado avisa cuando los repos de backup no son privados.
- **Discovery:** escanea todos los pares de cuentas en busca de relaciones de mirror existentes, con confianza decreciente: API de push mirror (confirmado), flag de pull mirror (probable), coincidencia de nombre (posible).

### pkg/terminals, pkg/launch, pkg/harness — acciones Open-in

`pkg/terminals` es dueño del catálogo compilado de terminales y shells soportados por OS, la detección del host, la composición de Profiles según el OS y la lógica de merge que mantiene `terminal_apps[]`, `shells[]` y `terminal_profiles[]` al día con lo que está instalado. `pkg/launch.ResolveArgs` expande la plantilla argv de un Profile para una carpeta fuera del runtime de Wails, así que las reglas de tokens tienen tests unitarios. `pkg/harness` parsea la tabla embebida [`tools-directory.md`](../../pkg/harness/tools-directory.md) para decidir qué AI CLI harnesses autodetecta la GUI; [agentic-tools-directory.md](agentic-tools-directory.md) documenta sus reglas. Consulta [Perfiles de terminal](#perfiles-de-terminal) para las reglas de detección y lanzamiento.

### pkg/i18n — idioma de la UI

Resuelve y normaliza el idioma de la UI, en este orden: la variable de entorno `GITBOX_LANG`, `global.language`, el locale del OS y el inglés como fallback. Los catálogos de strings se distribuyen en el frontend Svelte (`cmd/gui/frontend/src/lib/i18n.ts`); este paquete solo decide cuál usar.

---

## 4. Formato de config (v5)

La config vive en `~/.config/gitbox/gitbox.json`. El [ejemplo JSON anotado](../../json/gitbox.jsonc) muestra una config completa con comentarios, y el [JSON Schema](../../json/gitbox.schema.json) dirige la validación y el autocompletado en el editor. Las claves de nivel superior son `$schema` (opcional), `version`, `global`, `accounts`, `sources` y la opcional `mirrors`.

**Versionado:** `config.CurrentVersion` es 5. Gitbox carga v5 de forma estricta y migra los archivos v2, v3 y v4 al cargar (en memoria; se persiste en el siguiente guardado). El paso v3 → v4 convierte `global.folder` y `global.extra_folders` en `global.roots`, en ese orden, para que cada clon conserve su ruta. El paso v4 → v5 descarta la sección `workspaces`, y el siguiente reconcile de disco registra el archivo de workspace de cada contenedor en `repo.workspace`. Cualquier otra versión es un error. Los cambios aditivos nunca suben la versión.

**Backups automáticos:** cada guardado significativo escribe primero un backup fechado en el mismo directorio (por ejemplo, `gitbox-20260401-143025.json`) y conserva los 10 más recientes. La pantalla de recuperación de corrupción de la GUI restaura cualquiera de ellos con un clic. Los guardados que solo cambian la posición de la ventana, el layout (`collapsed`) o el tema se saltan el backup, así que los cambios cosméticos nunca expulsan del anillo las copias reales anteriores a una corrupción.

**Herencia del tipo de credencial:** los repos heredan `default_credential_type` de su cuenta salvo que definan su propio `credential_type`.

**Resolución de carpetas:** `root / sourceFolder / idFolder / cloneFolder`, donde la raíz es `repo.root`, si no `source.root`, si no la primera de `global.roots`, con overrides posibles en cada nivel. Un `clone_folder` absoluto reemplaza toda la jerarquía.

### Global

| Campo                             | Tipo   | Obligatorio | Descripción                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| --------------------------------- | ------ | ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `roots`                           | array  | Sí          | Raíces de clonado: las carpetas en las que Gitbox clona y que escanea. La primera es la predeterminada. Soporta `~`.                                                                                                                                                                                                                                                                                                                                                                |
| `nested_scan_depth`               | int    | No          | Niveles que Gitbox desciende bajo un repo contenedor para encontrar clones anidados. Por defecto `1` (hijos inmediatos).                                                                                                                                                                                                                                                                                                                                                            |
| `ignored_clones`                  | array  | No          | Rutas absolutas de clones bajo una raíz que el usuario ha decidido no seguir. Reconcile nunca los adopta ni los lista.                                                                                                                                                                                                                                                                                                                                                              |
| `language`                        | string | No          | Idioma de la UI. Vacío significa el locale del OS (inglés como fallback).                                                                                                                                                                                                                                                                                                                                                                                                           |
| `periodic_sync`                   | string | No          | Intervalo de fetch en segundo plano: off, `5m`, `15m` o `30m`.                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `view_mode`                       | string | No          | `"full"` o `"compact"`.                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `ui_mode`                         | string | No          | Experiencia de la GUI, `"simple"` o `"advanced"`. Si no existe, avanzada; el onboarding escribe `"simple"` en una instalación nueva.                                                                                                                                                                                                                                                                                                                                                |
| `theme`                           | string | No          | Tema de la GUI, `"light"` o `"dark"`. Si no existe, sigue al OS. Los guardados que solo cambian el tema se saltan el anillo de backups.                                                                                                                                                                                                                                                                                                                                             |
| `collapsed`                       | object | No          | Estado de layout de la GUI: grupos de cuenta plegados (`sources`), clones padre plegados (`repos`, como `"<source>/<repo>"`), `sidebar` (plegada al riel de iconos) y `view` (`attention`, `all`, `mirrors` o `account:<key>`). Si no existe, todo expandido y Requiere atención. `Save` poda las claves obsoletas, la vista de una cuenta borrada y la vista retirada `workspaces`; un renombrado reescribe la vista. Los guardados solo de layout se saltan el anillo de backups. |
| `window` / `compact_window`       | object | No          | Posición y tamaño guardados (`x`, `y`, `width`, `height`) por modo de vista.                                                                                                                                                                                                                                                                                                                                                                                                        |
| `check_global_gitignore`          | bool   | No          | Ejecuta la comprobación automática de `~/.gitignore_global` al arrancar. Por defecto `true`.                                                                                                                                                                                                                                                                                                                                                                                        |
| `pr_badges_enabled`               | bool   | No          | Obtiene y muestra indicadores de PR / review en las filas de clon. Por defecto `true`.                                                                                                                                                                                                                                                                                                                                                                                              |
| `pr_include_drafts`               | bool   | No          | Cuenta los PRs en borrador en el badge "my PRs". Por defecto `true`.                                                                                                                                                                                                                                                                                                                                                                                                                |
| `credential_ssh`                  | object | No          | Valores por defecto de plataforma para SSH. Su presencia indica que SSH está disponible.                                                                                                                                                                                                                                                                                                                                                                                            |
| `credential_ssh.ssh_folder`       | string | No          | Directorio de config SSH. Por defecto `~/.ssh`.                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `credential_gcm`                  | object | No          | Valores por defecto de plataforma para GCM. Su presencia indica que GCM está disponible.                                                                                                                                                                                                                                                                                                                                                                                            |
| `credential_gcm.helper`           | string | No          | Helper de credenciales. Normalmente `"manager"`.                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `credential_gcm.credential_store` | string | No          | `"wincredman"`, `"keychain"` o `"secretservice"`.                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `credential_token`                | object | No          | Valores por defecto de plataforma para Token/PAT (aún sin campos). Su presencia indica que la autenticación por token está disponible.                                                                                                                                                                                                                                                                                                                                              |
| `editors`                         | array  | No          | Editores de código para el menú "Open in". Se rellena automáticamente en el primer arranque.                                                                                                                                                                                                                                                                                                                                                                                        |
| `editors[].name`                  | string | Sí          | Nombre visible (por ejemplo, `"VS Code"`).                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `editors[].command`               | string | Sí          | Ruta completa o nombre de comando (por ejemplo, `"C:\\...\\code.cmd"`).                                                                                                                                                                                                                                                                                                                                                                                                             |
| `terminals`                       | array  | No          | Lista plana de terminales heredada de antes de Terminal Profiles. Solo se lee para migrar configs antiguas a los tres arrays de abajo al cargar; Gitbox nunca la escribe.                                                                                                                                                                                                                                                                                                           |
| `terminal_apps`                   | array  | No          | Emuladores de terminal detectados, del catálogo de `pkg/terminals` (consulta [Perfiles de terminal](#perfiles-de-terminal)).                                                                                                                                                                                                                                                                                                                                                        |
| `terminal_apps[].id`              | string | Sí          | Id estable (`"wt"`, `"wezterm"`, `"gnome-terminal"`, `"iterm"`, …), referenciado desde `terminal_profiles[].terminal`.                                                                                                                                                                                                                                                                                                                                                              |
| `terminal_apps[].name`            | string | Sí          | Nombre visible que se muestra en el gestor de terminales y en el lanzador por fila.                                                                                                                                                                                                                                                                                                                                                                                                 |
| `terminal_apps[].command`         | string | Sí          | Ruta absoluta resuelta, que se rellena en tiempo de detección.                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `terminal_apps[].args_template`   | array  | No          | Plantilla argv que `pkg/launch.ResolveArgs` expande por Profile. Tokens: `{path}` (se añade al final si falta), `{command}` (argv de un AI harness, vacío en lanzamientos normales), `{shell_command}` (binario del shell resuelto), `{shell_args}` (args por defecto del shell).                                                                                                                                                                                                   |
| `shells`                          | array  | No          | Shells detectados, del catálogo de `pkg/terminals`, más una fila `wsl-<distro>` por cada distro WSL instalada en Windows.                                                                                                                                                                                                                                                                                                                                                           |
| `shells[].id`                     | string | Sí          | Id estable (`"cmd"`, `"pwsh"`, `"git-bash"`, `"wsl-ubuntu"`, …), referenciado desde `terminal_profiles[].shell`.                                                                                                                                                                                                                                                                                                                                                                    |
| `shells[].name`                   | string | Sí          | Nombre visible.                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `shells[].command`                | string | Sí          | Ruta absoluta resuelta.                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `shells[].args`                   | array  | No          | Args por defecto insertados donde el `args_template` del terminal referencia `{shell_args}`.                                                                                                                                                                                                                                                                                                                                                                                        |
| `terminal_profiles`               | array  | No          | Profiles lanzables, que leen el lanzador por fila y el menú "Open in". La composición depende del OS (consulta [Composición según el OS](#composición-según-el-os)).                                                                                                                                                                                                                                                                                                                |
| `terminal_profiles[].id`          | string | Sí          | Id estable (`"wt+pwsh"`, `"wezterm+launchmenu-mybash"`, `"user-1"`, …).                                                                                                                                                                                                                                                                                                                                                                                                             |
| `terminal_profiles[].name`        | string | Sí          | Nombre visible (por ejemplo, `"Windows Terminal — PowerShell 7"`).                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `terminal_profiles[].terminal`    | string | Sí          | `terminal_apps[].id` que se lanza. Vacío solo en los Profiles de shell directo de Windows.                                                                                                                                                                                                                                                                                                                                                                                          |
| `terminal_profiles[].shell`       | string | No          | `shells[].id` que se ejecuta dentro del terminal. Vacío significa el valor por defecto del terminal, que en macOS y Linux es el shell de inicio de sesión del host.                                                                                                                                                                                                                                                                                                                 |
| `terminal_profiles[].args`        | array  | No          | Argv de override. Cuando está vacío, el lanzador usa `terminal_apps[].args_template`. Las filas `launch_menu` de WezTerm guardan aquí la forma completa `start --cwd {path} -- <argv>`.                                                                                                                                                                                                                                                                                             |
| `terminal_profiles[].default`     | bool   | No          | Marca el Profile que ejecuta la acción principal del lanzador por fila. Como mucho un Profile lo tiene.                                                                                                                                                                                                                                                                                                                                                                             |
| `terminal_profiles[].preferred`   | bool   | No          | Lista el Profile bajo Abrir con en los menús ⋯.                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `terminal_profiles[].hidden`      | bool   | No          | Oculta el Profile de los menús sin borrarlo. Es la única forma de suprimir un Profile que no es del usuario, ya que uno borrado vuelve en el siguiente ciclo de detección.                                                                                                                                                                                                                                                                                                          |
| `terminal_profiles[].source`      | string | No          | **Campo interno**, nunca se muestra. Etiqueta de origen que decide entre borrar u ocultar: solo los Profiles `"user"` se pueden borrar; las filas `"detected"`, `"wt-profile"`, `"wezterm-launchmenu"` y `"migrated"` solo se pueden ocultar.                                                                                                                                                                                                                                       |
| `ai_harnesses`                    | array  | No          | AI CLI harnesses para el menú "Open in". Se detectan en segundo plano (poco después del arranque, cada 10 minutos y al recuperar el foco la ventana) a partir del catálogo embebido más `~/.local/bin`, los prefijos de Homebrew y los directorios conocidos de cada herramienta. Se lanzan dentro del shell del Terminal Profile por defecto (consulta `terminal_profiles[].default`).                                                                                             |
| `ai_harnesses[].name`             | string | Sí          | Nombre visible (por ejemplo, `"Claude Code"`).                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `ai_harnesses[].command`          | string | Sí          | Ruta absoluta o binario en el PATH (por ejemplo, `"claude"`).                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `ai_harnesses[].args`             | array  | No          | Args extra opcionales para el harness. Normalmente vacío.                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `ai_harnesses[].source`           | string | No          | **Campo interno.** El sync de harnesses añade las entradas `"detected"` y puede volver a resolver su `command` tras una reinstalación; nunca altera las entradas `"user"` más allá de `missing`. Vacío en las entradas antiguas hasta que el primer sync las clasifica.                                                                                                                                                                                                             |
| `ai_harnesses[].missing`          | bool   | No          | Lo pone el sync de harnesses cuando ya no encuentra el binario. La entrada se oculta de los menús pero se conserva, así que una reinstalación la restaura con sus `args` intactos. Se limpia cuando el binario reaparece.                                                                                                                                                                                                                                                           |

### Account

| Campo                     | Tipo    | Obligatorio | Descripción                                                                               |
| ------------------------- | ------- | ----------- | ----------------------------------------------------------------------------------------- |
| `provider`                | string  | Sí          | `"github"`, `"gitlab"`, `"gitea"`, `"forgejo"`, `"bitbucket"`, `"generic"`                |
| `url`                     | string  | Sí          | URL del servidor (scheme+host, sin ruta).                                                 |
| `username`                | string  | Sí          | Nombre de usuario de la cuenta.                                                           |
| `name`                    | string  | Sí          | `git user.name` por defecto.                                                              |
| `email`                   | string  | Sí          | `git user.email` por defecto.                                                             |
| `default_credential_type` | string  | No          | Autenticación por defecto: `"gcm"`, `"ssh"` o `"token"`.                                  |
| `ssh.host`                | string  | Condicional | Alias SSH Host (por ejemplo, `"gt-myuser"`). **Obligatorio** cuando SSH está configurado. |
| `ssh.hostname`            | string  | No          | Hostname SSH real. Se deriva de la URL si se omite.                                       |
| `ssh.key_type`            | string  | Condicional | `"ed25519"` o `"rsa"`. **Obligatorio** cuando SSH está configurado.                       |
| `gcm.provider`            | string  | No          | Pista de proveedor para GCM.                                                              |
| `gcm.useHttpPath`         | boolean | No          | Acota las credenciales por ruta HTTP.                                                     |

Una cuenta es única por `(hostname, username)`. Borrar una cuenta elimina en cascada cada source y mirror que la referencia.

### Source

| Campo     | Tipo   | Obligatorio | Descripción                                                                               |
| --------- | ------ | ----------- | ----------------------------------------------------------------------------------------- |
| `account` | string | Sí          | Referencia una clave de cuenta.                                                           |
| `folder`  | string | No          | Sobrescribe la carpeta de clonado de primer nivel. Por defecto: la clave del source.      |
| `root`    | string | No          | Raíz de clonado en la que clona, recordada del último clon. Por defecto: la primera raíz. |
| `repos`   | object | Sí          | Repos con clave `org/repo`.                                                               |

### Repo (dentro de source.repos)

| Campo                 | Tipo   | Obligatorio | Descripción                                                                                                                     |
| --------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `credential_type`     | string | No          | Sobrescribe el método de autenticación. Se hereda de la cuenta.                                                                 |
| `name`                | string | No          | Sobrescribe `git user.name`.                                                                                                    |
| `email`               | string | No          | Sobrescribe `git user.email`.                                                                                                   |
| `id_folder`           | string | No          | Sobrescribe el directorio de 2º nivel (carpeta org).                                                                            |
| `clone_folder`        | string | No          | Sobrescribe el directorio de 3er nivel. Un valor absoluto reemplaza toda la ruta.                                               |
| `root`                | string | No          | Raíz de clonado, definida solo cuando difiere de la del source.                                                                 |
| `container`           | bool   | No          | Lo marca como contenedor multi-repo; Gitbox escanea dentro en busca de clones anidados.                                         |
| `workspace`           | string | No          | El archivo `.code-workspace` del contenedor, relativo a la raíz del clon. `pkg/workspace.Sync` lo mantiene al día con el disco. |
| `workspace_preferred` | string | No          | El archivo que el usuario eligió entre varios. Gana siempre que está en la raíz, y se conserva mientras falta.                  |

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
          "origin": "src"
        },
        "MyUser/dotfiles": {
          "direction": "pull",
          "origin": "dst"
        }
      }
    }
  }
}
```

| Campo                     | Tipo   | Obligatorio | Descripción                                                                  |
| ------------------------- | ------ | ----------- | ---------------------------------------------------------------------------- |
| `account_src`             | string | Sí          | Clave de la cuenta de origen.                                                |
| `account_dst`             | string | Sí          | Clave de la cuenta de destino (debe ser distinta de `account_src`).          |
| `repos.<key>.direction`   | string | Sí          | `"push"` o `"pull"`.                                                         |
| `repos.<key>.origin`      | string | Sí          | `"src"` o `"dst"`: qué cuenta tiene la fuente de verdad.                     |
| `repos.<key>.target_repo` | string | No          | Sobrescribe el nombre del repo de destino (por defecto: igual que la clave). |
| `repos.<key>.last_sync`   | string | No          | Marca de tiempo RFC3339 del último sync conocido.                            |
| `repos.<key>.error`       | string | No          | Último mensaje de error.                                                     |

El estado de los mirrors no se guarda: la GUI lo deriva de comprobaciones en vivo (consulta [pkg/mirror](#pkgmirror--mirroring-de-repositorios)).

### Perfiles de terminal

`pkg/terminals` es dueño del trío `terminal_apps[]` + `shells[]` + `terminal_profiles[]`. Incluye un catálogo compilado de los terminales y shells que Gitbox sabe detectar y lanzar en cada OS. Añadir un terminal es un cambio de código en `pkg/terminals/catalog.go`. El catálogo cubre:

- **Windows:** Windows Terminal, WezTerm, Alacritty, Tabby, ConEmu, Hyper, Mintty y ZOC; shells PowerShell 7, PowerShell 5, Command Prompt, Git Bash y WSL (una fila `wsl-<distro>` por cada distro instalada).
- **macOS:** iTerm2, Terminal, Warp, Kitty, Ghostty, WezTerm y Alacritty; shells Zsh, Bash, Fish y Dash.
- **Linux:** GNOME Terminal, Konsole, Terminator, Foot, Alacritty, Kitty, Tilda, Guake y xterm; shells Bash, Zsh, Fish, Ksh y Dash.

En cada arranque el catálogo sondea el host y combina el resultado con `gitbox.json`:

- Las entradas del catálogo instaladas se añaden a `terminal_apps[]` y `shells[]` solo si faltan, así que las filas existentes sobreviven a la nueva detección.
- Los flags hidden sobreviven a la nueva detección, también tras actualizaciones que amplían el catálogo.
- Los Profiles del usuario (`source: "user"`) y los Profiles heredados migrados (`source: "migrated"`) se conservan tal cual, aunque no estén en el conjunto recién detectado.
- Las entradas del catálogo que no están instaladas se omiten en silencio y aparecen en cuanto se instala el binario.

#### Composición según el OS

El conjunto de Profiles autoderivados sigue reglas distintas en cada plataforma:

- **Windows:** cada Profile empareja un Terminal con un Shell, para cada Terminal moderno y cada Shell. Los cuatro shells que se pueden lanzar directamente (`pwsh`, `powershell`, `cmd`, `wsl`) también reciben un Profile de shell directo, oculto por defecto. Cuando no hay ningún Terminal moderno instalado, cada shell recibe en su lugar un Profile de shell directo visible, y el gestor muestra un banner: "Instala Windows Terminal para tener la mejor experiencia."
- **macOS y Linux:** cada Profile es solo Terminal (`terminal_profiles[].shell == ""`). El shell de inicio de sesión del host es implícito: `pkg/launch.ResolveArgs` reduce los tokens de shell vacíos a cero elementos, y el gestor muestra el shell de inicio de sesión como un badge atenuado junto al nombre del Terminal. Un Terminal se puede seguir emparejando con un Shell que no sea el de inicio de sesión mediante el formulario `+ Añadir perfil` del gestor, que marca la fila con `source: "user"`.

El formulario para añadir perfiles sigue las mismas reglas: en macOS y Linux el selector de shell usa por defecto la entrada `(shell de inicio de sesión)`, y en Windows el shell es obligatorio.

#### Cómo funciona el emparejamiento de lanzamiento

Un Profile `WezTerm — PowerShell 7` o `Windows Terminal — PowerShell 7` no ejecuta sin más la plantilla genérica por Terminal. Gitbox busca primero una entrada que coincida en la propia config de terminal del usuario y solo recurre a la plantilla genérica cuando no coincide ninguna.

La búsqueda se ejecuta en cada lanzamiento, tras una caché en proceso invalidada por el mtime y el tamaño del archivo, así que las ediciones de `wezterm.lua` o `settings.json` se aplican sin reiniciar:

- **WezTerm:** Gitbox parsea `wezterm.lua` (`$WEZTERM_CONFIG_FILE`, luego `$XDG_CONFIG_HOME/wezterm/wezterm.lua`, luego `~/.config/wezterm/wezterm.lua`, luego `~/.wezterm.lua`) y busca una entrada de `config.launch_menu` cuya label coincida con el shell de Gitbox. Si la encuentra, lanza `wezterm-gui.exe start --cwd <path> -- <entry args>` y aplica los `set_environment_variables` de la entrada sobre el entorno del padre. El parser se ata solo a la tabla documentada `config.launch_menu`. Las entradas guardadas en una variable propia (`local profiles = { … }`) que alimenta un selector propio quedan invisibles; añadir `config.launch_menu = profiles` al final de `wezterm.lua` las expone sin cambiar el selector. Los callbacks Lua del selector (`color_scheme` por entrada, overrides de `mux.spawn_window`, handlers de `window-focus-changed`) no se reproducen, ya que solo se disparan cuando se elige una entrada desde el propio lanzador de WezTerm, nunca cuando un pane se crea desde fuera.
- **Windows Terminal:** Gitbox parsea `settings.json` (instalación de la Store, después la instalación Preview y después la instalación sin empaquetar bajo `%LOCALAPPDATA%`) y busca un perfil de `profiles.list` cuyo `name` coincida con el shell de Gitbox. Los perfiles de WT ocultos y los perfiles cuyo `source` aparece en el `disabledProfileSources` de nivel superior de WT nunca coinciden. Si lo encuentra, ejecuta `wt.exe -w 0 nt --profile "<name>" -d <path>`, y el propio `wt.exe` lee el `commandline`, la fuente, los colores y los flags de inicio del perfil. El prefijo `-w 0 nt` abre una pestaña nueva en la ventana de WT más reciente (o una ventana nueva si no existe ninguna), así que `firstWindowPreference: persistedWindowLayout` no abre una segunda ventana cuando WT se cerró con pestañas guardadas.
- **Sin coincidencia, sin config o terminal no instalado:** Gitbox recurre a la plantilla argv genérica (`wezterm-gui.exe start --cwd <path> -- <shell> <args>`, `wt.exe -d <path> <shell> <args>`, etc.), que sirve para los shells que no están conectados a la config del terminal.

Los Profiles de shell directo (sin terminal) se saltan la búsqueda: no hay config de terminal que consultar, así que ejecutar el shell directamente es lo correcto. En Windows se lanzan mediante `cmd.exe /C start "" /D <path> <shell>`, que da al shell una consola nueva y su carpeta de inicio.

El matcher de nombres de shell es tolerante:

- **Coincidencia directa:** el nombre normalizado de la entrada es igual al nombre visible del shell de Gitbox (`"PowerShell 7"`, `"WSL — Ubuntu-24.04"`).
- **Sufijo tras raya larga:** para nombres de Gitbox como `"WSL — Ubuntu-24.04"`, también coincide una entrada etiquetada solo como `"Ubuntu-24.04"`.
- **Fallback por patrón:** `pwsh` coincide con entradas que contienen `"powershell 7"`, `"powershell core"` o `"pwsh"`; `powershell` coincide con `"powershell 5"` o `"windows powershell"`; `cmd` coincide con `"command prompt"` o `"cmd exe"`; `git-bash` coincide con `"git bash"`; `wsl-<distro>` coincide con el slug pelado de la distro (`"ubuntu 24 04"`).

#### Lanzamientos de AI harnesses

Un AI harness se ejecuta dentro de un shell del Terminal Profile por defecto, así que hereda los archivos rc del usuario (donde nvm, npm y Git Bash ponen la mayoría de las CLIs de harness en el `PATH`), y el shell sigue abierto cuando el harness termina. `pkg/launch.HarnessInShell` envuelve el harness según la familia de shell:

- **PowerShell:** `-NoExit -EncodedCommand <base64>`, que sobrevive a la reconstrucción del argv de WT y al wrapper `cmd.exe start` sin problemas de comillas.
- **cmd:** `/K <harness> <args>`.
- **fish:** `-C '<harness> <args>'`.
- **WSL:** `-- sh -lc '<harness>; exec "${SHELL:-sh}" -l'`.
- **Otros shells POSIX** (bash, zsh, sh, dash, ksh, Git Bash): los args del shell más `-i -c '<harness>; exec <shell> <args>'`.

En Windows, Git Bash recibe los shims `.cmd` de npm por su nombre pelado (npm también instala un shim sin extensión en el mismo `PATH`) y los demás binarios por ruta con barras normales, y WSL siempre recibe el nombre pelado. Los shells nativos de Windows reciben la ruta resuelta.

`pkg/terminals.ResolveLaunch` coloca el shell envuelto:

- Un Profile sin shell (el valor por defecto en macOS y Linux) envuelve `$SHELL` y, si no existe, `/bin/sh`.
- Un perfil de WT emparejado recibe el shell envuelto como el commandline final de WT, y una entrada `launch_menu` de WezTerm emparejada se envuelve alrededor del shell de los args de la entrada.
- Terminal e iTerm en macOS se controlan mediante AppleScript (`osascript`), que ejecuta `cd <folder> && <harness>` en una ventana nueva.
- Los terminales cuya plantilla no puede alojar un comando (Warp, Kitty, Ghostty y Alacritty en macOS) devuelven un error que pide un Profile que aloje un shell.

La detección de harnesses resuelve los binarios mediante `doctor.Lookup` sin cambiar el `PATH` del proceso, así que es segura mientras se ejecutan fetches y clonados.

---

## 5. Arquitectura de credenciales

<p align="center">
  <img src="../diagrams/credential-flow.png" alt="Credential Flow" width="800" />
</p>

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Credential Types" width="800" />
</p>

### Flujo token

El usuario elige Token en los ajustes de credenciales de la cuenta → la app muestra la URL de creación de PAT específica del proveedor con los scopes necesarios → el usuario pega el token → la app lo valida mediante la API del proveedor y lo guarda en el archivo de credenciales (`~/.config/gitbox/credentials/<key>`). Al clonar, el token se embebe en la URL para autenticarse y después se elimina de la URL remota. El `.git/config` de cada clon recibe `credential.helper = store --file <path>` apuntando a ese mismo archivo, así que `git push` y `git pull` funcionan desde cualquier terminal sin GCM.

### Flujo GCM

El usuario elige GCM en los ajustes de credenciales de la cuenta → la app ejecuta `git credential fill`, que abre el inicio de sesión de GCM en el navegador → la app ejecuta `git credential approve` para persistir el resultado → prueba el acceso a la API con el token de GCM. Los clones usan HTTPS con el nombre de usuario. El `.git/config` de cada clon recibe `credential.helper = manager` más `username` y `provider` por host y `credential.credentialStore`, lo que lo hace autocontenido. El acceso a la API extrae el token OAuth mediante `git credential fill`.

### Flujo SSH

El usuario elige SSH en los ajustes de credenciales de la cuenta → la app escribe una entrada en `~/.ssh/config` y genera un par de claves (`ssh.key_type`, ed25519 por defecto) → muestra la clave pública para que el usuario la registre en el proveedor → prueba la conexión SSH. Los clones usan URLs `git@<host-alias>:repo.git` enrutadas mediante la config SSH. El acceso a la API usa opcionalmente un PAT para discovery, guardado en el archivo de credenciales. El `.git/config` de cada clon define un `credential.helper =` vacío para cancelar cualquier helper de credenciales global.

### Cambio de tipo de credencial

Cuando cambia el tipo de credencial de una cuenta, `ops.ChangeCredentialType` y `ops.ReconfigureClones`:

1. **Eliminan los artefactos del tipo anterior** (`ops.RemoveCredentialArtifacts`).
2. **Actualizan la config de la cuenta** con el tipo nuevo y el subobjeto de credenciales.
3. **Reconfiguran cada clon existente**: URL remota y config de credenciales por repo.

Lo que elimina el paso 1 depende solo del tipo anterior, así que ninguna credencial fantasma sobrevive a un cambio. `ops.DeleteCredential` ejecuta la misma limpieza.

| Tipo anterior | Archivo de credenciales `credentials/<key>` | Credencial GCM en caché (`git credential reject`) | Par de claves SSH + host en `~/.ssh/config` |
| ------------- | ------------------------------------------- | ------------------------------------------------- | ------------------------------------------- |
| Token         | Eliminado (el PAT)                          | ---                                               | ---                                         |
| GCM           | Eliminado si existe (PAT de mirror)         | Eliminada                                         | ---                                         |
| SSH           | Eliminado si existe (PAT de discovery)      | ---                                               | Eliminados                                  |

---

## 6. Arquitectura de la GUI

El backend Go (`cmd/gui/app.go`) expone métodos que el frontend Svelte llama mediante bindings TypeScript autogenerados, envueltos por `cmd/gui/frontend/src/lib/bridge.ts`. Las operaciones largas (clonado, refresco de estado, pull, discovery de mirrors, movimientos de repos) se ejecutan en goroutines y envían el progreso al frontend mediante eventos Wails.

**Estructura del frontend:**

- `App.svelte` compone el armazón y guarda el estado de la app, los listeners de eventos y las acciones que comparten varias partes.
- `src/lib/components/` contiene las partes del dashboard (barra superior, tira de actividad, banners, panel de ajustes, barra lateral, las vistas Requiere atención, Todos los repos, de cuenta y Mirrors, tarjeta y menú de cuenta, árbol de repos, filas y panel de detalle, pie, avisos, vista compacta). Se renderizan a partir de props.
- `src/lib/modals/` contiene un componente por modal, cada uno con su propio estado. App abre un modal mediante una pequeña función exportada y enlaza solo el estado que comparte, como el flujo de credenciales común a Añadir cuenta y Cambiar credencial. `modals.css` contiene el marco de diálogo, los botones y los controles de formulario que comparten los modales; `src/style.css` contiene los tokens del tema.
- `src/lib/status.ts` es el único sitio que traduce el estado de un repo a su forma, color, etiqueta del glosario, tipo Simple y rango de atención, y la credencial de una cuenta a sus palabras de salud y su acción de arreglo. `src/lib/views.ts` resuelve la vista guardada y construye la lista y los conteos de Requiere atención. `src/lib/activity.ts` sigue las ejecuciones de Fetch de todo y Pull de todo y sus resúmenes. `src/lib/notices.ts` contiene las tarjetas de resultado y los mensajes de error que sustituyen a `window.alert()`. `src/lib/repo-tree.ts` construye el árbol de clones anidados y ordena los hermanos (`compareRepoKeys`). Cada uno tiene un archivo `node:test`.
- Cada texto que ve el usuario pasa por `$t()` de `src/lib/i18n.ts`, que contiene los catálogos en inglés y español con las mismas claves. Una frase con una clave de config, negrita o énfasis en línea sigue siendo una sola entrada del catálogo con los marcadores `` `code` ``, `**strong**` y `*em*`, que renderiza `src/lib/Rich.svelte`. `src/lib/i18n.test.mjs` falla con texto fijo en las plantillas, con claves que faltan en algún idioma y con claves que el código pide y no existen.

**Estructura del layout** (la [guía de la GUI](gui-guide.md) describe cada parte desde el lado del usuario):

- **Barra superior:** el logo de GitBox (que pliega la barra lateral, ⌘B / Ctrl+B), el anillo de salud de los repos, el anillo de salud de los mirrors (en Avanzado, cuando existen mirrors), Pull de todo y Fetch de todo, después Modo borrar, Vista compacta y el tema en Avanzado, el interruptor Simple/Avanzado y el engranaje de Ajustes.
- **Experiencia:** Simple muestra estados en lenguaje llano y oculta el detalle de git, Mirrors y los controles avanzados; Avanzado lo muestra todo. Se guarda como `global.ui_mode`; las instalaciones nuevas empiezan en Simple.
- **Tira de actividad:** bajo la barra superior mientras se ejecuta Fetch de todo o Pull de todo, con su alcance y una barra de progreso.
- **Barra lateral:** Requiere atención y Todos los repos con sus conteos, una entrada por cuenta (logo del proveedor, salud, conteo de atención), Añadir cuenta, Encontrados en disco (huérfanos) y Mirrors en Avanzado. Se pliega a un riel de iconos, siempre es el riel por debajo de 780 px, y recuerda la vista y el riel en `global.collapsed`.
- **Vistas:** Requiere atención (cuentas con una credencial que arreglar, y después repos agrupados según lo que necesitan), Todos los repos (un árbol de clones anidados por grupo de cuenta, con las carpetas de workspace que faltan de un contenedor como filas atenuadas), la vista de cuenta (la tarjeta de cuenta y sus clones) y Mirrors (solo en Avanzado). No hay vista Workspaces: los workspaces viven en su contenedor dentro del árbol de clones.
- **Tarjetas de resultado:** lo que hizo Fetch de todo o Pull de todo, y el error de cualquier acción, abajo a la derecha, por encima de los modales.
- **Pie de resumen:** conteos agregados de repos y mirrors, más la píldora de actualización cuando existe una release más nueva.
- **Vista compacta:** una ventana estrecha con el anillo de salud, las píldoras de cuenta y una píldora de resumen de mirrors.

**Comportamiento transversal:**

- **Backup automático de la config:** consulta [Backups automáticos](#4-formato-de-config-v5).
- **Estado de la ventana:** la posición y el tamaño se guardan por modo de vista (`window` y `compact_window`) y se restauran al arrancar.
- **Autostart:** registro específico de cada plataforma (launch agent en macOS, registro en Windows), configurable desde la GUI.
- **Crear repo:** se pueden crear repos en el proveedor (bajo el usuario o una org) desde la tarjeta o el menú de la cuenta, con el desplegable de propietario rellenado mediante `OrgLister`.
- **Ediciones externas:** cuando `gitbox.json` cambia en disco, la GUI lo recarga cuando la ventana recupera el foco.

---

## 7. Principios de diseño UX

El usuario no necesita conocer los detalles internos de Git: las acciones son verbos que hacen lo que dicen.

- **Feedback en vivo:** el estado de cada repo se actualiza en cuanto se conoce, no por lotes. Las operaciones largas muestran el progreso y después saltan al estado final; Fetch de todo y Pull de todo indican su alcance mientras se ejecutan y dejan una tarjeta de resultado.
- **Silencioso por defecto:** la UI resalta los errores, los avisos y los repos que necesitan atención; los repos limpios se quedan tranquilos.
- **Colores coherentes:** los colores de estado coinciden en anillos, badges y filas. La [guía de la GUI](gui-guide.md#comprobación-automática) enumera qué significa cada estado y color.
- **Comportamiento predecible:** las listas siguen el orden del archivo de config, las acciones son idempotentes, los errores dicen qué hacer a continuación y los tokens nunca se muestran.

---

## 8. Seguridad

- **Sin secretos en el archivo de config.** `gitbox.json` contiene solo URLs, nombres de usuario, rutas de carpeta y flags de preferencia. Las llamadas a la API de los proveedores leen los tokens de los archivos de credenciales o de GCM en tiempo de ejecución.
- **Los PATs** viven en archivos de credenciales de gitbox (`~/.config/gitbox/credentials/<key>`), en texto plano con formato git-credential-store y permisos 0600: el mismo modelo que `~/.git-credentials` y las claves privadas SSH.
- **Los tokens OAuth de GCM** viven en el almacén de credenciales del OS (Windows Credential Manager, macOS Keychain, Linux Secret Service), gestionado por Git Credential Manager.
- **Las claves privadas SSH** son archivos estándar de `~/.ssh/` con permisos 0600.
- **URLs de clonado saneadas:** un token usado para clonar se elimina después de la URL remota. Las operaciones git posteriores se autentican mediante el helper de credenciales por repo, no mediante la URL.
- **Aislamiento de credenciales por repo:** el `.git/config` de cada clon cancela los helpers de credenciales globales y define el suyo, lo que evita fugas de credenciales entre cuentas y credenciales fantasma de los refresh tokens OAuth de GCM.
- **Sin secretos en la salida:** los tokens nunca se muestran, ni siquiera en los mensajes de error.
- **Repositorio público:** los archivos versionados no contienen hostnames, nombres de usuario, emails ni tokens reales.

---

## 9. Diagramas

Los diagramas viven en `docs/diagrams/` como archivos `.drawio` editables, que se abren con [draw.io](https://app.diagrams.net/) o con la extensión drawio de VS Code:

- `architecture-overview.drawio`: componentes de alto nivel del sistema.
- `credential-flow.drawio`: flujo de resolución de credenciales por tipo.
- `credential-types.drawio`: qué secretos alimentan el discovery, las operaciones git y los mirrors en cada tipo de credencial.
- `config-model.drawio`: el modelo de datos de accounts, sources, repos y mirrors.
