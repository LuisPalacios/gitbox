<p align="center">
  <img src="assets/logo.svg" width="128" alt="gitbox">
</p>

<h1 align="center">Gitbox</h1>

<p align="center">
  <a href="https://github.com/LuisPalacios/gitbox/actions/workflows/ci.yml">
    <img src="https://github.com/LuisPalacios/gitbox/actions/workflows/ci.yml/badge.svg" alt="CI" />
  </a>
</p>

<p align="center">
  <strong>Cuentas y clones — nada más.</strong><br>
  <em>gitbox nunca añade, commitea, hace push ni modifica tus árboles de trabajo.</em>
</p>

[Read in English](README.md)

---

## Por qué uso gitbox

Gestiono varias cuentas Git — personales, corporativas, open-source, self-hosted — en GitHub, GitLab, Gitea, Forgejo y Bitbucket. El dolor siempre es el mismo: las credenciales se mezclan, los clones acaban con la identidad equivocada y cada máquina nueva significa empezar desde cero.

Construí gitbox para arreglar esto. Una herramienta para configurar mis cuentas, descubrir mis repos, clonarlos con las credenciales correctas y mantener todo sincronizado. Funciona en Windows, macOS y Linux.

Gitbox no implementa ningún protocolo Git ni lógica plumbing. Actúa como una capa de orquestación que llama a herramientas ya presentes en el sistema: **git** para clone, fetch, pull, status y operaciones de credential-manager; **ssh** y **ssh-keygen** para validación y generación de claves SSH; y el **abridor de archivos nativo del SO** para gestionar archivos, carpetas y lanzar aplicaciones locales.

## Instalar con el script bootstrap

Para macOS, Linux o Windows (Git Bash): un solo comando que descarga, extrae y configura PATH:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
```

Esto instala en `~/bin/` (la GUI de macOS va a `/Applications/`). En Linux también registra la GUI en el menú Activities para que pueda buscarla o fijarla al dock (omitir con `--no-desktop`). Ejecuta con `--help` para ver opciones. Útil para servidores headless o entornos CI donde el instalador nativo no resulta práctico.

> [!WARNING]
> **Gitbox no está firmado ni notarizado.** Los binarios no están firmados con código, así que macOS Gatekeeper, Windows SmartScreen y protecciones similares del SO los señalarán. El instalador bootstrap elimina estos flags automáticamente (`xattr -cr` en macOS, `Unblock-File` en Windows) para que los binarios puedan ejecutarse. **Al hacer esto estás confiando explícitamente en código sin firmar.** Te recomiendo auditar el [código fuente](https://github.com/LuisPalacios/gitbox) y el [script bootstrap](scripts/bootstrap.sh) antes de ejecutar nada. Este proyecto es open source con licencia MIT: inspecciónalo, compílalo tú mismo o no lo uses.

## Qué hace

- **Gestión multi-cuenta** — define identidades por proveedor con credenciales aisladas (GCM, SSH o Token)
- **Discovery automático** — encuentra todos mis repos mediante APIs de proveedor en lugar de listarlos a mano
- **Clonado inteligente** — cada repo se clona con la identidad y estructura de carpetas correctas, autocontenido en su propio `.git/config`
- **Estado de sync** — ve de un vistazo qué repos están clean, behind, dirty, diverged o cuyo remoto fue eliminado
- **Pull seguro** — pulls solo fast-forward; los repos dirty o con conflictos nunca se tocan
- **Mirroring entre proveedores** — mirrors push o pull entre proveedores para backups (por ejemplo, Forgejo → GitHub)
- **Mover un repositorio** — reubica un clon de una cuenta a otra, incluso entre proveedores (GitHub ↔ GitLab ↔ Forgejo), con preflight guiado, comprobación de scopes de credencial, push mirror, rewire de origin, borrado remoto de origen opcional y borrado local opcional del clon. La carpeta local termina apuntando a la cuenta nueva sin pasos adicionales
- **Cambio de credenciales** — cambia tipos de auth (GCM ↔ SSH ↔ Token) con limpieza automática
- **Setup del host autocurable** — gitbox vigila las piezas de tu setup global de git que suelen causar fallos crípticos y ofrece un arreglo de un clic desde CLI, TUI o GUI: un `user.name` / `user.email` global persistente, un credential helper GCM ausente en `~/.gitconfig`, y un `~/.gitignore_global` ausente con un bloque curado de patrones de basura del SO (`.DS_Store`, `Thumbs.db`, `*~`, …)
- **Comprobación del sistema (doctor)** — `gitbox doctor` (y el equivalente GUI/TUI) sondea el host para cada herramienta externa de la que depende gitbox (git, Git Credential Manager, ssh, tmux, wsl) e imprime el comando de instalación específico del SO para cualquier cosa ausente, para que descubras una dependencia rota antes de que falle durante la autenticación
- **Borrado seguro de cuentas + recuperación** — borrar una cuenta recorre cada mirror y workspace que la referencia para que no quede nada colgando; cada guardado significativo mantiene una ventana rotatoria de 10 backups fechados, y la pantalla de recuperación de corrupción de la GUI puede restaurar cualquiera con un clic
- **Acciones de un clic** — cada fila de clon (y cada cabecera de cuenta) tiene un menú kebab para abrir el clon en navegador, gestor de archivos, terminal, editor o AI CLI harness (Claude Code, Codex, Gemini, …)
- **Indicadores de PR y review** — cada fila de clon muestra sus pull requests abiertos y solicitudes de review pendientes, obtenidas desde la API del proveedor
- **Workspaces por tarea** — agrupa clones de distintas cuentas en un workspace multi-root de VS Code o un layout tmuxinator; lanza desde el kebab de un clon o desde una pestaña Workspaces dedicada (GUI + TUI + CLI). El auto-discovery adopta archivos `.code-workspace` y `.tmuxinator` dejados en disco, y el soporte tmuxinator completo respaldado por WSL se activa automáticamente en Windows.

Cinco proveedores están soportados: GitHub, GitLab, Gitea, Forgejo y Bitbucket, y todos funcionan para discovery, clonado y creación de repos. El mirroring entre proveedores está completamente automatizado en Gitea, Forgejo y GitLab; para GitHub y Bitbucket gitbox imprime los pasos manuales de setup en lugar de manejar la UI. Lee la documentación para más detalles.

## Tres interfaces

Gitbox se distribuye como dos binarios construidos desde la misma librería Go (`pkg/`).

La CLI y la TUI viven en un único binario: si ejecutas `gitbox` sin argumentos en una terminal, se lanza la TUI; si pasas cualquier comando, se ejecuta la CLI. Los uso principalmente en hosts Linux headless.

La GUI es un binario separado construido con **[Wails](https://wails.io/)** + Svelte.

| Plataforma | CLI / TUI    | GUI             |
| ---------- | ------------ | --------------- |
| Windows    | `gitbox.exe` | `GitboxApp.exe` |
| macOS      | `gitbox`     | `GitboxApp.app` |
| Linux      | `gitbox`     | `GitboxApp`     |

**Escritorio (GUI)**:

<p align="center">
  <img src="assets/screenshot-gui.png" alt="Interfaz de escritorio de Gitbox que muestra tarjetas de cuenta, salud de repos y estado de mirror" width="800" />
</p>

**Terminal (TUI)**:

<p align="center">
  <img src="assets/screenshot-tui.png" alt="Dashboard TUI de Gitbox" width="600" />
</p>

**Terminal (CLI)**:

<p align="center">
  <img src="assets/screenshot-cli.png" alt="CLI de Gitbox que muestra estado de sync de repos con salida coloreada" width="600" />
</p>

<!-- TUI demo GIF recorded with VHS (https://github.com/charmbracelet/vhs) goes here -->

## Otros métodos de instalación

### Instalar con instalador nativo

Ten en cuenta que este método de instalación se queja de apps no firmadas ni notarizadas. Descarga el instalador para tu plataforma desde la página de [Releases](https://github.com/LuisPalacios/gitbox/releases):

| Plataforma | Instalador                                          | Qué hace                                                                                                                            |
| ---------- | --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Windows    | `gitbox-win-amd64-setup.exe`                        | Instala en Program Files, añade a PATH, crea accesos del menú Start                                                                 |
| macOS      | `gitbox-macos-arm64.dmg` / `gitbox-macos-amd64.dmg` | Abre el DMG, ejecuta `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal — instala GUI + CLI, limpia flags de cuarentena |
| Linux      | `gitbox-linux-amd64.AppImage`                       | Autocontenido, se ejecuta directamente — no necesita instalación                                                                    |

Cada release incluye también un archivo `checksums.sha256` para verificar descargas.

Una vez instalado, gitbox comprueba actualizaciones automáticamente (una vez al día). Ejecuta `gitbox update` desde la CLI o haz clic en "Update" en el banner de la GUI cuando haya una versión nueva disponible.

### Instalación manual (zip)

Ten en cuenta que este método de instalación se queja de apps no firmadas ni notarizadas. La página de [Releases](https://github.com/LuisPalacios/gitbox/releases) también tiene zips por plataforma (`gitbox-<platform>-<arch>.zip`) que contienen los binarios crudos. Extrae y colócalos donde quieras. La app no está firmada, así que el SO se quejará la primera vez.

En macOS: `xattr -cr GitboxApp.app && xattr -cr gitbox && chmod +x gitbox`. En Windows: SmartScreen muestra "Windows protected your PC" — haz clic en **More info** → **Run anyway**. En Linux: `chmod +x gitbox GitboxApp`.

<p align="center">
  <img src="assets/screenshot-mac.png" alt="Gitbox en macOS mostrando GUI y terminal lado a lado" width="800" />
</p>

## Documentación

El [índice de documentación](docs/es/README.md) lo tiene todo: guías de usuario (GUI, CLI, credenciales), guías de desarrollo (build, testing, arquitectura) y material de referencia (comandos, formato de config, JSON schema).

## Contribuir

Para compilar desde código fuente, ejecutar pruebas y probar en varias plataformas, empieza con la [Guía de desarrollo](docs/developer-guide.md). El [índice de docs](docs/es/README.md) tiene un orden de lectura sugerido para nuevos colaboradores.

## Disclaimer

Este software se proporciona **"tal cual"**, sin garantía de ningún tipo. No soy responsable de ningún daño, pérdida de datos o problema de seguridad derivado del uso de gitbox o de su instalador. Los binarios no están firmados: el script bootstrap y las instrucciones manuales eliminan flags de seguridad del SO para que puedan ejecutarse. Al instalar y ejecutar gitbox aceptas este riesgo. Todo el código fuente está disponible en este repositorio bajo la licencia MIT; audítalo antes de usarlo.

## Licencia

[MIT](LICENSE)
