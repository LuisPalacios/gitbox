# Primeros pasos con la CLI de gitbox

[Read in English](../cli-guide.md)

<p align="center">
  <img src="../../assets/screenshot-cli.png" alt="Gitbox CLI" width="800" />
</p>

<p align="center">
  <img src="../diagrams/cli-workflow.png" alt="Flujo CLI" width="700" />
</p>

Esta guía recorre el flujo completo de la CLI: inicializar la configuración, añadir cuentas, preparar credenciales, descubrir repositorios, clonar y mantenerlos al día. Los comandos y valores internos aparecen en inglés porque forman parte de la interfaz estable de `gitbox`.

## Requisitos previos

Necesitas `git` instalado y accesible desde `PATH`. Según el tipo de credencial que uses, también puedes necesitar Git Credential Manager, `ssh`, `ssh-agent`, `tmux`, WSL o un navegador disponible en la máquina.

Comprueba el entorno con:

```bash
gitbox doctor
```

`doctor` muestra las herramientas que faltan y explica qué funciones quedan afectadas.

## Instalación

Descarga una release o construye desde el repositorio. El binario de la CLI se llama `gitbox` en macOS y Linux, y `gitbox.exe` en Windows.

Después de instalar, confirma que el binario responde:

```bash
gitbox --version
gitbox --help
```

## Paso 1: inicializar

Inicio la configuración con:

```bash
gitbox init
```

El asistente crea el archivo `~/.config/gitbox/gitbox.json` y pregunta por la carpeta raíz donde `gitbox` colocará los clones. La estructura normal queda así:

```text
<root>/
  github-personal/
    LuisPalacios/
      repo-a/
      repo-b/
```

Puedo seleccionar español para la salida humana con `--lang es`, `GITBOX_LANG=es` o guardando `global.language` en la configuración:

```bash
gitbox --lang es --help
gitbox global update --language es
```

## Paso 2: añadir cuentas

Una cuenta representa una identidad de acceso a un proveedor. El `account key` debe ser estable, legible y único, por ejemplo `github-personal`, `github-work` o `forgejo-home`.

### Forgejo o Gitea con GCM

```bash
gitbox account add forgejo-home \
  --provider forgejo \
  --host git.example.test \
  --user alice \
  --default-credential-type gcm
```

Uso `--host` para instancias self-hosted. No incluyo `https://`; `gitbox` construye las URLs a partir del proveedor, host y cuenta.

### GitHub con GCM

```bash
gitbox account add github-personal \
  --provider github \
  --user alice \
  --default-credential-type gcm
```

GCM funciona bien en máquinas con navegador porque delega el login interactivo al proveedor.

### GitHub con SSH

```bash
gitbox account add github-ssh \
  --provider github \
  --user alice \
  --default-credential-type ssh \
  --ssh-host github.com \
  --ssh-user git \
  --ssh-key-path ~/.ssh/id_ed25519
```

SSH encaja mejor en servidores, entornos sin navegador y cuentas que ya usan claves por proveedor.

### GitHub con token

```bash
gitbox account add github-token \
  --provider github \
  --user alice \
  --default-credential-type token
```

Los tokens son útiles para automatización y para proveedores self-hosted donde GCM no encaja.

### Ver cuentas

```bash
gitbox account list
gitbox account show github-personal
```

## Paso 3: configurar credenciales

Después de crear la cuenta, preparo la credencial:

```bash
gitbox credential setup github-personal
```

El comando detecta el tipo configurado en la cuenta y guía el proceso:

- `gcm`: abre el flujo de Git Credential Manager.
- `ssh`: comprueba host, usuario, clave y agente SSH.
- `token`: pide o localiza un PAT y lo guarda usando el mecanismo disponible.

Verifico que la credencial funciona:

```bash
gitbox credential verify github-personal
```

Si el proveedor necesita un PAT para llamadas API aunque clones con GCM o SSH, revisa [Credenciales](credentials.md).

## Paso 4: descubrir repositorios

`discover` consulta el proveedor y añade repositorios a la configuración.

```bash
gitbox discover github-personal
```

El modo interactivo muestra una lista numerada para elegir qué repos guardar. Para añadir todo sin preguntar:

```bash
gitbox discover github-personal --all
```

También puedo excluir forks o repos archivados:

```bash
gitbox discover github-personal --all --exclude-forks --exclude-archived
```

Para scripting, uso JSON:

```bash
gitbox discover github-personal --json
```

Para recorrer todas las cuentas configuradas:

```bash
gitbox discover --all-accounts
```

## Paso 5: clonar

Clono todos los repos configurados:

```bash
gitbox clone
```

Clono una fuente concreta:

```bash
gitbox clone --source github-personal
```

Clono un repositorio concreto:

```bash
gitbox clone --repo cli
```

`gitbox` crea carpetas siguiendo la configuración y aplica la URL remota adecuada para el tipo de credencial. Si un repo ya existe, lo omite en lugar de reemplazarlo.

## Paso 6: trabajo diario

### Revisar estado

```bash
gitbox status
gitbox status --source github-personal
gitbox status --json
```

El estado muestra clones ausentes, repos adelantados o atrasados, ramas divergentes y errores de acceso. Los valores como `synced`, `behind` o `missing` permanecen en inglés porque se usan también en JSON y scripts.

### Traer cambios

```bash
gitbox pull
gitbox pull --source github-personal
```

`pull` solo avanza ramas con fast-forward. Si un repo tiene cambios locales, ramas divergentes o conflictos, `gitbox` lo informa y no fuerza nada.

### Abrir en el navegador

```bash
gitbox browse cli
gitbox browse cli --source github-personal
gitbox browse cli --json
```

`browse --json` imprime la URL sin abrir el navegador, útil para scripts o terminales remotas.

### Limpiar ramas obsoletas

```bash
gitbox sweep --dry-run
gitbox sweep
gitbox sweep --source github-personal
gitbox sweep --repo cli
```

Primero ejecuto `--dry-run` para ver qué ramas locales se eliminarían. `sweep` no elimina ramas con trabajo local sin fusionar.

### Escanear cualquier directorio

```bash
gitbox scan .
gitbox scan C:\src
gitbox scan ~/src --pull
```

`scan` busca repos Git bajo una carpeta aunque no formen parte de la configuración. Con `--pull`, trae cambios donde sea seguro hacer fast-forward.

### Adoptar repos huérfanos

```bash
gitbox adopt
gitbox adopt --dry-run
gitbox adopt --all
```

`adopt` compara repos encontrados en disco con la configuración y propone añadir los que reconoce por sus remotos. Si necesita mover carpetas, usa modo interactivo para evitar cambios inesperados.

### Mover un repo entre cuentas o proveedores

Cuando cambio un repo de organización, cuenta o proveedor, actualizo la configuración con `repo update` y luego reviso el remoto. Si el repo físico necesita moverse de carpeta, uso el flujo interactivo de adopción o muevo manualmente después de confirmar rutas.

### Instalar un gitignore global recomendado

```bash
gitbox gitignore status
gitbox gitignore install
```

`install` añade un bloque gestionado a `~/.gitignore_global`, crea backups con timestamp y evita duplicados dentro de los marcadores de `gitbox`.

## Paso 7: configurar mirrors opcionales

Un mirror sincroniza repos entre proveedores, por ejemplo Forgejo como origen privado y GitHub como destino público.

### Crear un grupo de mirror

```bash
gitbox mirror group add public-sync \
  --source-account forgejo-home \
  --destination-account github-personal
```

### Añadir repos al mirror

Push desde Forgejo hacia GitHub:

```bash
gitbox mirror repo add public-sync cli \
  --source-repo cli \
  --destination-repo cli \
  --direction push
```

Pull desde GitHub hacia Forgejo:

```bash
gitbox mirror repo add public-sync cli \
  --source-repo cli \
  --destination-repo cli \
  --direction pull
```

### Descubrir mirrors existentes

```bash
gitbox mirror discover public-sync
gitbox mirror discover public-sync --apply
```

### Revisar estado

```bash
gitbox mirror status
gitbox mirror status public-sync
gitbox mirror status --json
```

### Credenciales de mirror

Cada lado del mirror usa la credencial de su cuenta. Si un proveedor exige permisos API para crear o consultar mirrors, configura el PAT correspondiente aunque el clon normal use SSH o GCM.

## Paso 8: workspaces dinámicos opcionales

Los workspaces agrupan repos para abrirlos juntos en VS Code o tmuxinator.

### Crear un workspace de VS Code

```bash
gitbox workspace add platform \
  --repo api \
  --repo web \
  --repo cli \
  --type vscode
```

Abro el workspace:

```bash
gitbox workspace open platform
```

### Tmuxinator en macOS o Linux

```bash
gitbox workspace add platform \
  --repo api \
  --repo worker \
  --type tmuxinator
```

`gitbox` genera el archivo de tmuxinator y abre sesiones con los repos seleccionados.

### Tmuxinator en Windows mediante WSL

En Windows, `gitbox` usa WSL para abrir tmuxinator cuando está disponible. Las rutas se convierten con los helpers internos de WSL para que los paneles apunten a la carpeta correcta.

### Descubrir workspaces en disco

```bash
gitbox workspace discover
gitbox workspace discover --apply
```

Esto detecta archivos `.code-workspace` o configuraciones compatibles que existan en la carpeta raíz.

## Actualizar gitbox

```bash
gitbox update check
gitbox update install
```

`check` consulta la última versión disponible. `install` descarga y reemplaza el binario cuando el entorno lo permite.

## Qué hacer después

- Consulta [Credenciales](credentials.md) si el login falla o necesitas elegir entre GCM, SSH y token.
- Usa [Referencia](reference.md) cuando necesites un comando o flag concreto.
- Abre [Guía de la GUI](gui-guide.md) si prefieres gestionar cuentas, repos y mirrors desde `GitboxApp`.
