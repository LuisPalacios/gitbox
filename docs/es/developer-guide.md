# Guía de desarrollo

Esta guía cubre cómo compilar Gitbox desde el código fuente, comprobar un cambio, ampliar los proveedores, las operaciones y el schema de config, y publicar un release. Para ver cómo encajan las piezas, consulta [architecture.md](architecture.md).

---

## Requisitos previos

- **Go** 1.26+ — [instalar](https://go.dev/doc/install)
- **Node.js** 20.19+ o 22.12+ — [instalar](https://nodejs.org/) (frontend Svelte 5 + Vite 8; CI compila con Node 24)
- **Git** 2.39+
- **Wails CLI** v2 — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Específico por plataforma:** Windows necesita Git for Windows; macOS necesita Xcode CLI Tools (`xcode-select --install`); Linux necesita `libwebkit2gtk-4.1-dev` y `libgtk-3-dev`

Para pruebas multiplataforma vía SSH, consulta [multiplatform.md](multiplatform.md).

---

## Compilar desde el código fuente

Gitbox v2 construye un solo binario, la app de escritorio `GitboxApp`. La CLI y la TUI de v1 viven solo en la rama `release/v1`.

```bash
# Sello de versión en build time. Sin él, la app recurre a `git describe` en runtime.
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"

# Copiar iconos de app desde assets/ al directorio de build de Wails (ahí no se versionan)
cp assets/appicon.png cmd/gui/build/appicon.png
mkdir -p cmd/gui/build/windows                         # no existe en un clon nuevo
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # solo Windows

# Modo desarrollo (hot reload)
cd cmd/gui
wails dev

# Build de producción
# Solo macOS: apunta a macOS 13, el mínimo del runtime de Go (Wails usa 10.13 por defecto)
export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
wails build -ldflags "$LDFLAGS"          # Linux: añade -tags webkit2_41
# Salida: cmd/gui/build/bin/GitboxApp[.exe] (GitboxApp.app en macOS)
```

`wails build` no puede hacer cross-compile de la GUI: cada target necesita el webview nativo de su host (WebView2 en Windows, WebKit en macOS, WebKitGTK en Linux). Para construir para otra plataforma, construyo en esa plataforma; [multiplatform.md](multiplatform.md) muestra cómo lo hace `scripts/ship.sh` vía SSH.

El código Go embebe el frontend compilado desde `cmd/gui/frontend/dist`. Los comandos Go simples como `go vet ./...`, `go test ./...` o una comprobación rápida de compilación (`go build -o /dev/null ./cmd/gui`) necesitan que esa carpeta exista. `wails build` la crea; para crearla sin un build completo:

```bash
cd cmd/gui/frontend
npm ci
npm run build
```

Una vez construida la app, `GitboxApp --version` imprime la versión y sale.

### Reglas de diseño

Estas reglas se cumplen en cada cambio. [architecture.md](architecture.md) explica el resto del diseño.

- **`pkg/` contiene la lógica de negocio.** `cmd/gui` solo contiene los bindings de Wails, el locking, los eventos y lo que es exclusivo de la GUI.
- **`pkg/ops` nunca guarda ni bloquea.** El llamante en `cmd/gui` toma el lock de config, ejecuta la operación, guarda y emite eventos.
- **Git se ejecuta mediante `os/exec`.** Gitbox llama al binario `git` del sistema, no a libgit2; las APIs de proveedores usan `net/http`.
- **Sin console flash en Windows.** Cada `exec.Command` en `cmd/gui/` llama a `git.HideWindow(cmd)` antes de ejecutarse.

---

## Pruebas

Inicio rápido:

```bash
go test -short ./...    # pruebas unitarias (no necesitan preparación más allá de la carpeta dist del frontend)
go test ./...           # todo (necesita test-gitbox.json para el escenario de pkg/ops)
```

Activo el pre-push hook una vez por clon con `git config core.hooksPath .githooks`. [testing.md](testing.md) cubre los niveles de prueba, el fixture, las pruebas del frontend y las checklists pre-PR y de release. Uso `/test-plan` en Claude Code o `$test-plan` en Codex para automatizar las comprobaciones pre-PR.

### Comprobación de salud

Antes de abrir un PR ejecuto todos los comprobadores de código de una pasada:

```bash
./scripts/health.sh              # todas las comprobaciones, una línea de resumen cada una
./scripts/health.sh go svelte    # solo algunas comprobaciones ("go" = todas las de Go)
./scripts/health.sh -v           # todos los hallazgos en lugar de los 15 primeros
```

Es de solo lectura y termina con error cuando alguna comprobación encuentra algo. Comprobaciones de Go: `gofmt -s`, `go vet`, staticcheck, modernize, govulncheck, deadcode (con una lista de funciones que se mantienen a propósito) y `go test -short`. Comprobaciones del frontend: regresiones puras del árbol de clones (`npm test`), `svelte-check` (las variables y parámetros sin usar fallan mediante `noUnusedLocals`/`noUnusedParameters`), los warnings que imprime `npm run build` (los mismos diagnósticos de Svelte que CI muestra en su paso de build), los warnings de instalación de un `npm ci` limpio en una copia temporal (paquetes obsoletos, scripts de instalación no cubiertos por `allowScripts`) y `npm audit` (con una lista, `NPM_AUDIT_ALLOW`, para avisos sin corrección que nunca llegan al binario; un aviso de la lista que deja de aparecer hace fallar la comprobación). La comprobación de `npm ci` necesita npm 11.19 o posterior. Comprobaciones del repo: shellcheck en los scripts de shell, actionlint en los workflows markdownlint con la config del skill `fixing-markdown` forzada a solo lectura, y la comprobación del harness de agentes, que ejecuta las pruebas del motor y falla cuando un archivo renderizado del harness se desvía de su master en `ai/`.

Los analizadores de Go se instalan en una carpeta temporal en versiones fijadas al principio del script, así que no necesitan instalación. Vet, staticcheck y modernize analizan linux, darwin y windows en cada ejecución, así que los archivos específicos de cada plataforma se comprueban desde cualquier host. Las otras cuatro herramientas deben estar en el `PATH`:

```bash
scoop install shellcheck actionlint     # Windows (brew install … en macOS, apt/dnf en Linux)
npm install -g markdownlint-cli2
scoop install uv                        # brew install uv en macOS
```

El workflow de PR ejecuta el mismo script como barrera: cualquier hallazgo hace fallar el job, incluido un aviso nuevo de govulncheck o de `npm audit`. CI fija shellcheck 0.11.0, actionlint 1.7.12, markdownlint-cli2 0.23.3 y setup-uv 10.3.3; uso esas versiones en local para que ambas ejecuciones coincidan.

---

## Peculiaridades del entorno de desarrollo

### Lanzar GitboxApp desde Git Bash

Cuando arranco `GitboxApp.exe` desde una shell de Git Bash o MSYS2, la app hereda las variables de Windows en forma POSIX (`LOCALAPPDATA=/c/Users/you/AppData/Local`). Gitbox limpia el entorno que pasa a las terminales que abre, pero cuando Windows Terminal es el host de consola predeterminado, su camino de delegación puede saltarse esa limpieza. Las herramientas que leen esas variables como rutas de Windows, como `oh-my-posh` o los helpers de `$PROFILE`, fallan entonces con errores como `& '/c/Users/...' — not recognized as a cmdlet`.

Los usuarios finales nunca se encuentran con esto. Cualquiera de estas soluciones funciona:

- Lanzar `GitboxApp.exe` desde el Explorador, el menú Inicio o un acceso directo anclado, que lo arrancan con un entorno de Windows limpio.
- Usar un [perfil de Windows Terminal](gui-guide.md#perfiles-de-windows-terminal-y-wezterm) que coincida con la shell. Gitbox lanza entonces mediante `wt.exe --profile`, y Windows Terminal arranca la shell desde el contexto de su propio perfil.

---

## Trabajar con Claude Code y Codex

Claude Code y Codex comparten un único conjunto de instrucciones de proyecto y siete skills del repositorio, cada uno en su propio formato nativo. Edito solo los masters neutros de `ai/`: `main.md`, `rules/`, `context/` y `skills/`. El motor de `ai/scripts/sync.py` los renderiza para ambas herramientas como archivos reales y versionados: `.claude/CLAUDE.md`, `.claude/rules/`, `.claude/context/` y `.claude/skills/` para Claude Code, y `AGENTS.md`, `.codex/rules/`, `.codex/context/` y `.agents/skills/` para Codex.

La mayor parte del texto es común. Donde las herramientas difieren (nombres de herramientas, flujo de planificación, permisos), un master guarda ambas variantes en bloques de harness, y tokens como `{{INVOKE}}` y `{{CONTEXT}}` se expanden al prefijo y las carpetas de cada herramienta. Después de cualquier cambio bajo `ai/` renderizo y hago commit de los masters con ambos árboles, desde cualquiera de las dos herramientas:

```bash
uv run ai/scripts/sync.py diff   # qué cambiaría
uv run ai/scripts/sync.py sync   # renderiza ambos harness y verifica
```

La [comprobación de salud](#comprobación-de-salud) falla cuando un archivo renderizado se desvía de su master, así que una edición a mano de `AGENTS.md` o `.claude/CLAUDE.md` nunca llega a `main`.

Abro cualquiera de las dos herramientas en la raíz del repositorio e invoco una skill como `/work-issue 123` en Claude Code o `$work-issue 123` en Codex; las peticiones en lenguaje natural también seleccionan skills. Ambas herramientas siguen las mismas reglas de pruebas y requieren aprobación explícita antes de publicar en GitHub. La configuración nativa se sigue editando a mano: los permisos de Claude viven en `.claude/settings.json`, y Codex usa los permisos de su propia sesión.

Después de cambiar el harness, inicio una sesión nueva en cada herramienta y le pido que resuma las reglas del proyecto y enumere las siete skills. Para consultar el modelo, el presupuesto de tamaño y las comprobaciones en sesión nueva, consulta [la guía del harness](../../ai/context/guideline_harness.md).

---

## Añadir un proveedor nuevo

Los proveedores viven en `pkg/provider/`. GitHub, GitLab, Gitea/Forgejo y Bitbucket están implementados. Para añadir uno:

1. Crea `pkg/provider/newprovider.go` con un tipo que implemente `Provider` (`ListRepos`, paginado). Añade las interfaces opcionales de `provider.go` y `deleter.go` que el proveedor soporte: `RepoCreator` (crear repo, comprobar si existe), `OrgLister` (desplegable de propietario en Crear repo), `RepoDeleter`, `PushMirrorProvider`, `PullMirrorProvider`, `RepoInfoProvider` (comprobaciones de sincronización de mirrors) y `PRLister` (indicadores de PR). Los llamantes detectan cada una con una aserción de tipo y omiten o rechazan la función cuando falta.
2. Regístralo en `ByName` en `pkg/provider/provider.go`, la factory que usa cada llamante para obtener un proveedor por su nombre en la config:

    ```go
    case "newprovider":
        return &NewProvider{}, nil
    ```

3. Añade el proveedor a los switches por proveedor: la guía de tokens y los scopes en `pkg/provider/guide.go` y `ScopesForAction` en `pkg/provider/errors.go`, `SSHPublicKeyURL` en `pkg/credential/validate.go`, `providerDisplayName` en `pkg/credential/status.go`, y `GCMProviderFor` en `pkg/ops/credential.go` cuando GCM tiene un proveedor nativo para él.
4. Añade `"newprovider"` al enum `provider` en `json/gitbox.schema.json`, y a las opciones de proveedor en `cmd/gui/frontend/src/lib/modals/AddAccountModal.svelte` y `EditAccountModal.svelte`.
5. Escribe pruebas respaldadas por `httptest` en `pkg/provider/`, siguiendo `provider_test.go`.

---

## Añadir una operación a pkg/ops

Cuando una acción nueva de la GUI toca más de un paquete — config más clones en disco, credenciales más cada clon de una cuenta — pongo la lógica en `pkg/ops` y mantengo fino el binding de Wails:

1. Añade la función al archivo correspondiente en `pkg/ops/` (`account.go`, `credential.go`, `clone.go` o `discover.go`). Recibe un `*config.Config` y trabaja sobre los archivos que le pertenecen en disco.
2. No guardes ni bloquees dentro de `pkg/ops`. El llamante (`cmd/gui/app.go`) toma el lock de config, llama a la operación, guarda la config y emite eventos.
3. Mantén el trabajo lento por clon (como `ReconfigureClones`) en una función aparte, para que el llamante pueda ejecutarlo después de un guardado correcto.
4. Añade una prueba unitaria en `pkg/ops/ops_test.go`. Las pruebas lo aíslan todo: un `XDG_CONFIG_HOME` temporal, un `GIT_CONFIG_GLOBAL` temporal y una carpeta SSH temporal.
5. Añade el binding de Wails en `cmd/gui/app.go` y llámalo desde el frontend a través de `cmd/gui/frontend/src/lib/bridge.ts`.

---

## Evolución del schema de config

Al añadir campos nuevos a la configuración:

1. Añade el campo a la struct adecuada en `pkg/config/config.go`: `Account` para identidad y credenciales, `Source` para qué clonar, `Repo` para overrides por repo. Los campos opcionales usan `omitempty`. Las claves van en snake_case, salvo los campos que replican los nombres de otra herramienta (`useHttpPath` se queda en camelCase para coincidir con GCM).
2. Añade el campo a `json/gitbox.schema.json` con una descripción clara.
3. Añade un ejemplo a `json/gitbox.jsonc`.
4. Si hay implicaciones CRUD, actualiza `pkg/config/crud.go`.
5. Actualiza la [referencia de config](architecture.md#4-formato-de-config-v5) en `docs/architecture.md` y `docs/es/architecture.md`.
6. Añade pruebas para el campo nuevo en `pkg/config/config_test.go`.

**Los cambios aditivos nunca suben `config.CurrentVersion`.** La versión actual crece con campos opcionales. Solo un cambio incompatible (un renombre, una eliminación o un cambio de tipo) la sube, y entonces:

1. Sube `CurrentVersion` en `pkg/config/config.go`.
2. Añade el paso de migración a `decode` en `pkg/config/load.go`, condicionado a la versión leída del archivo (`from < N`), y enuméralo en el comentario doc de `decode`. `decode` respalda tanto `Parse` como `LoadWithRepair`, así que todos los cargadores migran igual, en memoria; el siguiente guardado escribe la versión nueva.
3. Actualiza la constante `version` en `json/gitbox.schema.json`, `json/gitbox.jsonc` y `json/test-gitbox.json.example`.
4. Añade una prueba de migración (`pkg/config/migrate_v<N>_test.go`) que cargue un archivo de la versión anterior.
5. Actualiza las notas de versionado en [architecture.md](architecture.md#4-formato-de-config-v5) y `ai/context/config-format.md`.

El ring de backups siempre conserva una copia de un archivo escrito con una versión anterior, para que un usuario pueda volver a un release que todavía lo lee.

---

## Proceso de release

### Versionado

Los builds locales detectan la versión desde los tags de git en runtime. Los builds de CI inyectan valores explícitos mediante ldflags:

```bash
# Build de CI con versión explícita (el SHA completo se trunca a 7 caracteres en runtime)
cd cmd/gui && wails build -ldflags "-X main.version=v2.0.0 -X main.commit=$(git rev-parse HEAD)"

# Los builds locales sin ldflags ejecutan:
#   git describe --tags --always   → versión, con "-dev" añadido
#   git rev-parse --short HEAD     → SHA de commit (por ejemplo, "a99cf17")
# Formato mostrado:
#   CI:                  "v2.0.0 (abc1234)"
#   Local:               "v2.0.0-3-ga99cf17-dev (a99cf17)"
#   Fuera de un repo:    "dev-none" (la carpeta de trabajo de la app no tiene .git)
```

### Crear un release

Hago push de un tag de versión y GitHub Actions construye la app para cada plataforma y crea un GitHub Release en borrador con los assets adjuntos. Después firmo los checksums del borrador en mi máquina, lo que lo publica:

```bash
git tag v2.0.0
git push origin v2.0.0
# cuando termina el workflow de CI:
./scripts/sign-release.sh v2.0.0
```

CI inyecta `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` en el build de la GUI. La clave de firma vive en mi agente SSH y nunca llega a CI. Consulta [release-signing.md](release-signing.md) para ver cómo funciona la firma, la configuración del agente y cómo usa un fork su propia clave.

### Líneas de release

v2 es solo GUI. La CLI y la TUI siguen vivas en 1.x, en la rama `release/v1`:

- `main` lleva v2 y posteriores. Los tags son del tipo `v2.y.z`.
- `release/v1` lleva el mantenimiento de v1. Solo recibe fixes críticos y de seguridad, con tags `v1.7.z`.
- `scripts/sign-release.sh` publica un release como "latest" de GitHub solo cuando no existe ningún release con una versión major superior. Un tag `v1.7.z` publicado después de `v2.0.0` se publica por tanto con `--latest=false`.

El updater de la GUI sigue el release que GitHub marca como latest, así que estas reglas deciden a qué versión se actualiza cada GUI instalada. La CLI de v1 se limita a 1.x (`MaxMajor: 1`). [architecture.md](architecture.md#pkgupdate--auto-update) describe cómo el updater comprueba, verifica e instala un release.

### Integración continua

Se ejecutan dos workflows de GitHub Actions:

- `.github/workflows/pr.yml` se ejecuta en los pull requests y en los push a `main`: `scripts/health.sh` como barrera (todos los comprobadores; cualquier hallazgo hace fallar el job), y después un `wails build` de Linux y un smoke test con `--version`.
- `.github/workflows/ci.yml` se ejecuta en los tags de versión: construye la app en cada plataforma, empaqueta los instaladores y crea el GitHub Release como borrador para que `scripts/sign-release.sh` lo firme y lo publique. Antes de etiquetar un cambio que solo ejercita el camino de release, lo ejecuto en seco con `gh workflow run ci.yml -f version=v0.0.0`: se ejecutan todos los jobs de build e instaladores, y el job de release se omite.

### Assets de release

Cada release produce estos artefactos:

| Asset                        | Contenido                                                                                            |
| ---------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gitbox-win-amd64.zip`       | `GitboxApp.exe`                                                                                      |
| `gitbox-win-amd64-setup.exe` | Instalador Windows Inno Setup (`GitboxApp.exe`, Start Menu, sin PATH; elimina un `gitbox.exe` de v1) |
| `gitbox-macos-arm64.zip`     | `GitboxApp.app`                                                                                      |
| `gitbox-macos-arm64.dmg`     | Imagen de disco macOS con instalador incluido                                                        |
| `gitbox-macos-amd64.zip`     | `GitboxApp.app`                                                                                      |
| `gitbox-macos-amd64.dmg`     | Imagen de disco macOS con instalador incluido                                                        |
| `gitbox-linux-amd64.zip`     | `GitboxApp`                                                                                          |
| `gitbox-x86_64.AppImage`     | App Linux autocontenida (incluye GTK 3 y WebKitGTK)                                                  |
| `checksums.sha256`           | Hashes SHA256 de todos los artefactos                                                                |
| `checksums.sha256.sig`       | Firma SSH sobre el tag y `checksums.sha256`, añadida por `scripts/sign-release.sh`                   |

El updater y el script bootstrap buscan estos assets por nombre, también desde instalaciones v1, así que nunca los renombro.

El instalador de Windows se construye con Inno Setup (`scripts/installer.iss`). Instala solo `GitboxApp.exe` y no añade nada al PATH; cuando actualiza una instalación v1, elimina el antiguo `gitbox.exe` y su entrada en el PATH. Los DMGs de macOS se construyen con `create-dmg` e incluyen un script `Install Gitbox.command` (`scripts/dmg/`) que copia `GitboxApp.app` a `/Applications/` y elimina los flags de cuarentena. El AppImage de Linux lo construye `scripts/appimage/build-appimage.sh`, que usa linuxdeploy y su plugin GTK para incluir GTK 3, WebKitGTK y los procesos auxiliares de WebKit, de modo que el AppImage funciona en sistemas sin esas bibliotecas instaladas. La GUI de Linux y el AppImage se construyen a propósito en el runner `ubuntu-22.04`: así las bibliotecas incluidas no necesitan una glibc más nueva que la 2.35, lo que mantiene el AppImage funcionando en distribuciones más antiguas.

### Firma de código en macOS

Los DMGs de macOS están **sin firmar** por ahora. El workflow de CI tiene pasos de firma de código y notarización, condicionados al secreto `APPLE_CERTIFICATE`. Consulta [macos-signing.md](macos-signing.md) para las instrucciones de configuración. Hasta que la firma esté configurada, el script "Install Gitbox" incluido en el DMG elimina el flag de cuarentena, y los usuarios también pueden instalar con el script bootstrap o las descargas ZIP.

---

## Ciclo de vida de features

Sigo el backlog en GitHub en [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues). Las features usan la etiqueta `enhancement` (más `priority:P1` para las siguientes); los bugs usan la etiqueta `bug`. El tamaño y la severidad viven en el cuerpo del issue para mantener mínimo el conjunto de etiquetas.

El workflow:

1. **Capturar** — abrir un issue con un título corto y un cuerpo que describa el concepto y cualquier nota del código que querría que tuviera una sesión futura de un agente.
2. **Planificar** — discutir en comentarios y después planificar en Claude Code o Codex para diseñar una implementación archivo por archivo.
3. **Construir** — implementar el plan y verificar con `/test-plan`.
4. **Publicar** — referenciar el issue en el mensaje de commit (por ejemplo, `Closes #22`) para que se cierre automáticamente al hacer push.

Reviso el backlog desde la terminal con `gh issue list --label enhancement` o `gh issue view <n>`, después de `gh auth switch --user LuisPalacios`.

### Push directo a main vs rama + PR

Elijo según la tarea y, cuando hay duda, uso rama + PR por defecto: un PR cuesta poco, un mal push a main cuesta un revert.

**Push directo a main** para cambios de un archivo y mecánicamente obvios: una errata, un fix de una línea, un ajuste de docs. `go vet ./...` y las pruebas enfocadas deben pasar en local. El mensaje de commit referencia el issue con `Closes #N` para que GitHub lo cierre al hacer push.

**Rama + PR** para todo lo demás: features multiarchivo, cambios en la superficie pública de `pkg/`, refactors, trabajo de UI — cualquier cosa que se beneficie de ver el diff completo o de dejar que CI controle el merge. Los nombres de rama siguen `<type>/<issue>-<slug>`, por ejemplo `fix/31-ide-flash` o `feat/22-open-in-terminal`. El cuerpo del PR cierra el issue con `Closes #N`; me autoapruebo y hago merge de inmediato.

Las contribuciones externas siempre entran mediante PRs desde forks — reviso, CI debe pasar, y luego hago merge.

---

## Logo e iconos de app

La fuente de verdad del logo es `assets/logo.svg`. Los archivos de iconos derivados que usa el build de Wails viven junto a él:

| Archivo              | Formato                   | Propósito                                                                         |
| -------------------- | ------------------------- | --------------------------------------------------------------------------------- |
| `assets/logo.svg`    | SVG                       | Archivo fuente, editable en [Boxy SVG](https://boxy-svg.com/) (app Windows/macOS) |
| `assets/appicon.png` | PNG 1024x1024             | Icono del bundle `.app` de macOS, icono de escritorio de Linux                    |
| `assets/icon.ico`    | ICO (256/128/64/48/32/16) | Icono del ejecutable de Windows                                                   |

### Editar el logo

1. Abre `assets/logo.svg` en [Boxy SVG](https://boxy-svg.com/) (disponible como app de escritorio para Windows y macOS).
2. Edita el diseño.
3. Exporta a PNG 1024x1024 — Boxy SVG tiene esto configurado en los metadatos `<bx:export>` del SVG. Guárdalo como `assets/appicon.png`.
4. Convierte el PNG a ICO con [icoconverter.com](https://www.icoconverter.com/) — selecciona los 6 tamaños (256, 128, 64, 48, 32, 16). Guárdalo como `assets/icon.ico`.
5. Copia los iconos a `cmd/gui/build/` y ejecuta `wails build`, como en [Compilar desde el código fuente](#compilar-desde-el-código-fuente).

### Flujo de iconos en build time

El build de Wails lee los iconos desde `cmd/gui/build/`:

- `cmd/gui/build/appicon.png` — Wails lo usa para todas las plataformas
- `cmd/gui/build/windows/icon.ico` — se embebe en el `.exe` de Windows

Estos **no se versionan** (gitignored bajo `cmd/gui/build/`). El workflow de CI y los builds locales los copian desde `assets/` antes de ejecutar `wails build`.

### Logos de herramientas e iconos de la barra superior

Los logos de herramientas de cada fila de repo y los iconos de la barra superior salen de originales SVG en `assets/svg/`, no del código. Sustituyo un archivo ahí y vuelvo a compilar, y la app lo adopta. Con `wails dev` la ventana se recarga sola.

- `assets/svg/brand/logo-<name>.svg` — original en color, se muestra tal cual en el tema claro
- `assets/svg/brand/logo-<name>-dark.svg` — versión de un solo color para el tema oscuro, repintada con el color de texto del tema
- `assets/svg/ui/<name>.svg` — icono de un solo color de la barra superior, repintado igual

El plugin de Vite `cmd/gui/frontend/vite-plugin-icons.ts` hace la conversión en cada build del frontend. Limpia cada archivo con svgo (quita el recuadro guía oculto de los iconos de la barra superior y los metadatos del editor, elimina los tamaños fijos) y expone el resultado como el módulo `virtual:icons` que leen `BrandIcon.svelte` y `UIIcon.svelte`. Un archivo de un solo color con un degradado, filtro, máscara, patrón o imagen hace fallar el build con su nombre de archivo. [`assets/svg/README.md`](../../assets/svg/README.md) tiene las reglas de nombres y de dibujo.

Para añadir un logo de herramienta nuevo uso la skill `preparing-brand-logos`. Descarga fuentes candidatas en la carpeta `tmp/` (ignorada por git), ajusta cada una a la plantilla de 150×150 con `scripts/fit_logo.py` y escribe `tmp/preview.html` con todos los logos sobre fondo claro y oscuro a tamaño real. Reviso y edito los archivos ahí, y solo después los muevo a `assets/svg/brand/`.

---

## Capturas del README

Hago las capturas del README con la app real ejecutándose sobre una flota falsa, así nunca muestran mis cuentas ni mis rutas. El kit vive en `scripts/demo-fleet/`:

- `build.sh` crea cuatro cuentas demo (Forgejo, GitHub personal, GitHub corporativo y una cuenta de GitLab con la credencial rota) con clones locales reales en estados elegidos (al día, por detrás, por delante, con cambios, divergido, sin clonar), un `gitbox.json`, archivos de token y una config global de git aislada bajo `$TEMP/gbdemo`.
- `mock.py` simula las APIs de los proveedores en `127.0.0.1:3001-3004`, así las credenciales se verifican, aparecen los indicadores de PR y de review, y los fetch funcionan contra los upstreams demo. El puerto 3004 rechaza todas las peticiones, así la cuenta de GitLab aparece en Requiere atención con su acción para arreglarla. Necesita [uv](https://docs.astral.sh/uv/).
- `run.sh` arranca el mock y lanza `GitboxApp` con `XDG_CONFIG_HOME` y `GIT_CONFIG_GLOBAL` apuntando a la demo, así mi config real, `~/.gitconfig` y el almacén de credenciales no se tocan. Cierra antes cualquier GitboxApp en ejecución.
- `shot.ps1` (Windows) redimensiona la ventana, hace clic en puntos dentro de ella y la captura a un PNG.

```bash
./scripts/demo-fleet/build.sh
./scripts/demo-fleet/run.sh &
pwsh scripts/demo-fleet/shot.ps1 -Out assets/screenshot-gui.png -Width 1360 -Height 1300
```

El README usa `assets/screenshot-gui.png` (modo Simple) y `assets/screenshot-advanced.png`, y la guía de la GUI usa `screenshot-advanced.png`, `screenshot-menu.png` y `screenshot-compact.png`, todas en el tema claro. `screenshot-gui.png` es también la captura AppStream del AppImage de Linux. Ajustes → Terminales lista las terminales y rutas reales del host, así que dejo esa pantalla fuera de las capturas.

---

## Estilo de código

- El código Go pasa la [comprobación de salud](#comprobación-de-salud): `gofmt -s`, `go vet`, staticcheck, modernize y deadcode, sin hallazgos. Ejecuto `gofmt -s -w` en los archivos cambiados antes de hacer commit.
- El código del frontend pasa `svelte-check` sin variables ni parámetros sin usar, y compila sin warnings de Svelte.
- Los scripts de shell pasan shellcheck, los workflows pasan actionlint y el Markdown pasa markdownlint con la config de `fixing-markdown`.
- Los mensajes de error van en minúsculas, sin puntuación final.
- Las funciones exportadas tienen comentarios doc.
- Las operaciones que la GUI puede cancelar reciben un `context.Context`.
