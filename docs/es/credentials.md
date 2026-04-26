# Configuración de credenciales

Cada cuenta de gitbox necesita credenciales para hacer dos cosas (y opcionalmente una tercera):

- **Operaciones** — operaciones git como clone, fetch y pull.
- **Discovery** — llamar a la API REST de tu proveedor para listar tus repositorios, crear nuevos y comprobar su estado de sync.
- **Mirrors** (opcional) — un PAT portable que los servidores remotos usan para hacer push/pull en tu nombre.

Eliges un tipo de credencial por cuenta. Según el tipo, y el proveedor, gitbox puede necesitar uno o dos secretos para cubrir las tres cosas.

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Tipos de credenciales" width="850" />
</p>

## Qué tipo debo usar

| Tipo      | Secretos                                                            | Operaciones | Discovery / API                           | Mirrors         | Mejor para                                             |
| --------- | ------------------------------------------------------------------- | ----------- | ----------------------------------------- | --------------- | ------------------------------------------------------ |
| **GCM**   | 1 (credencial gestionada por GCM, cacheada en el keyring del SO)    | Sí          | Depende del proveedor — ver nota de abajo | Necesita un PAT | Usuarios de escritorio (Windows, macOS, Linux con GUI) |
| **Token** | 1 (PAT en `~/.config/gitbox/credentials/`)                          | Sí          | Sí                                        | Mismo PAT       | Todas las plataformas, CI/CD, Gitea/Forgejo            |
| **SSH**   | 2 (clave SSH en `~/.ssh/` + PAT en `~/.config/gitbox/credentials/`) | Sí          | Necesita el PAT complementario            | Mismo PAT       | Usuarios que prefieren claves SSH                      |

### Cuándo necesito un PAT junto a GCM

La respuesta honesta es "depende del proveedor":

- **GitHub, GitLab:** GCM guarda un **token OAuth** que también sirve como bearer válido para la API. GCM basta para todo: discovery, creación de repos, todo. Solo necesitas un PAT si quieres mirrors push/pull, donde el token debe salir de tu máquina.
- **Gitea, Forgejo, Bitbucket (basic auth):** GCM te pide **usuario y contraseña** y guarda lo que pegues. Esos proveedores rechazan contraseñas en su API REST: allí solo funcionan PATs. Hay dos caminos de recuperación, elige uno:
  1. **Pega un PAT en el prompt de contraseña de GCM** durante el setup. GCM lo cachea y tanto git como la API funcionan con una sola credencial. Es lo más sencillo.
  2. **Mantén tu contraseña en GCM** (funciona para `git push`/`pull`) y guarda un **PAT separado** en el keyring de gitbox para operaciones API. El panel de estado de credenciales de la GUI tiene un botón _Setup API token_ exactamente para este caso.

La verificación de credenciales de gitbox te dirá en cuál de las dos situaciones estás. Si la insignia de la tarjeta es verde, ya está. Si está en "Warning" y el panel Current-status dice _"GCM token found but API check failed"_, tu credencial guardada en GCM es una contraseña que la API rechaza: usa uno de los dos caminos anteriores.

## Almacenamiento de PAT

Todos los Personal Access Tokens (PATs) se guardan en un archivo por cuenta en `~/.config/gitbox/credentials/<accountKey>` (modo de archivo `0600`, legible solo por el propietario).

El formato del archivo depende de quién necesita leerlo:

- Las **cuentas Token** usan **formato git-credential-store** (`https://user:token@host`). Motivo: la CLI de git lee este archivo directamente mediante `credential.helper = store --file <path>`, y ese es el formato que git espera. gitbox también lo lee para llamadas API: el mismo archivo, dos consumidores.
- Las **cuentas SSH y GCM** guardan el **token crudo** (solo el string del PAT, una línea). La CLI de Git nunca toca este archivo: SSH autentica con claves en `~/.ssh/`, y GCM gestiona sus propios tokens OAuth en el keyring del SO. Solo gitbox lo lee, para llamadas API (discovery, creación de repos) y operaciones de mirror.

gitbox lee tokens de ambos formatos de forma transparente: primero intenta parsear como URL y después cae al token crudo. Nunca necesitas preocuparte por qué formato usa un archivo.

La cadena de resolución cuando gitbox necesita un token:

1. Variable de entorno específica de la cuenta (`GITBOX_TOKEN_<ACCOUNT_KEY>`)
2. Variable de entorno genérica `GIT_TOKEN` (fallback de CI)
3. Archivo de credencial (`~/.config/gitbox/credentials/<accountKey>`)

## GCM (Git Credential Manager)

GCM gestiona todo mediante un único login. GCM guarda una credencial y la usa para operaciones git; que también funcione para la API depende de lo que GCM haya cacheado (ver la nota anterior).

### Requisitos previos

- **Windows:** ya instalado con Git for Windows.
- **macOS:** `brew install git-credential-manager`
- **Linux:** consulta la [documentación de instalación de GCM](https://github.com/git-ecosystem/git-credential-manager/blob/release/docs/install.md)

### Cómo funciona

1. gitbox dispara el flujo de login de GCM.
2. Para GitHub y GitLab, GCM abre tu navegador para autenticación OAuth.
3. Para Gitea y Forgejo, GCM pide usuario y contraseña. Si quieres _una sola credencial_ para cubrir git y la API, pega un PAT en el campo de contraseña: GCM lo cacheará como contraseña.
4. GCM guarda automáticamente la credencial en su propio almacenamiento.
5. Todas las operaciones de discovery y git usan esa credencial guardada.

**Almacenamiento:** GCM gestiona su propio almacenamiento de credenciales (keyring del SO). gitbox extrae el secreto cacheado mediante `git credential fill` para llamadas API.

### Requisitos de gitconfig global

GCM enruta por host mediante la clave global `credential.helper` en `~/.gitconfig`. Sin un `credential.helper = manager` de nivel superior y `credential.credentialStore = <keychain|wincredman|secretservice>`, `git credential fill` cae a un prompt TTY, y en un proceso GUI eso aparece como el críptico `fatal: could not read Password ... Device not configured` (errno ENXIO en `/dev/tty`).

gitbox lo detecta al arrancar cuando al menos una cuenta usa GCM. Cuando el `~/.gitconfig` global falta o está mal, aparece un banner naranja en la GUI (y una sección en la pantalla "Global Gitconfig" de la TUI) con un botón **Configure** que:

1. Escribe `credential.helper = manager` + `credential.credentialStore = <os default>` en `~/.gitconfig`.
2. Rellena los mismos valores por defecto del SO en `gitbox.json` para que la comprobación siga verde incluso si `~/.gitconfig` se edita más tarde.

Los valores por defecto del SO vienen de `pkg/credential.DefaultCredentialHelper()` y `pkg/credential.DefaultCredentialStore()`. Los overrides por host en otras partes de `~/.gitconfig` (por ejemplo `[credential "https://github.com"]` fijando `gh auth git-credential`) siguen teniendo precedencia sobre el helper global, así que corregir la entrada global siempre es seguro.

### Detección de navegador

Cuando configuras credenciales GCM, gitbox necesita abrir un navegador para autenticación OAuth (GitHub, GitLab). Que un navegador pueda abrirse depende de tu entorno:

- **Windows:** siempre funciona: el navegador del sistema se abre directamente.
- **macOS:** siempre funciona, incluso vía SSH: el comando `open` de macOS reenvía a la sesión de escritorio.
- **Linux desktop:** funciona cuando hay un servidor de pantalla disponible (X11 o Wayland).
- **Linux SSH / headless:** no hay navegador disponible. La TUI muestra "GCM browser authentication requires a desktop session" y sugiere ejecutar el setup de credenciales desde una terminal de escritorio. GCM seguirá preguntando interactivamente en el siguiente `git clone` o `git fetch` si continúas sin autenticación de navegador.

Esta detección la maneja `credential.CanOpenBrowser()` en `pkg/credential/credential.go`. Comprueba las variables de entorno `SSH_CLIENT`, `SSH_TTY`, `DISPLAY` y `WAYLAND_DISPLAY`.

### GCM en la TUI

La pantalla de credenciales de la TUI soporta autenticación GCM interactiva con navegador en sesiones de escritorio:

1. Navega a una cuenta → credential setup → GCM seleccionado.
2. En escritorio: pulsa Enter → el navegador se abre para OAuth → vuelve a la TUI cuando termines.
3. gitbox verifica que la credencial se guardó y prueba el acceso API.
4. Si la credencial guardada por GCM no tiene scope API (común con Forgejo/Gitea cuando se cacheó una contraseña), gitbox pide un PAT separado.

En sesiones SSH o headless, la TUI omite la autenticación de navegador y muestra guía: ejecutar desde una terminal de escritorio o dejar que GCM lo gestione en la siguiente operación git.

### Mirrors con GCM

Los tokens OAuth de GCM son locales de la máquina y no pueden usarse por servidores remotos para mirroring. Si necesitas mirrors, guarda un PAT separado:

```bash
gitbox account credential setup github-personal --token
```

Esto guarda el PAT en `~/.config/gitbox/credentials/` junto a la credencial GCM. El PAT se usa para operaciones de mirror; GCM sigue gestionando las operaciones git normales.

## Token (Personal Access Token)

Un PAT es un string parecido a una contraseña que generas en el sitio web de tu proveedor. Un token gestiona tanto discovery como operaciones git.

### Cómo crear un PAT

Cuando configuras una credencial Token (en la GUI o CLI), gitbox te muestra la URL exacta que debes visitar y los permisos que debes seleccionar. Los pasos son:

1. Haz clic en el enlace que te da gitbox (abre la página de tokens de tu proveedor).
2. Nombra el token con algo como `gitbox-<your-account>`.
3. Selecciona los permisos recomendados por gitbox.
4. Copia el token generado.
5. Pégalo en gitbox.

Esto es lo que necesita cada proveedor:

| Proveedor           | Permisos requeridos                                    |
| ------------------- | ------------------------------------------------------ |
| **GitHub**          | `repo` (full), `read:user`                             |
| **GitLab**          | `api`                                                  |
| **Gitea / Forgejo** | Repository: Read+Write, User: Read, Organization: Read |
| **Bitbucket**       | Repositories: Read+Write                               |

**Almacenamiento:** 1 secreto → `~/.config/gitbox/credentials/<account>` (formato git-credential-store, usado por gitbox y por la CLI de git).

## SSH

SSH usa un par de claves criptográficas para operaciones git. gitbox genera el par de claves y configura todo por ti.

Sin embargo, las claves SSH **no pueden** llamar a la API de tu proveedor, así que discovery y creación de repos necesitan un PAT separado. Esto convierte SSH en una configuración de dos secretos.

### Flujo de setup

1. gitbox crea un par de claves ed25519 en `~/.ssh/`.
2. Escribe la entrada necesaria en `~/.ssh/config`.
3. Te muestra la clave pública y un enlace directo a la página de claves SSH del proveedor.
4. Pegas allí la clave pública y haces clic en guardar.
5. gitbox verifica que la conexión SSH funciona.
6. Guardas un PAT para acceso API (discovery, creación de repos, mirrors).

### PAT complementario de SSH

El PAT guardado para cuentas SSH se usa para:

- **Discovery** — listar repos desde la API del proveedor.
- **Creación de repos** — crear repos nuevos mediante la API del proveedor.
- **Mirrors** — servidores remotos haciendo push/pull en tu nombre.

Necesita permisos más amplios que un token de solo lectura:

| Proveedor           | Permisos requeridos                                    |
| ------------------- | ------------------------------------------------------ |
| **GitHub**          | `repo` (read/write), `read:user`, `read:org`           |
| **GitLab**          | `api` (full API access)                                |
| **Gitea / Forgejo** | Repository: Read+Write, User: Read, Organization: Read |
| **Bitbucket**       | Repositories: Read+Write, Account: Read                |

**Almacenamiento:** 2 secretos:

- **Operaciones** → par de claves SSH en `~/.ssh/` (clave privada + clave pública + entrada de config).
- **Acceso API** → PAT en `~/.config/gitbox/credentials/<account>` (token crudo).

Sin el PAT, la cuenta funciona para operaciones git, pero no puede descubrir repos, crear repos ni configurar mirrors.

## Cambiar credenciales

Puedes cambiar el tipo de credencial de una cuenta en cualquier momento. Cuando cambias el tipo:

- gitbox elimina la credencial antigua y sus artefactos (tokens, claves, entradas de config).
- Configura el nuevo tipo de credencial.
- Todos los clones existentes se reconfiguran automáticamente para usar la nueva credencial.

No hace falta volver a clonar nada.

## Verificar credenciales

Puedes verificar que las credenciales funcionan en cualquier momento:

- **GUI:** la insignia de credencial en cada tarjeta de cuenta aparece verde (funciona), naranja (limitada) o roja (rota). Haz clic en la insignia para abrir el modal Change Credential: el panel Current-status de arriba muestra el estado real con el mensaje de error subyacente (si lo hay).
- **TUI:** mismas insignias de color en las tarjetas del dashboard; el detalle de cuenta muestra estado con un mensaje.
- **CLI:** `gitbox account credential verify <account-key>` imprime el estado de la credencial primaria y el estado de acceso API, incluido el error crudo cuando alguna falla.

## Herramientas ausentes en el host

Los tipos de credencial dependen de binarios externos que podrían no estar instalados en la máquina actual: GCM necesita `git-credential-manager`; SSH necesita `ssh` / `ssh-keygen` / `ssh-add`. Gitbox los detecta antes de iniciar un setup:

- **GUI** — abrir el modal add-account o change-credential ejecuta una comprobación previa. Si falta una herramienta requerida, ves un banner amarillo que la nombra y muestra el comando exacto de instalación para tu SO. El setup no se ejecuta automáticamente hasta que lo arregles (puedes avanzar manualmente si sabes lo que haces).
- **TUI** — `Credential → change type → <new type>` se niega a continuar cuando falta una herramienta y muestra la pista de instalación como mensaje de error.
- **CLI** — ejecuta `gitbox doctor` para un inventario completo en cualquier momento (consulta [reference.md](reference.md#comprobación-del-sistema-doctor)). El código de salida es `1` cuando falta alguna herramienta requerida por tu config actual, lo que permite usarlo en scripts.

Arreglo típico en cada SO:

- macOS: `brew install --cask git-credential-manager` (ssh/ssh-keygen vienen preinstalados).
- Linux: instala el paquete GCM de tu distro (consulta la [documentación de instalación de GCM](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)); `sudo apt install openssh-client` para SSH.
- Windows: GCM viene incluido con Git for Windows; el cliente OpenSSH es una característica opcional integrada.

## Troubleshooting

### Leer el panel Current-status de la GUI

Haz clic en la insignia de credencial de cualquier tarjeta de cuenta. El panel Current-status dentro del modal Change Credential muestra:

- **Primary (GCM/SSH/TOKEN):** el estado real de la credencial primaria (OK / Warning / Offline / Error / Not configured) y, debajo de la fila, el error subyacente tal como lo reporta la capa network/HTTP de gitbox. Ese texto de error es el primer sitio donde mirar cuando algo va mal: nombra la URL que falla, el código HTTP y lo que reportó el kernel / stack TLS.
- **API token (PAT):** estado del PAT complementario (solo relevante para GCM y SSH). Cuando la credencial primaria ya cubre la API, el mensaje del PAT dice _"not needed — GCM covers the API"_. Cuando la API rechazó la primaria, el mensaje del PAT dice _"needed for discovery and repo creation"_ y aparece un botón **Setup API token** junto a él.
- Un botón **Re-check** vuelve a ejecutar la verificación sin salir del modal.
- Un banner informativo azul aparece bajo el panel cuando gitbox detecta que el error parece una denegación de permiso de red a nivel de SO (ver secciones macOS / Windows / Linux más abajo).

La misma información está disponible desde la shell con `gitbox account credential verify <account>`: útil cuando quieres copiar y pegar un error en un bug report.

### macOS: permiso de red local

**Síntoma.** La GUI muestra _Offline_ para una cuenta cuyo servidor está en tu LAN (`192.168.x.x`, `10.x.x.x` o un hostname `.local`). La línea de detalle contiene `dial tcp <ip>:<port>: connect: no route to host`. La CLI al mismo tiempo, ejecutada desde Terminal, llega al mismo servidor sin problema.

**Causa raíz.** macOS Sonoma y posteriores protegen las conexiones salientes a IPs de red local detrás del permiso de privacidad **Local Network** de TCC. Cada app GUI se registra por identidad de firma de código + ruta del bundle. Terminal recibió acceso la primera vez que lo usaste. Tu bundle de gitbox quizá todavía no fue aprobado, fue denegado silenciosamente (común en binarios ad-hoc), o se movió/renombró y TCC ya no lo reconoce.

**Arreglo — mover la app a `/Applications/` y volver a pedir permiso.**

```bash
# Cierra primero GitboxApp en ejecución (⌘Q), luego como el usuario que la ejecutará:
xattr -dr com.apple.quarantine /Applications/GitboxApp.app
codesign --force --deep --sign - /Applications/GitboxApp.app   # firma ad-hoc
tccutil reset LocalNetwork                                     # olvidar decisiones existentes
open /Applications/GitboxApp.app
```

El `codesign --sign -` ad-hoc no requiere una cuenta Apple Developer. Lo que aporta es un **requisito designado de código estable**: un hash que TCC puede seguir entre lanzamientos, para que el permiso permanezca después de concederlo.

En el primer acceso LAN, el SO debería mostrar _"GitboxApp would like to find and connect to devices on your local network"_. Haz clic en **Allow**. Después, `GitboxApp` aparece en _System Settings → Privacy & Security → Local Network_ con un toggle verde.

#### Cuando `tccutil reset` falla

La llamada `tccutil` es best-effort, y en versiones recientes de macOS a menudo imprime `tccutil: Failed to reset LocalNetwork`, a veces incluso con `sudo` y aunque Terminal tenga Full Disk Access. Esto ocurre por debajo:

- `tccutil` modifica registros TCC por usuario. `sudo tccutil` edita la base de datos TCC de **root**, que no es la que lee tu sesión GUI, así que el error en parte indica que lo ejecutaste con la identidad equivocada. Ejecútalo como tu usuario normal, no con `sudo`.
- La base de datos TCC por usuario vive bajo `~/Library/Application Support/com.apple.TCC/TCC.db`. SIP bloquea escrituras directas y enruta todo mediante `tccutil`; si Terminal (o tu emulador de shell) no tiene **Full Disk Access** concedido en _System Settings → Privacy & Security → Full Disk Access_, la llamada falla.
- Aunque `tccutil` devuelva `Failed`, el SO puede volver a pedir permiso en la siguiente conexión LAN, porque la cache interna de decisiones es distinta de la DB en disco en releases nuevas. Ya viste esto: el reset reportó fallo, pero GitboxApp aun así mostró el diálogo "Allow Local Network" al alcanzar el servidor.

Pasar un bundle identifier (`tccutil reset LocalNetwork com.wails.GitboxApp`) acota el reset a una app, pero devuelve el mismo error cuando el reset amplio ya no puede escribir. El bundle identifier de un build gitbox estándar es `com.wails.GitboxApp`: `wails build` lo hereda de su plantilla `Info.plist` predeterminada porque el repo no lo sobrescribe. Después de un `codesign --sign -` ad-hoc, el identificador se conserva (puedes confirmarlo con `defaults read /Applications/GitboxApp.app/Contents/Info.plist CFBundleIdentifier`).

**Si `tccutil` falla y tampoco aparece el prompt**: omítelo. Abre directamente _System Settings → Privacy & Security → Local Network_ y activa el toggle de GitboxApp si aparece en la lista. Si no aparece, la app realmente no intentó todavía una conexión LAN (o TCC la registró con otra identidad de un ship anterior); relanza, dispara una verificación de credenciales en una cuenta LAN y el SO la añadirá.

### macOS: "Device not configured" durante setup de GCM

**Síntoma.** Configurar una credencial GCM en la GUI devuelve `fatal: could not read Password ... Device not configured`.

**Causa raíz.** `git credential fill` intentó abrir `/dev/tty` para un prompt interactivo de contraseña porque `credential.helper` y `credential.credentialStore` faltaban o estaban mal en `~/.gitconfig`. Los procesos GUI no tienen tty de control; `open()` devuelve `ENXIO` y git muestra el mensaje críptico.

**Arreglo.** Haz clic en el banner **Configure global gitconfig** de la GUI (o el elemento equivalente en la pantalla Global Gitconfig de la TUI). Gitbox escribe las entradas correctas y verifica. Repite el setup de credenciales.

### Windows: firewall

**Síntoma.** La GUI muestra _Offline_ para una cuenta en tu LAN. La línea de detalle contiene `A connection attempt failed` o `No route to host` (Windows traduce `WSAEHOSTUNREACH`).

**Causa raíz.** Windows Defender Firewall, o un producto endpoint de terceros, bloquea conexiones salientes desde `GitboxApp.exe` hacia la subred LAN. El bloqueo es por ejecutable, así que que Chrome / Edge / git.exe estén permitidos no dice nada sobre la app Wails.

**Arreglo.**

1. Abre _Windows Security → Firewall & network protection → Allow an app through firewall_.
2. Haz clic en _Change settings_ → _Allow another app_ → navega hasta el `GitboxApp.exe` instalado.
3. Marca las columnas **Private** y **Public** para la red que uses realmente.
4. Si hay software endpoint corporativo, normalmente necesita una regla del lado admin: escala a IT con la ruta del ejecutable y el servidor destino.

Vuelve a ejecutar la verificación en la GUI cuando la regla esté activa.

### Linux: troubleshooting de red

**Síntoma.** La GUI muestra _Offline_ para una cuenta en tu LAN o una IP privada. La línea de detalle contiene `connect: no route to host`, `connect: network is unreachable` o `connect: connection refused`.

**Causa raíz.** Los escritorios Linux normalmente no tienen un mecanismo de permiso de red a nivel de SO. O la red está caída (VPN apagada, interfaz equivocada, subred equivocada) o un firewall local (`ufw`, `firewalld`, nftables) bloquea la conexión saliente.

**Arreglo.**

1. Prueba la misma petición desde una shell: `curl -sSI https://<host>/`; si eso también falla, el problema es la red, no la app.
2. Confirma que la VPN está activa (si hace falta) y que existen las rutas correctas: `ip route get <server-ip>`.
3. Inspecciona tu firewall: `sudo ufw status verbose`, `sudo firewall-cmd --list-all` o `sudo nft list ruleset`. Permite TCP saliente al servidor destino.
4. Si llegas al servidor desde shell pero no desde la GUI, el binario Wails puede haberse iniciado dentro de un namespace restringido; es raro, normalmente solo en sandboxes Flatpak/Snap. Lanza desde una Terminal normal para confirmarlo.

### HTTP 401 desde Forgejo o Gitea

**Síntoma.** El panel Current-status muestra _Primary (GCM): Warning_ con `gitea: authentication failed (HTTP 401) — token is invalid or expired`.

**Causa raíz.** Forgejo y Gitea rechazan autenticación usuario/contraseña en la API REST. Tu credencial guardada en GCM probablemente es una contraseña real: funciona para `git push` / `pull`, pero no para `/api/v1/*`.

**Arreglo.** Uno de:

- Elimina la entrada GCM actual y repite el setup de credenciales pegando un **PAT** en el prompt de contraseña de GCM. El PAT se cachea igual, pero también funciona para la API.
- Deja GCM como está y haz clic en **Setup API token** dentro del panel Current-status para guardar un PAT complementario en el keyring de gitbox. La API usará el PAT, las operaciones git seguirán usando GCM.

### "Connection refused" (cualquier SO)

**Síntoma.** La línea de detalle contiene `connect: connection refused`.

**Causa raíz.** El servidor es alcanzable pero no hay nada escuchando en el puerto destino: el servicio está caído o estás usando host/puerto equivocado.

**Arreglo.** Confirma la URL del servidor en la config de cuenta (`gitbox account list --json`), luego comprueba el estado del servicio en el servidor. Esto nunca es un problema del lado de la app.

### Errores DNS

**Síntoma.** La línea de detalle contiene `no such host`, `server misbehaving` o `i/o timeout` durante la resolución DNS.

**Causa raíz.** El hostname de la URL de cuenta no puede resolverse desde esta máquina. Posibles razones: typo en la URL de cuenta, VPN / DNS split-horizon requerido pero no activo, resolver DNS privado no configurado.

**Arreglo.** Prueba `dig +short <host>` o `nslookup <host>` desde una shell. Corrige tu DNS antes de volver a verificar en gitbox.

### Errores TLS / certificado

**Síntoma.** La línea de detalle contiene `x509:`, `certificate signed by unknown authority` o `tls: failed to verify certificate`.

**Causa raíz.** El servidor presenta un certificado que el cliente HTTP de Go no puede validar: normalmente un certificado self-signed, un cert firmado por una CA interna que no está en el trust store del sistema, o un hostname que no coincide.

**Arreglo.** Añade el certificado de la CA interna al trust store de tu SO (macOS Keychain, almacén de certificados Windows o Linux `/usr/local/share/ca-certificates/` + `update-ca-certificates`). Gitbox usa el trust store del sistema mediante `net/http`; no tiene bypass por app y deliberadamente no ofrecerá uno.

## Scopes de token por capacidad

Diferentes operaciones de gitbox necesitan diferentes scopes de PAT. La base (listar + clonar + fetch + pull) es el mínimo que necesita cada cuenta; create, delete y mirror añaden uno o más scopes encima. Da a cada cuenta solo los scopes que necesita: añade los destructivos (delete) solo cuando planees usarlos.

| Proveedor       | Discovery + clone + fetch              | Crear repo (GUI "Create repo" / move dest) | Eliminar repo (move con "delete source")             | Mirror (push/pull entre cuentas)  |
| --------------- | -------------------------------------- | ------------------------------------------ | ---------------------------------------------------- | --------------------------------- |
| GitHub          | `repo`, `read:org`                     | `repo`                                     | `delete_repo` (sensible — añadir solo si hace falta) | `repo` más los scopes del destino |
| GitLab          | `api`                                  | `api`                                      | `api`                                                | `api`                             |
| Gitea / Forgejo | `read:repository`, `read:organization` | `write:repository`                         | `write:repository` (o admin en destino)              | `write:repository` en destino     |
| Bitbucket Cloud | `repository`                           | `repository:admin`                         | `repository:delete`                                  | `repository:admin` en destino     |

Cuando falta un scope, gitbox muestra un aviso que nombra el scope exacto requerido y la URL del proveedor para regenerar el PAT; por ejemplo: _"Source repo delete refused: your github PAT is missing the `delete_repo` scope. Regenerate it at <https://github.com/settings/tokens> (keep existing scopes, add `delete_repo`), then re-run `gitbox account credential setup <account>`."_

El wizard de setup de cuenta de la TUI + GUI muestra la misma tabla por capacidad cuando guardas un PAT, para que decidas qué scopes incluir desde el principio.

### Cuando el error dice algo inusual

Gitbox muestra el error de nivel Go literalmente: no lo traduce ni resume. Copia el string exacto en un issue en [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues) junto con:

- El SO y versión (por ejemplo "macOS 14.5 Sonoma").
- El tipo de credencial en uso (GCM / SSH / Token) y el proveedor (GitHub / Forgejo / ...).
- La salida de `gitbox account credential verify <account-key>`.
- Si la misma petición funciona desde `curl` en una shell de la misma máquina.

Normalmente eso basta para distinguir problemas de red de problemas de la app en una sola ida y vuelta.
