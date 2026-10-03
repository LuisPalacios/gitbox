<p align="center">
  <img src="assets/logo.svg" width="128" alt="gitbox">
</p>

<h1 align="center">Gitbox</h1>

<p align="center">
  <strong>Una app de escritorio para todas tus cuentas Git.</strong><br>
  <em>Cuentas y clones, nada más. Gitbox nunca hace commit, push ni toca tus árboles de trabajo.</em>
</p>

<p align="center">
  <a href="https://github.com/LuisPalacios/gitbox/actions/workflows/ci.yml"><img src="https://github.com/LuisPalacios/gitbox/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/LuisPalacios/gitbox/releases/latest"><img src="https://img.shields.io/github/v/release/LuisPalacios/gitbox" alt="Última release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/LuisPalacios/gitbox" alt="Licencia MIT" /></a>
</p>

<p align="center">
  <a href="readme.md">Read in English</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-dark.png">
    <img src="assets/screenshot-gui.png" alt="Gitbox mostrando tres cuentas con sus anillos de sincronización, y clones sincronizados, por detrás, por delante o con cambios locales" width="820">
  </picture>
</p>

## Qué es

Manejo un GitHub personal, un GitHub corporativo y un Forgejo self-hosted, y cada máquina que preparaba acababa igual: credenciales mezcladas, clones haciendo commit con la identidad equivocada y una tarde de volver a clonar a mano.

Gitbox lo arregla. Añado cada cuenta una vez, con su propia credencial (GCM, SSH o token). Gitbox descubre mis repos mediante las APIs de los proveedores, clona cada uno con la identidad correcta en un árbol de carpetas predecible y muestra la salud de toda la flota de un vistazo. Funciona en Windows, macOS y Linux, y con GitHub, GitLab, Gitea, Forgejo y Bitbucket.

Es para cualquiera que trabaje con más de una cuenta o proveedor Git y quiera cada clon bien configurado sin pensar en ello. Gitbox no reimplementa Git: maneja el `git`, el `ssh` y el Git Credential Manager que ya tienes en el sistema.

## Instalar

Descarga el instalador para tu plataforma desde la [última release](https://github.com/LuisPalacios/gitbox/releases/latest):

| Plataforma | Descarga                                            | Cómo instalar                                                                        |
| ---------- | --------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Windows    | `gitbox-win-amd64-setup.exe`                        | Ejecútalo. Instala `GitboxApp.exe` en Program Files con accesos en el menú Start     |
| macOS      | `gitbox-macos-arm64.dmg` / `gitbox-macos-amd64.dmg` | Abre el DMG y ejecuta `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal |
| Linux      | `gitbox-x86_64.AppImage`                            | `chmod +x` y ejecútalo. Autocontenido, incluye GTK 3 y WebKitGTK                     |

¿Prefieres la terminal? El script bootstrap descarga la última release y la instala de una vez en macOS, Linux o Windows (Git Bash). En Linux también añade gitbox al menú de aplicaciones:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
```

Ejecútalo con `--help` para ver opciones. Si prefieres colocar la app tú mismo, cada release incluye también zips simples (`gitbox-<platform>-<arch>.zip`) y un archivo `checksums.sha256`.

> [!WARNING]
> **Los binarios no están firmados ni notarizados**, así que macOS Gatekeeper y Windows SmartScreen los señalarán. El instalador DMG y el script bootstrap eliminan esos flags por ti (`xattr -cr` en macOS, `Unblock-File` en Windows). Desde un zip, ejecuta `xattr -cr GitboxApp.app` en macOS, o elige **More info → Run anyway** en SmartScreen. En cualquier caso estás confiando en código sin firmar, así que audita el [código fuente](https://github.com/LuisPalacios/gitbox) y el [script bootstrap](scripts/bootstrap.sh) antes, o compílalo tú mismo.

## Funcionalidades

- **Cuentas y credenciales.** Identidades aisladas por cuenta con autenticación GCM, SSH o token, y cambio entre ellas con un clic.
- **Descubrir y clonar.** Encuentra cada repo mediante la API del proveedor y clónalo con la identidad correcta, configurado en su propio `.git/config`.
- **Salud de la flota.** Ve qué clones están sincronizados, por detrás, por delante, con cambios o divergidos, además de sus pull requests abiertos y reviews pendientes.
- **Sync seguro.** Hace fetch de todo y pull solo fast-forward. Los clones con cambios o conflictos nunca se tocan.
- **Mirrors y movimientos.** Configura mirrors push o pull entre proveedores para backups, y mueve un repo a otra cuenta o proveedor con un flujo guiado.
- **Workspaces y lanzadores.** Abre cualquier clon en tu terminal, editor, gestor de archivos o AI harness (Claude Code, Codex, …), y abre workspaces de VS Code existentes.
- **Clones en cualquier sitio.** Adopta clones que viven fuera del árbol de carpetas estándar, incluidos repos anidados dentro de un contenedor multi-repo.
- **Setup autocurable.** Una comprobación del sistema para las herramientas que gitbox necesita, arreglos de un clic para ajustes globales de git que causan fallos crípticos, y backups fechados de la config que puedes restaurar.

Discovery, clonado y creación de repos funcionan en los cinco proveedores. El mirroring está completamente automatizado en Gitea, Forgejo y GitLab; para GitHub y Bitbucket gitbox muestra los pasos manuales.

<p align="center">
  <img src="assets/screenshot-menu.png" alt="Menú de acciones de un clon con entradas de navegador, gestor de archivos, perfil de terminal, editor y AI harness" width="560">
  &nbsp;
  <img src="assets/screenshot-compact.png" alt="Vista compacta: anillo de sincronización global y listas de repos por cuenta" width="220">
</p>

## Actualizaciones

Gitbox comprueba una vez al día si hay una release nueva. Cuando la hay, aparece una píldora de actualización en el pie de la app: haz clic para descargar, verificar e instalar la nueva versión, y después reinicia.

## Actualizar desde v1

v2 es solo app de escritorio: la CLI `gitbox` y su TUI ya no existen.

- **Tu config sigue funcionando.** v2 lee el mismo `~/.config/gitbox/gitbox.json` sin migrar nada.
- **La app se actualiza sola.** La app de escritorio v1 ofrece v2 como cualquier otra actualización, y el instalador de Windows elimina la CLI antigua y su entrada en PATH.
- **La CLI sigue viva en v1.** La rama [`release/v1`](https://github.com/LuisPalacios/gitbox/tree/release/v1) recibe fixes críticos como `v1.7.x`. En un host headless, ejecuta el script bootstrap con `--cli-only` para instalarla.

## Documentación

Empieza por la [guía de la GUI](docs/es/gui-guide.md) y la [configuración de credenciales](docs/es/credentials.md). El [índice de documentación](docs/es/README.md) cubre todo lo demás, desde la arquitectura hasta el formato de config.

## Contribuir

La [guía de desarrollo](docs/es/developer-guide.md) cubre cómo compilar desde código fuente, las pruebas y las comprobaciones multiplataforma.

## Disclaimer

Gitbox se proporciona **"tal cual"**, sin garantía de ningún tipo. No soy responsable de daños, pérdida de datos o problemas de seguridad derivados del uso de gitbox o de sus instaladores. Los binarios no están firmados, e instalarlos significa aceptar ese riesgo. Todo el código fuente está aquí bajo la licencia MIT; audítalo antes de usarlo.

## Licencia

[MIT](LICENSE)
