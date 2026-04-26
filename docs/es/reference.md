# Guía de referencia

[Read in English](../reference.md)

Esta referencia resume comandos, flags habituales y formato de configuración. Los nombres de comandos, flags, claves JSON, proveedores, códigos de salida y valores de estado no se traducen.

## Idioma

`gitbox` resuelve el idioma de textos humanos en este orden:

- Flag global `--lang en|es`.
- Variable `GITBOX_LANG`.
- Campo `global.language` en `gitbox.json`.
- Locale del sistema operativo.
- Inglés como fallback.

Ejemplos:

```bash
gitbox --lang es status
GITBOX_LANG=es gitbox status
gitbox global update --language es
```

## Gestión de cuentas

### Añadir cuentas

GitHub personal:

```bash
gitbox account add github-personal \
  --provider github \
  --user alice \
  --default-credential-type gcm
```

Forgejo o Gitea self-hosted con SSH:

```bash
gitbox account add forgejo-home \
  --provider forgejo \
  --host git.example.test \
  --user alice \
  --default-credential-type ssh \
  --ssh-host git.example.test \
  --ssh-user git \
  --ssh-key-path ~/.ssh/id_ed25519
```

GitLab:

```bash
gitbox account add gitlab-work \
  --provider gitlab \
  --user alice \
  --default-credential-type token
```

### Listar e inspeccionar

```bash
gitbox account list
gitbox account show github-personal
gitbox account show github-personal --json
```

### Actualizar

```bash
gitbox account update github-personal --user alice2
gitbox account update github-personal --default-credential-type ssh
gitbox account update forgejo-home --host git.internal.test
```

### Eliminar

```bash
gitbox account delete github-personal
```

Si una source, repo o mirror todavía referencia la cuenta, `gitbox` debe rechazar el borrado e indicar qué referencia queda.

## Gestión de sources

Una source agrupa repos bajo una cuenta.

```bash
gitbox source add github-personal --account github-personal
gitbox source list
gitbox source show github-personal
gitbox source delete github-personal
```

## Gestión de repos

### Añadir repos

Repo simple que hereda el tipo de credencial de la cuenta:

```bash
gitbox repo add github-personal cli --owner LuisPalacios --name gitbox
```

Repo de otra organización usando la misma cuenta:

```bash
gitbox repo add github-personal docs --owner ExampleOrg --name docs
```

Sobrescribir el tipo de credencial de un repo:

```bash
gitbox repo add github-personal cli \
  --owner LuisPalacios \
  --name gitbox \
  --credential-type ssh
```

### Overrides de carpeta

Cambiar el segundo nivel de carpeta:

```bash
gitbox repo update github-personal cli --folder-owner myorg-rest
```

Cambiar el nombre final del clon:

```bash
gitbox repo update github-personal cli --folder-name website
```

Usar una ruta absoluta:

```bash
gitbox repo update github-personal cli --path ~/.config/my-config
```

### Listar, inspeccionar, actualizar y eliminar

```bash
gitbox repo list github-personal
gitbox repo show github-personal cli
gitbox repo update github-personal cli --credential-type token
gitbox repo delete github-personal cli
```

## Estructura de carpetas

Por defecto, `gitbox` clona en:

```text
<global.folder>/<source>/<owner>/<repo>/
```

Ejemplo:

```text
~/00.git/github-personal/LuisPalacios/gitbox/
```

Los overrides permiten adaptar repos con nombres especiales, organizaciones cruzadas o rutas absolutas.

## Autenticación

Preparar credenciales:

```bash
gitbox credential setup github-personal
```

Verificar:

```bash
gitbox credential verify github-personal
```

Eliminar credenciales guardadas:

```bash
gitbox credential delete github-personal
```

Convención de variables para tokens:

```bash
GITBOX_TOKEN_<ACCOUNT_KEY>
```

Ejemplo:

```bash
export GITBOX_TOKEN_GITHUB_PERSONAL="ghp_example"
```

## Estado

Todos los repos:

```bash
gitbox status
```

Filtrar por source:

```bash
gitbox status --source github-personal
```

Salida JSON:

```bash
gitbox status --json
```

Los estados como `synced`, `behind`, `ahead`, `diverged`, `missing` o `error` se mantienen en inglés para que los scripts sean estables.

## Clonado

Clonar todos los repos configurados:

```bash
gitbox clone
```

Clonar una source:

```bash
gitbox clone --source github-personal
```

Clonar un repo concreto:

```bash
gitbox clone --source github-personal --repo cli
```

## Pull

Traer todos los repos atrasados con fast-forward:

```bash
gitbox pull
```

Filtrar por source:

```bash
gitbox pull --source github-personal
```

`pull` no fuerza cambios ni resuelve conflictos. Si un repo diverge o tiene cambios locales incompatibles, lo informa.

## Navegador

Abrir un repo:

```bash
gitbox browse cli
```

Acotar por source:

```bash
gitbox browse cli --source github-personal
```

Imprimir URL en JSON:

```bash
gitbox browse cli --json
```

## Sweep

Vista previa:

```bash
gitbox sweep --dry-run
```

Ejecutar:

```bash
gitbox sweep
gitbox sweep --source github-personal
gitbox sweep --repo cli
```

`sweep` elimina ramas locales obsoletas cuando es seguro hacerlo. No borra trabajo local sin fusionar.

## Scan

Escanear desde el directorio actual:

```bash
gitbox scan .
```

Escanear una ruta concreta:

```bash
gitbox scan ~/src
```

Escanear y hacer pull de repos atrasados:

```bash
gitbox scan ~/src --pull
```

## Adopt

Adopción interactiva:

```bash
gitbox adopt
```

Vista previa:

```bash
gitbox adopt --dry-run
```

Adoptar coincidencias sin preguntar:

```bash
gitbox adopt --all
```

Si adoptar requiere mover carpetas, usa modo interactivo para revisar rutas.

## Discovery

Interactivo:

```bash
gitbox discover github-personal
```

Añadir todo:

```bash
gitbox discover github-personal --all
```

Excluir forks y archivados:

```bash
gitbox discover github-personal --all --exclude-forks --exclude-archived
```

Salida JSON:

```bash
gitbox discover github-personal --json
```

Descubrir mirrors existentes:

```bash
gitbox mirror discover public-sync
gitbox mirror discover public-sync --apply
```

## Doctor

Tabla legible:

```bash
gitbox doctor
```

JSON para scripts o informes:

```bash
gitbox doctor --json
```

`doctor` revisa herramientas externas y no debe exigir componentes que no corresponden al flujo elegido.

## Gitignore global

Estado:

```bash
gitbox gitignore status
```

Instalar o refrescar el bloque recomendado:

```bash
gitbox gitignore install
```

Salida JSON:

```bash
gitbox gitignore status --json
gitbox gitignore install --json
```

El instalador crea backups con timestamp y mantiene una ventana corta de backups antiguos.

## Mirrors

### Grupos

Crear un grupo:

```bash
gitbox mirror group add public-sync \
  --source-account forgejo-home \
  --destination-account github-personal
```

Listar:

```bash
gitbox mirror group list
```

Ver detalles:

```bash
gitbox mirror group show public-sync --json
```

Eliminar:

```bash
gitbox mirror group delete public-sync
```

### Repos de mirror

Push desde origen a destino:

```bash
gitbox mirror repo add public-sync cli \
  --source-repo cli \
  --destination-repo cli \
  --direction push
```

Pull desde destino hacia origen:

```bash
gitbox mirror repo add public-sync cli \
  --source-repo cli \
  --destination-repo cli \
  --direction pull
```

Añadir y ejecutar setup:

```bash
gitbox mirror repo add public-sync cli --setup
```

Quitar un repo del grupo:

```bash
gitbox mirror repo delete public-sync cli
```

### Setup

Todos los mirrors pendientes:

```bash
gitbox mirror setup
```

Un grupo:

```bash
gitbox mirror setup public-sync
```

Un repo:

```bash
gitbox mirror setup public-sync --repo cli
```

### Estado

```bash
gitbox mirror status
gitbox mirror status public-sync
gitbox mirror status --json
```

### Credenciales

Cada lado usa la cuenta configurada para ese proveedor. Si el setup usa API, puede necesitar PAT incluso cuando el clone normal usa SSH o GCM.

### Formato de configuración

```json
{
  "mirrors": {
    "public-sync": {
      "source_account": "forgejo-home",
      "destination_account": "github-personal",
      "repos": {
        "cli": {
          "source_repo": "cli",
          "destination_repo": "cli",
          "direction": "push"
        }
      }
    }
  }
}
```

## Workspaces

Comandos principales:

```bash
gitbox workspace list
gitbox workspace add platform --repo api --repo web --type vscode
gitbox workspace open platform
gitbox workspace delete platform
gitbox workspace discover
gitbox workspace discover --apply
```

Flags habituales de `add`:

- `--repo`: repo incluido; puede repetirse.
- `--type`: `vscode` o `tmuxinator`.
- `--name`: nombre visible si quieres separarlo del key.

Los `.code-workspace` generados apuntan a las rutas reales de los clones configurados.

## Shell completion

Bash:

```bash
gitbox completion bash
```

Zsh:

```bash
gitbox completion zsh
```

Fish:

```bash
gitbox completion fish
```

PowerShell:

```bash
gitbox completion powershell
```

Consulta [Completado de shell](completion.md) para instalación permanente.

## Auto-update

```bash
gitbox update check
gitbox update install
```

`check` informa la versión disponible. `install` intenta descargar y reemplazar el binario actual cuando permisos y plataforma lo permiten.

## Referencia del archivo de configuración

El archivo vive en `~/.config/gitbox/gitbox.json` y usa `version: 2`.

```json
{
  "version": 2,
  "global": {
    "folder": "~/00.git",
    "language": "es"
  },
  "accounts": {},
  "sources": {}
}
```

### Global

- `folder`: carpeta raíz de clones.
- `language`: idioma preferido para textos humanos, `en` o `es`.
- Configuración de fetch periódico y herramientas externas cuando estén disponibles.

### Account

- `provider`: `github`, `gitlab`, `gitea`, `forgejo` o `bitbucket` cuando esté soportado.
- `user`: usuario principal.
- `host`: host para proveedores self-hosted.
- `default_credential_type`: `gcm`, `ssh` o `token`.
- Campos SSH como host, usuario y clave cuando aplica.

### Source

- `account`: cuenta usada por la source.
- `repos`: mapa de repos gestionados bajo esa source.

### Repo

- `owner`: propietario remoto.
- `name`: nombre remoto.
- `credential_type`: override opcional.
- Overrides de carpeta o ruta absoluta cuando el layout por defecto no sirve.

## Resolución de problemas

### Config no encontrado

Comprueba dónde busca `gitbox`:

```bash
gitbox global show
```

Crea una configuración nueva:

```bash
gitbox init
```

O especifica una ruta custom si el comando la admite en tu flujo:

```bash
gitbox --config ./gitbox.json status
```

### GCM abre la cuenta equivocada

Cierra sesión en el navegador, borra la credencial equivocada del almacén de GCM o usa una sesión de navegador separada. Luego repite:

```bash
gitbox credential setup github-personal
```

### SSH connection refused

Comprueba host, clave, `ssh-agent` y que la clave esté añadida al proveedor:

```bash
ssh-add -l
ssh -T git@github.com
```

### GCM en headless o SSH remoto

Si no hay navegador disponible, usa `ssh` o `token`. GCM puede funcionar en algunos entornos remotos, pero requiere configuración explícita fuera de `gitbox`.

### Repository not found al clonar

Revisa owner, repo, host, permisos y tipo de credencial. En organizaciones, confirma que el token o login tiene acceso a esa org.
