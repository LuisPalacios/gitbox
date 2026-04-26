# Guía de desarrollo

## Requisitos previos

- **Go** 1.26+ — [instalar](https://go.dev/doc/install)
- **Node.js** 20+ — [instalar](https://nodejs.org/) (para el frontend Svelte)
- **Git** 2.39+
- **Wails CLI** v2 — `go install github.com/wailsapp/wails/v2/cmd/wails@latest` (solo builds de GUI)
- **Específico por plataforma:** Windows necesita Git for Windows; macOS necesita Xcode CLI Tools (`xcode-select --install`); Linux necesita `libwebkit2gtk-4.1-dev` y `libgtk-3-dev`

Para pruebas multiplataforma vía SSH, consulta [multiplatform.md](multiplatform.md).

---

## Compilar desde el código fuente

### Solo CLI

```bash
# Desde la raíz del repositorio
go build -o build/gitbox ./cmd/cli

# Cross-compile para otras plataformas
GOOS=linux   GOARCH=amd64 go build -o build/gitbox-linux-amd64         ./cmd/cli
GOOS=darwin  GOARCH=arm64 go build -o build/gitbox-darwin-arm64        ./cmd/cli
GOOS=darwin  GOARCH=amd64 go build -o build/gitbox-darwin-amd64        ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o build/gitbox-windows-amd64.exe   ./cmd/cli
GOOS=windows GOARCH=arm64 go build -o build/gitbox-windows-arm64.exe   ./cmd/cli
```

### GUI (Wails)

```bash
# Copiar iconos de app desde assets/ al directorio de build de Wails
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # solo Windows

# Modo desarrollo (hot reload)
cd cmd/gui
wails dev

# Build de producción
wails build
# Salida: cmd/gui/build/bin/GitboxApp[.exe]
```

### Decisiones clave de diseño

- **`pkg/` es el corazón** — tanto CLI como GUI importan desde ahí. Toda la lógica de negocio vive en `pkg/`.
- **La CLI es una envoltura fina** — `cmd/cli/main.go` conecta subcomandos con funciones de `pkg/`.
- **La GUI llama a Go directamente** — los bindings de Wails exponen funciones de `pkg/` a Svelte. No hay subprocess spawning.
- **Las operaciones git usan `os/exec`** — llamo al binario `git` del sistema, no a libgit2.
- **Las APIs de proveedores usan `net/http`** — Go estándar, sin dependencias de cliente HTTP externas.
- **Accounts (WHO) + Sources (WHAT)** — las cuentas definen identidad en un servidor (hostname, username, credenciales); las sources referencian una cuenta y contienen la lista de repos a gestionar. Esta separación permite que varias sources compartan la misma cuenta.
- **Unicidad de cuenta** — una cuenta es única por `(hostname, username)`.
- **Las claves de repo usan formato `org/repo`** — esto produce una estructura de carpetas de 3 niveles: `<source>/<org>/<repo>`. El campo `id_folder` sobrescribe el 2º nivel (org), y `clone_folder` sobrescribe el 3º nivel (o reemplaza toda la ruta cuando es absoluto).
- **Herencia de credenciales** — las cuentas tienen un `default_credential_type`; los repos lo heredan salvo que definan su propio `credential_type`.
- **La CLI usa Cobra** — cada subcomando vive en su propio archivo `*_cmd.go`, registrado en el `init()` de `main.go`.
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

// Optional: RepoCreator interface — enables repo creation from GUI/CLI
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

## Añadir un subcomando CLI nuevo

Cada comando vive en su propio archivo siguiendo la convención de nombre `*_cmd.go`. Este es el patrón usado en todo el codebase:

1. Crea `cmd/cli/newcommand_cmd.go`:

```go
package main

import (
    "fmt"

    "github.com/LuisPalacios/gitbox/pkg/config"
    "github.com/spf13/cobra"
)

var newcommandCmd = &cobra.Command{
    Use:   "newcommand",
    Short: "Description of the new command",
}

var newcommandListCmd = &cobra.Command{
    Use:   "list",
    Short: "List something",
    RunE: func(cmd *cobra.Command, args []string) error {
        cfg, err := loadConfig()
        if err != nil {
            return err
        }
        // Call pkg/ functions using cfg
        fmt.Println("done")
        return nil
    },
}

func init() {
    newcommandCmd.AddCommand(newcommandListCmd)
}
```

1. Registra el comando padre en el `init()` de `main.go`:

```go
rootCmd.AddCommand(newcommandCmd)
```

---

## Pruebas

Inicio rápido:

```bash
go test -short ./...    # pruebas unitarias (no necesitan preparación)
go test ./...           # todo (necesita test-gitbox.json para pruebas de integración)
```

Activa el pre-push hook una vez por clone: `git config core.hooksPath .githooks` — ejecuta `go vet` + pruebas unitarias antes de cada push.

Para el workflow completo de pruebas (preparación de fixture, pruebas de integración, checklists pre-PR y de release), consulta [testing.md](testing.md). Para pruebas multiplataforma vía SSH, consulta [multiplatform.md](multiplatform.md). Si usas Claude Code, `/test-plan` automatiza las comprobaciones pre-PR.

---

## Evolución del schema de config

Al añadir campos nuevos a la configuración:

1. Añade el campo a la struct Go adecuada en `pkg/config/config.go` — usa `json:"fieldName,omitempty"` con el casing correcto (por ejemplo, `useHttpPath` es camelCase para coincidir con convenciones GCM)
2. Añade el campo a `json/gitbox.schema.json` con una descripción clara
3. Actualiza `json/gitbox.jsonc` con un ejemplo
4. Si el campo pertenece a una cuenta vs una source, asegúrate de que está en la struct correcta (`Account` para identidad/credenciales, `Source` para qué clonar, `Repo` para overrides por repo)
5. Si hay implicaciones CRUD, actualiza `pkg/config/crud.go`
6. Actualiza la tabla de referencia de config en `docs/reference.md`
7. Añade pruebas para el campo nuevo en `pkg/config/config_test.go`

**Nunca subas el número de versión para cambios aditivos.** La versión 2 puede crecer con campos opcionales. Solo sube a versión 3 si hacen falta cambios incompatibles (renombres, eliminaciones, cambios de tipo).

---

## Proceso de release

### Versionado

La versión se **autodetecta desde tags de git** en runtime para builds locales. CI inyecta valores explícitos mediante ldflags:

```bash
# Build de CI con versión explícita (el SHA completo se trunca a 7 caracteres en runtime)
go build -ldflags "-X main.version=v0.2.0 -X main.commit=$(git rev-parse HEAD)" -o build/gitbox ./cmd/cli

# Los builds locales autodetectan ejecutando:
#   git describe --tags --always   → versión (por ejemplo, "v1.2.11")
#   git rev-parse --short HEAD     → SHA de commit (por ejemplo, "a99cf17")
# Formato mostrado:
#   CI:    "v0.2.0 (abc1234)"
#   Local: "v1.2.11-dev (a99cf17)"
#   Sin tags: "dev-a99cf17"
```

### Crear un release

Los releases están completamente automatizados mediante CI. Haz push de un tag de versión y GitHub Actions construye todos los binarios, crea un GitHub Release y adjunta los assets:

```bash
git tag v1.0.0
git push origin v1.0.0
```

CI inyecta `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` en los builds de CLI y GUI.

### Assets de release

Cada release produce estos artefactos:

| Asset                         | Contenido                                                                |
| ----------------------------- | ------------------------------------------------------------------------ |
| `gitbox-win-amd64.zip`        | `gitbox.exe` + `GitboxApp.exe`                                           |
| `gitbox-win-arm64.zip`        | `gitbox.exe` (solo CLI — build GUI ARM64 pendiente de runner ARM nativo) |
| `gitbox-win-amd64-setup.exe`  | Instalador Windows Inno Setup (PATH, Start Menu)                         |
| `gitbox-macos-arm64.zip`      | `gitbox` + `GitboxApp.app`                                               |
| `gitbox-macos-arm64.dmg`      | Imagen de disco macOS con instalador incluido                            |
| `gitbox-macos-amd64.zip`      | `gitbox` + `GitboxApp.app`                                               |
| `gitbox-macos-amd64.dmg`      | Imagen de disco macOS con instalador incluido                            |
| `gitbox-linux-amd64.zip`      | `gitbox` + `GitboxApp`                                                   |
| `gitbox-linux-amd64.AppImage` | App Linux autocontenida (CLI + GUI)                                      |
| `checksums.sha256`            | Hashes SHA256 de todos los artefactos                                    |

El instalador de Windows se construye con Inno Setup (`scripts/installer.iss`). Los DMGs de macOS se construyen con `create-dmg` e incluyen un script `Install Gitbox.command` incluido (`scripts/dmg/`) que copia binarios y elimina flags de cuarentena. El AppImage de Linux se construye con `appimagetool` usando los archivos de soporte en `scripts/appimage/`.

### Firma de código en macOS

Los DMGs de macOS están actualmente **sin firmar**. Los pasos de firma de código y notarización existen en el workflow de CI, pero están gated por el secreto `APPLE_CERTIFICATE`. Consulta [macos-signing.md](macos-signing.md) para instrucciones de configuración. Hasta que la firma esté configurada, el DMG incluye un script "Install Gitbox" que gestiona automáticamente la eliminación de cuarentena. Los usuarios también pueden usar el script bootstrap o las descargas ZIP.

### Auto-update

El paquete `pkg/update/` proporciona comprobación de versión y capacidades de self-update. Tanto la CLI (`gitbox update`) como la GUI (comprobación en background + banner) lo usan. El updater descarga el artefacto específico de la plataforma desde GitHub Releases, verifica el checksum SHA256 y reemplaza los binarios in place.

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

**Rama + PR** para todo lo demás: features multiarchivo, cambios de superficie pública en `pkg/`, refactors, trabajo UI — cualquier cosa que se beneficie de ver el diff completo o de dejar que CI gatee el merge. Los nombres de rama siguen `<type>/<issue>-<slug>`, por ejemplo `fix/31-ide-flash` o `feat/22-open-in-terminal`. El cuerpo del PR cierra el issue con `Closes #N`; puedo auto-aprobar y mergear inmediatamente.

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

## Grabaciones demo TUI (VHS)

Usa [VHS](https://github.com/charmbracelet/vhs) (del equipo Charm) para grabar GIFs demo de terminal para el README y los docs. VHS lee archivos declarativos `.tape` y renderiza salida GIF/MP4/WebM.

### Instalar VHS

```bash
# macOS
brew install charmbracelet/tap/vhs

# Windows (scoop)
scoop install charmbracelet/vhs/vhs

# Go install
go install github.com/charmbracelet/vhs@latest
```

VHS requiere `ffmpeg` y `ttyd`. En la primera ejecución pedirá instalarlos.

### Grabar una demo

1. Crea un archivo `.tape` bajo `assets/` (por ejemplo, `assets/demo-tui.tape`):

   ```text
   Output assets/demo-tui.gif

   Set Shell "bash"
   Set FontSize 14
   Set Width 1200
   Set Height 600
   Set Theme "Catppuccin Mocha"

   Type "gitbox"
   Enter
   Sleep 2s
   Type "j"
   Sleep 0.5s
   Type "j"
   Sleep 0.5s
   Enter
   Sleep 2s
   ```

2. Ejecútalo:

   ```bash
   vhs assets/demo-tui.tape
   ```

3. El GIF de salida se escribe en la ruta especificada en la directiva `Output`.

### Convenciones

- Los archivos tape viven en `assets/` junto a los prototipos GUI
- Los GIFs de salida también van en `assets/` (por ejemplo, `assets/demo-tui.gif`)
- Usa el tema `Catppuccin Mocha` para coincidir con el tema oscuro de TUI
- Mantén las grabaciones por debajo de 15 segundos para embeds del README
- Añade los archivos tape a git, pero las salidas `.gif`/`.mp4` deberían estar gitignored (regenerar bajo demanda)

---

## Estilo de código

- Sigue convenciones Go estándar (`gofmt`, `go vet`)
- Usa `golangci-lint` si está disponible
- Los mensajes de error deben ir en minúsculas, sin puntuación final
- Las funciones exportadas necesitan comentarios doc
- Usa `context.Context` para operaciones que pueden cancelarse (operaciones async de GUI)
