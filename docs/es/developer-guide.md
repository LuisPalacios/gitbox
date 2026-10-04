# Guía de desarrollo

## Requisitos previos

- **Go** 1.26+ — [instalar](https://go.dev/doc/install)
- **Node.js** 20.19+ o 22.12+ — [instalar](https://nodejs.org/) (frontend Svelte 5 + Vite 8; CI compila con Node 24)
- **Git** 2.39+
- **Wails CLI** v2 — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Específico por plataforma:** Windows necesita Git for Windows; macOS necesita Xcode CLI Tools (`xcode-select --install`); Linux necesita `libwebkit2gtk-4.1-dev` y `libgtk-3-dev`

Para pruebas multiplataforma vía SSH, consulta [multiplatform.md](multiplatform.md).

---

## Compilar desde el código fuente

gitbox v2 construye un solo binario, la app de escritorio `GitboxApp`. La CLI y la TUI viven solo en la rama `release/v1`.

```bash
# Sello de versión en build time. Sin él, la app recurre a `git describe` en runtime.
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"

# Copiar iconos de app desde assets/ al directorio de build de Wails (ahí no se versionan)
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # solo Windows

# Modo desarrollo (hot reload)
cd cmd/gui
wails dev

# Build de producción
wails build -ldflags "$LDFLAGS"
# Salida: cmd/gui/build/bin/GitboxApp[.exe]
```

`wails build` no puede hacer cross-compile de la GUI: cada target necesita el webview nativo de su host (WebView2 en Windows, WebKit en macOS, WebKitGTK en Linux). Para construir para otra plataforma, construye en esa plataforma — [multiplatform.md](multiplatform.md) muestra cómo lo hace `scripts/ship.sh` vía SSH.

El código Go embebe el frontend compilado desde `cmd/gui/frontend/dist`. Los comandos Go simples como `go vet ./...`, `go test ./...` o una comprobación rápida de compilación (`go build -o /dev/null ./cmd/gui`) necesitan que esa carpeta exista. `wails build` la crea; para crearla sin un build completo:

```bash
cd cmd/gui/frontend
npm ci
npm run build
```

Una vez construida la app, `GitboxApp --version` imprime la versión y sale.

### Decisiones clave de diseño

- **`pkg/` es el corazón** — toda la lógica de negocio vive en `pkg/`. `cmd/gui` solo contiene los bindings de Wails, el locking, los eventos y lo que es exclusivo de la GUI.
- **`pkg/ops` es la capa de servicio** — las operaciones que abarcan varios paquetes (ciclo de vida de cuentas, cambio de credenciales, planificación de clones, discovery) viven ahí, así los bindings de la GUI se mantienen finos y la lógica se puede probar sin Wails.
- **La GUI llama a Go directamente** — los bindings de Wails exponen métodos Go a Svelte. No hay subprocess spawning del propio gitbox.
- **Las operaciones git usan `os/exec`** — llamo al binario `git` del sistema, no a libgit2.
- **Las APIs de proveedores usan `net/http`** — Go estándar, sin dependencias de cliente HTTP externas.
- **Accounts (WHO) + Sources (WHAT)** — las cuentas definen identidad en un servidor (hostname, username, credenciales); las sources referencian una cuenta y contienen la lista de repos a gestionar. Esta separación permite que varias sources compartan la misma cuenta.
- **Unicidad de cuenta** — una cuenta es única por `(hostname, username)`.
- **Las claves de repo usan formato `org/repo`** — esto produce una estructura de carpetas de 3 niveles: `<source>/<org>/<repo>`. El campo `id_folder` sobrescribe el 2º nivel (org), y `clone_folder` sobrescribe el 3º nivel (o reemplaza toda la ruta cuando es absoluto).
- **Herencia de credenciales** — las cuentas tienen un `default_credential_type`; los repos lo heredan salvo que definan su propio `credential_type`.
- **Sin console flash en Windows** — cada `exec.Command` en `cmd/gui/` llama a `git.HideWindow(cmd)` antes de ejecutarse.
- **Autodetección de versión** — los builds locales ejecutan `git describe --tags --always` en runtime; CI inyecta versión y commit mediante ldflags.

---

## Añadir un proveedor nuevo

> Los proveedores se implementan en `pkg/provider/`. GitHub, GitLab, Gitea/Forgejo y Bitbucket funcionan. Para añadir un proveedor nuevo:

1. Crea `pkg/provider/newprovider.go`:

```go
package provider

import "context"

type NewProvider struct{}

// Required: Provider interface
func (p *NewProvider) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
    // Implement paginated API call to list repositories
}

// Optional: RepoCreator interface — enables repo creation from the GUI
func (p *NewProvider) CreateRepo(ctx context.Context, baseURL, token, username, owner, repoName, description string, private bool) error {
    // If owner is empty, create under the user's personal namespace.
    // If owner is non-empty, create under that organization.
}

func (p *NewProvider) RepoExists(ctx context.Context, baseURL, token, username, owner, repoName string) (bool, error) {
    // Check if a repo exists (used by mirror setup)
}

// Optional: OrgLister interface — enables the owner dropdown in "Create repo"
func (p *NewProvider) ListUserOrgs(ctx context.Context, baseURL, token, username string) ([]string, error) {
    // Return organization names the user belongs to
}

// Optional: PushMirrorProvider, PullMirrorProvider, RepoInfoProvider
// See existing implementations for examples.
```

1. Registra el proveedor en `pkg/provider/provider.go` (cuando la interfaz y la factory estén definidas):

```go
func NewFromConfig(acct *config.Account) (Provider, error) {
    switch acct.Provider {
    case "github":
        return &GitHub{...}, nil
    case "newprovider":
        return &NewProvider{...}, nil
    // ...
    }
}
```

1. Añade `"newprovider"` al enum `provider` en `json/gitbox.schema.json`.

2. Escribe pruebas en `pkg/provider/newprovider_test.go`.

---

## Añadir una operación a pkg/ops

Cuando una acción nueva de la GUI toca más de un paquete — config más clones en disco, credenciales más cada clone de una cuenta — pongo la lógica en `pkg/ops` y mantengo fino el binding de Wails:

1. Añade la función al archivo correspondiente en `pkg/ops/` (`account.go`, `credential.go`, `clone.go` o `discover.go`). Recibe un `*config.Config` y trabaja sobre los archivos que le pertenecen en disco.
2. No guardes ni bloquees dentro de `pkg/ops`. El llamante (`cmd/gui/app.go`) toma el lock de config, llama a la operación, guarda la config y emite eventos.
3. Mantén el trabajo lento por clone (como `ReconfigureClones`) en una función aparte, para que el llamante pueda ejecutarlo después de un guardado correcto.
4. Añade una prueba unitaria en `pkg/ops/ops_test.go`. Las pruebas lo aíslan todo: un `XDG_CONFIG_HOME` temporal, un `GIT_CONFIG_GLOBAL` temporal y una carpeta SSH temporal.
5. Añade el binding de Wails en `cmd/gui/app.go` y llámalo desde el frontend a través de `cmd/gui/frontend/src/lib/bridge.ts`.

---

## Pruebas

Inicio rápido:

```bash
go test -short ./...    # pruebas unitarias (no necesitan preparación más allá de la carpeta dist del frontend)
go test ./...           # todo (necesita test-gitbox.json para el escenario de pkg/ops)
```

Activa el pre-push hook una vez por clone: `git config core.hooksPath .githooks` — ejecuta una comprobación `gofmt -s`, `go vet` y pruebas unitarias antes de cada push.

### Comprobación de salud

Antes de abrir un PR ejecuto todos los comprobadores de código de una pasada:

```bash
./scripts/health.sh              # todas las comprobaciones, una línea de resumen cada una
./scripts/health.sh go svelte    # solo algunas comprobaciones ("go" = todas las de Go)
./scripts/health.sh -v           # todos los hallazgos en lugar de los 15 primeros
```

Es de solo lectura y termina con error cuando alguna comprobación encuentra algo. Comprobaciones de Go: `gofmt -s`, `go vet`, staticcheck, modernize, govulncheck, deadcode (con una lista de funciones que se mantienen a propósito) y `go test -short`. Comprobaciones del frontend: `svelte-check` (las variables y parámetros sin usar fallan mediante `noUnusedLocals`/`noUnusedParameters`), los warnings que imprime `npm run build` (los mismos diagnósticos de Svelte que CI muestra en su paso de build), los warnings de instalación de un `npm ci` limpio en una copia temporal (paquetes obsoletos, scripts de instalación no cubiertos por `allowScripts`) y `npm audit`. La comprobación de `npm ci` necesita npm 11.19 o posterior. Comprobaciones del repo: shellcheck en los scripts de shell, actionlint en los workflows y markdownlint con la config del skill `fixing-markdown` forzada a solo lectura.

Los analizadores de Go se ejecutan con `go run` en versiones fijadas al principio del script, así que no necesitan instalación. Las otras tres herramientas deben estar en el `PATH`:

```bash
scoop install shellcheck actionlint     # Windows (brew install … en macOS, apt/dnf en Linux)
npm install -g markdownlint-cli2
```

El workflow de PR ejecuta el mismo script como barrera: cualquier hallazgo hace fallar el job, incluido un aviso nuevo de govulncheck o de `npm audit`. CI fija shellcheck 0.11.0, actionlint 1.7.12 y markdownlint-cli2 0.23.3; usa esas versiones en local para que ambas ejecuciones coincidan.

Para el workflow completo de pruebas (preparación de fixture, pruebas de integración, checklists pre-PR y de release), consulta [testing.md](testing.md). Para pruebas multiplataforma vía SSH, consulta [multiplatform.md](multiplatform.md). Si usas Claude Code, `/test-plan` automatiza las comprobaciones pre-PR.

---

## Evolución del schema de config

Al añadir campos nuevos a la configuración:

1. Añade el campo a la struct Go adecuada en `pkg/config/config.go` — usa `json:"fieldName,omitempty"` con el casing correcto (por ejemplo, `useHttpPath` es camelCase para coincidir con convenciones GCM)
2. Añade el campo a `json/gitbox.schema.json` con una descripción clara
3. Actualiza `json/gitbox.jsonc` con un ejemplo
4. Si el campo pertenece a una cuenta vs una source, asegúrate de que está en la struct correcta (`Account` para identidad/credenciales, `Source` para qué clonar, `Repo` para overrides por repo)
5. Si hay implicaciones CRUD, actualiza `pkg/config/crud.go`
6. Actualiza las tablas de referencia de config en `docs/architecture.md` y `docs/es/architecture.md`
7. Añade pruebas para el campo nuevo en `pkg/config/config_test.go`

**Nunca subas el número de versión para cambios aditivos.** La versión 3 puede crecer con campos opcionales. Solo sube a versión 4 si hacen falta cambios incompatibles (renombres, eliminaciones, cambios de tipo).

---

## Proceso de release

### Versionado

La versión se **autodetecta desde tags de git** en runtime para builds locales. CI inyecta valores explícitos mediante ldflags:

```bash
# Build de CI con versión explícita (el SHA completo se trunca a 7 caracteres en runtime)
cd cmd/gui && wails build -ldflags "-X main.version=v2.0.0 -X main.commit=$(git rev-parse HEAD)"

# Los builds locales autodetectan ejecutando:
#   git describe --tags --always   → versión (por ejemplo, "v1.2.11")
#   git rev-parse --short HEAD     → SHA de commit (por ejemplo, "a99cf17")
# Formato mostrado:
#   CI:    "v2.0.0 (abc1234)"
#   Local: "v1.2.11-dev (a99cf17)"
#   Sin tags: "dev-a99cf17"
```

### Crear un release

Los releases están completamente automatizados mediante CI. Haz push de un tag de versión y GitHub Actions construye la app para cada plataforma, crea un GitHub Release y adjunta los assets:

```bash
git tag v2.0.0
git push origin v2.0.0
```

CI inyecta `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` en el build de la GUI.

### Integración continua

Se ejecutan dos workflows de GitHub Actions:

- `.github/workflows/pr.yml` se ejecuta en los pull requests: `go vet`, `go test -short`, `svelte-check` en el frontend y un `wails build` de Linux.
- `.github/workflows/ci.yml` se ejecuta en los tags de versión: construye la app en cada plataforma, empaqueta los instaladores y publica el GitHub Release.

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

Los nombres de los assets coinciden con los de v1, así el updater y el script bootstrap los encuentran del mismo modo. v2 elimina `gitbox-win-arm64.zip`, que solo llevaba la CLI.

El instalador de Windows se construye con Inno Setup (`scripts/installer.iss`). Instala solo `GitboxApp.exe` y no añade nada al PATH; cuando actualiza una instalación v1, elimina el antiguo `gitbox.exe` y su entrada en el PATH. Los DMGs de macOS se construyen con `create-dmg` e incluyen un script `Install Gitbox.command` incluido (`scripts/dmg/`) que copia `GitboxApp.app` a `/Applications/` y elimina flags de cuarentena. El AppImage de Linux lo construye `scripts/appimage/build-appimage.sh`, que usa linuxdeploy y su plugin GTK para incluir GTK 3, WebKitGTK y los procesos auxiliares de WebKit, de modo que el AppImage funciona en sistemas sin esas bibliotecas instaladas. La GUI de Linux y el AppImage se construyen a propósito en el runner `ubuntu-22.04`: así las bibliotecas incluidas no necesitan una glibc más nueva que la 2.35, lo que mantiene el AppImage funcionando en distribuciones más antiguas.

### Firma de código en macOS

Los DMGs de macOS están actualmente **sin firmar**. Los pasos de firma de código y notarización existen en el workflow de CI, pero están gated por el secreto `APPLE_CERTIFICATE`. Consulta [macos-signing.md](macos-signing.md) para instrucciones de configuración. Hasta que la firma esté configurada, el DMG incluye un script "Install Gitbox" que gestiona automáticamente la eliminación de cuarentena. Los usuarios también pueden usar el script bootstrap o las descargas ZIP.

### Auto-update

El paquete `pkg/update/` proporciona comprobación de versión y capacidades de self-update. La GUI ejecuta una comprobación en background una vez al día y muestra una píldora de actualización en el pie. El updater descarga el artefacto específico de la plataforma desde GitHub Releases, verifica el checksum SHA256 y reemplaza la app in place.

La GUI y la CLI de v1 se actualizan de forma distinta:

- La GUI sigue el release que GitHub marca como latest, incluido el salto de 1.x a 2.x. Solo reemplaza lo que ya está instalado junto a ella; en macOS reemplaza el bundle `GitboxApp.app` completo en la carpeta que lo contiene.
- La CLI de v1, mantenida en `release/v1`, se queda en la línea 1.x (`MaxMajor: 1`) y solo reemplaza su propio binario, así nunca degrada una GUI v2 instalada junto a ella. Usa su propio fichero de throttle (`.update-check-cli`).

### Líneas de release

v2 es solo GUI. La CLI y la TUI siguen vivas en 1.x, en la rama `release/v1`:

- `main` lleva v2 y posteriores. Los tags son del tipo `v2.y.z`.
- `release/v1` lleva el mantenimiento de v1. Solo recibe fixes críticos y de seguridad, con tags `v1.7.z`.
- CI publica un release como "latest" de GitHub solo cuando no existe ningún release con una versión major superior. Un tag `v1.7.z` publicado después de `v2.0.0` se publica por tanto con `--latest=false`, y las GUIs v2 nunca lo ven como actualización.

---

## Ciclo de vida de features

Sigo el backlog en GitHub en [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues). Las features usan la etiqueta `enhancement` (más `priority:P1` para las siguientes); los bugs usan la etiqueta `bug`. El tamaño y la severidad viven en el cuerpo del issue para mantener mínimo el conjunto de etiquetas.

El workflow:

1. **Capturar** — abrir un issue con un título corto y un cuerpo que describa el concepto y cualquier nota del codebase que querría que una sesión futura de Claude tuviera.
2. **Planificar** — discutir en comentarios, luego entrar en plan mode en Claude Code para diseñar una implementación archivo por archivo.
3. **Construir** — implementar el plan y verificar con `/test-plan`.
4. **Publicar** — referenciar el issue en el mensaje de commit (por ejemplo, `Closes #22`) para que se cierre automáticamente al hacer push.

Usa `gh issue list --label enhancement` o `gh issue view <n>` para revisar el radar desde la terminal (ejecuta primero `gh auth switch --user LuisPalacios`).

### Push directo a main vs rama + PR

Elijo según la tarea. Por defecto uso rama + PR cuando hay duda — el coste de un PR es trivial, el coste de un mal push a main es un revert.

**Push directo a main** para cambios de un archivo y mecánicamente obvios: una errata, un fix de una línea, un ajuste de docs. `go vet ./...` y las pruebas enfocadas deben pasar localmente. Referencia el issue con `Closes #N` en el mensaje de commit para que GitHub lo cierre automáticamente en push.

**Rama + PR** para todo lo demás: features multiarchivo, cambios de superficie pública en `pkg/`, refactors, trabajo UI — cualquier cosa que se beneficie de ver el diff completo o de dejar que CI gatee el merge. Los nombres de rama siguen `<type>/<issue>-<slug>`, por ejemplo `fix/31-ide-flash` o `feat/22-open-in-terminal`. El cuerpo del PR cierra el issue con `Closes #N`; auto-apruebo y mergeo inmediatamente.

Las contribuciones externas siempre entran mediante PRs desde forks — reviso, CI debe pasar, luego mergeo.

---

## Logo e iconos de app

La fuente de verdad para el logo es `assets/logo.svg`. Los archivos de iconos derivados que usa el build de Wails viven junto a él:

| Archivo              | Formato                   | Propósito                                                                         |
| -------------------- | ------------------------- | --------------------------------------------------------------------------------- |
| `assets/logo.svg`    | SVG                       | Archivo fuente, editable en [Boxy SVG](https://boxy-svg.com/) (app Windows/macOS) |
| `assets/appicon.png` | PNG 1024x1024             | Icono del bundle `.app` de macOS, icono desktop de Linux                          |
| `assets/icon.ico`    | ICO (256/128/64/48/32/16) | Icono del ejecutable Windows                                                      |

### Editar el logo

1. Abre `assets/logo.svg` en [Boxy SVG](https://boxy-svg.com/) (disponible como app desktop para Windows y macOS)
2. Edita el diseño
3. Exporta a PNG 1024x1024 — Boxy SVG tiene esto configurado en los metadatos `<bx:export>` del SVG. Guarda como `assets/appicon.png`
4. Convierte PNG a ICO con [icoconverter.com](https://www.icoconverter.com/) — selecciona los 6 tamaños (256, 128, 64, 48, 32, 16). Guarda como `assets/icon.ico`
5. Ejecuta `wails build` desde `cmd/gui/` — el build copia iconos desde `assets/` automáticamente

### Flujo de iconos en build time

El build de Wails lee iconos desde `cmd/gui/build/`:

- `cmd/gui/build/appicon.png` — Wails lo usa para todas las plataformas
- `cmd/gui/build/windows/icon.ico` — se embebe en el `.exe` de Windows

Estos **no se versionan** (gitignored bajo `cmd/gui/build/`). En su lugar, el workflow de CI y los builds locales los copian desde `assets/` antes de ejecutar `wails build`.

---

## Capturas del README

Hago las capturas del README con la app real ejecutándose sobre una flota falsa, así nunca muestran mis cuentas ni mis rutas. El kit vive en `scripts/demo-fleet/`:

- `build.sh` crea tres cuentas demo (Forgejo, GitHub personal, GitHub corporativo) con clones locales reales en estados elegidos (sincronizado, por detrás, por delante, con cambios, sin clonar), un `gitbox.json`, archivos de token y una config global de git aislada bajo `$TEMP/gbdemo`.
- `mock.py` simula las APIs de los proveedores en `127.0.0.1:3001-3003`, así las credenciales se verifican, aparecen los indicadores de PR y review, y los fetch funcionan contra los upstreams demo. Necesita [uv](https://docs.astral.sh/uv/).
- `run.sh` arranca el mock y lanza `GitboxApp` con `XDG_CONFIG_HOME` y `GIT_CONFIG_GLOBAL` apuntando a la demo, así mi config real, `~/.gitconfig` y el almacén de credenciales no se tocan. Cierra antes cualquier GitboxApp en ejecución.
- `shot.ps1` (Windows) redimensiona la ventana, hace clic en puntos dentro de ella y la captura a un PNG.

```bash
./scripts/demo-fleet/build.sh
./scripts/demo-fleet/run.sh &
pwsh scripts/demo-fleet/shot.ps1 -Out assets/screenshot-gui.png -Width 1360 -Height 1300
```

El README usa `assets/screenshot-gui.png` (claro), `assets/screenshot-dark.png`, `assets/screenshot-menu.png` y `assets/screenshot-compact.png`. `screenshot-gui.png` es también la captura AppStream del AppImage de Linux. Settings → Terminals lista los terminales y rutas reales del host, así que deja esa pantalla fuera de las capturas.

---

## Estilo de código

- Sigue convenciones Go estándar (`gofmt`, `go vet`)
- Usa `golangci-lint` si está disponible
- Los mensajes de error deben ir en minúsculas, sin puntuación final
- Las funciones exportadas necesitan comentarios doc
- Usa `context.Context` para operaciones que pueden cancelarse (operaciones async de GUI)
