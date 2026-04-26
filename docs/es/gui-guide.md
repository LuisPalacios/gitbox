<p align="center">
  <img src="../../assets/screenshot-gui.png" alt="Gitbox" width="800" />
</p>

# Gitbox Desktop — Guía de usuario

Gitbox es una app de escritorio que te ayuda a mantener todos tus proyectos Git organizados y actualizados, incluso cuando trabajas con varias cuentas en GitHub, GitLab, Forgejo y otros proveedores.

Esta guía recorre todo, desde el primer arranque hasta el uso diario.

## Requisitos previos

Descarga el instalador para tu plataforma desde la página de [Releases](https://github.com/LuisPalacios/gitbox/releases):

- **Windows** — `gitbox-win-amd64-setup.exe` (instalador con configuración de PATH y accesos del menú Start)
- **macOS** — `gitbox-macos-arm64.dmg` o `gitbox-macos-amd64.dmg` (abre el DMG, ejecuta el script de instalación desde Terminal)
- **Linux** — `gitbox-linux-amd64.AppImage` (autocontenido, solo descarga y ejecuta)

También puedes descargar los archivos ZIP (`gitbox-<platform>-<arch>.zip`) y extraerlos manualmente.

> **Nota macOS:** La app no está firmada por Apple. El DMG incluye un script "Install Gitbox" que copia los binarios y elimina automáticamente los flags de cuarentena. Ejecuta `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal. Para instalación manual, usa `xattr -cr /path/to/GitboxApp.app` y `xattr -cr /path/to/gitbox`.

### Linux AppImage

Descarga la AppImage, dale permiso de ejecución y ejecútala:

```bash
chmod +x gitbox-linux-amd64.AppImage
./gitbox-linux-amd64.AppImage
```

La GUI requiere un entorno de escritorio con servidor de pantalla (X11 o Wayland).

## Paso 1: primer arranque

La primera vez que abres Gitbox, te pide elegir una **root folder**: ahí vivirán todos tus proyectos en disco. Algo como `~/00.git` o `C:\repos` funciona bien.

Haz clic en **Get started** y ya estás dentro.

## Paso 2: añadir cuentas

Una cuenta le dice a Gitbox quién eres en un servidor concreto. Por ejemplo, tu cuenta de GitHub o el GitLab de tu empresa.

Haz clic en la tarjeta **+** para añadir una. Rellenarás:

1. **Account key** — un nombre corto que eliges (por ejemplo, `github-personal`). También se convierte en el nombre de carpeta en disco.
2. **Provider** — elige tu servicio (GitHub, GitLab, Gitea, Forgejo o Bitbucket).
3. **URL** — la dirección del servidor. Para GitHub es `https://github.com`.
4. **Username** — tu nombre de cuenta en ese servicio.
5. **Name and Email** — la identidad usada en tus commits Git.
6. **Credential type** — cómo autenticará Gitbox (ver más abajo).

### Configurar credenciales

Después de crear la cuenta, Gitbox necesita una forma de iniciar sesión en tu proveedor. Hay tres opciones:

- **GCM (Git Credential Manager)** — La opción más sencilla. Gitbox abre tu navegador para que inicies sesión. Mejor para GitHub y GitLab.
- **Token (Personal Access Token)** — Creas un token en el sitio web de tu proveedor y lo pegas en Gitbox. La app te dice exactamente qué URL visitar y qué permisos seleccionar.
- **SSH** — Gitbox genera un par de claves por ti. Copias la clave pública y la añades a los ajustes de tu proveedor. La app te da el enlace directo.

Cuando las credenciales están configuradas, la tarjeta de cuenta muestra una **insignia verde** con el tipo de credencial: todo listo. Para más detalles sobre cada tipo y qué permisos seleccionar, consulta [credentials.md](credentials.md).

## Paso 3: encontrar y añadir proyectos

Haz clic en **Find projects** en una tarjeta de cuenta. Gitbox contacta con tu proveedor y lista todos los repositorios visibles para tu cuenta.

La ventana de discovery incluye:

- **Search field** — escribe para filtrar la lista cuando tienes muchos repos
- **Alphabetical sorting** — los repos se listan de la A a la Z para navegar fácilmente
- **Select all** — marca la casilla para seleccionar todo lo visible (respeta el filtro)
- **Already added** — los repos ya añadidos aparecen atenuados y no se pueden seleccionar de nuevo

Elige los que quieras y haz clic en **Add & Pull**. Gitbox los guarda en tu config y empieza a clonarlos en tu carpeta.

## Paso 4: día a día

### Entender las tarjetas de cuenta

Cada cuenta aparece como una tarjeta en la pestaña **Accounts**. Esto significa cada elemento:

- **Credential badge** (arriba a la derecha) — muestra tu tipo de credencial con un fondo de color:
  - **Green** — todo funciona
  - **Orange** — hay un problema menor (por ejemplo, permisos limitados)
  - **Red** — la credencial está rota o caducada
  - **Blue "config"** — todavía no hay credencial configurada; haz clic para empezar
- **Sync ring** — un círculo pequeño que muestra cuántos proyectos están sincronizados
- **Find projects** — descubre repos de tu cuenta (deshabilitado si las credenciales no funcionan)
- **Create repo** — crea un repositorio nuevo en el proveedor (deshabilitado si las credenciales no funcionan)

Si falta una credencial o está rota, toda la tarjeta se vuelve **rojo claro** para que lo notes de inmediato.

### Mantener proyectos sincronizados

#### Comprobación automática

Gitbox vigila tus proyectos y muestra su estado:

- **Synced** (verde) — actualizado con el remoto
- **Behind** (magenta) — el remoto tiene commits nuevos que puedes traer
- **Local changes** (naranja) — tienes trabajo sin commitear
- **Ahead** (azul) — tienes commits que no has pusheado
- **Not local** (gris) — el repo todavía no se ha clonado
- **Local branch** (verde) — en una feature branch sin upstream tracking (normal)
- **No upstream** (gris) — la rama predeterminada no tiene upstream tracking (requiere atención)

Cuando un repo está checked out en una rama no predeterminada, aparece una pequeña insignia de rama junto al nombre del repo (por ejemplo, `feature-xyz`). Los repos en la rama predeterminada no muestran insignia. El estado detached HEAD muestra una insignia roja `detached`.

#### Pull All

Haz clic en el botón **Pull All** (icono de flecha hacia abajo) en la barra superior para actualizar todo con un clic. Clona repos ausentes y hace pull de repos que están safely behind (omite cualquier cosa con cambios locales).

#### Fetch All

Haz clic en el botón **Fetch All** (icono ↻) para consultar todos los remotos por commits nuevos sin hacer pull. Esto actualiza los indicadores de estado para que veas qué cambió antes de decidir si quieres hacer pull.

#### Fetch periódico

En Settings puedes activar fetch automático cada 5, 15 o 30 minutos. Gitbox comprueba todos los remotos y re-verifica salud de credenciales en segundo plano.

#### Ver detalles

Haz clic en un repo que muestre cambios locales, conflictos u otros problemas. Aparece un panel expansible que muestra:

- La rama actual y cuántos commits estás ahead o behind
- Una lista de cada archivo modificado con iconos que muestran qué ocurrió (added, deleted, renamed, modified)
- Cualquier archivo untracked

Esta vista de detalle **se actualiza automáticamente** cuando Gitbox detecta cambios nuevos: no necesitas cerrarla y volver a abrirla.

### Adoptar repos huérfanos

El modal de orphans lista clones bajo tu carpeta padre que todavía no están en `gitbox.json`, agrupados por cómo Gitbox puede gestionarlos:

- **Ready to adopt** — Gitbox emparejó el clon con una cuenta usando su URL remota, el `credential.<url>.username` del repo o la carpeta donde vive. Marca la casilla y haz clic en **Adopt** para registrarlo (y opcionalmente reubicarlo a la ruta canónica).
- **Unknown account** — ninguna cuenta configurada coincide con el host remoto. Añade una cuenta primero y vuelve a abrir el modal.
- **Unknown account, `ambiguous: a | b`** — dos o más cuentas en el mismo host empatan en cada señal de identidad que Gitbox mira. La casilla está deshabilitada para que no se muevan archivos. Para desambiguar: mueve el clon bajo el subtree de source correcto, edita `gitbox.json` para reflejar la cuenta deseada, o establece `credential.<url>.username` en el clon; luego vuelve a abrir el modal.
- **Local only** — sin remoto `origin`, no adoptable.

### Crear repositorios

Haz clic en **Create repo** en una tarjeta de cuenta para crear un repositorio nuevo directamente en el proveedor sin salir de Gitbox.

El modal pide:

- **Owner** — un dropdown que lista tu usuario personal y cualquier organización a la que pertenezcas. La API del proveedor determina qué organizaciones están disponibles.
- **Name** — el nombre del repositorio. Los caracteres inválidos se eliminan automáticamente (solo se permite `a-z`, `A-Z`, `0-9`, `.`, `_`, `-`). Los espacios se convierten en guiones mientras escribes.
- **Description** — un resumen opcional de una línea.
- **Private** — marcado por defecto. Desmarca para crear un repo público.
- **Clone after creating** — marcado por defecto. Cuando está activo, Gitbox añade el repo a tu config y lo clona inmediatamente.

El texto del botón cambia según la casilla de clone: **Create & Clone** o **Create**.

La creación de repos está soportada en todos los proveedores (GitHub, GitLab, Gitea, Forgejo y Bitbucket) y funciona con todos los tipos de credencial. Se usa para creación el mismo token API que para discovery.

### Editar una cuenta

Haz clic en el nombre de la cuenta en cualquier tarjeta para abrir la pantalla de edición. Puedes cambiar:

- **Account key** — si lo renombras, Gitbox se encarga de todo: renombra la carpeta en disco, actualiza tus claves SSH y config, migra tokens guardados y corrige todas las referencias internas.
- **Provider** — por si elegiste el equivocado originalmente.
- **All other fields** — URL, username, name, email, default branch.

### Gestionar credenciales

Haz clic en la insignia de credencial de una tarjeta para abrir la pantalla de gestión de credenciales. Para detalles de cada tipo y permisos necesarios, consulta [credentials.md](credentials.md).

#### Cambiar tipo de credencial

Usa el dropdown para cambiar entre GCM, Token y SSH. Haz clic en **Setup** para aplicar el cambio. gitbox elimina la credencial antigua y sus artefactos, configura la nueva y reconfigura automáticamente todos los clones existentes.

#### Eliminar una credencial

Cuando ves el tipo de credencial actual, haz clic en el botón rojo **Delete** para eliminar todos los datos de autenticación guardados. Esto es útil cuando necesitas empezar limpio: por ejemplo, si un token caducó o quieres comenzar de nuevo.

Después de eliminar, la tarjeta se vuelve roja y la insignia muestra "config". Haz clic para configurar una credencial nueva.

## Paso 5: mirrors (opcional)

Los mirrors mantienen copias de backup de repos en otro proveedor: por ejemplo, push desde un Forgejo de homelab a GitHub. Los repos se mirrorizan server-side mediante APIs de proveedor, no se clonan localmente.

### Pestañas Accounts y Mirrors

La pantalla principal usa dos pestañas sobre la sección de tarjetas:

- **Accounts** (por defecto) — muestra tarjetas de cuenta y debajo la lista de repos. Aquí gestionas cuentas, descubres proyectos y creas repos.
- **Mirrors** — muestra tarjetas de grupos mirror y debajo la lista de detalle de mirrors. Cada grupo mirror aparece como una tarjeta con un sync ring que muestra la proporción active/total.

Cambia de pestaña haciendo clic en los botones de pestaña. El **summary footer** de abajo siempre muestra conteos de repos y mirrors independientemente de la pestaña activa.

### Tarjetas de mirror

Cada tarjeta de grupo mirror muestra:

- **Status dot** — verde si todos los repos están activos, rojo si hay errores, ámbar en caso contrario
- Etiqueta **MIRROR** y par de cuentas (por ejemplo, `forgejo ↔ github`)
- **Sync ring** — proporción de mirrors activos frente al total del grupo
- Botón **Check status** — verifica estado de sync comparando commits HEAD en ambos lados
- Una tarjeta **+** siempre visible en la pestaña Mirrors para crear un grupo mirror nuevo

### Anillo de salud de mirror

Cuando hay mirrors configurados, aparece un segundo **health ring** en la barra superior junto al sync ring de repos. Muestra `active/total` mirrors y se vuelve rojo si algún mirror tiene errores.

### Acciones de mirror

La pestaña Mirrors ofrece dos botones de sección:

- **Discover** — escanea todos los pares de cuentas para detectar relaciones de mirror existentes. Durante el escaneo, una barra de progreso muestra avance por cuenta (indeterminado durante el listado de repos, determinado durante el análisis). Cuando aparecen resultados, los repos ya presentes en tu config se marcan como **"configured"** y aparecen atenuados. Cada resultado no configurado tiene un botón individual **+ Add** para añadirlo a tu config uno a uno, o puedes usar **Apply to config** para añadir todos a la vez.
- **Check all** — comprueba el estado de sync de cada grupo mirror.

### Lista de detalle de mirror

Bajo las tarjetas de mirror, cada grupo se expande en una lista de detalle con repos mirrorizados individuales:

- Etiqueta de dirección (por ejemplo, `origin → backup (mirror)`)
- Estado de sync (Synced OK, Backup is behind origin, etc.)
- Icono de aviso si el repo de backup es público
- Botón **Setup** para repos pendientes que todavía no se han configurado mediante API
- Botón **+ Repo** para añadir repos nuevos al grupo

## Paso 6: workspaces (opcional)

La pestaña **Workspaces**, junto a Accounts y Mirrors, agrupa N clones en un único artefacto (archivo `.code-workspace` o YAML de tmuxinator) que los abre juntos. Crea uno desde el botón `+ New workspace` de la pestaña o marcando clones en la pestaña Accounts y usando `Create workspace from selected`. Consulta la [guía CLI](cli-guide.md#paso-8-workspaces-dinámicos-opcional) para el modelo backend: la GUI usa el mismo formato de config.

### Auto-discovery al arrancar

Cuando dejo a mano un archivo `*.code-workspace` bajo la carpeta gestionada por gitbox, o traigo uno desde otra máquina, la GUI lo recoge en el siguiente arranque y lo adopta en `gitbox.json` con `discovered: true`. Lo mismo ocurre con archivos `~/.tmuxinator/*.yml`. La pestaña Workspaces muestra la nueva entrada sin que yo haga nada más.

La pestaña también tiene un botón **Discover** que vuelve a ejecutar el escaneo bajo demanda. El escaneo resuelve cada ruta de carpeta parseada de vuelta a un clon conocido usando una coincidencia deepest-prefix. Los workspaces con al menos un miembro ambiguo (una ruta que empata entre dos clones) se marcan por separado y nunca se auto-adoptan: abre la pestaña y elige a mano el candidato correcto.

### Tmuxinator en Windows

Los usuarios de Windows con WSL instalado tienen el mismo soporte de tmuxinator que macOS / Linux: gitbox escribe el YAML en el lado WSL `~/.tmuxinator/<key>.yml` (mediante su ruta UNC `\\wsl.localhost\…`) y `Open` lanza la terminal configurada ejecutando `wsl.exe -- tmuxinator start <key>`. Sin WSL, los workspaces tmuxinator siguen sin estar soportados y muestran un error claro.

## Vistas del dashboard

### Vista completa

El dashboard completo muestra la barra superior con health rings, la barra de pestañas (Accounts/Mirrors), tarjetas, listas de detalle de repos o mirrors, y el summary footer. Los botones de acción en la barra superior incluyen Pull All, Fetch All, Delete mode y Compact view.

### Vista compacta

Haz clic en el botón **◧** en la barra superior para cambiar a modo compacto: una tira estrecha de estado (~220px de ancho) que muestra:

- **Global health ring** — porcentaje y conteo global de sync
- **Account pills** — una por cuenta con un mini ring y conteo de problemas. Haz clic para expandir y ver repos individuales debajo
- **Mirror pill** — cuando hay mirrors configurados, muestra el conteo active/total con un punto de color
- **Theme toggle** y un botón **Full view** abajo

Esto es útil cuando quieres tener gitbox visible como sidebar mientras trabajas en otras apps. Haz clic en **◧ Full view** para volver al dashboard completo.

## Ajustes y mantenimiento

### Panel de ajustes

Haz clic en el **icono de engranaje** para abrir el panel de ajustes:

- **Config** — muestra la ruta a tu archivo de config con un botón "Open in Editor"
- **Root folder** — dónde se guardan los proyectos, con un botón "Change"
- **Theme** — cambia entre System, Light y Dark
- **Fetch periódico** — intervalo de fetch automático (off, 5m, 15m, 30m)
- **Run at startup** — lanzar Gitbox automáticamente al iniciar sesión (dependiente de plataforma)
- **System check** — **Run** abre un informe de cada herramienta externa que usa gitbox (git, Git Credential Manager, ssh, tmux, …), dónde está instalada, su versión y, para cualquier cosa ausente que tu config necesite, un comando de instalación. Mismos datos que `gitbox doctor` en la CLI.
- **Versión** — versión actual de la app
- **Author** — autor del proyecto y enlace al repositorio de GitHub

Los flujos add-account y change-credential ejecutan la misma comprobación automáticamente: si eliges el tipo de credencial `gcm` en una máquina sin Git Credential Manager instalado, recibes un banner amarillo con el comando de instalación en lugar de un fallo críptico de autenticación más tarde.

### Acciones de clones

Cada fila de repo clonado tiene un **menú kebab (⋮)** en el lado derecho. El menú se divide en tres secciones para que los elementos que más usas no queden enterrados detrás de scroll:

1. **Siempre visible** — `🌐 Open in browser` y `📁 Open folder`.
2. **Defaults** — una entrada por categoría, usando la primera entrada de config como valor por defecto: `>_ Open in <terminals[0]>`, `✎ Open in <editors[0]>`, `🤖 Open in <ai_harnesses[0]>`. Una entrada se oculta cuando esa categoría tiene cero elementos configurados.
3. **Submenús** — `Terminals ▸`, `Editors ▸`, `AI Harnesses ▸`. Cada submenú aparece solo cuando la categoría tiene **dos o más** entradas: con una sola, el default ya la cubre. Haz clic en el submenú para expandirlo (no hover), haz clic en otro submenú para cambiar, haz clic fuera o elige un elemento para cerrarlo todo.

Bajo los submenús:

- **🧹 Sweep branches** — encuentra y elimina ramas locales obsoletas (gone, merged o squash-merged). Muestra un diálogo de confirmación con la lista de ramas antes de eliminar nada.

Para cambiar qué terminal/editor/harness aparece como default de nivel superior, reordena el array en `gitbox.json`: la primera entrada siempre es el default. No hace falta un flag separado.

La lista de terminales detectadas cubre Windows Terminal, PowerShell 7/5, Git Bash, WSL y Command Prompt en Windows; Terminal, iTerm y Warp en macOS; gnome-terminal, Konsole, Kitty, Alacritty, Xfce Terminal y Terminator en Linux. Los editores cubren VS Code, Cursor, Zed y cualquier otro detectable en `PATH`. Los AI harnesses (Claude Code, Codex, Gemini, Aider, Cursor Agent, OpenCode) se ejecutan dentro de la primera terminal configurada: consulta [AI harness actions](#acciones-de-ai-harness) más abajo.

### Acciones de cuenta

Cada grupo de source en la lista de repos tiene un **menú kebab (⋮)** en el lado derecho de su cabecera (el título de cuenta sobre la lista de clones). El kebab de cuenta usa la **misma estructura e iconos** que el kebab de fila de repo: defaults de nivel superior, submenús por categoría, mismas reglas de ocultación, pero aplicado a la carpeta padre de la cuenta (`<global.folder>/<account-key>`) en lugar de a un clon concreto:

- **🌐 Open in browser** — abre la página de perfil/org del proveedor para la cuenta (por ejemplo, `https://github.com/<username>`, la página de grupo de GitLab, la página de usuario de Gitea/Forgejo).
- **📁 Open folder** — abre la carpeta padre de la cuenta en el gestor de archivos del SO. La carpeta es la raíz natural de workspace para greps multi-repo, ediciones multi-repo o loops de shell. Si la carpeta todavía no existe (nada clonado bajo esa cuenta), la acción falla silenciosamente: clona al menos un repo primero.
- **>\_ Open in \<terminal\>**, **✎ Open in \<editor\>**, **🤖 Open in \<AI harness\>** — las mismas entradas default-first que el kebab de repo, más submenús de categoría cuando tienes varias opciones configuradas. Sweep branches no aparece aquí: solo tiene sentido en un clon concreto.

En vista compacta, al pasar por encima de una account pill aparecen los mismos accesos de folder / editor / terminal / AI harness como iconos pequeños a la derecha, igual que en filas compactas de repo.

Los editores se auto-detectan al arrancar escaneando PATH. Gitbox escribe los editores detectados en `global.editors` en tu archivo de config con sus rutas completas. Puedes reordenar entradas o añadir editores custom editando la config: el menú siempre refleja el orden de la config.

Las terminales siguen el mismo patrón: se detectan al arrancar por plataforma y se escriben en `global.terminals` con sus comandos y plantillas de argumentos. Cada entrada tiene `name`, `command` (ruta absoluta o launcher en PATH) y `args`. Usa el token literal `{path}` dentro de `args` para marcar dónde se inyecta la ruta del repo; si falta el token, la ruta se añade como argumento final. Edita o reordena libremente: el orden del menú coincide con el orden de la config.

En Windows, las entradas shell desnudas (`cmd.exe`, `powershell.exe`, `pwsh.exe`, `wsl.exe`) tienen `args` vacío: el launcher las envuelve en `cmd.exe /C start "" /D <path>`, lo que da a cada terminal una consola nueva y fija el directorio inicial.

#### Abrir un perfil concreto de Windows Terminal

Cuando Windows Terminal está instalado, gitbox auto-descubre tus perfiles WT y reescribe `global.terminals` para reflejar el menú WT: una entrada por perfil visible, en el mismo orden que `profiles.list`, cada una lanzando `wt.exe --profile "<name>" -d "{path}"`. La shell se abre con el perfil exacto que ajustaste en WT (colores, fuente, directorio inicial, oh-my-posh, distro WSL concreta). Los lanzamientos de binario desnudo (`pwsh.exe`, `powershell.exe`, `wsl.exe`, `cmd.exe`, `git-bash.exe`) siempre caen al perfil _default_ de WT y pierden ese ajuste, así que se eliminan de `global.terminals` cuando WT discovery tiene éxito.

Discovery se ejecuta al arrancar y en cada sync de config. Renombrar, añadir, ocultar o deshabilitar un perfil en WT se recoge en el siguiente launch: las entradas obsoletas se podan para que el menú nunca derive de WT. Un perfil se excluye cuando su flag `hidden` es `true` o cuando su `source` aparece en el `disabledProfileSources` de nivel superior de WT (por ejemplo, perfiles dinámicos de Visual Studio deshabilitados en bloque).

Las customizaciones existentes se conservan cuando el `name` de la entrada coincide con un perfil visible actual: si antes añadiste `--maximized` u otro flag a una entrada de perfil, gitbox conserva tu `command` y `args` intactos y solo restaura entradas que eliminó. Las entradas cuyo nombre no coincide con ningún perfil visible se eliminan, por diseño: así se limpian automáticamente las entradas legacy `Windows Terminal` / `PowerShell 7` / `WSL` / `Command Prompt` en el primer sync tras actualizar.

Ubicaciones comprobadas, en orden: `%LOCALAPPDATA%\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json` (Store), `…\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\…` (Preview), `%LOCALAPPDATA%\Microsoft\Windows Terminal\settings.json` (sin empaquetar). Si ninguna parsea, porque falta el archivo, JSON mal formado o no hay `profiles.list`, gitbox vuelve a las entradas de binario desnudo para que algo siempre funcione.

Si quieres sobrescribir una entrada auto-descubierta (renombrarla, apuntarla a otro perfil, añadir `--maximized`), edita `gitbox.json` directamente. Formato:

```json
{
  "name": "WSL — Ubuntu",
  "command": "C:\\Users\\<you>\\AppData\\Local\\Microsoft\\WindowsApps\\wt.exe",
  "args": ["--profile", "Ubuntu 24.04.1 LTS", "-d", "{path}"]
}
```

Notas:

- `--profile "<name>"` toma el display name del perfil _literalmente_, incluidos sufijos de versión como `Ubuntu 24.04.1 LTS` o la ortografía exacta `PowerShell 7` configurada en los ajustes de WT.
- `-d "{path}"` establece el directorio inicial.
- Enrutar mediante `wt.exe --profile` también evita la peculiaridad de fuga de env de Git Bash descrita abajo: WT inicia la shell desde su propio contexto de perfil guardado, así que las variables de entorno con forma MSYS heredadas por la GUI no llegan a la shell.

#### Lanzar gitbox desde Git Bash (nota de desarrollo)

Si lanzas `GitboxApp.exe` desde una shell Git Bash / MSYS2, las variables de entorno Windows heredadas por la GUI llegan en forma posix (por ejemplo, `LOCALAPPDATA=/c/Users/you/AppData/Local`). Esos valores se propagan a terminales abiertas desde gitbox mediante la ruta default `cmd.exe /C start …`, y herramientas que las leen como rutas Windows — `oh-my-posh`, algunos helpers de `$PROFILE` — pueden atragantarse (`& '/c/Users/...' — not recognized as a cmdlet`). Gitbox sanea el bloque env que entrega a la terminal lanzada, pero cuando Windows Terminal es el host de consola predeterminado, la ruta de delegación de WT puede saltarse ese bloque.

Dos arreglos igual de limpios:

- **Lanza `GitboxApp.exe` desde Explorer, el menú Start o un acceso anclado**: cualquier lugar donde Windows origine un env limpio. Los usuarios finales nunca ven esto, así que el comportamiento de producción no se afecta.
- **Cambia la entrada de terminal afectada a la forma `wt.exe --profile "<name>" -d "{path}"`** mostrada arriba. WT inicia la shell desde su propio contexto de perfil, que tiene env Windows limpio independientemente de cómo se haya lanzado la GUI.

En **modo compacto**, las acciones de clone aparecen como botones de icono pequeños (navegador, carpeta, editor, terminal y AI harness) que se muestran al hacer hover sobre cada fila de repo. Solo se muestran el primer editor, terminal y AI harness configurados: cambia a vista completa para ver la lista completa.

### Acciones de AI harness

Los AI CLI harnesses (Claude Code, Codex, Gemini, Aider, Cursor Agent, OpenCode, …) son procesos shell interactivos: necesitan una terminal para ejecutarse. Gitbox añade una entrada **Open in \<harness\>** por harness configurado tanto al kebab de repo como al kebab de cabecera de source (cuenta). Hacer clic lanza la primera terminal configurada en la carpeta objetivo y ejecuta el harness dentro.

Le digo a gitbox qué terminal usar ordenando `global.terminals`: la primera entrada es la terminal host. Para cambiar de host, mueve otra entrada al principio del array.

La terminal host debe soportar lanzar un comando. En la plantilla `args`, esto se marca con el token literal `"{command}"`: en launch time, gitbox inserta el argv del harness en lugar de ese token. Las plantillas auto-detectadas (perfiles Windows Terminal, gnome-terminal, konsole, alacritty, kitty y similares) incluyen `{command}` por defecto, así los lanzamientos de harness funcionan sin editar config. Para lanzamientos solo de terminal, el token se expande a cero elementos, así la misma entrada sirve para ambas rutas.

Si la primera terminal no puede lanzar un comando (falta `{command}` en args), hacer clic en una entrada AI harness muestra un error accionable: "_\<name\>_ in global.terminals[0] doesn't support launching a command. Add `{command}` to its args, or reorder global.terminals so a compatible entry is first." Los launchers solo-shell como `pwsh.exe`, `cmd.exe`, `wsl.exe`, `git-bash.exe` y `open -a Terminal.app` caen en esta categoría: enruta harnesses mediante un perfil WT (Windows), una terminal GUI con flag de comando (Linux) o el seguimiento próximo de macOS.

Los harnesses se auto-detectan en PATH al arrancar. Gitbox escribe las entradas detectadas en `global.ai_harnesses` con la ruta resuelta del binario. Cada entrada tiene `name` (display en el menú), `command` (ruta de binario o nombre en PATH) y un array opcional `args` para flags específicos del harness (por ejemplo `["--model", "sonnet-4.6"]`). La mayoría no necesita flags: `args` suele estar vacío.

El conjunto de harnesses que gitbox intenta auto-detectar se mantiene como una tabla markdown embebida en el binario. La lista autoritativa vive en [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md): para añadir o quitar un harness detectado, edita ese archivo. Una fila se auto-detecta cuando su `Category` es `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE` o `Agentic IDE / CLI`, y su celda `Executable / CLI Command` contiene un único identificador entre backticks (por ejemplo `` `claude` ``, `` `aider` ``, `` `cursor` ``, `` `cursor-agent` ``). Las filas de framework, orchestrator y cloud-platform se documentan como referencia pero el detector las omite: no se lanzan desde una terminal en una carpeta. Los Agentic IDEs (Cursor, Windsurf) se tratan como herramientas AI, no como editores: por tanto, la entrada "Open in Cursor" aparecerá en la sección AI harness del menú, no en la sección editor.

En el kebab de cuenta, las mismas entradas aparecen con orden idéntico: la única diferencia en runtime es que el directorio de trabajo es `<global.folder>/<account-key>` (la carpeta padre de la cuenta) en lugar de un clon concreto. Si la carpeta padre no existe todavía (nada clonado bajo esa cuenta), la acción falla con "account folder does not exist": clona al menos un repo primero. La vista compacta expone el harness como icono 🤖 tanto en la fila de repo como en la account pill, llamando a `global.ai_harnesses[0]`.

### Notificación de actualización

Gitbox comprueba actualizaciones una vez al día en segundo plano. Cuando hay una versión más nueva, aparece una píldora ámbar en el lado derecho de la barra de estado del footer mostrando la nueva versión. Haz clic para descargar y aplicar la actualización in-place. Cuando termina, haz clic en **Quit** y reinicia la app para usar la nueva versión.

### Eliminar repos y cuentas

Haz clic en el **icono de papelera** de la barra superior para entrar en modo delete. Aparecen botones X rojos en tarjetas de cuenta, tarjetas de grupo mirror y filas de repo. Haz clic en uno para eliminarlo. El borrado de cuenta también elimina su source y carpetas de clones locales.

Sal del modo delete haciendo clic de nuevo en el icono de papelera.

### Mover un repositorio entre cuentas / proveedores

Abre el kebab (⋮) en cualquier fila de repo y elige **Move repository…**. La entrada está deshabilitada cuando el clon no está limpio y sincronizado; el tooltip explica por qué. El modal:

1. **Form** — elige la cuenta + owner de destino (personal u org, cargado asincrónicamente), confirma el nuevo nombre de repo, define visibilidad y opcionalmente acepta eliminar el repo origen y/o el clon local después de un movimiento exitoso. Ambos toggles de delete están desmarcados por defecto.
2. **Confirm** — un resumen con borde rojo que lista cada efecto destructivo. Escribe la clave del repo origen (por ejemplo, `acme/widget`) para desbloquear el botón **Move**.
3. **Progress** — cada fase (preflight → fetch → create destination → push mirror → rewire origin → optional deletes → update config) aparece como una línea propia con estado en vivo.

El move preserva cada ref y tag mediante `git push --mirror`, reconfigura `origin` en el clon local hacia la URL nueva y actualiza la config de gitbox para que el repo viva ahora bajo la source de la cuenta destino. Un fallo de source-delete o local-clone-delete (fases 6–7) se captura como warning: el move ya está completo en ese punto.

Los scopes de token requeridos en ambos lados están listados en [Token scopes for destructive actions](credentials.md#scopes-de-token-por-capacidad).

### Aviso de identidad global

Si tu `~/.gitconfig` tiene un `user.name` o `user.email` global, Gitbox muestra un **banner naranja de aviso** arriba del dashboard. Una identidad global puede sobrescribir las identidades por repo que gitbox configura para cada cuenta.

Haz clic en **Remove** para limpiar las entradas de identidad global, o descarta el banner con el botón **✕**.

### Aviso de credential helper global

Cuando al menos una cuenta usa **GCM** (Git Credential Manager), Gitbox verifica que tu `~/.gitconfig` global tiene `credential.helper = manager` y `credential.credentialStore` fijado al valor apropiado del SO (`keychain` en macOS, `wincredman` en Windows, `secretservice` en Linux). Si falta alguno o está mal, aparece un segundo banner naranja.

Sin esos globales, GCM cae a un prompt TTY durante la autenticación y falla con `fatal: could not read Password ... Device not configured` en la GUI: mira el texto del banner para el desajuste concreto (ausente o valor inesperado).

Haz clic en **Configure** para corregir ambas entradas en un paso. Gitbox también rellena los mismos defaults en tu `gitbox.json` para que la comprobación pase permanentemente, incluso si `~/.gitconfig` se edita después. Descarta el banner con el botón **✕** si prefieres gestionarlo manualmente.

### Aviso de gitignore global

Gitbox detecta cuando falta `~/.gitignore_global`, cuando tiene un bloque recomendado desactualizado, cuando hay patrones gestionados duplicados fuera de los marcadores sentinel o cuando `core.excludesfile` no está configurado. En cualquiera de esos estados aparece un banner con un botón **Install** que hace todo esto: escribe un bloque curado de patrones de basura del SO (`.DS_Store`, `Thumbs.db`, `*~`, …), apunta `core.excludesfile` a él y guarda un backup con timestamp `.bak-YYYYMMDD-HHMMSS` de cualquier archivo existente. Solo se conservan los últimos 3 backups.

La comprobación automática al arrancar se puede cambiar mediante **Settings → Global gitignore → On/Off**. Las acciones explícitas siempre se ejecutan: el toggle del engranaje, el botón Install y la CLI `gitbox gitignore check|install` nunca se silencian por la preferencia. Consulta [Gitignore global en la referencia](reference.md#gitignore-global) para el formato del bloque gestionado y el atajo `G` de la TUI.

## Consejos

- **Window position** — Gitbox recuerda tamaño y posición de ventana. Si desconectas un monitor secundario y la ventana se abriría fuera de pantalla, se centra automáticamente en tu pantalla principal.
- **External edits** — si editas `gitbox.json` a mano (o mediante la CLI), la GUI recoge los cambios automáticamente cuando la ventana recupera foco.
- **Same config** — la app de escritorio y la CLI (`gitbox`) comparten el mismo archivo de config. Los cambios en una son visibles en la otra.
- **Automatic backups** — cada vez que se guarda un cambio significativo, Gitbox crea un backup fechado (por ejemplo, `gitbox-20260401-143025.json`) en el mismo directorio. Los 10 backups más recientes se conservan automáticamente; los más antiguos se podan. La pantalla de recuperación de corrupción de la GUI puede restaurar cualquiera de ellos con un clic. Los guardados solo de posición de ventana (mover o redimensionar la app) no crean backup: son churn cosmético y rotarían copias reales pre-corrupción fuera del anillo.

## Ver también

- [Inicio rápido de CLI](cli-guide.md) — para usuarios de terminal
- [Referencia de configuración](reference.md) — formato detallado de config y comandos
- [Arquitectura](architecture.md) — diseño técnico
