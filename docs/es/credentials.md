# Configuración de credenciales

[Read in English](../credentials.md)

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Tipos de credenciales" width="850" />
</p>

`gitbox` admite tres tipos de credencial: `gcm`, `ssh` y `token`. Cada cuenta define un `default_credential_type`, y cada repo puede sobrescribirlo con `credential_type`. Esos valores no se traducen porque forman parte de la configuración y de la lógica de ejecución.

## Qué tipo debo usar

Uso `gcm` cuando trabajo en una máquina con navegador y quiero el flujo normal de Git Credential Manager. Encaja bien con GitHub, GitLab y algunos entornos self-hosted.

Uso `ssh` cuando ya gestiono claves por proveedor, trabajo en servidores, uso terminales remotas o quiero separar identidades mediante `IdentityFile` y host aliases.

Uso `token` cuando necesito automatización, una máquina sin navegador, o proveedores Gitea y Forgejo donde un PAT es el camino más directo para API y Git HTTP.

## Cuándo necesito un PAT junto a GCM o SSH

Git puede clonar con GCM o SSH, pero algunas acciones de `gitbox` hablan con la API del proveedor: descubrir repos, crear mirrors, consultar estado remoto o preparar integraciones. Si el proveedor no entrega esa capacidad mediante la credencial principal, configura un PAT complementario.

El PAT debe tener permisos suficientes para la acción:

- Lectura de repos para `discover` y estado.
- Escritura o administración de repos para crear mirrors o configurar repos remotos.
- Permisos de organización cuando el repo vive bajo una org y no bajo el usuario.

## Almacenamiento de PAT

`gitbox` busca tokens de forma explícita y predecible. La convención recomendada para variables de entorno es:

```bash
GITBOX_TOKEN_<ACCOUNT_KEY>
```

Convierte el `account key` a mayúsculas y cambia guiones por guiones bajos. Para `github-personal`, la variable queda:

```bash
GITBOX_TOKEN_GITHUB_PERSONAL
```

En PowerShell:

```powershell
$env:GITBOX_TOKEN_GITHUB_PERSONAL = "ghp_example"
```

En Bash o Zsh:

```bash
export GITBOX_TOKEN_GITHUB_PERSONAL="ghp_example"
```

No guardes tokens reales en documentación, issues, commits o archivos de ejemplo.

## GCM

Git Credential Manager delega el login en el navegador y guarda la credencial en el almacén seguro del sistema operativo.

### Requisitos previos

Comprueba que GCM está instalado:

```bash
git credential-manager --version
gitbox doctor
```

En Windows normalmente llega con Git for Windows. En macOS y Linux puede requerir instalación separada.

### Funcionamiento

Configura la cuenta con `gcm`:

```bash
gitbox account add github-personal \
  --provider github \
  --user alice \
  --default-credential-type gcm
```

Luego prepara y verifica:

```bash
gitbox credential setup github-personal
gitbox credential verify github-personal
```

Si el navegador abre una cuenta equivocada, cierra sesión en el proveedor o fuerza una nueva autorización desde GCM antes de repetir `credential setup`.

### Requisitos de gitconfig global

GCM suele requerir `credential.helper` en la configuración global de Git. `gitbox doctor` avisa cuando falta o parece inconsistente. Si tienes varios helpers antiguos, revisa `~/.gitconfig` antes de cambiarlo para no romper otros flujos.

### Detección de navegador

GCM necesita abrir un navegador para completar OAuth. En servidores, SSH remotos o contenedores, ese paso puede fallar. En esos entornos, usa `ssh` o `token` salvo que tengas un flujo de navegador remoto ya configurado.

### GCM en la TUI

La TUI puede iniciar el flujo de setup, pero la autenticación se completa fuera de la pantalla cuando GCM abre el navegador. Vuelve a `gitbox` después del login y ejecuta la verificación si la pantalla no se actualiza sola.

### Mirrors con GCM

Un mirror puede clonar con GCM, pero configurar el mirror mediante API puede exigir un PAT. Si `mirror setup` falla con 401 o permisos insuficientes, añade el PAT complementario para esa cuenta.

## Token

Un Personal Access Token permite autenticar Git HTTP y llamadas API sin navegador.

### Crear un PAT

En GitHub, crea un token con permisos sobre los repos que quieras gestionar. En GitLab, Gitea o Forgejo, crea un token equivalente con lectura de repos y permisos de escritura si vas a crear mirrors o repos.

Después, configura la cuenta:

```bash
gitbox account add github-token \
  --provider github \
  --user alice \
  --default-credential-type token
```

Define la variable:

```bash
export GITBOX_TOKEN_GITHUB_TOKEN="ghp_example"
```

Y verifica:

```bash
gitbox credential verify github-token
```

Si prefieres no usar variables de entorno permanentes, ejecuta el setup interactivo y deja que `gitbox` use el mecanismo seguro disponible.

## SSH

SSH usa claves locales y configuración de host para separar identidades.

### Flujo de setup

Configura la cuenta:

```bash
gitbox account add github-ssh \
  --provider github \
  --user alice \
  --default-credential-type ssh \
  --ssh-host github.com \
  --ssh-user git \
  --ssh-key-path ~/.ssh/id_ed25519
```

Comprueba que la clave existe y está cargada:

```bash
ssh-add -l
ssh -T git@github.com
gitbox credential verify github-ssh
```

Para varias cuentas del mismo proveedor, usa host aliases en `~/.ssh/config` y apunta cada cuenta a su alias.

### PAT complementario para SSH

SSH autentica Git, pero no siempre cubre API. Para `discover`, mirrors o consultas avanzadas, puede hacer falta un PAT complementario siguiendo la convención `GITBOX_TOKEN_<ACCOUNT_KEY>`.

## Cambiar credenciales

Actualizo el tipo por defecto de una cuenta:

```bash
gitbox account update github-personal --default-credential-type ssh
```

También puedo cambiar un repo concreto para que use otro tipo:

```bash
gitbox repo update github-personal cli --credential-type token
```

Después de cambiar credenciales, revisa los clones existentes. Si el remoto todavía apunta al protocolo anterior, actualiza el remoto o deja que el flujo de reconfiguración de la TUI lo haga cuando esté disponible.

## Verificar credenciales

```bash
gitbox credential verify github-personal
gitbox doctor
```

`credential verify` prueba una cuenta concreta. `doctor` revisa herramientas externas como `git`, GCM, `ssh`, `tmux` y `wsl`.

## Herramientas ausentes en el host

Si falta una herramienta, `gitbox` debe fallar en el punto de uso con un mensaje claro. Por ejemplo, un flujo SSH no debe exigir GCM, y un flujo GCM no debe exigir `ssh-agent`. Usa `doctor` para ver qué funciones quedan limitadas.

## Resolución de problemas

### Panel de estado actual en la GUI

El panel muestra el tipo de credencial configurado, si el token complementario está disponible y qué chequeos fallan. Lee primero ese panel antes de cambiar configuración; normalmente señala el requisito exacto que falta.

### macOS: permiso de red local

macOS puede bloquear conexiones locales usadas por algunos flujos de autenticación. Cierra `GitboxApp`, restablece el permiso si procede y abre la app de nuevo. Si `tccutil reset` falla, revisa que lo ejecutas con el usuario que lanza la app y que el identificador de la aplicación coincide.

### macOS: "Device not configured" durante GCM

Ese error suele venir del entorno de GCM o del acceso al almacén seguro. Verifica que GCM funciona fuera de `gitbox` con un comando Git normal, luego repite `credential setup`.

### Windows: firewall

Si el navegador abre pero el flujo no vuelve a la app, revisa que el firewall no bloquee el callback local de GCM o del proveedor. Prueba también desde una terminal normal para separar problema de GUI y problema del sistema.

### Linux: diagnóstico de red

Comprueba DNS, proxy, certificados del sistema y conectividad al host:

```bash
git ls-remote https://github.com/owner/repo.git
curl -I https://github.com
```

### HTTP 401 en Forgejo o Gitea

Un 401 casi siempre significa token ausente, token sin scope suficiente, host equivocado o cuenta equivocada. Confirma `--host`, usuario, proveedor y variable `GITBOX_TOKEN_<ACCOUNT_KEY>`.

### Connection refused

Comprueba que el host y puerto existen desde la máquina actual. En redes privadas, VPNs y homelabs, el error puede venir de estar fuera de la red correcta.

### Errores DNS

Verifica que el nombre resuelve:

```bash
nslookup git.example.test
```

Si solo resuelve dentro de VPN o WSL, ejecuta `gitbox` en el entorno que tenga esa resolución.

### TLS o certificados

Instala el certificado raíz correcto en el sistema operativo o en el entorno Git que uses. Evita desactivar verificación TLS salvo para una prueba local controlada; no lo conviertas en configuración permanente.

## Scopes de token por capacidad

Para lectura y descubrimiento, concede lectura de repos. Para crear repos o mirrors, concede escritura o administración según el proveedor. Para organizaciones, confirma que el token tiene acceso a la org y que las políticas de SSO o aprobación de tokens permiten usarlo.

## Cuando el error dice algo inusual

Ejecuta el comando con la cuenta mínima que falla, copia el mensaje exacto y compara con:

```bash
gitbox doctor
gitbox credential verify <account>
git ls-remote <remote-url>
```

Si Git falla fuera de `gitbox`, corrige primero la credencial o red del sistema. Si Git funciona fuera pero `gitbox` falla, revisa proveedor, host, tipo de credencial y token complementario.
