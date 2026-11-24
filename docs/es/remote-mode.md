# Modo remoto (experimental)

El modo remoto deja que GitboxApp, en mi portátil, gestione los repositorios de otro ordenador por SSH, como un servidor Linux sin escritorio o un Mac. GitboxApp instala por sí mismo un `gitbox-server` que coincide con él en ese host: sin root, sin internet en el host y sin ningún puerto a la escucha. El diseño sigue a VS Code Remote-SSH.

Esta versión conecta, instala, verifica y desinstala el servidor, muestra los repositorios del host, cambia su configuración y sus clones, configura en él credenciales de token y SSH, abre sus carpetas en mi editor, mi terminal y mis harnesses de IA locales, y sigue adelante cuando se cae la conexión: un daemon en el host mantiene el trabajo en marcha, y la ventana vuelve a conectarse a él. Se admiten hosts Linux y macOS (consulta [Hosts macOS](#hosts-macos)); los hosts Windows llegan después.

## Antes de empezar: un alias que no pida nada

El modo remoto usa el `ssh` del sistema y mi propia configuración de SSH: `~/.ssh/config`, `known_hosts`, `ProxyJump` y mi agente se aplican sin cambios. Nunca añade `StrictHostKeyChecking`, `UserKnownHostsFile` ni `-F`.

Cada remoto es un alias `Host` de mi `ssh_config`. GitboxApp guarda el alias y nada más: ni nombre de host, ni usuario, ni clave.

```text
Host my-server
  HostName my-server.example.org
  User me
```

El modo remoto ejecuta `ssh` con `BatchMode=yes`, así que nunca responde a una pregunta. Antes de añadir un remoto, `ssh my-server` tiene que entrar sin pedir nada:

- La clave está cargada en un agente, o no tiene frase de paso. Un host que solo acepta contraseñas, o una clave cifrada sin agente, falla con `ssh_auth`. Un agente que pide aprobación para cada aplicación, como 1Password, muestra la petición de Gitbox en su icono de la barra de menús o de la bandeja y no en una ventana; si nadie la responde, falla con `ssh_agent` al cabo de un minuto. Cuando el inicio de sesión tarda más de 3 segundos, la ventana de conexión lo dice y, con 1Password, señala su icono.
- La clave del host ya está en `known_hosts`. Si no lo está, la conexión falla enseguida con `ssh_hostkey`. Ejecuto `ssh my-server` una vez en un terminal y acepto la clave.
- Lo mismo vale para cada salto de un `ProxyJump`.

## Activar el modo remoto

El modo remoto viene desactivado. Se activa con la opción `--experimental-remote` o con esta clave en mi `gitbox.json`:

```json
"global": { "experimental_remote": true }
```

Cualquiera de las dos muestra el selector de equipo junto al título, y **Equipos remotos** en Ajustes, cuyo **Gestor** abre la ventana Equipos remotos.

## Añadir un remoto

En la ventana Equipos remotos, que también abre **Gestionar equipos remotos…** del selector de equipo, escribo el alias, o elijo una línea de mi configuración de SSH, y, si la necesito, una carpeta del servidor (`server_dir`), y luego **Añadir**. La ventana muestra una línea por cada entrada `Host` de `~/.ssh/config` y de los archivos que incluye, y una línea rellena su primer nombre, el que va tras `Host`; quedan fuera las entradas cuyo primer nombre es un patrón o un equipo ya añadido. Al escribir, las líneas se filtran y el alias se completa en el propio campo. GitboxApp solo lee esa configuración.

GitboxApp comprueba el alias con `ssh -G <alias>`, que lee la configuración sin conectar. Eso detecta un `ssh_config` roto, no un host desconocido: `ssh` resuelve cualquier nombre. Un host equivocado solo se nota al conectar. El remoto se guarda en la lista `remotes` del `gitbox.json` de este ordenador.

## Conectar

**Conectar** abre el remoto en una ventana propia: un proceso nuevo de GitboxApp arrancado como `GitboxApp --remote <alias> --experimental-remote`. Un segundo **Conectar** trae al frente la ventana abierta. La ventana del remoto sigue abierta cuando cierro la ventana principal.

La ventana aparece enseguida y conecta en segundo plano. Dice **Conectando con my-server…** e **Instalando gitbox en my-server…** en la primera conexión, con **Desconectar**, **Ver registro** y **Desinstalar servidor**. Hasta que lleguen los repositorios, sigue el tema claro u oscuro del sistema. Cuando el servidor responde, la ventana muestra los repositorios del host: consulta [Qué puede hacer una ventana remota](#qué-puede-hacer-una-ventana-remota). Su título sigue siendo `gitbox · my-server`.

`--remote` sin `--experimental-remote` sale con un mensaje.

## Qué puede hacer una ventana remota

La ventana del remoto es la ventana completa de gitbox, alimentada por el host. Necesita un host que ya tenga un `gitbox.json`, por ejemplo un ordenador que también ejecuta GitboxApp; las credenciales de token y SSH se pueden configurar desde la ventana. Un host sin él se queda en la tarjeta de conexión, que lo dice; configurarlo desde aquí llega en una versión posterior.

Un texto que habla de este ordenador nombra el host en su lugar, porque la acción se ejecuta allí por SSH: el menú de la fila dice **Borrar de my-server…**, y el diálogo de borrado dice de qué host es la carpeta que se va.

Esto funciona, y todo se ejecuta en el host:

- el estado de cada repositorio, **Fetch de todo**, **Fetch** en un repositorio y la comprobación periódica del estado, que el host ejecuta por su cuenta (consulta [El daemon](#el-daemon));
- los detalles del repositorio, las insignias de pull requests y sus listas, y el estado de los espejos;
- **Buscar proyectos** de una cuenta, que lista los repositorios del proveedor sin añadirlos;
- las comprobaciones de credenciales, la vista previa de la limpieza;
- **Comprobación del sistema**, que muestra las herramientas del host;
- abrir una carpeta de repositorio o de cuenta del host en VS Code, un perfil de terminal o un harness de IA de este ordenador: consulta [Abrir las carpetas del host](#abrir-las-carpetas-del-host);
- la ventana de **Raíces**, que acepta una carpeta escrita para el host: consulta [Escribir una carpeta del host](#escribir-una-carpeta-del-host).

Esto cambia la configuración del host, y también se ejecuta allí:

- editar, renombrar y borrar una cuenta;
- añadir y quitar raíces, elegir la raíz predeterminada y la profundidad de búsqueda anidada;
- los grupos de espejos y sus repositorios;
- añadir repositorios descubiertos a la configuración, e ignorar un clon;
- los ajustes que viven en el `gitbox.json` del host, como las insignias de pull requests y la comprobación del gitignore global;
- los arreglos de la configuración de git del host (la identidad global, el helper de GCM y el gitignore global);
- reparar un `gitbox.json` roto o restaurar una de sus copias de seguridad.

Esto cambia los clones del host y los proveedores, y también se ejecuta allí:

- clonar, actualizar, borrar clones y crear repositorios;
- la limpieza de ramas fusionadas, la configuración de espejos y añadir los espejos que encontró el descubrimiento;
- la reconciliación con los clones del disco, que también se ejecuta cuando la ventana recupera el foco, y adoptar los clones que encuentra.

Esto configura las credenciales del host, y también se ejecuta allí (consulta [Hosts remotos](credentials.md#hosts-remotos)):

- añadir una cuenta con token, y guardar, sustituir o borrar su token;
- cambiar el tipo de credencial de una cuenta, salvo a GCM, que un host rechaza;
- configurar, verificar y regenerar la clave SSH de una cuenta, cuya clave privada se queda en el host.

La vista compacta y el arranque automático están desactivados, con el aviso «No disponible en una ventana remota»: una ventana remota los mantiene apagados.

Esto no tiene equivalente remoto, así que está desactivado con el aviso «No disponible para hosts remotos»: **Mostrar en Finder** o **Mostrar en el Explorador**, abrir la carpeta de la cuenta y **Abrir en editor** del archivo de configuración en Ajustes.

Mis propias preferencias, como el idioma, el tema, Simple o Avanzado, la barra lateral y el tamaño de la ventana, siguen funcionando y se quedan en este ordenador.

### Escribir una carpeta del host

Una ventana remota nunca abre el diálogo de carpetas: elegiría una carpeta de este ordenador. El onboarding y la ventana de **Raíces** muestran en su lugar un campo de texto. Escribo una carpeta del host, como `~/git` o `/srv/git`, y 300 ms después de dejar de escribir el host dice a qué corresponde: la ruta absoluta, que se creará, que es un archivo o que no se puede escribir en ella. El botón sigue desactivado hasta que la carpeta se puede usar.

El host comprueba cada carpeta por sí mismo, envíe lo que envíe la ventana: debe ser una ruta absoluta una vez expandido `~`, sin caracteres de control, o la acción falla con `remote_path_invalid`. Restaurar una copia de seguridad solo acepta un archivo de la lista de copias.

### Cuando otra ventana guardó antes

Varios procesos pueden guardar el `gitbox.json` del host: mis ventanas, un GitboxApp que se ejecuta en el host y su ventana de terminales. Un guardado nunca sobrescribe un cambio que otro hizo desde que leí el archivo. En su lugar falla con «La configuración cambió en otra ventana o proceso y se ha recargado. Inténtalo de nuevo.», y la ventana muestra lo que hay ahora en el archivo. Un cambio de mis propias preferencias nunca falla así.

Si la conexión se cae mientras se guarda un cambio, el `gitbox.json` del host contiene la versión anterior o la nueva, nunca una mezcla: el archivo se sustituye de una vez. Un cambio enviado dos veces, por ejemplo tras agotarse el tiempo, se aplica una sola vez: el host responde a la repetición con la primera respuesta, y tras una reconexión un cambio que ya está hecho falla con «Eso ya existe» o «Eso ya no existe» en lugar de aplicarse otra vez. La detección de harnesses de IA del host guarda lo que encuentra, y la ventana pide al host que vuelva a detectar sus harnesses cuando recupera el foco. El fetch y el pull reparan antes el `.git/config` de cada clon, como en un escritorio. Una cuenta o un repositorio plegado sigue plegado solo en esta ventana.

### Cuando la conexión se cae durante una acción larga

Un clon, una actualización, una configuración de espejo o un **Fetch de todo** se ejecuta en el host, en su daemon (consulta [El daemon](#el-daemon)). Cerrar la ventana, **Desconectar** o una conexión caída solo separan la ventana: la acción sigue allí, junto con sus procesos de git, y nada en este ordenador la cancela.

Cuando la conexión se cae, la ventana vuelve a conectarse por sí sola, como describe [El selector de equipo y la conexión](#el-selector-de-equipo-y-la-conexión). La nueva conexión recibe los resultados de lo que terminó mientras tanto, y una acción todavía en curso vuelve a preguntar al host con el mismo id de petición: el host responde con el resultado del primer intento y nunca la ejecuta dos veces. Un **Fetch de todo** que la caída interrumpió termina con su tarjeta de resultado habitual. Cualquier otra llamada que la ventana hace mientras vuelve a conectarse espera a la nueva conexión en lugar de fallar. Después de **Desconectar**, el siguiente **Conectar** hace lo mismo. Una acción solo falla en la ventana cuando la ventana deja de intentar volver a conectarse o se cierra.

Una sesión que el host sirve sin el daemon se comporta como antes: cerrar la ventana, desconectar o perder la conexión detiene la acción allí, junto con sus procesos de git:

- Un clon se detiene, y git borra la carpeta que creó. Si el host mató a git de golpe, la carpeta se queda: la siguiente reconciliación la señala como un clon que se detuvo antes de terminar, y yo borro la carpeta y vuelvo a clonar. Gitbox nunca la borra por mí.
- Una actualización se detiene, y git deja el clon como estaba o avanzado.
- Una configuración de espejo se detiene, y lo que el proveedor ya creó se queda.

La ventana marca entonces la acción como fallida con el error de la conexión, así que nada espera una respuesta que no va a llegar.

### El selector de equipo y la conexión

En una ventana remota el selector de equipo junto al título muestra el alias, el sistema del host en el modo avanzado y el estado de la conexión. Su menú abre el registro de conexión, otro equipo remoto ya añadido, la ventana local y la ventana Equipos remotos. El botón **Registro de conexión** de la ventana Equipos remotos trae al frente una ventana abierta del equipo con su registro, y cada ventana registra el estado de su conexión para que el selector de las demás ventanas pueda mostrarlo.

Cuando se cae la conexión con el daemon, por un `ssh` que muere, la tapa del portátil cerrada o tres latidos perdidos con 10 segundos entre ellos, la ventana vuelve a conectarse por sí sola. Desde el momento en que se cae la conexión dice **Se cortó la conexión; el host sigue con su trabajo. Conectando de nuevo, intento 1 de 3…**, y sus acciones quedan desactivadas hasta la siguiente conexión. Hace tres intentos, tras 2, 5 y 10 segundos. Una conexión nueva tiene que llegar al mismo build del servidor, como la primera. Cuando fallan todos los intentos, o cuando el host sirve la sesión sin el daemon, la ventana muestra el fallo como sigue.

Cuando no hay conexión, la ventana conserva el último estado que recibió, atenuado e inactivo, bajo **Desconectado de my-server.** y un botón **Conectar**. Mientras está desconectada, la ventana no lanza ninguna recarga en segundo plano ni la comprobación periódica, así que no aparece ningún otro error. **Conectar** vuelve a hacer la misma conexión. Una conexión nueva, automática o con **Conectar**, conserva la vista que tenía. Cuando reanuda el daemon del host con todos los eventos que la ventana se perdió, la ventana conserva también sus filas y el trabajo en curso, y vuelve a leer la configuración y el estado. Si no, cuando el daemon se reinició o ya no guardaba algunos de esos eventos, la ventana borra lo que dejó la desconexión y recarga todo, y después reconcilia los clones del host. En ambos casos actualiza su estado antes de volver a ofrecer acciones, bajo **Reconectado a my-server. Leyendo sus clones antes de devolver las acciones…**

### En qué confía la ventana

Todo lo que envía el host es no fiable. Los enlaces solo se abren tras una comprobación en este ordenador: los enlaces `https` se abren en el navegador, un enlace `http` se abre después de que confirme la dirección completa, y cualquier otro, como `vscode:` o `ms-settings:`, se rechaza. Los nombres y las rutas se muestran como texto y nunca se ejecutan como un comando. La ventana admite como mucho 10.000 repositorios y 1.000 cuentas; una lista más larga se corta, la insignia avisa y el registro de conexión dice qué lista se cortó.

## Abrir las carpetas del host

Los lanzadores de una ventana remota abren la carpeta del host desde este ordenador, como hace VS Code Remote-SSH. La carpeta viene del host, así que GitboxApp nunca la mete como texto en una línea de comandos. Primero la comprueba: tiene que ser una ruta absoluta del host, de 4.096 bytes como mucho, sin NUL, CR ni LF. Cualquier otra cosa falla con `not_available_remote_path` antes de que arranque nada. El alias siempre sale de mi propia lista `remotes`, nunca del host.

### Editores

VS Code (`code`) y VS Code Insiders (`code-insiders`) abren la carpeta con `code --remote ssh-remote+<alias> <carpeta>`, cada parte como un argumento propio. VS Code necesita su extensión Remote-SSH, y conecta con mi propio `ssh` y el mismo alias. Un archivo de workspace de un contenedor se abre igual.

Cualquier otro editor, como Zed o VSCodium, aparece desactivado con «Este editor no puede abrir carpetas en hosts remotos». La prueba de humo remota desde macOS y Windows cubre la entrada de VS Code; la entrada de Insiders usa la misma línea de comandos de la documentación de Remote-SSH.

Cada remoto puede tener su propio editor. En la ventana Equipos remotos, la lista de editores de un remoto ofrece **Automático** y los editores configurados que abren carpetas remotas. Con un editor elegido, la ventana de ese remoto abre las carpetas solo en él: los demás editores siguen visibles, atenuados con «Este remoto se abre en el editor elegido en la ventana Equipos remotos», y una llamada que nombra otro editor falla con `remote_editor_not_chosen`. **Automático** ofrece todos los editores que abren carpetas remotas. La elección se guarda en el campo `editor` de la entrada del remoto en el `gitbox.json` de este ordenador, y una ventana remota abierta la usa desde su siguiente lanzamiento. Si el editor elegido ya no está instalado, la ventana Equipos remotos lo muestra como no instalado.

En Windows, `code` es un archivo por lotes que `cmd.exe` vuelve a leer, así que una carpeta con `%`, `!`, `^`, `&`, `|`, `<`, `>` o `"` falla con `not_available_remote_path`.

### Terminales

Un perfil de terminal abre un terminal local que ejecuta `ssh -t <alias>` y entra en la carpeta del host:

```text
ssh -t -- my-server 'cd -- '\''/srv/my repo'\'' && exec "$SHELL" -l'
```

El `cd` se escribe para el shell de inicio que informó el host: sh, bash, zsh, dash y ksh reciben comillas POSIX, y fish recibe las suyas. Cualquier otro shell pasa el `cd` a `sh` solo con palabras entre comillas simples. El modo remoto no añade aquí ninguna opción de `ssh`, así que decide mi `ssh_config`, y una pregunta de frase de paso o de clave del host aparece en el terminal. En un host sin un locale UTF-8, fish muestra estropeado en su prompt y en `pwd` el nombre de una carpeta con letras que no son ASCII, pero el terminal está en la carpeta correcta.

Cómo arranca el terminal depende de este ordenador:

- **macOS:** la app del perfil ejecuta `ssh`. Terminal.app e iTerm lo ejecutan a través de mi shell de inicio, con las comillas que necesita, fish incluido; un perfil cuya app no puede ejecutar un comando, como `open -a Warp`, se rechaza.
- **Linux:** el terminal del perfil ejecuta `ssh` como su programa, sin ningún shell en medio.
- **Windows:** un perfil de WezTerm o Alacritty con sus argumentos predeterminados ejecuta `ssh` como su programa, igual que en Linux. Cualquier otro perfil, incluidos Windows Terminal, un perfil de shell, un terminal que añadí yo mismo, o WezTerm o Alacritty con los argumentos editados, arranca `ssh.exe` en una consola nueva propia, sin `cmd.exe` en medio, y Windows la muestra en su terminal predeterminado: esos lanzadores vuelven a leer la línea de comandos.

### Harnesses de IA

Un harness de IA se ejecuta en el host, en la carpeta, en un terminal de mi perfil predeterminado: el mismo `ssh -t` con `exec <harness>` después del `cd`. Los harnesses son los que se detectan en el host. Solo se ejecuta un harness del catálogo, por su nombre o por una ruta absoluta que termina en él, y sin argumentos; cualquier otra cosa que liste el host falla con `harness_not_allowed`.

## Qué se instala y dónde

Todo vive bajo la **raíz del servidor**: el `server_dir` del remoto si lo configuro, y si no `~/.gitbox-server`.

```text
<root>/<build>/gitbox-server       el servidor, modo 0700
<root>/<build>/in-use.<client id>  uno por cada sesión conectada
<root>/<build>/state/              la carpeta de estado del daemon, modo 0700
<root>/<build>/state/rpc.sock      el socket del daemon, modo 0600
<root>/<build>/state/daemon.pid    el id de proceso del daemon; daemon.lock es su bloqueo
<root>/<build>/state/events.log    los últimos 1.000 eventos que terminaron una operación
<root>/<build>/daemon.fallback     por qué la última sesión funcionó sin el daemon
<root>/.gc.lock/                   el bloqueo de la recolección de basura
<root>/logs/<build>.log[.1..3]     el registro del servidor
```

`<build>` es `<version>-<commit>-<pin8>`, y `<pin8>` es el principio del SHA-256 que este GitboxApp fija para el binario. Dos builds del mismo commit pueden llevar bytes distintos, así que se instalan uno al lado del otro. Todas las carpetas se crean con modo 0700, y nada va a `/tmp`.

La raíz del servidor tiene que ser una carpeta propia. GitboxApp rechaza con `server_dir_unsafe` un `server_dir` que sea la carpeta home o la carpeta raíz, también a través de un enlace.

### Instalación

La instalación solo se ejecuta cuando falta el servidor de este build:

1. GitboxApp sondea el sistema y la arquitectura del host en un solo comando.
2. Comprueba el espacio libre con `df` y necesita el doble del tamaño del servidor.
3. Envía el servidor, que lleva dentro, a un archivo temporal en la carpeta del build.
4. Comprueba el SHA-256 de ese archivo contra el pin, con la primera de `shasum`, `sha256sum` u `openssl` que tenga el host. Si no coincide, borra el archivo antes de que llegue a ejecutarse.
5. Ejecuta el archivo una vez con `--version`. En un home montado `noexec` esto falla, el archivo se borra, y el error dice que configure `server_dir` con una carpeta que permita ejecutar programas. GitboxApp nunca recurre a `/tmp`.
6. Renombra el archivo a su sitio, que es atómico. Dos clientes que instalan a la vez acaban con el mismo archivo.

Un archivo de servidor que ya está ahí con un hash equivocado, o que no se puede leer, nunca se sustituye en silencio. Falla la verificación, y la ventana ofrece **Reinstalar servidor**, que borra esa carpeta de build cuando confirmo y la instala de nuevo.

### Verificar antes de cada arranque

Cada arranque verifica el servidor y lo arranca en el mismo comando remoto, así que conectar con un servidor instalado cuesta dos comandos SSH: el sondeo y el arranque. El comando solo ejecuta el servidor cuando su hash es igual al pin, y el cliente comprueba el hash otra vez antes de hablar con el servidor.

Esto detecta la corrupción, una subida truncada y un archivo sustituido por error. No puede parar a un atacante que ya ejecuta código como mi usuario remoto y cambia el archivo entre la comprobación y el arranque. Frente a ese atacante, el cliente trata como no fiable todo lo que dice el servidor.

### El daemon

El arranque verificado ejecuta `gitbox-server proxy`, no el servidor. El proxy se conecta al daemon de su build a través de `state/rpc.sock`, y cuando ninguno responde arranca `gitbox-server run` desde el mismo archivo verificado. Después retransmite la sesión. Hay un daemon por usuario y build, compartido por todas las ventanas de ese build, y guarda una sola copia del estado del host para todas ellas. Cuando la sesión termina, el proxy sale y el daemon se queda.

- **Qué guarda.** El trabajo que empezó una ventana sigue cuando la ventana se va. El resultado de una petición que cambia el host se guarda durante 10 minutos después de terminar, por ventana, así que una ventana que vuelve a preguntar recibe la misma respuesta. Los últimos 1.000 eventos que terminaron una operación van a `state/events.log`, y una ventana que vuelve a conectarse recibe los que se perdió. El archivo contiene lo que la ventana habría mostrado, y nunca un token.
- **Sincronizar sin ninguna ventana conectada.** El daemon ejecuta el fetch de todo, el estado de los espejos y la actualización de las pull requests con el intervalo de la comprobación periódica del estado. Cada ventana envía su propio intervalo cuando se conecta y cuando lo cambio, y gana el último. Una ventana conectada a un daemon no tiene temporizador propio. El fetch nunca toca un árbol de trabajo.
- **Cuánto vive.** El daemon termina cuando ninguna ventana ha estado conectada durante 24 horas y no hay nada en marcha. Desinstalar, **Reinstalar servidor** y la recolección de basura de su build lo detienen antes. Para detenerlo a mano, ejecuto `kill $(cat ~/.gitbox-server/<build>/state/daemon.pid)` en el host. La siguiente conexión arranca un daemon nuevo, que no tiene ninguno de los eventos del anterior para dar.
- **Quién puede llegar a él.** El socket tiene modo 0600 y está en una carpeta de modo 0700, los dos míos. El daemon también comprueba el id de usuario de cada conexión, rechaza a cualquier otro usuario y solo registra ese id. No abre ningún puerto TCP. Al arrancar, registra el SHA-256 de su propio archivo; ese valor es solo informativo, porque el proceso lo dice de sí mismo.
- **Cuando no puede funcionar.** Cuando la carpeta de estado no es solo mía, o el daemon no arranca o no responde, el proxy sirve la sesión en su propio proceso, como antes del daemon, y el trabajo se detiene cuando se cae la conexión. El registro del servidor dice por qué, el registro de conexión dice **the host serves this session without the daemon (…): work stops if the link drops**, y **Diagnosticar** muestra el motivo. Un host Windows todavía no tiene daemon.
- **Cerrar sesión.** En un host Linux cuyo systemd-logind configura `KillUserProcesses=yes`, cerrar la última sesión termina el daemon. `loginctl enable-linger` para mi usuario lo mantiene en marcha.

### Recolección de basura

Después de cada arranque, el servidor borra lo que ya nadie usa:

- cualquier otra carpeta de build sin un marcador de uso refrescado en los últimos 10 minutos y sin una subida en curso, después de detener el daemon de ese build; un build cuyo daemon no se detiene se queda;
- los archivos temporales de subida con más de una hora;
- los registros de los builds que ya no están.

Una sesión conectada refresca su marcador cada minuto y borra su propio marcador, y ningún otro, cuando se detiene. La recolección toma el bloqueo creando `.gc.lock`, que también funciona en NFS, y rompe un bloqueo con más de 5 minutos, y lo dice en el registro. Solo toca nombres que crea gitbox, así que el resto de archivos de la raíz del servidor se quedan.

## Desinstalar

**Desinstalar servidor**, en la ventana del remoto o junto al remoto en Ajustes, desconecta y quita el servidor del host. Si otra ventana sigue usando el servidor allí, GitboxApp pregunta otra vez antes.

Antes detiene el daemon de cada build. Solo borra lo que puso gitbox: las carpetas de build, los registros de build de `logs/`, `.gc.lock/` y los archivos temporales de subida. Quita `logs/` y después la raíz del servidor solo cuando están vacías. Un archivo que yo mismo puse en la raíz del servidor se queda, y también la raíz que lo contiene. Los repositorios y el `gitbox.json` del host no se tocan.

**Quitar** en Ajustes solo olvida el remoto en este ordenador.

## Leer el registro de conexión

**Ver registro** abre el registro de conexión de la ventana: las últimas 2.000 líneas, cada una con su hora. Registra:

- cada paso con su código de salida y su duración, y el tamaño del banner de inicio de sesión como `[banner: N bytes]`;
- las decisiones de instalación, el hash del archivo subido y el pin;
- lo que `ssh` y el servidor imprimen en stderr;
- si el host arrancó su daemon o se unió al que ya estaba en marcha, cuántos eventos perdidos trajo una reconexión, y cuándo el host sirve la sesión sin el daemon.

**Copiar registro** lo copia, y también puedo seleccionar y copiar cualquier parte del registro o del informe del diagnóstico, que se desplazan juntos. **Diagnosticar** ejecuta `gitbox-server --doctor` en el host en un comando aparte, verificado como un arranque, y muestra el build del servidor, la versión de git, la ruta de la configuración y si esa carpeta permite escribir y ejecutar programas. Para cada raíz de clonado de la configuración del host, también muestra si el servidor puede leerla, y nombra la causa cuando no puede: la carpeta no existe, la protección de privacidad de macOS la bloquea (consulta [Hosts macOS](#hosts-macos)), los permisos la rechazan, o no se puede leer. También muestra el daemon de este build: si su carpeta de estado es segura, si está en marcha y por qué la última sesión funcionó sin él, si alguna lo hizo.

En el host, el servidor también escribe `<root>/logs/<build>.log`, que rota a 1 MiB y guarda tres archivos anteriores.

El registro nunca contiene un token ni una contraseña: GitboxApp limpia cada línea antes de mostrarla. Un token que escribo viaja al host una vez y ninguno de los dos lados lo registra.

## Códigos de error

El modo simple muestra una frase sencilla; el código en sí aparece en el registro.

- `ssh_missing`: no hay ningún comando `ssh` en este ordenador. Instala OpenSSH.
- `ssh_auth`: `ssh` no pudo entrar sin pedir nada. Carga la clave en un agente o usa una clave sin frase de paso, y luego ejecuta `ssh <alias>` una vez.
- `ssh_agent`: el agente no firmó el inicio de sesión. Un agente que pide aprobación, como 1Password, muestra la petición en su icono de la barra de menús o de la bandeja y desiste al cabo de un minuto. Pulsa **Conectar** y apruébala a tiempo.
- `ssh_hostkey`: la clave del host no está en `known_hosts`, o ha cambiado. Ejecuta `ssh <alias>` una vez en un terminal.
- `ssh_timeout`: el host no respondió a tiempo.
- `ssh_config`: `ssh` no puede leer la configuración del alias. Corrige `ssh_config`.
- `ssh_failed`: cualquier otro fallo de `ssh`, como un host desconocido o una conexión rechazada. El registro muestra lo que imprimió `ssh`.
- `banner_overflow`: el shell de inicio de sesión imprimió más de 1 MiB antes de la respuesta. Silencia los archivos de arranque del shell en las sesiones no interactivas.
- `unsupported_os`: el host no es ni Linux ni macOS. Los hosts Windows llegan en una versión posterior.
- `unsupported_arch`: el host no es ni amd64 ni arm64.
- `server_target_missing`: este GitboxApp no lleva servidor para ese host. Un build de desarrollo solo lleva los de Linux y el de Apple silicon; vuelve a compilar con los seis destinos.
- `server_build_mismatch` y `unstamped_build`: este GitboxApp se compiló sin un sello que coincida. Vuelve a compilarlo como dice la guía de desarrollo.
- `hash_mismatch`: el servidor del host no coincide con el pin, o no se puede leer para comprobarlo, y no se ejecutó. Usa **Reinstalar servidor**.
- `version_mismatch`: el servidor que respondió es otro build. Vuelve a conectar.
- `disk_full`: la carpeta del servidor tiene menos espacio libre que el doble del servidor.
- `no_hash_tool`: el host no tiene ni `shasum`, ni `sha256sum`, ni `openssl`.
- `noexec`: la carpeta del servidor no permite ejecutar programas. Configura `server_dir` con una carpeta que lo permita.
- `server_dir_unsafe`: `server_dir` es la carpeta home o la raíz. Elige una carpeta propia.
- `server_crashed`: el servidor se detuvo durante la conexión. El registro muestra sus últimas líneas.
- `not_connected`: la ventana no está conectada.
- `not_implemented_remote`: la acción todavía no tiene lado remoto.
- `not_available_remote`: la acción no tiene equivalente remoto, como el gestor de archivos.
- `not_available_remote_path`: la carpeta no es una ruta absoluta válida del host, o una capa de este ordenador no puede llevarla con seguridad.
- `harness_not_allowed`: el harness de IA no es del catálogo, o lleva argumentos.
- `gcm_unsupported_remote`: el host no ejecuta GCM, cuyo inicio de sesión y almacén necesitan una sesión de escritorio. Usa un token: consulta [Hosts remotos](credentials.md#hosts-remotos).
- `read_only`: la ventana rechaza la acción sin enviarla, porque el servidor de este remoto no la ofrece. Hoy se ofrecen todas las acciones de una ventana remota.
- `remote_path_invalid`: la carpeta no es una ruta absoluta del host, o contiene un carácter de control.

## Hosts macOS

Un Mac también funciona como host, como un Mac mini sin pantalla. Lo pruebo en Apple silicon.

- **Sesión remota.** La activo en Ajustes del Sistema › General › Compartir › Sesión remota y permito allí mi usuario. Cuando la lista de usuarios permitidos está restringida, macOS solo deja entrar a los miembros del grupo `com.apple.access_ssh`.
- **zsh lee `~/.zshenv`.** Para un comando por SSH, zsh lee `~/.zshenv`, no `~/.zshrc`. Lo que imprima ese archivo cuenta como banner, así que lo mantengo en silencio (consulta `banner_overflow`).
- **Git.** En una sesión SSH el `PATH` solo contiene las carpetas del sistema, así que un `git` a secas es el de Apple. El servidor prefiere el git de Homebrew (`/opt/homebrew/bin/git`) cuando está instalado, como hace GitboxApp en un Mac, y **Diagnosticar** muestra qué versión respondió.
- **Credenciales.** El llavero de inicio de sesión sigue bloqueado en una sesión SSH, así que el modo remoto rechaza Git Credential Manager con `gcm_unsupported_remote`. Las cuentas de token funcionan, porque su token vive en un archivo 0600, no en el llavero. Las cuentas SSH funcionan como en Linux.
- **Carpetas protegidas por la privacidad.** macOS mantiene los procesos que arranca `sshd` fuera de `~/Documents`, `~/Desktop`, `~/Downloads` e iCloud Drive (TCC). Una raíz de clonado en una de ellas falla con `Operation not permitted`, y **Diagnosticar** nombra esa causa. Muevo la raíz a una carpeta como `~/git`. La otra opción es dar a `/usr/libexec/sshd-keygen-wrapper` Acceso total al disco en Ajustes del Sistema › Privacidad y seguridad › Acceso total al disco, pero eso abre esas carpetas a cualquier inicio de sesión SSH en el Mac.
- **Sin cuarentena, firma ad hoc.** GitboxApp envía el servidor por SSH y nada lo descarga, así que el archivo no lleva el atributo `com.apple.quarantine` y Gatekeeper nunca lo comprueba. El linker de Go lo firma ad hoc, algo que Apple silicon necesita para ejecutarlo. No tiene Developer ID, y un programa de línea de comandos que su propietario ejecuta por SSH no lo necesita. Comprobé ambas cosas con `xattr -l` y `codesign -dv` en macOS 27.

## Clientes Windows y macOS

En macOS y Linux, GitboxApp reutiliza una sola conexión SSH para todos sus comandos. Si mi `ssh_config` ya tiene un control master en marcha para el alias, usa ese; si no, arranca el suyo, con el socket en la carpeta de configuración de gitbox, y lo mantiene 10 minutos. Una reconexión en ese tiempo entra sin otra firma, así que un agente que pide aprobación la pide una vez.

Win32-OpenSSH no tiene control master, así que en Windows cada comando se autentica por su cuenta: dos por cada conexión con un servidor instalado, y algunos más durante una instalación. Con la clave en el agente no se nota.
