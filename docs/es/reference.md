# Guía de referencia

Referencia completa de todos los comandos de gitbox, formato de configuración, estructura de carpetas y troubleshooting.

Para empezar, consulta la [guía CLI](cli-guide.md) o la [guía GUI](gui-guide.md). Para instalación, consulta el [README](../../README.md).

---

## Gestión de cuentas

Una cuenta representa QUIÉN eres en un servidor git: un par único `(hostname, username)`.

### Añadir cuentas

```bash
# GitHub personal
gitbox account add github-personal \
  --provider github \
  --url https://github.com \
  --username MyGitHubUser \
  --name "My Name" \
  --email "myuser@example.com" \
  --default-credential-type gcm

# Forgejo homelab (SSH — host y key-type son obligatorios)
gitbox account add forgejo-homelab \
  --provider forgejo \
  --url https://forge.mylab.lan \
  --username myuser \
  --name "My Name" \
  --email "myuser@mylab.lan" \
  --default-credential-type ssh \
  --ssh-host gt-myuser \
  --ssh-key-type ed25519

# GitLab
gitbox account add gitlab-work \
  --provider gitlab \
  --url https://gitlab.com \
  --username youruser \
  --name "Your Name" \
  --email "you@company.com"
```

### Listar e inspeccionar cuentas

```bash
gitbox account list
gitbox account show github-personal
gitbox account show github-personal --json
```

### Actualizar una cuenta

Solo cambian los flags que especifiques:

```bash
gitbox account update github-personal --name "New Name" --email "new@email.com"
```

### Eliminar una cuenta

Una cuenta solo puede eliminarse si ninguna source la referencia:

```bash
gitbox account delete github-personal
# Error: cannot delete — referenced by source "github-personal"

gitbox source delete github-personal
gitbox account delete github-personal  # ahora tiene éxito
```

---

## Gestión de sources

Una source representa QUÉ clonas desde una cuenta. Cada source referencia una cuenta y contiene una lista de repos.

### Añadir una source

```bash
gitbox source add github-personal --account github-personal
gitbox source add forgejo-homelab --account forgejo-homelab
```

Por defecto, la clave de source se usa como carpeta de clone de primer nivel. Para sobrescribirla:

```bash
gitbox source add my-server --account forgejo-homelab --folder "server-repos"
```

### Listar sources

```bash
gitbox source list
gitbox source list --account github-personal
```

---

## Gestión de repos

Los repos usan formato `org/repo`. La parte org se convierte en la carpeta de segundo nivel, y la parte repo se convierte en la carpeta de tercer nivel (clone).

### Añadir repos

```bash
# Simple — hereda el tipo de credencial del default de la cuenta
gitbox repo add github-personal "MyGitHubUser/gitbox"
gitbox repo add github-personal "MyGitHubUser/dotfiles"

# Cross-org — acceder al repo de otra org con tus credenciales
gitbox repo add github-personal "other-org/their-repo"

# Varias orgs en el mismo servidor (Forgejo/Gitea)
gitbox repo add forgejo-homelab "infra/homelab"
gitbox repo add forgejo-homelab "infra/migration"
gitbox repo add forgejo-homelab "personal/my-project"

# Sobrescribir el tipo de credencial para un repo concreto
gitbox repo add github-personal "MyGitHubUser/private-repo" --credential-type ssh
```

### Overrides de carpeta

```bash
# Sobrescribir la carpeta de 2º nivel (org → nombre custom)
gitbox repo add github-work "MyOrg/myorg.github.io" --id-folder "myorg-rest"
# Clona en: ~/git/github-work/myorg-rest/myorg.github.io/

# Sobrescribir la carpeta de 3º nivel (nombre del clone)
gitbox repo add github-work "MyOrg/myorg.web" --clone-folder "website"
# Clona en: ~/git/github-work/MyOrg/website/

# Ruta absoluta — reemplaza todo
gitbox repo add forgejo-homelab "myuser/my-config" --clone-folder "~/.config/my-config"
# Clona en: ~/.config/my-config/
```

### Listar e inspeccionar repos

```bash
gitbox repo list
gitbox repo list --source github-personal
gitbox repo show github-personal "MyGitHubUser/gitbox"
```

### Actualizar un repo

```bash
gitbox repo update github-personal "MyGitHubUser/private-repo" --credential-type gcm
gitbox repo update forgejo-homelab "infra/homelab" --id-folder "infra-prod"
```

### Eliminar un repo

```bash
gitbox repo delete github-personal "MyGitHubUser/old-project"
```

---

## Estructura de carpetas

Los repos se clonan en una estructura de tres niveles:

```text
~/00.git/                           ← global.folder
  <source-key>/                     ← 1er nivel (o source.folder si está configurado)
    <org>/                          ← 2º nivel (de repo key, o override id_folder)
      <repo>/                      ← 3er nivel (de repo key, o override clone_folder)
```

Ejemplo con datos reales:

```text
~/00.git/
  git-example/                      ← source key
    personal/my-project/            ← org/repo
    infra/homelab/
    infra/migration/
  github-MyGitHubUser/             ← source key
    MyGitHubUser/gitbox/       ← org/repo
    external-org/ext-project/       ← cross-org
  github-myorg/
    MyOrg/myorg.browser/
    myorg-rest/myorg.github.io/     ← override id_folder
```

---

## Autenticación

Consulta [credentials.md](credentials.md) para tipos de credencial, permisos requeridos y almacenamiento.

```bash
# Configurar credenciales (idempotente — detecta tipo y guía el proceso)
gitbox account credential setup <account-key>

# Verificar que las credenciales funcionan
gitbox account credential verify <account-key>

# Eliminar credenciales
gitbox account credential del <account-key>
```

**Override de token CI/CD (variables de entorno):**

```bash
# Convención: GITBOX_TOKEN_<ACCOUNT_KEY> (mayúsculas, guiones → guiones bajos)
export GITBOX_TOKEN_MY_GITEA="ghp_your_token_here"
gitbox clone
```

Resolución de token: env var `GITBOX_TOKEN_<KEY>` → `GIT_TOKEN` → keyring del SO.

---

## Monitorización de estado

```bash
# Todos los repos
gitbox status

# Filtrar por source
gitbox status --source github-personal

# Salida JSON (para scripting)
gitbox status --json
```

Indicadores de estado:

| Símbolo | Color   | Estado      | Significado             |
| ------- | ------- | ----------- | ----------------------- |
| `+`     | Verde   | clean       | Actualizado             |
| `!`     | Naranja | dirty       | Cambios sin commitear   |
| `<`     | Morado  | behind      | Necesita pull           |
| `>`     | Azul    | ahead       | Necesita push           |
| `!`     | Rojo    | diverged    | Ahead y behind a la vez |
| `!`     | Rojo    | conflict    | Conflictos de merge     |
| `o`     | Morado  | not cloned  | El directorio no existe |
| `~`     | Naranja | no upstream | Sin tracking branch     |
| `x`     | Rojo    | error       | Error de Git            |

Cuando un repo está checked out en una rama no predeterminada, aparece una insignia `[branch-name]` después del nombre del repo. Los repos en la rama predeterminada no muestran insignia. Si un repo está en una rama local sin upstream tracking, el detalle muestra "local branch" en lugar de "no upstream": esto es normal para feature branches y no cuenta como problema en los resúmenes de cuenta.

La salida `--json` incluye los campos `branch` (nombre de la rama actual) e `is_default` (boolean) para cada repo.

---

## Clonado

```bash
# Clonar todos los repos configurados (default — no hacen falta flags)
gitbox clone

# Clonar solo repos de una source concreta
gitbox clone --source github-personal

# Clonar solo un repo concreto
gitbox clone --source github-personal --repo "MyGitHubUser/gitbox"
```

---

## Pull

```bash
# Pull de todos los repos behind (solo fast-forward)
gitbox pull

# Pull desde una source concreta
gitbox pull --source github-personal
```

Los repos dirty o con conflictos se omiten con un aviso.

---

## Navegación

Abre la página web remota de un repositorio en el navegador predeterminado:

```bash
# Abrir un repo concreto
gitbox browse --repo alice/hello-world

# Acotar a una source concreta
gitbox browse --source github-personal --repo alice/hello-world

# Sacar URL como JSON (sin abrir navegador)
gitbox browse --repo alice/hello-world --json
```

| Flag       | Descripción                               |
| ---------- | ----------------------------------------- |
| `--repo`   | Repositorio que abrir (requerido)         |
| `--source` | Restringir búsqueda a una source concreta |

---

## Sweep

Elimina ramas locales obsoletas en todos los repos configurados:

```bash
# Vista previa de lo que se eliminaría (sin cambios)
gitbox sweep --dry-run

# Limpiar todos los repos
gitbox sweep

# Limpiar una source o repo concreto
gitbox sweep --source github-personal
gitbox sweep --repo alice/hello-world
```

Se detectan tres tipos de ramas obsoletas:

| Tipo     | Significado                                       | Modo de borrado           |
| -------- | ------------------------------------------------- | ------------------------- |
| Gone     | La rama remota de tracking se eliminó             | `git branch -D` (forzado) |
| Merged   | Completamente fusionada en la rama predeterminada | `git branch -d` (seguro)  |
| Squashed | Squash-merged o rebase-merged en el servidor      | `git branch -D` (forzado) |

| Flag        | Descripción                       |
| ----------- | --------------------------------- |
| `--dry-run` | Listar ramas obsoletas sin borrar |
| `--source`  | Restringir a una source concreta  |
| `--repo`    | Restringir a un repo concreto     |

---

## Scan

Scan recorre el sistema de archivos (sin config requerida) y reporta el estado de sync de cada repo git que encuentra:

```bash
# Escanear desde el directorio actual
gitbox scan

# Escanear un directorio concreto
gitbox scan --dir ~/projects

# Escanear y hacer pull de repos behind (solo fast-forward)
gitbox scan --pull
```

La salida usa líneas coloreadas con símbolos: `+` ok, `!` dirty, `<` behind, `>` ahead, `x` error.

A diferencia de `status`, `scan` no requiere una configuración de gitbox: funciona en cualquier árbol de directorios.

Cuando existe una configuración de gitbox y se escanea dentro de la carpeta padre, los repos se anotan como `[tracked]` u `[ORPHAN]` con pistas de coincidencia de cuenta y un conteo resumen.

Etiquetas de huérfano:

- `[ORPHAN → <account>]` — scan emparejó el huérfano con una cuenta configurada.
- `[ORPHAN — local only]` — el repo no tiene remoto `origin`.
- `[ORPHAN — unknown account]` — no hay ninguna cuenta configurada que coincida con el host.
- `[ORPHAN — ambiguous: a | b]` — dos o más cuentas en el mismo host empatan en cada señal de identidad. Mueve la carpeta bajo el subtree de source correcto, edita `gitbox.json` o establece `credential.<url>.username` en el repo para desambiguar; luego vuelve a escanear.

Para elegir una cuenta para un huérfano, gitbox puntúa cada cuenta con host coincidente usando las señales de abajo (todos los valores son aditivos, gana el mayor):

| Señal                                                                      | Puntuación | Fuente                                                                   |
| -------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------ |
| Coincidencia de host (baseline requerido)                                  | 1          | hostname de URL de cuenta o alias SSH frente al host remoto parseado     |
| Owner igual a `account.username`                                           | +3         | segmento owner de la ruta de URL remota                                  |
| Repo vive bajo la carpeta source de la cuenta                              | +5         | primer componente de ruta del repo relativo a la carpeta padre de gitbox |
| URL HTTPS incrusta `user@` donde user iguala `account.username`            | +10        | `url.User.Username()` del remoto origin                                  |
| `.git/config` tiene `credential.<url>.username` igual a `account.username` | +10        | `git config --get-regexp '^credential\..*\.username$'` en el repo        |

Si la mejor puntuación está compartida por dos o más cuentas (incluido un empate solo por host), la coincidencia se marca ambigua: no se elige cuenta, no se mueven archivos, y el scan y la GUI muestran la lista de candidatos.

---

## Adopt

Adopt descubre repos huérfanos (no presentes en `gitbox.json`) bajo la carpeta padre y los trae al mundo gitbox:

```bash
# Adopción interactiva — pregunta por cada huérfano
gitbox adopt

# Vista previa de lo que ocurriría
gitbox adopt --dry-run

# Adoptar todos los huérfanos coincidentes sin preguntar (las reubicaciones requieren modo interactivo)
gitbox adopt --all
```

| Flag        | Descripción                                                                                          |
| ----------- | ---------------------------------------------------------------------------------------------------- |
| `--dry-run` | Mostrar el plan de adopción sin hacer cambios                                                        |
| `--all`     | Adoptar todos los huérfanos coincidentes sin preguntar (adopta in-place, no reubica automáticamente) |

Por cada repo adoptado, gitbox:

- Lo añade a `gitbox.json` bajo la source coincidente
- Configura aislamiento de credenciales por repo (GCM, SSH o token)
- Establece `user.name` y `user.email` desde la config de cuenta
- Reescribe la URL remota para ajustarla al tipo de credencial (SSH o HTTPS)
- Opcionalmente reubica el repo a la estructura de carpetas estándar

Los huérfanos sin cuenta coincidente se listan con su URL remota y una sugerencia para crear primero la cuenta.

---

## Discovery

Discover consulta la API de un proveedor para encontrar todos los repos visibles para una cuenta:

```bash
# Interactivo — muestra lista numerada, eliges cuáles añadir
gitbox account discover my-forgejo

# Añadir todos los repos sin preguntar
gitbox account discover my-forgejo --all

# Excluir forks y repos archivados
gitbox account discover my-forgejo --skip-forks --skip-archived

# Salida JSON (para scripting)
gitbox account discover my-forgejo --json
```

Discovery es **solo aditivo**: añade repos a tu config pero nunca los elimina. Si repos de tu config ya no se encuentran upstream, se marcan como stale con un aviso.

**Mirror discovery** escanea todos los pares de cuentas para detectar relaciones de mirror existentes:

```bash
# Escanear y mostrar resultados
gitbox mirror discover

# Escanear y aplicar resultados a config
gitbox mirror discover --apply
```

Métodos de detección (en confianza decreciente): push mirror API (confirmed), pull mirror flag (likely), coincidencia de nombre (possible). En la GUI, discovery muestra una barra de progreso por cuenta y marca repos que ya están en tu config.

---

## Gestión de credenciales

Configura, verifica y elimina credenciales de cuentas:

```bash
# Configurar credenciales (idempotente — seguro repetir)
gitbox account credential setup <account-key>

# Verificar que las credenciales funcionan
gitbox account credential verify <account-key>

# Eliminar credenciales
gitbox account credential del <account-key>
```

`credential setup` es el punto de entrada recomendado: detecta el tipo de credencial y te guía en el setup. Consulta [credentials.md](credentials.md) para detalles de cada tipo, permisos requeridos y almacenamiento.

---

## Comprobación del sistema (doctor)

Sondea el host para cada herramienta externa que gitbox puede llamar e informa qué está instalado, dónde y qué versión.

```bash
# Tabla legible por humanos
gitbox doctor

# JSON legible por máquinas (para scripts, bug reports)
gitbox doctor --json
```

Cada fila se marca `ok`, `missing` (requerida por tu config) u `optional` (no requerida por ninguna cuenta/workspace que tengas). Cuando falta una herramienta, doctor imprime un comando de instalación para el SO actual.

**Herramientas sondeadas:** `git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, `tmux`, `tmuxinator`, `wsl` (en Windows).

**Códigos de salida:** `0` cuando todas las herramientas requeridas están presentes, `1` cuando falta al menos una requerida.

La GUI expone el mismo informe mediante **Settings → System check → Run**. Tanto el flujo add-account de la GUI como el cambio de tipo de credencial de la TUI se niegan a iniciar un setup si falta una herramienta requerida: ves primero el comando de instalación, en lugar de llegar a un error críptico en runtime.

---

## Gitignore global

Instala un conjunto curado de patrones de basura del SO (`.DS_Store`, `Thumbs.db`, `*~`, …) en `~/.gitignore_global` y apunta `core.excludesfile` a él, para que los `.gitignore` por proyecto no tengan que repetirlos.

```bash
# Mostrar si el bloque recomendado está instalado y core.excludesfile configurado.
gitbox gitignore check

# Instalar o refrescar el bloque recomendado (idempotente; crea backup de cualquier
# archivo existente en ~/.gitignore_global.bak-YYYYMMDD-HHMMSS, ventana rotatoria de 3).
gitbox gitignore install

# Salida legible por máquinas para ambos subcomandos
gitbox gitignore check --json
gitbox gitignore install --json

# Comprobación verbose: listar cada patrón gestionado que también vive fuera de los
# marcadores sentinel (duplicados que `install` saneará).
gitbox gitignore check --verbose
```

El bloque instalado se envuelve con sentinels (`# >>> gitbox:global-gitignore >>>` / `# <<< gitbox:global-gitignore <<<`); los patrones y comentarios añadidos por el usuario fuera de los sentinels se preservan entre ejecuciones. Los patrones de negación (`!.DS_Store`) sobreviven al saneamiento.

**Opt-out:** la comprobación automática al arrancar está controlada por `global.check_global_gitignore` en `gitbox.json` (por defecto `true`). Cambia el toggle desde el panel de engranaje de la GUI o la pantalla de settings de la TUI. Los comandos explícitos — `gitbox gitignore check|install`, pulsar `G` en el dashboard TUI, hacer clic en **Install** en el banner GUI — siempre se ejecutan independientemente de la preferencia.

La GUI muestra un banner amarillo cuando falta el archivo, el bloque está stale, hay patrones duplicados fuera de los sentinels o `core.excludesfile` no está configurado. El footer del dashboard TUI recibe una pista roja en negrita `G gitignore!` en los mismos estados.

---

## Mirroring

Los mirrors mantienen copias de backup de repos en otro proveedor. Los repos se mirrorizan server-side mediante APIs de proveedor: NO se clonan localmente.

### Grupos mirror

Un grupo mirror empareja dos cuentas. Cada repo del grupo especifica dirección (push/pull) y origin (src/dst):

```bash
# Crear un grupo mirror
gitbox mirror add forgejo-github \
  --account-src my-forgejo \
  --account-dst github-personal

# Listar todos los grupos mirror
gitbox mirror list

# Mostrar detalles como JSON
gitbox mirror show forgejo-github

# Eliminar un grupo mirror
gitbox mirror delete forgejo-github
```

### Repos mirror

```bash
# Push desde source a destination (la cuenta source es el origin)
gitbox mirror add-repo forgejo-github infra/homelab \
  --origin src --direction push

# Pull desde destination hacia source (la cuenta destination es el origin)
gitbox mirror add-repo forgejo-github MyUser/dotfiles \
  --origin dst --direction pull

# Añadir y configurar inmediatamente mediante API
gitbox mirror add-repo forgejo-github infra/tools \
  --origin src --direction push --setup

# Quitar un repo de un grupo mirror
gitbox mirror delete-repo forgejo-github infra/homelab
```

### Setup de mirror

Ejecuta setup API para mirrors pendientes (crea repos destino, configura mirrors push/pull):

```bash
# Configurar todos los mirrors pendientes en todos los grupos
gitbox mirror setup

# Configurar todos los pendientes de un grupo
gitbox mirror setup forgejo-github

# Configurar un repo concreto
gitbox mirror setup forgejo-github --repo infra/homelab
```

### Estado de mirror

Comprueba estado de sync vivo comparando commits HEAD en ambos lados:

```bash
# Todos los mirrors
gitbox mirror status

# Grupo concreto
gitbox mirror status forgejo-github

# Salida JSON
gitbox mirror status --json
```

Indicadores de estado:

| Símbolo | Color  | Estado | Significado                               |
| ------- | ------ | ------ | ----------------------------------------- |
| `+`     | Verde  | synced | Los commits HEAD coinciden en ambos lados |
| `<`     | Morado | behind | El backup va por detrás del origin        |
| `+`     | Verde  | active | El mirror existe pero no puede comparar   |
| `x`     | Rojo   | error  | Error API o repo ausente                  |

Aparece un aviso `⚠ backup repo is PUBLIC` si el repo de backup no es privado.

### Credenciales de mirror

Los servidores remotos necesitan PATs portables (no tokens GCM locales de máquina). Consulta [credentials.md](credentials.md#mirrors-con-gcm) para instrucciones de setup.

### Matriz de automatización

| Escenario                | Automatizable          | Configurado en |
| ------------------------ | ---------------------- | -------------- |
| Forgejo/Gitea push → any | Sí (push mirror API)   | Forgejo/Gitea  |
| Forgejo/Gitea pull ← any | Sí (migrate API)       | Forgejo/Gitea  |
| GitLab push → any        | Sí (remote mirror API) | GitLab         |
| GitHub push → any        | No (solo guía)         | N/A            |
| Bitbucket push → any     | No (solo guía)         | N/A            |

### Formato de config de mirror

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

| Campo                     | Tipo   | Requerido | Descripción                                                       |
| ------------------------- | ------ | --------- | ----------------------------------------------------------------- |
| `account_src`             | string | Sí        | Clave de cuenta source                                            |
| `account_dst`             | string | Sí        | Clave de cuenta destination (debe diferir de src)                 |
| `repos.<key>.direction`   | string | Sí        | `"push"` o `"pull"`                                               |
| `repos.<key>.origin`      | string | Sí        | `"src"` o `"dst"` — qué cuenta es la fuente de verdad             |
| `repos.<key>.target_repo` | string | No        | Sobrescribir nombre del repo destino (por defecto: mismo que key) |
| `repos.<key>.method`      | string | No        | `"api"` o `"manual"`                                              |
| `repos.<key>.status`      | string | No        | `"active"`, `"pending"`, `"error"`, `"paused"`                    |
| `repos.<key>.last_sync`   | string | No        | Timestamp RFC3339 del último sync conocido                        |
| `repos.<key>.error`       | string | No        | Último mensaje de error                                           |

---

## Workspaces

Los workspaces agrupan N clones en un único artefacto (VS Code `.code-workspace` o YAML de tmuxinator) que los abre juntos.

### Comandos

```bash
gitbox workspace list                                       # tabla resumen
gitbox workspace list --json                                # legible por máquinas
gitbox workspace show <key>                                 # detalle completo
gitbox workspace add <key> --type <type> [flags]            # crear
gitbox workspace delete <key>                               # quitar de config (conserva archivo)
gitbox workspace add-member <key> <source>/<repo-key>       # añadir clon al workspace
gitbox workspace delete-member <key> <source>/<repo-key>    # quitar clon
gitbox workspace generate <key> [--dry-run]                 # (re)escribir archivo en disco
gitbox workspace open <key>                                 # regenerar + lanzar
gitbox workspace discover [--apply]                         # escanear disco, opcionalmente adoptar
```

### Flags de `add`

| Flag       | Requerido     | Descripción                                                                                                                            |
| ---------- | ------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `--type`   | Sí            | `codeWorkspace` o `tmuxinator`                                                                                                         |
| `--name`   | No            | Nombre visible en `show` y UI (por defecto la key)                                                                                     |
| `--file`   | No            | Sobrescribir ruta generada (por defecto: ancestro común más cercano para `codeWorkspace`, `~/.tmuxinator/<key>.yml` para `tmuxinator`) |
| `--layout` | No            | Solo tmuxinator: `windowsPerRepo` (default) o `splitPanes`                                                                             |
| `--member` | No, repetible | `<source-key>/<repo-key>`                                                                                                              |

### Ruta de archivo por defecto

- `codeWorkspace` — `<common-ancestor>/<key>.code-workspace`. El ancestro común es el prefijo de directorio compartido más largo de todas las rutas de clones miembros. Si no se encuentra un ancestro razonable (cross-filesystem), gitbox cae al padre del primer miembro.
- `tmuxinator` — `~/.tmuxinator/<key>.yml` (fijado por la herramienta). En Windows la ruta resuelve al `~/.tmuxinator/<key>.yml` del lado WSL accedido mediante su equivalente UNC `\\wsl.localhost\<distro>\…`, así que `Generate` escribe a través de la vista Windows del filesystem WSL. WSL debe estar instalado (`wsl.exe --status` tiene éxito); si no, los workspaces tmuxinator fallan con `ErrTmuxinatorUnsupported`.

### Comportamiento de open

`open` siempre regenera primero el archivo para que el artefacto esté actualizado, y luego lanza:

- `codeWorkspace` → primera entrada en `global.editors` invocada como `<editor.command> <workspace-file>`. Añade un editor con `gitbox global editor add` o editando la config directamente.
- `tmuxinator` → primera entrada en `global.terminals` invocada como `<term.command> <term.args...> tmuxinator start <key>`. El token `{command}` en los args de terminal se reemplaza por `tmuxinator start <key>`; el token `{path}` se descarta (los workspaces no tienen una ruta única). En Windows el argv hijo se convierte en `wsl.exe -- tmuxinator start <key>`, así tmuxinator se ejecuta dentro de WSL independientemente de qué perfil de terminal esté seleccionado.

### Comportamiento de discover

`discover` recorre la carpeta gestionada por gitbox en busca de archivos `*.code-workspace` y `~/.tmuxinator/*.yml` (más el `~/.tmuxinator/` del lado WSL en Windows cuando WSL está disponible). Cada ruta de carpeta parseada se asocia de vuelta a un clon conocido usando una coincidencia deepest-prefix contra las rutas de repo resueltas:

- **Adoptable** — cada miembro resolvió exactamente a un clon. Persistido por `--apply` con `discovered: true`.
- **Ambiguous** — al menos un miembro empató entre dos o más clones. Se muestra para revisión humana; nunca se auto-adopta.
- **Skipped** — la key de workspace ya existe en `gitbox.json`, o ningún miembro resolvió.

Discovery es read-only sin `--apply`. La GUI lo llama al arrancar la app (en una goroutine); la TUI lo llama al lanzarse y en cada tick de periodic-sync. La barra de estado muestra una pista `! N ambiguous workspace(s)` cuando existen coincidencias ambiguas.

### Formato de config de workspace

```json
{
  "workspaces": {
    "feat-x": {
      "type": "codeWorkspace",
      "name": "Feature X",
      "file": "/home/me/00.git/feat-x.code-workspace",
      "members": [
        { "source": "github-personal", "repo": "myorg/frontend" },
        { "source": "gitea-work", "repo": "team/backend" }
      ]
    },
    "pair-session": {
      "type": "tmuxinator",
      "layout": "windowsPerRepo",
      "file": "/home/me/.tmuxinator/pair-session.yml",
      "members": [
        { "source": "github-personal", "repo": "myorg/frontend" },
        { "source": "github-personal", "repo": "myorg/backend" }
      ]
    }
  }
}
```

| Campo              | Tipo   | Requerido | Descripción                                                                                                    |
| ------------------ | ------ | --------- | -------------------------------------------------------------------------------------------------------------- |
| `type`             | string | Sí        | `"codeWorkspace"` o `"tmuxinator"`                                                                             |
| `name`             | string | No        | Nombre visible; por defecto la key                                                                             |
| `file`             | string | No        | Ruta absoluta del artefacto generado; se rellena automáticamente con `generate`                                |
| `layout`           | string | No        | Solo tmuxinator: `"windowsPerRepo"` o `"splitPanes"`                                                           |
| `members`          | array  | Sí        | Lista de miembros, cada uno con `source` y `repo`                                                              |
| `members[].source` | string | Sí        | Source key (debe existir en `sources`)                                                                         |
| `members[].repo`   | string | Sí        | Repo key dentro de esa source                                                                                  |
| `discovered`       | bool   | No        | `true` para workspaces adoptados desde disco por `gitbox workspace discover --apply` (o auto-adopción GUI/TUI) |

### Contenido generado de `.code-workspace`

```json
{
  "folders": [
    { "path": "github-personal/myorg/frontend", "name": "frontend" },
    { "path": "gitea-work/team/backend", "name": "backend" }
  ],
  "settings": {
    "git.autoRepositoryDetection": true,
    "git.repositoryScanMaxDepth": 2,
    "git.openRepositoryInParentFolders": "always"
  }
}
```

`folders[].path` es relativo cuando cada miembro vive bajo el directorio del archivo de workspace, absoluto en caso contrario. El bloque `settings` se mantiene mínimo a propósito: tres claves que hacen que VS Code detecte realmente repos anidados bajo una raíz compartida. Edito el archivo generado a mano si necesito más; gitbox solo lo sobrescribirá cuando ejecute `generate`/`open`.

---

## Completado de shell

Genera scripts de autocompletado para tu shell:

```bash
# Bash
gitbox completion bash > /etc/bash_completion.d/gitbox

# Zsh
gitbox completion zsh > "${fpath[1]}/_gitbox"

# Fish
gitbox completion fish > ~/.config/fish/completions/gitbox.fish

# PowerShell
gitbox completion powershell > gitbox.ps1
```

Consulta [completion.md](completion.md) para instrucciones detalladas de setup.

---

## Auto-update

Gitbox puede buscar y aplicar actualizaciones desde GitHub releases.

```bash
gitbox update           # comprobar e instalar interactivamente
gitbox update --check   # solo comprobar, sin instalar (exit code 0 = actualizado)
```

El updater descarga el artefacto específico de plataforma, verifica el checksum SHA256 (si `checksums.sha256` está presente en la release) y reemplaza los binarios in-place. En Windows, el binario en ejecución se renombra a `.old` y se limpia en el siguiente startup.

La GUI comprueba actualizaciones automáticamente una vez cada 24 horas y muestra un banner cuando hay una versión nueva.

---

## Referencia del archivo de configuración

La config vive en `~/.config/gitbox/gitbox.json`. Consulta [gitbox.jsonc](../../json/gitbox.jsonc) para un ejemplo completamente anotado.

**Backups automáticos:** Cada guardado significativo crea un backup fechado en el mismo directorio (por ejemplo, `gitbox-20260401-143025.json`). Se conservan los 10 más recientes; los más antiguos se podan automáticamente. La pantalla de recuperación de corrupción de la GUI puede restaurar cualquiera de ellos con un clic. Los guardados solo de posición de ventana (mover o redimensionar la GUI) omiten el backup, así las copias reales pre-corrupción no se rotan fuera por churn cosmético.

### Global

| Campo                             | Tipo   | Requerido | Descripción                                                                                                                                                                                                                                                  |
| --------------------------------- | ------ | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `folder`                          | string | Sí        | Directorio raíz para todos los clones. Soporta `~`.                                                                                                                                                                                                          |
| `credential_ssh`                  | object | No        | Defaults SSH de plataforma. Su presencia indica que SSH está disponible.                                                                                                                                                                                     |
| `credential_ssh.ssh_folder`       | string | No        | Directorio de config SSH. Default `~/.ssh`.                                                                                                                                                                                                                  |
| `credential_gcm`                  | object | No        | Defaults GCM de plataforma. Su presencia indica que GCM está disponible.                                                                                                                                                                                     |
| `credential_gcm.helper`           | string | No        | Helper de credenciales. Normalmente `"manager"`.                                                                                                                                                                                                             |
| `credential_gcm.credential_store` | string | No        | `"wincredman"`, `"keychain"` o `"secretservice"`.                                                                                                                                                                                                            |
| `credential_token`                | object | No        | Defaults de Token/PAT de plataforma. Su presencia indica que token auth está disponible.                                                                                                                                                                     |
| `editors`                         | array  | No        | Editores de código para el menú "Open in". Auto-poblado en el primer launch.                                                                                                                                                                                 |
| `editors[].name`                  | string | Sí        | Nombre visible (por ejemplo `"VS Code"`).                                                                                                                                                                                                                    |
| `editors[].command`               | string | Sí        | Ruta completa o nombre de comando (por ejemplo `"C:\\...\\code.cmd"`).                                                                                                                                                                                       |
| `terminals`                       | array  | No        | Emuladores de terminal para el menú "Open in". Auto-poblado en el primer launch.                                                                                                                                                                             |
| `terminals[].name`                | string | Sí        | Nombre visible (por ejemplo `"Windows Terminal"`).                                                                                                                                                                                                           |
| `terminals[].command`             | string | Sí        | Ruta completa o launcher en PATH (por ejemplo `"wt.exe"`, `"gnome-terminal"`).                                                                                                                                                                               |
| `terminals[].args`                | array  | No        | Argumentos pasados antes de la ruta. Usa `"{path}"` como placeholder de ruta; si falta, la ruta se añade al final. Usa `"{command}"` para marcar dónde se inserta el argv de un AI harness (se expande a cero elementos para lanzamientos solo de terminal). |
| `ai_harnesses`                    | array  | No        | AI CLI harnesses para el menú "Open in". Auto-poblado en el primer launch (claude, codex, gemini, aider, cursor-agent, opencode). Lanzado dentro de `global.terminals[0]`, que debe contener `"{command}"` en sus args.                                      |
| `ai_harnesses[].name`             | string | Sí        | Nombre visible (por ejemplo `"Claude Code"`).                                                                                                                                                                                                                |
| `ai_harnesses[].command`          | string | Sí        | Ruta absoluta o binario en PATH (por ejemplo `"claude"`).                                                                                                                                                                                                    |
| `ai_harnesses[].args`             | array  | No        | Args opcionales para el harness. Normalmente vacío.                                                                                                                                                                                                          |

### Account

| Campo                     | Tipo    | Requerido   | Descripción                                                                              |
| ------------------------- | ------- | ----------- | ---------------------------------------------------------------------------------------- |
| `provider`                | string  | Sí          | `"github"`, `"gitlab"`, `"gitea"`, `"forgejo"`, `"bitbucket"`, `"generic"`               |
| `url`                     | string  | Sí          | URL del servidor (scheme+host, sin path).                                                |
| `username`                | string  | Sí          | Username de la cuenta.                                                                   |
| `name`                    | string  | Sí          | `git user.name` por defecto.                                                             |
| `email`                   | string  | Sí          | `git user.email` por defecto.                                                            |
| `default_credential_type` | string  | No          | Auth por defecto: `"gcm"`, `"ssh"` o `"token"`.                                          |
| `ssh.host`                | string  | Condicional | Alias SSH Host (por ejemplo `"gt-myuser"`). **Obligatorio** cuando SSH está configurado. |
| `ssh.hostname`            | string  | No          | Hostname SSH real. Auto-derivado de URL si se omite.                                     |
| `ssh.key_type`            | string  | Condicional | `"ed25519"` o `"rsa"`. **Obligatorio** cuando SSH está configurado.                      |
| `gcm.provider`            | string  | No          | Pista de proveedor GCM.                                                                  |
| `gcm.useHttpPath`         | boolean | No          | Acotar credenciales por HTTP path.                                                       |

### Source

| Campo     | Tipo   | Requerido | Descripción                                                           |
| --------- | ------ | --------- | --------------------------------------------------------------------- |
| `account` | string | Sí        | Referencia una account key.                                           |
| `folder`  | string | No        | Sobrescribe la carpeta de clone de primer nivel. Default: source key. |

### Repo (dentro de source.repos)

| Campo             | Tipo   | Requerido | Descripción                                                                      |
| ----------------- | ------ | --------- | -------------------------------------------------------------------------------- |
| `credential_type` | string | No        | Sobrescribe método de auth. Hereda de la cuenta.                                 |
| `name`            | string | No        | Sobrescribe `git user.name`.                                                     |
| `email`           | string | No        | Sobrescribe `git user.email`.                                                    |
| `id_folder`       | string | No        | Sobrescribe directorio de 2º nivel (carpeta org).                                |
| `clone_folder`    | string | No        | Sobrescribe directorio de 3er nivel. Si es absoluto, reemplaza la ruta completa. |

---

## Troubleshooting

### Config no encontrado

```bash
# Comprobar dónde busca gitbox la config
gitbox global config path

# Crear una config nueva
gitbox init

# O especificar una ruta custom
gitbox status --config /path/to/my-config.json
```

### GCM abre la cuenta equivocada en el navegador

Limpia credenciales cacheadas:

- **Windows:** Control Panel > Credential Manager > elimina entradas `git:https://github.com`
- **macOS:** Keychain Access > busca `github.com` > delete
- **Linux:** `secret-tool clear protocol https host github.com`

### SSH connection refused

```bash
ssh -T git@gh-YourAlias -v
# Comprobar: clave añadida al proveedor, IdentityFile correcto, ssh-agent en marcha
```

### Auth de navegador GCM en headless/SSH

Si el setup de credencial GCM no abre navegador:

- **Comprueba tu entorno:** En Linux, la auth de navegador de GCM necesita un servidor de pantalla. Ejecuta `echo $DISPLAY`; si está vacío, estás en una sesión headless.
- **Ejecuta desde una terminal de escritorio:** Entra por SSH en la máquina desde una sesión de escritorio con X forwarding (`ssh -X`) o ejecuta el comando directamente en el escritorio.
- **Deja que GCM lo gestione más tarde:** Omite el setup de navegador. GCM preguntará interactivamente en el siguiente `git clone` o `git fetch` si la credencial todavía no está guardada.
- **Usa un token en su lugar:** Si la auth de navegador no es práctica, cambia la cuenta al tipo de credencial token: `gitbox account credential setup <key> --token`.

### "Repository not found" al clonar

- Verifica que el nombre `org/repo` coincide con el repo real
- Verifica tus credenciales: prueba con `git ls-remote <url>`
- Para repos cross-org, asegúrate de que tu cuenta tiene acceso
