# Configuración de credenciales

Cada cuenta de Gitbox necesita credenciales para hacer dos cosas (y opcionalmente una tercera):

- **Operaciones** — operaciones git como clone, fetch y pull.
- **Discovery** — llamar a la API REST de tu proveedor para listar tus repositorios, crear otros nuevos y comprobar su estado de sincronización.
- **Mirrors** (opcional) — un PAT portable que los servidores remotos usan para hacer push/pull en tu nombre.

Eliges un tipo de credencial por cuenta. Según el tipo, y el proveedor, Gitbox puede necesitar uno o dos secretos para cubrir las tres cosas.

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Tipos de credenciales" width="850" />
</p>

## ¿Qué tipo debo usar?

| Tipo      | Secretos                                                            | Operaciones | Discovery / API                           | Mirrors         | Ideal para                                             |
| --------- | ------------------------------------------------------------------- | ----------- | ----------------------------------------- | --------------- | ------------------------------------------------------ |
| **GCM**   | 1 (credencial que GCM guarda en el almacén de credenciales del SO)  | Sí          | Depende del proveedor — ver nota de abajo | Necesita un PAT | Usuarios de escritorio (Windows, macOS, Linux con GUI) |
| **Token** | 1 (PAT en `~/.config/gitbox/credentials/`)                          | Sí          | Sí                                        | El mismo PAT    | Todas las plataformas, CI/CD, Gitea/Forgejo            |
| **SSH**   | 2 (clave SSH en `~/.ssh/` + PAT en `~/.config/gitbox/credentials/`) | Sí          | Necesita el PAT complementario            | El mismo PAT    | Usuarios que prefieren claves SSH                      |

### ¿Cuándo necesito un PAT junto a GCM?

Depende del proveedor:

- **GitHub, GitLab:** GCM guarda un **token OAuth** que también sirve como bearer válido para la API. GCM por sí solo cubre el discovery y la creación de repos. Solo necesitas un PAT para los mirrors push/pull, donde el token tiene que salir de tu máquina.
- **Gitea, Forgejo, Bitbucket (basic auth):** GCM te pide **usuario y contraseña** y guarda lo que pegues. Estos proveedores pueden rechazar contraseñas en su API REST y aceptar solo PATs. Elige uno de estos dos caminos de recuperación:
  1. **Pega un PAT cuando GCM te pida la contraseña** durante la configuración. GCM lo guarda, y tanto git como la API funcionan con una sola credencial. Es el camino más sencillo.
  2. **Mantén tu contraseña en GCM** (funciona para `git push`/`pull`) y guarda un **PAT separado** en el archivo de credenciales de Gitbox para las operaciones de API. El botón **Configurar token de API** del panel Estado actual de la cuenta hace esto.

La comprobación de credenciales de Gitbox te dice en qué situación estás. Si la cuenta muestra **La credencial funciona**, ya está. Si muestra **Funciona, con un aviso** y la fila del token de API del panel Estado actual dice _API rejected the GCM credential (HTTP 401)_, la API rechaza tu credencial de GCM: usa uno de los dos caminos de recuperación anteriores.

## Almacenamiento de PAT

Gitbox guarda cada Personal Access Token (PAT) en un archivo por cuenta, `~/.config/gitbox/credentials/<accountKey>` (modo de archivo `0600`, legible solo por el propietario).

El formato del archivo depende de quién lo lee:

- Las **cuentas Token** usan el **formato git-credential-store** (`https://user:token@host`), porque git lee este archivo directamente mediante `credential.helper = store --file <path>`. Gitbox lee el mismo archivo para las llamadas a la API.
- Las **cuentas SSH y GCM** guardan el **token en bruto** (la cadena del PAT, en una línea). Git nunca lee este archivo: SSH se autentica con las claves de `~/.ssh/`, y GCM guarda su propia credencial en el almacén de credenciales del SO. Solo Gitbox lo lee, para las llamadas a la API (discovery, creación de repos) y las operaciones de mirror.

Gitbox lee ambos formatos: primero interpreta el archivo como una URL y, si no lo es, como un token en bruto.

Cuando Gitbox necesita un token, lo busca en este orden:

1. Variable de entorno específica de la cuenta (`GITBOX_TOKEN_<ACCOUNT_KEY>`, la clave de la cuenta en mayúsculas y con los guiones convertidos en guiones bajos)
2. Variable de entorno genérica `GIT_TOKEN` (alternativa para CI)
3. Archivo de credencial (`~/.config/gitbox/credentials/<accountKey>`)

## GCM (Git Credential Manager)

GCM lo gestiona todo con un único inicio de sesión. GCM guarda una credencial y la usa para las operaciones git; que también sirva para la API depende del proveedor (ver [¿Cuándo necesito un PAT junto a GCM?](#cuándo-necesito-un-pat-junto-a-gcm)).

### Requisitos previos

- **Windows:** ya viene instalado con Git for Windows.
- **macOS:** `brew install --cask git-credential-manager`
- **Linux:** consulta la [documentación de instalación de GCM](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)

### Cómo funciona

1. Gitbox inicia el inicio de sesión de GCM.
2. Para GitHub y GitLab, GCM abre tu navegador para la autenticación OAuth.
3. Para Gitea y Forgejo, GCM pide usuario y contraseña. Para cubrir git y la API con una sola credencial, pega un PAT en el campo de contraseña.
4. GCM guarda la credencial en el almacén de credenciales del SO (Administrador de credenciales de Windows, Llavero de macOS o Secret Service en Linux).
5. Las operaciones git usan esa credencial, y Gitbox la lee mediante `git credential fill` para las llamadas a la API.

GitboxApp siempre pide a GCM su propia ventana de inicio de sesión, diga lo que diga tu entorno. Si el inicio de sesión no termina en 5 minutos, Gitbox deja de esperar y nombra las variables `GCM_*` que encontró (ver [Windows: el inicio de sesión de GCM agota el tiempo de espera](#windows-el-inicio-de-sesión-de-gcm-agota-el-tiempo-de-espera)).

### Requisitos de gitconfig global

GCM necesita dos entradas globales en `~/.gitconfig`: un `credential.helper` que resuelva a Git Credential Manager y `credential.credentialStore = <keychain|wincredman|secretservice>`. Sin ellas, `git credential fill` recurre a un prompt de terminal, que falla en una app GUI con `fatal: could not read Password ... Device not configured`.

Un helper resuelve a GCM cuando es `manager`, el heredado `manager-core` o una ruta absoluta al binario `git-credential-manager` (la forma que escribe `git-credential-manager configure`).

Cuando al menos una cuenta usa GCM y estas entradas faltan o son incorrectas, Gitbox muestra un banner naranja con un botón **Configurar**. Escribe `credential.helper = manager` y el `credential.credentialStore` por defecto del SO en `~/.gitconfig`, y registra los mismos valores por defecto en `gitbox.json`. Una cadena de helpers que ya resuelve a GCM queda intacta. El inicio de sesión con el navegador durante la configuración de una cuenta aplica antes el mismo arreglo. Los overrides por host en `~/.gitconfig` (por ejemplo, `[credential "https://github.com"]` fijando `gh auth git-credential`) siguen teniendo precedencia, así que el arreglo siempre es seguro.

### Detección de navegador

GCM abre un navegador para el inicio de sesión OAuth (GitHub, GitLab). Que se pueda abrir un navegador depende de tu entorno:

- **Windows:** siempre funciona — el navegador del sistema se abre directamente.
- **macOS:** siempre funciona, incluso por SSH — el comando `open` de macOS lo reenvía a la sesión de escritorio.
- **Linux de escritorio:** funciona cuando hay un servidor gráfico disponible (X11 o Wayland).
- **Linux por SSH / headless:** no hay navegador disponible. GitboxApp necesita una sesión de escritorio, así que esto solo importa en un host al que llegas por SSH. GCM sigue preguntando en el siguiente `git clone` o `git fetch` desde una terminal en ese host. Cuando el inicio de sesión con el navegador no sea práctico, cambia la cuenta al tipo de credencial Token.

### Mirrors con GCM

Los tokens OAuth de GCM son locales de la máquina, así que los servidores remotos no pueden usarlos para hacer mirroring. Si necesitas mirrors, guarda un PAT separado. Cuando un mirror lo necesita, su fila en la pestaña Mirrors muestra un botón **Arreglar credenciales** que abre la configuración del token de API de esa cuenta. Cuando la credencial de GCM no cubre la API, el panel Estado actual de los ajustes de credencial de la cuenta también ofrece **Configurar token de API**.

Gitbox guarda este PAT en `~/.config/gitbox/credentials/`. El PAT se encarga de las operaciones de mirror y de API; GCM sigue encargándose de las operaciones git.

## Token (Personal Access Token)

Un PAT es una cadena parecida a una contraseña que generas en el sitio web de tu proveedor. Un solo token se encarga tanto del discovery como de las operaciones git.

### Cómo crear un PAT

Cuando configuras una credencial Token, Gitbox te muestra el enlace a la página de tokens de tu proveedor y los permisos que debes seleccionar:

1. Haz clic en el enlace que te da Gitbox (abre la página de tokens de tu proveedor).
2. Ponle al token un nombre como `gitbox-<your-account>`.
3. Selecciona los scopes para lo que piensas hacer, según [Scopes de token por capacidad](#scopes-de-token-por-capacidad).
4. Copia el token generado.
5. Pégalo en Gitbox.

**Almacenamiento:** 1 secreto → `~/.config/gitbox/credentials/<account>` (formato git-credential-store, que leen tanto Gitbox como git).

## SSH

SSH usa un par de claves para las operaciones git. Gitbox genera el par de claves y lo configura todo por ti.

Las claves SSH **no pueden** llamar a la API de tu proveedor, así que el discovery y la creación de repos necesitan un PAT separado. Esto convierte SSH en una configuración de dos secretos.

### Flujo de configuración

1. Gitbox crea un par de claves ed25519 en `~/.ssh/`.
2. Escribe la entrada de `~/.ssh/config`, con el alias de host `gitbox-<account-key>`.
3. Te muestra la clave pública y un enlace directo a la página de claves SSH de tu proveedor.
4. Pegas allí la clave pública y haces clic en guardar.
5. Gitbox verifica la conexión SSH.
6. Guardas un PAT para el acceso a la API (discovery, creación de repos, mirrors).

### PAT complementario de SSH

El PAT complementario cubre:

- **Discovery** — listar repos desde la API del proveedor.
- **Creación de repos** — crear repos nuevos mediante la API del proveedor.
- **Mirrors** — servidores remotos que hacen push/pull en tu nombre.

Necesita los mismos scopes que el PAT de una cuenta Token: ver [Scopes de token por capacidad](#scopes-de-token-por-capacidad).

**Almacenamiento:** 2 secretos:

- **Operaciones** → par de claves SSH en `~/.ssh/` (clave privada + clave pública + entrada de config).
- **Acceso a la API** → PAT en `~/.config/gitbox/credentials/<account>` (token en bruto).

Sin el PAT, la cuenta funciona para las operaciones git, pero no puede descubrir repos, crear repos ni configurar mirrors.

## Scopes de token por capacidad

Esta es la referencia de cada PAT que usa Gitbox: cuentas Token, PATs complementarios de SSH y el PAT que una cuenta GCM guarda para mirrors o para la API. La primera columna es el mínimo que necesita cada cuenta. Cada una de las demás columnas añade lo que necesita una capacidad encima de ese mínimo. Da a cada cuenta solo los scopes que necesita, y añade los destructivos (delete) solo cuando pienses usarlos.

| Proveedor       | Discovery + clone + fetch      | Selectores de propietario (orgs) | Crear repo (**Crear repo**, destino de un move) | Eliminar repo (move con "delete source")              | Mirror                                        |
| --------------- | ------------------------------ | -------------------------------- | ----------------------------------------------- | ----------------------------------------------------- | --------------------------------------------- |
| GitHub          | `repo`                         | `read:org`                       | `repo`                                          | `delete_repo` (sensible — añádelo solo si hace falta) | `repo`                                        |
| GitLab          | `api`                          | `api`                            | `api`                                           | `api`                                                 | `api`                                         |
| Gitea / Forgejo | `read:repository`, `read:user` | `read:organization`              | `write:repository`                              | `write:repository`                                    | `write:repository`                            |
| Bitbucket Cloud | Repositories: Read, Write      | Account: Read                    | Repositories: Admin                             | Repositories: Delete (sensible)                       | No disponible a través de la API de Bitbucket |

En la página de tokens de Gitea o Forgejo, estos scopes aparecen como niveles de permiso: **repository** Read y Write, **user** Read y **organization** Read cubren todas las columnas. En Bitbucket, Gitbox abre la página de app passwords.

Cuando falta un scope, Gitbox muestra un aviso que nombra el scope y la página donde regenerar el PAT, por ejemplo: _"Source repo delete refused: your github PAT is missing the `delete_repo` scope. Regenerate it at <https://github.com/settings/tokens> (keep existing scopes, add `delete_repo`), then store the new token in the account's credential settings."_

## Cambiar credenciales

Puedes cambiar el tipo de credencial de una cuenta en cualquier momento. Cuando cambias el tipo:

- Gitbox elimina la credencial antigua y sus artefactos (tokens, claves, entradas de config).
- Configura el nuevo tipo de credencial.
- Reconfigura todos los clones existentes para que usen la nueva credencial.

No hace falta volver a clonar nada.

Gitbox también repara la identidad, el remote y los ajustes de credencial de un clone antes de cada fetch. Si un include de git sigue sobrescribiéndolos, avisa y omite el fetch de ese clone.

## Verificar credenciales

Puedes verificar las credenciales en cualquier momento:

- La entrada de cada cuenta en la barra lateral y su tarjeta muestran su salud en palabras junto a una marca: verde (funciona), naranja (limitada) o roja (rota o sin conexión). Las cuentas rotas encabezan también la vista Requiere atención, con su botón para arreglarlas.
- Un token de GitHub o GitLab que caduca en 14 días o menos pone en naranja la salud en palabras: "El token caduca en N días". En GitLab esto requiere GitLab 15.5 o posterior y un personal access token. Una cuenta Token cuyo token rechaza el servidor (HTTP 401, por ejemplo al caducar) aparece como fallida.
- Elige **Cambiar credencial…** en el menú ⋯ de la cuenta para abrir el modal Cambiar credencial. Su panel Estado actual muestra el estado real de la credencial principal y del token de API, con el mensaje de error subyacente (si lo hay).
- Haz clic en **Volver a comprobar** en ese panel para ejecutar de nuevo la verificación.

## Herramientas ausentes en el host

Los tipos de credencial dependen de binarios externos que pueden faltar en la máquina actual: GCM necesita `git-credential-manager`; SSH necesita `ssh`, `ssh-keygen` y `ssh-add`. Gitbox los comprueba antes de iniciar una configuración:

- **Flujos de configuración** — abrir el modal de añadir cuenta o de cambiar credencial ejecuta una comprobación previa. Si falta una herramienta necesaria, un banner amarillo la nombra con el comando de instalación para tu SO. La configuración no se ejecuta automáticamente hasta que la instales y hagas clic en **Volver a comprobar**; aun así, puedes continuar manualmente.
- **Comprobación del sistema** — abre **Ajustes → Comprobación del sistema → Ejecutar** para obtener un inventario completo en cualquier momento: cada herramienta que Gitbox puede llamar (`git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add` y `wsl` en Windows), dónde está instalada, su versión y si tu config actual la necesita.

Arreglo típico en cada SO:

- macOS: `brew install --cask git-credential-manager` (`ssh` y `ssh-keygen` vienen preinstalados).
- Linux: instala el paquete de GCM de tu distro (consulta la [documentación de instalación de GCM](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)); `sudo apt install openssh-client` para SSH.
- Windows: GCM viene con Git for Windows y no necesita estar en el `PATH`: Gitbox pregunta primero a `git credential-manager --version` y luego comprueba las carpetas de instalación de Git for Windows y de GCM independiente. El cliente OpenSSH es una característica opcional integrada.

## Resolución de problemas

### Leer el panel Estado actual de la GUI

Elige **Cambiar credencial…** en el menú ⋯ de cualquier cuenta. El panel Estado actual dentro del modal Cambiar credencial muestra:

- **Principal (GCM/SSH/TOKEN):** el estado de la credencial principal (OK / Aviso / Sin conexión / Error / Sin configurar) y, debajo de la fila, el error subyacente. Ese texto de error es el primer sitio donde mirar: nombra la URL que falla, el código HTTP y lo que informó la red o la pila TLS.
- **Token de API (PAT):** el estado del PAT complementario (solo GCM y SSH). Cuando GCM ya cubre la API, la fila dice _No creado_ y su detalle empieza por _API via GCM_. Cuando ningún PAT cubre la API, aparece un botón **Configurar token de API** junto a la fila; cuando falla un PAT guardado, aparece en su lugar un botón **Sustituir token de API**.
- Un botón **Volver a comprobar** ejecuta de nuevo la verificación sin salir del modal.
- Aparece una pista azul bajo el panel cuando el error parece un bloqueo de acceso a la red por parte del SO, con un enlace a la sección correspondiente de más abajo.

Copia ese texto de error tal cual cuando abras un bug report.

### "Repository not found" al clonar

**Síntoma.** Un clone falla con `Repository not found`.

**Causa.** El nombre `org/repo` es incorrecto, o la cuenta no tiene acceso a ese repo.

**Arreglo.**

- Comprueba que el nombre `org/repo` coincide con el repo real.
- Comprueba tus credenciales desde una shell: `git ls-remote <url>`.
- Para repos de otra organización, asegúrate de que tu cuenta tiene acceso.

### HTTP 401 desde Forgejo o Gitea

**Síntoma.** Una cuenta GCM muestra **Funciona, con un aviso**, y la fila del token de API dice _API rejected the GCM credential (HTTP 401)_.

**Causa.** O bien la credencial de GCM es incorrecta, o bien tu servidor Forgejo o Gitea rechaza contraseñas en su API REST. Una contraseña de cuenta real funciona para `git push` / `pull`, pero no para `/api/v1/*`.

**Arreglo.** Una de estas opciones:

- Elimina la credencial de GCM actual y repite la configuración de la credencial, pegando un **PAT** cuando GCM te pida la contraseña. GCM lo guarda igual, y también funciona para la API.
- Deja GCM como está y haz clic en **Configurar token de API** en el panel Estado actual para guardar un PAT complementario en el archivo de credenciales de Gitbox. La API usa el PAT; las operaciones git siguen usando GCM.

### GCM abre la cuenta equivocada en el navegador

**Síntoma.** La configuración de GCM inicia sesión con una cuenta distinta de la configurada en Gitbox.

**Causa.** El navegador ya tiene una sesión abierta con otro login, y GCM la reutiliza.

**Arreglo.** Borra las credenciales guardadas para ese host, cierra la sesión de la otra cuenta en el navegador y repite la configuración de la credencial:

- **Windows:** Control Panel > Credential Manager > elimina las entradas `git:https://github.com`
- **macOS:** Keychain Access > busca `github.com` > elimina
- **Linux:** `secret-tool clear protocol https host github.com`

### Falla la verificación SSH

**Síntoma.** La comprobación de credenciales falla en una cuenta SSH.

**Causa.** La clave pública no está registrada en el proveedor, `~/.ssh/config` apunta a una clave equivocada o `ssh-agent` no está en ejecución.

**Arreglo.** Prueba la conexión desde una shell con el alias de host de la cuenta (`ssh.host` en la cuenta, `gitbox-<account-key>` por defecto) y lee la salida detallada:

```bash
ssh -T git@gitbox-<account-key> -v
```

Comprueba que la clave pública está registrada en el proveedor, que el `IdentityFile` de `~/.ssh/config` apunta a la clave correcta y que `ssh-agent` está en ejecución.

### Errores DNS

**Síntoma.** La línea de detalle contiene `no such host`, `server misbehaving` o `i/o timeout` durante la resolución DNS.

**Causa.** Esta máquina no puede resolver el hostname de la URL de la cuenta: una errata en la URL, una VPN o un DNS split-horizon que no está activo, o un resolver DNS privado que no está configurado.

**Arreglo.** Prueba `dig +short <host>` o `nslookup <host>` desde una shell. Corrige tu DNS y vuelve a comprobar en Gitbox.

### Errores TLS / de certificado

**Síntoma.** La línea de detalle contiene `x509:`, `certificate signed by unknown authority` o `tls: failed to verify certificate`.

**Causa.** El servidor presenta un certificado que el sistema no puede validar: normalmente un certificado autofirmado, uno firmado por una CA interna que no está en el almacén de confianza del sistema, o un hostname que no coincide.

**Arreglo.** Añade el certificado de la CA interna al almacén de confianza de tu SO (Llavero de macOS, almacén de certificados de Windows o, en Linux, `/usr/local/share/ca-certificates/` + `update-ca-certificates`). Gitbox usa el almacén de confianza del sistema y no tiene ninguna excepción propia.

### "Connection refused" (cualquier SO)

**Síntoma.** La línea de detalle contiene `connect: connection refused`.

**Causa.** El servidor es alcanzable, pero nada escucha en el puerto de destino: el servicio está caído, o el host o el puerto son incorrectos.

**Arreglo.** Comprueba la URL del servidor en los ajustes de la cuenta (haz clic en el nombre de la cuenta en su tarjeta) y luego comprueba el servicio en el lado del servidor.

### macOS: permiso de red local

**Síntoma.** Una cuenta cuyo servidor está en tu LAN (`192.168.x.x`, `10.x.x.x` o un hostname `.local`) aparece _Sin conexión_, y la línea de detalle contiene `connect: no route to host`. `curl` o `git` desde Terminal llegan al mismo servidor.

**Causa.** macOS protege las conexiones a la red local con el permiso de privacidad **Local Network**, app por app. GitboxApp todavía no se ha aprobado, se ha denegado (algo habitual en builds ad-hoc) o se ha movido o renombrado, así que macOS ya no la reconoce. Los rechazos breves justo después del arranque son normales: Gitbox reintenta las comprobaciones de credencial sin conexión, y los fetches rechazados en milisegundos, durante un máximo de 10 segundos antes de marcar la cuenta como sin conexión.

**Arreglo.** Cierra GitboxApp (⌘Q) y, como el usuario que la ejecuta:

```bash
xattr -dr com.apple.quarantine /Applications/GitboxApp.app
codesign --force --deep --sign - /Applications/GitboxApp.app   # firma ad-hoc
tccutil reset LocalNetwork                                     # olvidar decisiones existentes
open /Applications/GitboxApp.app
```

La firma ad-hoc no necesita una cuenta de Apple Developer; da a macOS una identidad estable con la que recordar el permiso. En la primera conexión a la LAN, macOS pregunta _"GitboxApp would like to find and connect to devices on your local network"_. Haz clic en **Allow**.

**Si `tccutil` falla.** A menudo imprime `tccutil: Failed to reset LocalNetwork`. Ejecútalo como tu usuario normal, no con `sudo`, desde una terminal que tenga **Full Disk Access**. Aun así, macOS puede pedir permiso en la siguiente conexión a la LAN. Si no lo hace, abre _System Settings → Privacy & Security → Local Network_ y activa GitboxApp. Si GitboxApp no aparece en la lista, vuelve a lanzarla y haz clic en **Volver a comprobar** en una cuenta de la LAN para que macOS la añada.

### Windows: firewall

**Síntoma.** Una cuenta de tu LAN aparece _Sin conexión_, y la línea de detalle contiene `A connection attempt failed` o `No route to host`.

**Causa.** Windows Defender Firewall, o un producto endpoint de terceros, bloquea las conexiones salientes de `GitboxApp.exe` hacia la LAN. La regla es por ejecutable, así que el hecho de que un navegador o `git.exe` pasen no dice nada sobre GitboxApp.

**Arreglo.**

1. Abre _Windows Security → Firewall & network protection → Allow an app through firewall_.
2. Haz clic en _Change settings_ → _Allow another app_ → navega hasta el `GitboxApp.exe` instalado.
3. Marca las columnas **Private** y **Public** para la red que uses.
4. Si hay software endpoint corporativo, pide una regla a IT, indicando la ruta del ejecutable y el servidor de destino.

Vuelve a comprobar en la GUI cuando la regla esté activa.

### Linux: resolución de problemas de red

**Síntoma.** Una cuenta de tu LAN o de una IP privada aparece _Sin conexión_, y la línea de detalle contiene `connect: no route to host`, `connect: network is unreachable` o `connect: connection refused`.

**Causa.** Los escritorios Linux normalmente no tienen un permiso de red a nivel de SO. O la red está caída (VPN apagada, interfaz equivocada, subred equivocada) o un firewall local (`ufw`, `firewalld`, nftables) bloquea la conexión saliente.

**Arreglo.**

1. Prueba la misma petición desde una shell: `curl -sSI https://<host>/`. Si también falla, el problema es la red, no la app.
2. Confirma que la VPN está activa (si hace falta) y que existe la ruta: `ip route get <server-ip>`.
3. Revisa tu firewall: `sudo ufw status verbose`, `sudo firewall-cmd --list-all` o `sudo nft list ruleset`. Permite TCP saliente hacia el servidor de destino.
4. Si la shell llega al servidor pero la GUI no, puede que un sandbox (Flatpak, Snap) restrinja la app. Lánzala desde una terminal normal para confirmarlo.

### macOS: "Device not configured" durante la configuración de GCM

**Síntoma.** Configurar una credencial GCM devuelve `fatal: could not read Password ... Device not configured`.

**Causa.** `credential.helper` o `credential.credentialStore` faltan o son incorrectos en `~/.gitconfig`, así que git recurre a un prompt de terminal, que una app GUI no tiene (ver [Requisitos de gitconfig global](#requisitos-de-gitconfig-global)).

**Arreglo.** Haz clic en **Configurar** en el banner del credential helper y repite la configuración de la credencial.

### Windows: el inicio de sesión de GCM agota el tiempo de espera

**Síntoma.** Configurar una credencial GCM nunca muestra la ventana de GCM y se detiene a los 5 minutos con un mensaje que nombra variables `GCM_*`. O el inicio de sesión funciona, pero GCM guarda el secreto en texto claro bajo `%USERPROFILE%\.gcm\store\`. Otro usuario de Windows en el mismo PC, con el mismo `~/.gitconfig`, no tiene el problema.

**Causa.** Las variables de entorno `GCM_*` de usuario sobrescriben `~/.gitconfig`. Algunos scripts de configuración pensados para SSH o para uso headless las definen:

- `GCM_GUI_PROMPT=false` hace que GCM pregunte en una terminal, que GitboxApp no tiene.
- `GCM_INTERACTIVE=never` hace que el inicio de sesión falle en vez de preguntar.
- `GCM_CREDENTIAL_STORE=plaintext` (o `dpapi`, `cache`) gana sobre `credential.credentialStore = wincredman`, así que GCM guarda los secretos fuera del Administrador de credenciales de Windows.
- `GCM_GITHUB_AUTHMODES=device` fuerza el flujo device-code de GitHub, cuyo código solo aparece en una terminal.

Gitbox fuerza la ventana de GCM durante el inicio de sesión, pero las variables siguen cambiando dónde y cómo guarda GCM la credencial. Elimínalas.

**Comprobar.** Como el usuario afectado, en PowerShell:

```powershell
[Environment]::GetEnvironmentVariables('User').Keys | Where-Object { $_ -like 'GCM_*' }
[Environment]::GetEnvironmentVariables('Machine').Keys | Where-Object { $_ -like 'GCM_*' }
```

**Arreglo como el usuario afectado.** Borra las variables de usuario y después cierra sesión y vuelve a entrar, porque Explorer solo lee el entorno al iniciar sesión:

```powershell
'GCM_GUI_PROMPT','GCM_INTERACTIVE','GCM_CREDENTIAL_STORE','GCM_GITHUB_AUTHMODES' |
  ForEach-Object { [Environment]::SetEnvironmentVariable($_, $null, 'User') }
```

Un resultado en `Machine` necesita un PowerShell elevado con `'Machine'` en lugar de `'User'`, y afecta a todos los usuarios del PC.

**Arreglo desde una cuenta de administrador.** Con el usuario afectado fuera de sesión, carga su hive del registro, borra los valores y descárgalo. Ejecútalo elevado, sustituye `<user>` y mantén al usuario fuera de sesión hasta que `reg unload` termine bien, o Windows le dará un perfil temporal:

```powershell
reg load 'HKU\gbx_tmp' 'C:\Users\<user>\NTUSER.DAT'
foreach ($n in 'GCM_GUI_PROMPT','GCM_INTERACTIVE','GCM_CREDENTIAL_STORE','GCM_GITHUB_AUTHMODES') {
  Remove-ItemProperty -Path 'Registry::HKEY_USERS\gbx_tmp\Environment' -Name $n -ErrorAction SilentlyContinue
}
[GC]::Collect(); reg unload 'HKU\gbx_tmp'
```

**Después.** Inicia sesión como el usuario, repite la configuración de GCM en GitboxApp y confirma que aparece la ventana de GCM. Cuando la credencial esté en el Administrador de credenciales de Windows, borra `%USERPROFILE%\.gcm\store\`, que sigue guardando los secretos antiguos en texto claro.

### Informar de un error inusual

Gitbox muestra el error subyacente literalmente, sin traducirlo ni resumirlo. Copia la cadena exacta en un issue en [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues) junto con:

- El SO y su versión (por ejemplo, "macOS 14.5 Sonoma").
- El tipo de credencial en uso (GCM / SSH / Token) y el proveedor (GitHub / Forgejo / ...).
- El texto de error del panel Estado actual en los ajustes de credencial de la cuenta.
- Si la misma petición funciona desde `curl` en una shell de la misma máquina.
