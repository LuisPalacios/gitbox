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

[Read in English](readme.md)

> [!NOTE]
> **gitbox v2 es solo GUI.** Trabajo casi siempre desde la app de escritorio, así que v2 elimina la CLI y la TUI para mantener bien una sola interfaz. Si vienes de v1, lee [Actualizar desde v1](#actualizar-desde-v1).

---

## Por qué uso gitbox

Gestiono varias cuentas Git — personales, corporativas, open-source, self-hosted — en GitHub, GitLab, Gitea, Forgejo y Bitbucket. El dolor siempre es el mismo: las credenciales se mezclan, los clones acaban con la identidad equivocada y cada máquina nueva significa empezar desde cero.

Construí gitbox para arreglar esto. Una app de escritorio para configurar mis cuentas, descubrir mis repos, clonarlos con las credenciales correctas y mantener todo sincronizado. Funciona en Windows, macOS y Linux.

Gitbox no implementa ningún protocolo Git ni lógica plumbing. Actúa como una capa de orquestación que llama a herramientas ya presentes en el sistema: **git** para clone, fetch, pull, status y operaciones de credential-manager; **ssh** y **ssh-keygen** para validación y generación de claves SSH; y el **abridor de archivos nativo del SO** para gestionar archivos, carpetas y lanzar aplicaciones locales.

## Instalar con el script bootstrap

Para macOS, Linux o Windows (Git Bash): un solo comando que descarga la última release, la extrae e instala la app de escritorio:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
```

Esto instala `GitboxApp` en `~/bin/` (macOS instala `GitboxApp.app` en `/Applications/`). En Linux también registra la app en el menú Activities para que pueda buscarla o fijarla al dock (omitir con `--no-desktop`). Ejecuta con `--help` para ver opciones.

Los hosts headless reciben en su lugar la CLI 1.x: `--cli-only` instala la última CLI `gitbox` 1.x, y el script la elige automáticamente en Linux sin pantalla (`DISPLAY` y `WAYLAND_DISPLAY` sin definir).

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
- **Setup del host autocurable** — gitbox vigila las piezas de tu setup global de git que suelen causar fallos crípticos y ofrece un arreglo de un clic: un `user.name` / `user.email` global persistente, un credential helper GCM ausente en `~/.gitconfig`, y un `~/.gitignore_global` ausente con un bloque curado de patrones de basura del SO (`.DS_Store`, `Thumbs.db`, `*~`, …)
- **Comprobación del sistema** — **Settings → System check** sondea el host para cada herramienta externa de la que depende gitbox (git, Git Credential Manager, ssh, ssh-keygen, ssh-add, wsl) y muestra el comando de instalación específico del SO para cualquier cosa ausente, para que descubras una dependencia rota antes de que falle durante la autenticación
- **Borrado seguro de cuentas + recuperación** — borrar una cuenta recorre cada mirror y workspace que la referencia para que no quede nada colgando; cada guardado significativo mantiene una ventana rotatoria de 10 backups fechados, y la pantalla de recuperación de corrupción puede restaurar cualquiera con un clic
- **Acciones de un clic** — cada fila de clon (y cada cabecera de cuenta) tiene un menú kebab para abrir el clon en navegador, gestor de archivos, terminal, editor o AI CLI harness (Claude Code, Codex, Antigravity, …)
- **Indicadores de PR y review** — cada fila de clon muestra sus pull requests abiertos y solicitudes de review pendientes, obtenidas desde la API del proveedor
- **Workspaces de solo lectura** — gitbox descubre los archivos `.code-workspace` de VS Code existentes bajo las carpetas gestionadas, los lista en una pestaña Workspaces dedicada y abre uno en mi editor. Nunca los crea ni los edita — los archivos son míos.
- **Clones no estándar y contenedores multi-repo** — incorpora clones que viven fuera del árbol de carpetas estándar (raíces de escaneo extra configurables) y marca un repo "contenedor" para que gitbox descubra y adopte los repos hermanos clonados dentro de su árbol de trabajo (asociados a su cuenta real, almacenados en su sitio).

Cinco proveedores están soportados: GitHub, GitLab, Gitea, Forgejo y Bitbucket, y todos funcionan para discovery, clonado y creación de repos. El mirroring entre proveedores está completamente automatizado en Gitea, Forgejo y GitLab; para GitHub y Bitbucket gitbox muestra los pasos manuales de setup en lugar de manejar la UI. Lee la documentación para más detalles.

## La app de escritorio

Gitbox se distribuye como una única app de escritorio, `GitboxApp`, construida con **[Wails](https://wails.io/)** + Svelte sobre una librería Go compartida (`pkg/`). Solo necesita herramientas que ya están en el sistema: git, Git Credential Manager, ssh y tus terminales y editores.

| Plataforma | Binario         |
| ---------- | --------------- |
| Windows    | `GitboxApp.exe` |
| macOS      | `GitboxApp.app` |
| Linux      | `GitboxApp`     |

`GitboxApp --version` imprime la versión y termina.

<p align="center">
  <img src="assets/screenshot-gui.png" alt="Interfaz de escritorio de Gitbox que muestra tarjetas de cuenta, salud de repos y estado de mirror" width="800" />
</p>

## Otros métodos de instalación

### Instalar con instalador nativo

Ten en cuenta que este método de instalación se queja de apps no firmadas ni notarizadas. Descarga el instalador para tu plataforma desde la página de [Releases](https://github.com/LuisPalacios/gitbox/releases):

| Plataforma | Instalador                                          | Qué hace                                                                                                                                          |
| ---------- | --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Windows    | `gitbox-win-amd64-setup.exe`                        | Instala `GitboxApp.exe` en Program Files y crea accesos del menú Start                                                                            |
| macOS      | `gitbox-macos-arm64.dmg` / `gitbox-macos-amd64.dmg` | Abre el DMG, ejecuta `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal — copia la app a `/Applications/` y limpia flags de cuarentena |
| Linux      | `gitbox-x86_64.AppImage`                            | Autocontenido, se ejecuta directamente — no necesita instalación (incluye GTK 3 y WebKitGTK)                                                      |

Cada release incluye también un archivo `checksums.sha256` para verificar descargas.

### Instalación manual (zip)

Ten en cuenta que este método de instalación se queja de apps no firmadas ni notarizadas. La página de [Releases](https://github.com/LuisPalacios/gitbox/releases) también tiene zips por plataforma (`gitbox-<platform>-<arch>.zip`) que contienen la app cruda. Extráela y colócala donde quieras. La app no está firmada, así que el SO se quejará la primera vez.

En macOS: `xattr -cr GitboxApp.app`. En Windows: SmartScreen muestra "Windows protected your PC" — haz clic en **More info** → **Run anyway**. En Linux: `chmod +x GitboxApp`.

<p align="center">
  <img src="assets/screenshot-mac.png" alt="App de escritorio de Gitbox ejecutándose en macOS" width="800" />
</p>

## Actualizaciones

Gitbox comprueba actualizaciones en segundo plano una vez al día. Cuando hay una release más nueva disponible, aparece una píldora de actualización en el pie de la app. Haz clic en ella para descargar la release, verificar su checksum y reemplazar la app en su sitio; después reinicia la app.

## Actualizar desde v1

v2 elimina la CLI `gitbox` y su TUI. `GitboxApp` es ahora la única interfaz, y cubre lo que usaba de la CLI en el día a día: cuentas, credenciales, discovery, clone, pull, fetch, status, mirrors, workspaces, adopción de huérfanos, barrido de ramas, movimiento de repos y la comprobación del sistema.

Qué se mantiene igual y qué esperar:

- **Tu config funciona sin cambios.** v2 lee el mismo `~/.config/gitbox/gitbox.json`. El formato de config se mantiene en la versión 3, así que no se migra nada.
- **La GUI v1 se actualiza sola a v2.** El banner de actualización dentro de la app ofrece v2 como cualquier otra release. Solo reemplaza lo que ya está instalado a su lado; una CLI v1 junto a la app se queda donde está y sigue actualizándose dentro de 1.x.
- **El instalador de Windows limpia la CLI antigua.** Ejecutar `gitbox-win-amd64-setup.exe` sobre una instalación v1 elimina el antiguo `gitbox.exe` y su entrada en PATH.
- **La CLI y la TUI siguen vivas en v1.** La rama [`release/v1`](https://github.com/LuisPalacios/gitbox/tree/release/v1) mantiene v1 (CLI + TUI + GUI) y recibe fixes críticos y de seguridad como releases `v1.7.x`.
- **Los hosts headless conservan la CLI.** Ejecuta el script bootstrap con `--cli-only` para instalar la última CLI 1.x.

## Documentación

El [índice de documentación](docs/es/README.md) lo tiene todo: guías de usuario (GUI, credenciales), guías de desarrollo (build, testing, arquitectura) y material de referencia (formato de config, JSON schema).

## Contribuir

Para compilar desde código fuente, ejecutar pruebas y probar en varias plataformas, empieza con la [Guía de desarrollo](docs/es/developer-guide.md). El [índice de docs](docs/es/README.md) tiene un orden de lectura sugerido para nuevos colaboradores.

## Disclaimer

Este software se proporciona **"tal cual"**, sin garantía de ningún tipo. No soy responsable de ningún daño, pérdida de datos o problema de seguridad derivado del uso de gitbox o de su instalador. Los binarios no están firmados: el script bootstrap y las instrucciones manuales eliminan flags de seguridad del SO para que puedan ejecutarse. Al instalar y ejecutar gitbox aceptas este riesgo. Todo el código fuente está disponible en este repositorio bajo la licencia MIT; audítalo antes de usarlo.

## Licencia

[MIT](LICENSE)
