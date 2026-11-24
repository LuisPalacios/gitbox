<p align="center">
  <img src="../../assets/screenshot-advanced.png" alt="Gitbox en modo Avanzado: la barra lateral de cuentas y la vista Requiere atención" width="800" />
</p>

# Gitbox desktop — guía de usuario

Gitbox es una app de escritorio que mantiene tus proyectos Git organizados y al día, incluso cuando trabajas con varias cuentas en GitHub, GitLab, Gitea, Forgejo y Bitbucket.

Esta guía cubre todo, desde el primer arranque hasta el uso diario.

## Instalación

Descarga el instalador para tu plataforma desde la página de [Releases](https://github.com/LuisPalacios/gitbox/releases):

- **Windows** — `gitbox-win-amd64-setup.exe` instala `GitboxApp.exe` en Program Files con accesos en el menú Inicio.
- **macOS** — `gitbox-macos-arm64.dmg` o `gitbox-macos-amd64.dmg`, para macOS 13 Ventura o posterior. Abre el DMG y ejecuta su script de instalación desde Terminal.
- **Linux** — `gitbox-x86_64.AppImage`, autocontenido: descárgalo y ejecútalo.

También puedes descargar un archivo ZIP (`gitbox-<platform>-<arch>.zip`) y extraerlo a mano. Cada uno contiene solo la app: `GitboxApp.exe`, `GitboxApp.app` o `GitboxApp`.

> **macOS:** Apple no firma la app. El DMG incluye un script "Install Gitbox" que copia `GitboxApp.app` a `/Applications/` y elimina los flags de cuarentena. Ejecuta `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal. Para una instalación manual, ejecuta `xattr -cr /path/to/GitboxApp.app`.

Gitbox llama a herramientas que ya están en el sistema: **Git** en tu PATH y [Git Credential Manager](https://github.com/git-ecosystem/git-credential-manager) para las cuentas GCM. **Ajustes → Comprobación del sistema** lista lo que falte con el comando de instalación para tu SO. Consulta [Panel de ajustes](#panel-de-ajustes).

### Script de bootstrap

En macOS, Linux o Windows (Git Bash), el script de bootstrap descarga la última release e instala la app:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
```

Usa `--version <tag>` para una release concreta o `--prefix <dir>` para cambiar el directorio de instalación (por defecto `~/bin`; macOS instala `GitboxApp.app` en `/Applications/`).

En Linux el script también registra la app en el menú Activities, para que puedas buscar "Gitbox" o arrastrarla al dock. Omítelo con `--no-desktop`, o ejecútalo más tarde por separado:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/register-gitbox.sh)
```

Pasa `--uninstall` al mismo script para eliminar la entrada del menú. Las actualizaciones desde la app y las ejecuciones posteriores del bootstrap no necesitan volver a registrarla.

En Windows el setup exe y el script de bootstrap son alternativas: el setup exe instala en Program Files con una entrada en el menú Inicio; el script de bootstrap, en `~/bin` sin ninguna. Elige uno. Cuando el script de bootstrap encuentra una copia del setup exe, avisa de que el menú Inicio sigue abriendo esa copia y la deja donde está. También detecta una instalación a medio quitar (una entrada de desinstalación cuyos archivos ya no existen) e imprime los comandos para limpiarla.

Los hosts headless no pueden ejecutar la app. Ahí, `--cli-only` instala la última CLI `gitbox` 1.x de la línea de mantenimiento v1. El script la elige por su cuenta en Linux cuando no está definida ni `DISPLAY` ni `WAYLAND_DISPLAY`.

### Linux AppImage

Descarga la AppImage, dale permiso de ejecución y ejecútala:

```bash
chmod +x gitbox-x86_64.AppImage
./gitbox-x86_64.AppImage
```

La app necesita un entorno de escritorio con servidor de pantalla (X11 o Wayland).

## Paso 1: primer arranque

La primera vez que abres Gitbox, una configuración guiada corta te lleva por tres pasos:

1. **¿Dónde guardas tus proyectos?** — elige tu primera **raíz de clonado**, la carpeta donde viven tus proyectos en disco, como `~/00.git` o `C:\repos`. La vista previa muestra la estructura de carpetas y cuenta los proyectos cercanos. Los clones existentes conservan su ubicación. Elige **Descargar un proyecto** o **Usar un proyecto existente**. Puedes añadir más raíces después en Ajustes.
2. **Conecta dónde están guardados tus proyectos** — elige GitHub, GitLab, Bitbucket o la tarjeta **Forgejo** y **Gitea** (después elige cuál y escribe la dirección del servidor). Escribe tu usuario, tu nombre y tu email. El nombre y el email vienen rellenos desde tu identidad global de git cuando la tiene, y aparecen en los cambios que guardes. Gitbox sugiere un nombre para la carpeta de la cuenta, como `github-alice`. Elige **Editar nombre de carpeta** para cambiarlo antes de iniciar sesión. Haz clic en **Iniciar sesión con el navegador** e inicia sesión. Gitbox usa Git Credential Manager (GCM) para esto.
3. **Elige tu primer proyecto** — comprueba el proveedor, el servidor y el usuario conectados. Elige un proyecto para descargar o uno de los clones existentes para esa cuenta. Comprueba la carpeta exacta del proyecto y confirma. Cuando el proyecto esté listo, abre el panel. Puedes añadir más proyectos desde la vista de la cuenta.

Si GCM no está instalado, el paso 2 muestra cómo instalarlo y ofrece **Usar un token**, que enlaza a la página donde creas un token y te pide pegarlo. Si el inicio de sesión falla, puedes usar **Reintentar**, cambiar a un token o volver atrás con **Cambiar los datos**.

Puedes usar **Omitir por ahora** en los pasos 2 y 3 y añadir cuentas más tarde. Las instalaciones nuevas empiezan en [modo Simple](#modos-simple-y-avanzado). Si sales después del primer paso, el siguiente arranque abre la pantalla principal; termina desde **Conectar una cuenta** en la barra lateral.

En los arranques posteriores, Gitbox muestra tus clones, preferencias y la anidación de contenedores al instante, antes de que terminen las comprobaciones de estado. Los indicadores giratorios marcan los clones que todavía está comprobando o a los que está haciendo fetch.

## Paso 2: añadir cuentas

Una cuenta le dice a Gitbox quién eres en un servidor concreto, por ejemplo tu cuenta de GitHub o el GitLab de tu empresa.

En modo Simple, **Conectar una cuenta** en la barra lateral usa el mismo formulario de inicio de sesión y después te permite elegir los proyectos para descargar. En modo Avanzado, **Añadir cuenta** abre el formulario completo, que también ofrece SSH y te deja elegir la clave de cuenta. Rellena:

1. **Clave de cuenta** — un nombre corto que eliges, como `github-personal`. También se convierte en el nombre de la carpeta en disco.
2. **Proveedor** — GitHub, GitLab, Gitea, Forgejo o Bitbucket.
3. **URL** — la dirección del servidor. Para GitHub es `https://github.com`.
4. **Usuario de Git** — tu nombre de cuenta en ese servicio.
5. **Nombre y Email** — la identidad que usan tus commits de Git.
6. **Tipo de credencial** — cómo se autentica Gitbox (ver más abajo).

### Configurar credenciales

Después de crear la cuenta, Gitbox necesita una forma de iniciar sesión en tu proveedor. Hay tres opciones:

- **GCM (Git Credential Manager)** — la más sencilla. Gitbox abre tu navegador para que inicies sesión. Es la mejor para GitHub y GitLab.
- **Token (personal access token)** — creas un token en el sitio web de tu proveedor y lo pegas en Gitbox. La app te dice qué URL visitar y qué permisos seleccionar.
- **SSH** — Gitbox genera un par de claves. Copias la clave pública y la añades a los ajustes de tu proveedor; la app te da el enlace directo.

Cuando la credencial funciona, la entrada de la cuenta en la barra lateral y su tarjeta indican **La credencial funciona** junto a una marca verde. Para los detalles de cada tipo y sus permisos, consulta [credentials.md](credentials.md).

## Paso 3: encontrar y añadir proyectos

Selecciona la cuenta en la barra lateral y haz clic en **Buscar** en su tarjeta, o elige **Buscar proyectos…** en el menú ⋯ de la cuenta. Gitbox pide a tu proveedor todos los repositorios que tu cuenta puede ver y los lista de la A a la Z.

- **Campo de búsqueda** — escribe para filtrar la lista.
- **Seleccionar todo** — selecciona todo lo visible, respetando el filtro.
- **Ya añadidos** — los repos que ya están en tu config aparecen atenuados y no se pueden seleccionar de nuevo.

Elige los que quieras y haz clic en **Añadir y Pull**. Gitbox los guarda en tu config y los clona en la raíz de clonado de la cuenta. Con dos o más raíces, primero pregunta a cuál van.

Discovery solo añade repos a tu config; nunca los elimina.

## Día a día

Cierra cualquier diálogo con **Escape** o haciendo clic fuera de él. Los diálogos ocupados o que necesitan una decisión explícita (un clon en curso, una confirmación en marcha) siguen abiertos hasta que terminan. Los nombres de cuenta, las filas de repo y las insignias de organización también responden a **Enter** y **Espacio** cuando llegas a ellos con **Tab**.

### Modos Simple y Avanzado

Gitbox tiene dos experiencias. **Simple** muestra el estado de los proyectos y las acciones en lenguaje sencillo. **Avanzado** muestra cada detalle y herramienta de git. Cambia de modo con el interruptor de la barra superior, a la izquierda del engranaje de ajustes: gris es Simple, rojo es Avanzado, el modo con poder destructivo. Las instalaciones nuevas empiezan en Simple; las configs anteriores a este ajuste se quedan en Avanzado. La elección se guarda como `global.ui_mode` en `gitbox.json`.

Si otra ventana o un editor cambia el archivo de configuración mientras trabajo, Gitbox conserva la edición externa. El aviso ofrece **Reintentar** para ajustes como los indicadores de pull requests, la comprobación del gitignore global y la profundidad del escaneo anidado.

En Simple, cada proyecto muestra lo que Gitbox ha observado y cuándo hay que actuar.

- **Sin cambios locales** — el proyecto no tiene archivos cambiados en este ordenador.
- **Cambios en este ordenador · N archivos cambiados aquí** — Gitbox ha encontrado archivos cambiados en este ordenador. Abre el proyecto con tu IA y pídele que los guarde.
- **Guardado aquí, aún no en el servidor** — los cambios guardados van por delante de la última versión conocida del servidor. Pasa el ratón por encima para ver el detalle. Una rama local independiente muestra **Rama local** y cuenta como guardada. Cuando Gitbox no puede identificar la rama predeterminada, la fila muestra **Comprobando…** y solo cuenta para el total hasta que un escaneo la identifique.
- **Actualización disponible** — Gitbox ha visto trabajo más reciente en el servidor. **Actualizar** incorpora ese trabajo al proyecto cuando puede conservar tus cambios locales. Gitbox nunca actualiza por su cuenta.
- **Solo en línea** — el proyecto aún no está en este ordenador. **Descargar** lo trae.
- **Necesita ayuda** — algo ha ido mal, por ejemplo cambiaron las dos copias. Haz clic en el proyecto para ver qué ha ocurrido y ábrelo con tu IA para arreglarlo.

Cuando una fila con **Actualización disponible**, **Solo en línea** o **Necesita ayuda** muestra un resultado conocido del servidor, indica cuándo lo comprobó Gitbox. Pasa el ratón sobre ese texto para ver la fecha y la hora de la comprobación.

Los nombres se muestran sin el prefijo de la organización; pasa el ratón sobre un nombre para verlo completo. Las filas mantienen los tres [botones de lanzamiento](#acciones-de-clones), y el menú ⋯ ofrece Abrir en la web, la carpeta, los editores, los AI harnesses y **Dejar de mostrar este proyecto**.

Simple oculta el resto: la vista Mirrors, el modo borrar, los botones de vista compacta y de tema (el tema sigue en Ajustes), las insignias de contenedor y de rama, los conteos por delante, por detrás y de cambios, y el botón de fetch de cada fila. La barra superior mantiene **Comprobar todo** y **Actualizar todo**. La salud de la cuenta muestra **Hay que revisar la conexión** cuando no se puede llegar al servidor y **Reconectar** cuando una credencial falla. Ajustes oculta las filas Configuración, PR / revisiones, Gitignore global y Terminales. La vista compacta es igual en los dos modos.

Con los indicadores de PR activados, Simple muestra **Revisión pendiente** cuando alguien te pide una revisión. Haz clic en el texto para ver las solicitudes. Avanzado muestra insignias con los conteos de tus PR abiertos (📝) y de los PR que esperan tu revisión (👀).

### ¿Por qué esta cuenta?

En Avanzado, hago clic en un proyecto clonado, incluso si su árbol de trabajo está limpio, y selecciono **¿Por qué esta cuenta?**. El panel muestra la cuenta, el servidor y el nombre de usuario configurados, el origen del nombre y el correo de autor previstos, la URL de descarga resuelta y los gestores de credenciales que aplica Git. Identifica los ajustes en conflicto y oculta sus valores. En Simple, los detalles del proyecto muestran una frase sobre la cuenta o indican que requiere atención.

**Seguro** significa que los ajustes de Git comprobados coinciden y que la sesión actual verificó la credencial en las últimas 24 horas. **Aviso** significa que Gitbox no tiene ninguna verificación satisfactoria reciente, que la última verificación falló o que el token caduca en 14 días o menos. **Inseguro** significa que los ajustes comprobados entran en conflicto o que Gitbox no pudo leerlos. El panel lee la configuración en el equipo que aloja el clon.

Uso **Reparar** para aplicar el mantenimiento habitual del clon, **Corregir credencial** para abrir los ajustes de credenciales de la cuenta o **Abrir la configuración de Git** para editar la configuración compartida de Git del clon local. En una ventana remota edito el archivo en el equipo que aloja el clon. Selecciono **Actualizar prueba** después de hacer una edición manual.

### Vista completa

La vista completa muestra la barra superior, la barra lateral, la vista seleccionada y el footer de resumen. La barra superior contiene los anillos de salud y los botones Pull de todo, Fetch de todo, Modo borrar, Vista compacta, tema, Simple/Avanzado y Ajustes; el logo de GitBox del extremo izquierdo pliega la barra lateral. El footer de resumen siempre muestra los conteos de repos y de mirrors.

El anillo de repos lleva un icono de rama y se llena a medida que los clones se ponen al día. Una pequeña insignia cuenta lo que aún falta, y al pasar el ratón sobre el anillo se ve la línea completa `N de total al día` (Simple: `Sin acciones pendientes en N de total`). Cuando el anillo tiene algo pendiente, al hacer clic se abre **Requiere atención** (Simple: **Te necesitan**). Avanzado añade un [anillo de mirrors](#anillo-de-salud-de-mirror) cuando hay mirrors configurados.

La ventana se abre con 1040 px de ancho la primera vez y puede encogerse hasta 640 × 480. No crece más allá de la barra lateral más 1100 px de contenido, y al plegar la barra lateral su ancho pasa al contenido.

### La barra lateral

La barra lateral de la izquierda elige qué muestra el área principal:

- **Requiere atención** (Simple: **Te necesitan**) — primero las cuentas cuya credencial necesita un arreglo, cada una con su botón de arreglo, y después cada repo que te necesita, agrupados como necesita ayuda, trabajo local, por detrás y no clonado. En Simple esos grupos se llaman **Necesita ayuda**, **Cambios locales**, **Actualizaciones encontradas** y **Solo en línea**. Dentro de cada grupo, las filas quedan bajo una cabecera por cuenta, en orden alfabético. Un contenedor anida los clones que tiene al lado, como en Todos los repos; un clon cuyo contenedor está en otro grupo muestra una etiqueta "en \<parent\>". Una fila que pasa a estar bien mientras miras, después de un clonado o un pull por ejemplo, sale de la vista al momento. Cuando nada te necesita, la vista lo dice.
- **Todos los repos** (Simple: **Todos los proyectos**) — cada clon, agrupado por cuenta, en orden alfabético.
- **Cuentas** — una entrada por cuenta con el logo de su proveedor, una pequeña marca de salud, su nombre, su salud en palabras y cuántos repos te necesitan. Haz clic en una para abrir la vista de la cuenta.
- **Más** — bajo el separador inferior, todo lo que no es una cuenta:
  - **Añadir cuenta** (Simple: **Conectar una cuenta**) — consulta el [Paso 2](#paso-2-añadir-cuentas).
  - **Encontrados en disco** — aparece cuando Gitbox encuentra clones en disco que no ha podido ubicar por su cuenta, con cuántos son, o cuando has ignorado alguno. Abre **Clones sin ubicar** (consulta [Clones encontrados en disco](#clones-encontrados-en-disco)).
  - **Mirrors**, solo en Avanzado.

El logo de GitBox de arriba a la izquierda, o ⌘B en macOS y Ctrl+B en el resto, pliega la barra lateral a un riel de iconos. El riel mantiene los logos de los proveedores con su marca de salud y su conteo de atención; al pasar el ratón sobre uno se ven el nombre de la cuenta y su salud, y un clic cambia a ella. En una ventana de menos de 780 px la barra lateral siempre se muestra como el riel, y el logo abre la barra lateral completa sobre el contenido hasta que eliges una vista.

Gitbox recuerda la última vista y la barra lateral plegada entre reinicios. Si esa cuenta ya no existe, o Simple no puede mostrar Mirrors, se abre Requiere atención en su lugar. Renombrar una cuenta conserva su vista.

### Entender la tarjeta de cuenta

La vista de una cuenta empieza con una tarjeta de tres líneas:

1. **Marca de salud y nombre** a la izquierda; haz clic en el nombre para editar la cuenta. El proveedor y el menú ⋯ de la cuenta están a la derecha.
2. **Salud en palabras**: La credencial funciona, Funciona, con un aviso, Sin conexión · no se llega al servidor, La credencial ha fallado o Sin credencial configurada (Simple: Conectado, No se puede conectar ahora, Hay que reconectar, Sin conectar). Cuando algo necesita arreglo, el botón de la derecha lo hace: **Reintentar** vuelve a comprobar una cuenta sin conexión, y **Arreglar credencial**, **Configurar credencial** o **Reconectar** de Simple abren la pantalla de credenciales. En Avanzado, al pasar el ratón sobre las palabras se ven el tipo de credencial y sus mensajes.
3. **Conteos**: cuántos repos tiene la cuenta y cuántos te necesitan. **Buscar** descubre repos y **+ Crear** crea uno, en los dos modos. Ambos se deshabilitan, con el motivo al pasar el ratón, cuando la credencial no llega a la API del proveedor.

Cuando a un token de GitHub o GitLab que funciona le quedan 14 días o menos para caducar, la salud en palabras pasa a **El token caduca en N días** en naranja, y **Cambiar token** (Simple: **Renovar**) abre la pantalla de credenciales. La entrada de la barra lateral y su tooltip dicen lo mismo. En Avanzado, al pasar el ratón sobre las palabras también se ve la fecha de caducidad de cualquier token que la tenga. Las APIs de Gitea, Forgejo y Bitbucket no informan de la caducidad de los tokens, así que esas cuentas nunca muestran este aviso.

Una credencial ausente o rota tiñe la tarjeta de rojo claro, y un aviso la tiñe de naranja. Los clones de la cuenta siguen a la tarjeta, en orden alfabético. Un clon anidado en el contenedor de otra cuenta aparece aquí con una etiqueta "en \<parent\>"; **Todos los repos** lo mantiene bajo su contenedor, mientras que sus acciones siguen usando su propia cuenta.

### Contraer y expandir

La vista **Todos los repos** se pliega a dos niveles, y la vista de una cuenta también pliega los clones padre:

- **Grupos de cuenta** — cada cuenta va en su propia tarjeta suave, aquí y en **Requiere atención**. Su nombre, en mayúsculas y en el color de su proveedor (GitHub índigo, GitLab naranja, Gitea verde, Forgejo rojo, Bitbucket azul), queda en la columna de los puntos de estado de debajo, junto al triángulo ▾ relleno que cuelga a su izquierda. Haz clic en la barra de cabecera o en el nombre para plegar su lista de clones. La cabecera siempre muestra cuántos clones contiene y cuántos requieren atención, plegada o no, así que un problema nunca queda oculto tras un pliegue. El menú ⋯ de la cabecera sigue funcionando y nunca pliega el grupo.
- **Clones con clones debajo** — un clon que tiene clones anidados o carpetas de workspace aún sin clonar bajo él muestra un chevron ▾ de línea en lugar de su punto de estado, así que su nombre queda alineado con el de sus hermanos; sus palabras de estado siguen diciendo cómo está. Haz clic en el chevron para plegar todo lo que hay debajo, a cualquier profundidad. Cuando el clon tiene actualizaciones, cambios locales u otros detalles que revisar, su nombre abre el panel de detalle; en caso contrario, su nombre también pliega los hijos. Una fila plegada muestra cuántas filas oculta y cuántas de ellas requieren atención.

Gitbox recuerda lo que pliegas entre reinicios (`global.collapsed` en `gitbox.json`) y olvida las entradas de cuentas o clones que borras o renombras. La vista compacta mantiene su propia expansión por cuenta.

### Mantener proyectos sincronizados

#### Comprobación automática

Gitbox vigila tus proyectos y muestra su estado:

- **Al día** (verde) — coincide con el remoto.
- **N por detrás** (magenta) — el remoto tiene N commits nuevos que puedes traer.
- **N cambios locales · protegidos** (naranja) — tienes trabajo sin commitear, y nada en Gitbox lo toca.
- **N por delante** (azul) — tienes N commits que no has subido con push.
- **Divergido**, **Conflicto**, **Upstream eliminado** y **Error** (rojo) — el repo necesita una revisión; haz clic en él para ver los detalles.
- **No clonado** (gris) — el repo todavía no está en este ordenador; **Clonar** lo descarga.
- **Raíz no disponible** (gris; Simple: **Carpeta no encontrada**) — su raíz de clonado no está, por ejemplo un disco desconectado. No se pierde nada: el repo vuelve cuando vuelve la raíz.
- **Rama local** (verde) — en una feature branch sin upstream tracking, lo cual es normal.
- **No upstream** (gris) — la rama predeterminada no tiene upstream tracking y requiere atención. Ambos modos esperan a que Gitbox identifique la rama predeterminada antes de contabilizar una rama sin upstream como guardada o que requiere atención.

Los clones aparecen siempre en orden alfabético del nombre que muestra la fila, también los anidados. Cuando alguna fila de una lista te necesita, las filas sin nada que hacer se atenúan ligeramente hasta que pasas el ratón por encima, para que los problemas destaquen.

Un repo con checkout en una rama no predeterminada muestra una pequeña insignia de rama junto a su nombre, como `feature-xyz`. Un detached HEAD muestra una insignia roja `desacoplado`.

#### Pull de todo

Haz clic en **Pull de todo** (icono de flecha hacia abajo) en la barra superior para comprobar si hay actualizaciones para los clones configurados. Gitbox actualiza los clones limpios que están por detrás de su upstream conocido con `git pull --ff-only --no-autostash` y luego clona los repositorios cuyo destino no existe. El botón **Pull · solo FF** de una fila por detrás comprueba las mismas condiciones locales y puede descubrir commits más recientes en el upstream al pulsarlo. En Simple, el botón es **Actualizar todo**.

En Simple, **Actualizar todo** abre un plan por defecto. Muestra cuántos proyectos revisó Gitbox, qué puede descargar o actualizar y qué proyectos dejará como están. Avanzado solo abre el plan si lo activo e indica cada repositorio, la acción prevista y el motivo para omitirlo, si lo hay. En Ajustes, **Mostrar plan antes de Pull de todo** (Simple: **Mostrar plan antes de Actualizar todo**) puede seguir el modo o mantenerse activado o desactivado en ambos modos. Gitbox guarda una elección explícita como `global.show_pull_plan`.

El plan usa la última comprobación completa del estado local. Gitbox avisa de que el plan puede estar desactualizado si falta la comprobación, si corresponde a una configuración anterior o si se hizo antes de un fetch. **Cancelar** lo cierra. **Continuar** vuelve a comprobar las descargas y actualizaciones previstas antes de iniciarlas. Los nuevos candidatos esperan a la siguiente ejecución. La tarjeta de resultado cuenta los proyectos que Gitbox pensaba cambiar pero tuvo que omitir, por ejemplo si edité archivos después de abrir el plan.

El estado da prioridad a los conflictos sin resolver y a los cambios locales sobre el historial de la rama. Antes de un pull, Gitbox vuelve a comprobar el árbol de trabajo y omite el clon si encuentra cambios locales, conflictos o una operación en curso. Pasa el puntero sobre el estado de la fila para ver por qué Gitbox lo omitió. El resultado cuenta estas omisiones por separado de los fallos. Un editor aún puede escribir después de la última comprobación. Si Git rechaza el pull porque la edición coincide con la actualización, la fila muestra el nuevo trabajo local sin guardarlo en el stash.

#### Qué significan los mensajes

La vista Requiere atención (Simple: **Te necesitan**), los detalles del clon, la tarjeta de la cuenta y el asistente de configuración explican qué ha bloqueado una operación. Gitbox ofrece una acción cuando puede realizarla desde esa pantalla.

| Mensaje | Qué puedo hacer |
| --- | --- |
| No se puede conectar con el servidor | Reviso la conexión y pulso **Reintentar**. |
| Debes iniciar sesión | Pulso **Arreglar credencial** para revisar o sustituir la credencial de la cuenta. |
| Al token le faltan permisos | Pulso **Abrir página de tokens** y uso los permisos indicados para ese proveedor. |
| Servidor ocupado, inténtalo más tarde | Espero. Gitbox ofrece **Reintentar** cuando termina la espera indicada por el servidor. |
| Cambios en el clon | Uso **Abrir en el editor** para revisar los cambios por los que Gitbox omitió la actualización. |
| Las ramas necesitan una fusión | Uso **Abrir terminal** para revisar los historiales con mi herramienta Git. |
| No se encuentra la copia en línea | Primero compruebo el acceso. Si el proveedor y la cuenta lo permiten, **Volver a añadir el repositorio en línea** abre un formulario para crear un repositorio en línea vacío. El clon se queda en el disco. Publico su historial por separado con mi herramienta Git. También puedo usar **Dejar de mostrar este proyecto** en el menú del clon para conservar la carpeta y quitar su entrada. |
| Unidad desconectada | Vuelvo a conectar la unidad o monto la carpeta. |
| Algo ha fallado | En Avanzado puedo usar **Mostrar detalles** si hay un diagnóstico disponible. |

#### Fetch de todo

Haz clic en **Fetch de todo** (icono ↻) para consultar todos los remotos en busca de commits nuevos sin hacer pull. Los indicadores de estado se actualizan para que veas qué cambió antes de hacer pull. En Simple, el botón es **Comprobar todo**.

#### Tarjetas de actividad y de resultado

Mientras se ejecuta Fetch de todo o Pull de todo, una tira bajo la barra superior dice qué abarca, como "Haciendo fetch de 17 clones en 3 cuentas" o "Comprobando si 2 clones pueden avanzar y luego clonando 1", con una barra de progreso. Cuando termina, una tarjeta de resultado en la esquina inferior derecha lo resume: a cuántos clones hizo fetch, cuántos están ahora por detrás y listos para pull, cuáles omitió porque su cuenta está sin conexión, cualquier fallo con el primer error, y que los cambios locales quedaron intactos. Las tarjetas de éxito desaparecen tras unos segundos; una tarjeta con un fallo se queda hasta que la cierras. Los fetch del arranque y los periódicos solo dejan tarjeta cuando algo cambió o falló.

Al terminar un clonado o pull, la fila muestra **Comprobando…** mientras Gitbox comprueba los archivos locales. Si encuentra cambios, la fila pasa a **Cambios en este ordenador** en modo Simple.

Otras acciones informan de sus errores de la misma forma, por ejemplo una herramienta que no se abre o un mirror que no se puede añadir.

#### Fetch periódico

**Ajustes → Comprobación periódica** (Simple: **Buscar cambios**) hace fetch de todos los remotos cada 5, 15 o 30 minutos y vuelve a comprobar la salud de las credenciales en segundo plano.

Las comprobaciones del arranque, las periódicas y las manuales comparten un único ciclo de refresco. El estado local aparece en cuanto terminan las comprobaciones de Git; el indicador de fetch sigue activo hasta que terminan también las comprobaciones de credenciales y de proveedores. Un proveedor lento o inaccesible nunca convierte un clon conocido en un upstream ausente. Si Gitbox no puede restaurar la identidad, el origin o el aislamiento de credenciales configurados de un clon, muestra un aviso y omite el fetch de ese clon.

Cuando no hay red, Gitbox se detiene en vez de intentarlo con cada clon. Antes de que salga cualquier trabajo de red (una comprobación, un pull, un clone, una comprobación de mirrors o de PRs), una conexión corta por servidor dice si el servidor está ahí. Si no lo está, no se intenta nada de lo encolado para ese servidor. Mientras hay trabajo en marcha, Gitbox prueba cada 3 segundos los servidores que está usando; si uno deja de responder, también se detiene el trabajo en curso contra él. Un banner **Sin conexión** bajo la barra superior nombra los servidores a los que no llega, y **Reintentar** vuelve a probar la red en el momento. Mientras tanto Gitbox lo sigue intentando en segundo plano sin molestar, más o menos cada 30 segundos; cuando el servidor responde, el banner desaparece y la comprobación que se detuvo se ejecuta sola. Los servidores que siguen respondiendo continúan funcionando mientras otro está caído.

#### Ver detalles

Haz clic en el nombre de un repo que muestre actualizaciones disponibles, cambios locales, conflictos u otros problemas. Un panel se abre justo debajo de su fila; haz clic otra vez en el nombre para cerrarlo. En Simple y Avanzado puedo revisar:

- La rama actual y cuántos commits voy por delante o por detrás (solo Avanzado).
- Cada commit entrante, del más reciente al más antiguo, con su autor y mensaje. Son los commits del upstream de la rama actual que mi rama local no contiene.
- Cada archivo cambiado, con un icono de lo que ocurrió (añadido, eliminado, renombrado, modificado).
- Cualquier archivo sin seguimiento.

El panel mantiene un tamaño compacto y permite recorrer la lista completa con scroll. Conserva los detalles de los archivos pendientes junto a los commits entrantes cuando existen ambos. La lista refleja la última comprobación de actualizaciones; **Comprobar todo** (Avanzado: **Fetch de todo**) la refresca. Abrir el panel no ejecuta fetch ni pull, y **Actualizar** sigue siendo la acción explícita que incorpora los cambios entrantes al clon.

El panel se refresca cuando Gitbox detecta cambios nuevos y se cierra cuando el clon ya no requiere atención. Cambiar de repositorio mientras cargan los detalles nunca sustituye la nueva selección por un resultado anterior.

### Vista compacta

Haz clic en **◧** en la barra superior para cambiar al modo compacto, una tira estrecha de estado de unos 220 px de ancho:

- **Anillo de salud global** — porcentaje y conteo global de sincronización.
- **Píldoras de cuenta** — una por cuenta con un mini anillo y un conteo de problemas. Haz clic en una para ver sus repos.
- **Píldora de mirrors** — cuando hay mirrors configurados, el conteo active/total con un punto de color.
- **Selector de tema** y un botón **◧ Vista completa** abajo.

Así puedes tener Gitbox visible junto a otras apps. El modo compacto solo muestra el estado: vuelve a la vista completa para llegar a las acciones de clones y de cuentas.

<p align="center">
  <img src="../../assets/screenshot-compact.png" alt="Vista compacta: el anillo global de sincronización y una fila por cuenta, una de ellas desplegada con sus clones" width="220" />
</p>

### Clones encontrados en disco

Gitbox mantiene `gitbox.json` al día con los clones bajo tus raíces de clonado. Lo comprueba al arrancar, cuando la ventana recupera el foco, tras un **Fetch de todo** manual y cuando cambian las raíces:

- **Los clones nuevos que reconoce** se añaden solos, en su sitio; Gitbox nunca los mueve. Un clon cuenta como reconocido cuando exactamente una cuenta coincide con él y algo más allá del servidor lo confirma: el owner es el usuario de la cuenta, está bajo la carpeta de esa cuenta en una raíz, o su URL remota o su `credential.<url>.username` nombra la cuenta.
- **Clones movidos** — cuando la carpeta de un repo seguido ha desaparecido pero aparece un clon del mismo repo en otro lugar bajo una raíz, Gitbox apunta la entrada al nuevo sitio en lugar de listarlo dos veces.
- **Los clones borrados** siguen listados como **No clonado**, para que puedas clonarlos de nuevo o borrar la entrada.
- **Los repos bajo una raíz que falta** se leen como **Raíz no disponible** y se dejan en paz hasta que la raíz vuelve.

Para cada clon añadido, Gitbox configura el aislamiento de credenciales por repo, fija `user.name` y `user.email` a partir de la cuenta y reescribe la URL remota para que coincida con el tipo de credencial.

Todo lo demás espera en **Clones sin ubicar**, que se abre desde **Encontrados en disco** en la barra lateral. Cada fila dice por qué Gitbox no ha podido ubicarlo:

- **solo coincide el servidor** — una cuenta en ese servidor, pero nada más vincula el clon a ella, como el proyecto de otra persona clonado para leerlo.
- **coinciden varias cuentas** — dos o más cuentas en el mismo servidor empatan.
- **ninguna cuenta en este servidor**
- **sin remoto** — sin `origin`.
- **ya gestionado en otra ruta** — un segundo checkout de un repo que Gitbox ya sigue.
- **un worktree de git de otro clon**
- **Este clon cambió durante el escaneo.** Gitbox conservó la configuración más reciente y programó otro escaneo. Pulsa **Volver a escanear** para actualizar la lista.

Donde una cuenta puede aplicarse, elígela y haz clic en **Adoptar**; el clon se une a esa cuenta en su sitio. **Ignorar** oculta un clon para siempre, y **Mostrar ignorados** lista los ignorados, donde todavía puedes **Adoptar** uno o usar **Dejar de ignorar**. Las rutas ignoradas se guardan en `global.ignored_clones`. Las reglas de puntuación detrás del emparejamiento de cuenta están en [Arquitectura › pkg/adopt](architecture.md#pkgadopt--discovery-de-repos-huérfanos).

Si Gitbox adopta un clon pero no puede reparar su configuración de Git, conserva el clon en la cuenta. Abre **Requiere atención** para ver qué clon necesita una reparación y qué ha fallado. Una vez resuelta la causa, pulsa **Reintentar reparación** en la fila del clon. Gitbox comprueba los ajustes antes de hacer fetch.

## Acciones de repo y de cuenta

### Acciones de clones

Cada fila de repo clonado tiene tres botones de lanzamiento, en los dos modos: tu primer AI harness, tu primer editor y tu perfil de terminal predeterminado. Cada uno muestra el logo de la herramienta cuando Gitbox lo conoce (Claude, Codex, Gemini, Cursor, VS Code, Zed y cada app de terminal que Gitbox detecta) o un icono genérico, y su nombre al pasar el ratón.

Junto a ellos, el **menú ⋯** es una lista plana por secciones, sin submenús:

1. **Abrir** — **Ir al repo** abre el repo en el proveedor, y **Mostrar en Finder** (el Explorador, o el gestor de archivos en Linux) abre su carpeta.
2. **Abrir con** — cada AI harness y editor, y después el perfil de terminal predeterminado y los marcados como Preferido, cada uno con su logo. La herramienta que usa el botón de lanzamiento propio de la fila lleva una etiqueta **predeterminado**.
3. **Mantenimiento** (Avanzado) — **Limpiar ramas**, **Marcar como contenedor multi-repo** o **Desmarcar como contenedor multi-repo**, **Elegir workspace…** en un contenedor con archivos de workspace, y **Volver a buscar clones anidados**.
4. **Dejar de mostrar este proyecto** en cualquiera de los dos modos. **Eliminar carpeta del clon…** también aparece en Avanzado.

<p align="center">
  <img src="../../assets/screenshot-menu.png" alt="El menú ⋯ de un clon: Abrir, Abrir con cada AI harness, editor y terminal, Mantenimiento y las opciones para dejar de gestionar o eliminar el clon" width="560" />
</p>

El editor y el AI harness predeterminados son las primeras entradas de `global.editors` y `global.ai_harnesses` en `gitbox.json`; reordena esos arrays para cambiarlos, y el menú sigue el orden de la config. La terminal predeterminada es el perfil marcado como Predeterminado en **Ajustes → Terminales → Gestor** (consulta [Editores y perfiles de terminal](#editores-y-perfiles-de-terminal)).

Cuando el clon no ha terminado o su estado todavía no se ha cargado, las entradas de carpeta, editor, terminal y AI harness dicen que primero hay que clonar en lugar de no hacer nada.

**🧹 Limpiar ramas** muestra las ramas locales obsoletas en tres grupos:

- **Upstream desaparecido** — desapareció el upstream configurado para la rama. Su último commit local aún puede contener trabajo sin fusionar.
- **Fusionadas** — la rama predeterminada contiene los commits de la rama.
- **Squashed** — los árboles de ambos extremos son idénticos, o la rama predeterminada contiene parches equivalentes y aún coincide con la rama en cada ruta que esta modificó. Si Gitbox no puede verificar un squash, la rama se conserva en el clon. En un clon parcial, Gitbox comprueba únicamente los objetos que ya están en disco.

Revisa la lista antes de confirmar. Gitbox solo elimina ramas fusionadas. Antes de borrar, comprueba que el commit coincide con el de la vista previa y que la rama sigue fusionada tanto en la predeterminada como en el upstream o, si no tiene upstream, en `HEAD`. Las ramas abiertas en otro árbol de trabajo, implicadas en un rebase o bisect, o elegidas como predeterminadas se conservan en el clon. Para las ramas con el upstream desaparecido o con cambios equivalentes a un squash, usa **Abrir en una terminal** para abrir el perfil predeterminado en el clon y revísalas en tu cliente Git.

### Acciones de cuenta

Cada cuenta tiene un **menú ⋯**: en su tarjeta en la vista de la cuenta, y en la cabecera de su grupo en **Todos los repos**. Usa las mismas secciones que el menú de repo, aplicadas a la carpeta de la cuenta (`<root>/<account-key>`, en la raíz de clonado de la cuenta) en lugar de a un clon concreto:

- **Ir a la cuenta** — abre la página de perfil o de grupo de la cuenta en el proveedor, como `https://github.com/<username>`.
- **Mostrar en Finder** (el Explorador, o el gestor de archivos) — abre la carpeta de la cuenta, útil para búsquedas entre repos y ediciones multi-repo.
- **Abrir con** — las mismas entradas de AI harness, editor y terminal que el menú de repo, abiertas en la carpeta de la cuenta. Aquí no hay Limpiar ramas.
- **Cuenta** — **Buscar proyectos…** y **Crear repo…**, deshabilitadas cuando la credencial no llega a la API del proveedor; **Editar cuenta…**; y **Cambiar credencial…** en Avanzado, o **Reconectar…** en Simple cuando la credencial está rota.

La carpeta de la cuenta solo existe cuando hay algo clonado bajo la cuenta. Hasta entonces, las entradas de carpeta y editor no hacen nada y las de terminal y AI harness muestran "account folder does not exist", así que clona al menos un repo primero.

### Editores y perfiles de terminal

Gitbox detecta VS Code y Zed en tu PATH al arrancar y los escribe en `global.editors` con sus rutas completas. Añade cualquier otro editor a mano con su `name` y su `command`. Cursor y otros IDE agénticos aparecen entre los [AI harnesses](#acciones-de-ai-harness), no entre los editores.

Las terminales usan perfiles. Al arrancar, Gitbox detecta las apps de terminal y los shells instalados, los escribe en `global.terminal_apps` y `global.shells`, y los combina en perfiles lanzables en `global.terminal_profiles`:

- **Windows** — terminales: Windows Terminal, WezTerm, Alacritty, Tabby, ConEmu, Hyper, Mintty y ZOC. Shells: PowerShell 7 y 5, Command Prompt, Git Bash y WSL. Un perfil empareja una terminal con un shell, por ejemplo `Windows Terminal — PowerShell 7`.
- **macOS** — iTerm2, Terminal, Warp, Kitty, Ghostty, WezTerm y Alacritty. Un perfil es solo la terminal y ejecuta tu login shell (Zsh, Bash, Fish o Dash).
- **Linux** — GNOME Terminal, Konsole, Terminator, Foot, Alacritty, Kitty, Tilda, Guake y xterm. Un perfil es solo la terminal y ejecuta tu login shell (Bash, Zsh, Fish, Ksh o Dash).

**Ajustes → Terminales → Gestor** abre el editor de perfiles en su propia ventana, que se cierra con la ventana principal. Lista las apps de terminal y los shells detectados (solo lectura) y los perfiles. En cada perfil puedes:

- Marcarlo como **Predeterminado** (el botón de terminal de la fila y la etiqueta **predeterminado** del menú), **Preferido** (listado bajo **Abrir con**) u **Oculto**.
- Editar su nombre, su terminal y su shell, añadir tus propios perfiles o eliminar los que añadiste.
- Hacer clic en **Volver a detectar** para recoger shells, terminales o entradas `launch_menu` de WezTerm nuevos sin reiniciar.

La re-detección conserva tus flags, renombrados, args editados a mano y los perfiles que añadiste. Para pasar flags extra a un perfil, edita sus `args` en `gitbox.json`.

En Windows, los perfiles de shell desnudo que abren un shell sin app de terminal (`pwsh.exe`, `powershell.exe`, `cmd.exe`, `wsl.exe`) están ocultos por defecto y solo aparecen por sí mismos cuando no hay ninguna terminal moderna instalada. Muestra uno en el Gestor para tener un acceso directo.

#### Perfiles de Windows Terminal y WezTerm

Estas dos terminales se lanzan a través de tu propia configuración de terminal cuando pueden, así que el shell se abre con la fuente, los colores y los ajustes de arranque que configuraste allí.

- **Windows Terminal** — un perfil coincide con el perfil de WT que tiene el mismo nombre de shell, como `PowerShell 7`, o con el de esa distro WSL. Se abre como pestaña nueva en tu ventana de WT más reciente. Los perfiles de WT ocultos y los de una fuente de perfiles desactivada nunca coinciden. Gitbox lee `settings.json` de la instalación de la Store (`%LOCALAPPDATA%\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\`), después de Preview y después de la instalación sin empaquetar (`%LOCALAPPDATA%\Microsoft\Windows Terminal\`), y recoge los cambios en el siguiente lanzamiento.
- **WezTerm** — en todos los SO, cada entrada de `config.launch_menu` en `wezterm.lua` se convierte en un perfil, y al lanzarlo se ejecutan los `args` propios de esa entrada. Los callbacks de selector en Lua (`color_scheme` por entrada, hooks de `mux.spawn_window`) no se ejecutan, porque solo se disparan cuando eliges la entrada desde el propio launcher de WezTerm.

Sin coincidencia, Gitbox usa los argumentos de lanzamiento genéricos de la terminal. Las reglas de emparejamiento están en [Arquitectura › Cómo funciona el emparejamiento de lanzamiento](architecture.md#cómo-funciona-el-emparejamiento-de-lanzamiento).

### Acciones de AI harness

Los AI CLI harnesses (Claude Code, Codex, Antigravity, Aider, Cursor Agent, OpenCode, …) son programas interactivos que necesitan una terminal. Cada uno que encuentra Gitbox aparece bajo **Abrir con** en los menús ⋯ de repo y de cuenta, y el primero es el botón de lanzamiento de IA de la fila.

Al hacer clic en uno se abre el **perfil de terminal predeterminado** en la carpeta y se ejecuta el harness dentro del shell de ese perfil. Se aplican los archivos de arranque y el PATH de tu shell (`nvm` y `npm` en macOS y Linux, Git Bash en Windows), y acabas en un prompt interactivo cuando el harness termina. Los perfiles emparejados de Windows Terminal y WezTerm mantienen su fuente y sus colores. Para cambiar de host, marca otro perfil como Predeterminado en **Ajustes → Terminales → Gestor**.

En macOS, Terminal e iTerm2 ejecutan el harness mediante AppleScript, así que la primera ejecución puede pedir permiso de Automation. Warp, Kitty, Ghostty y Alacritty en macOS no pueden ejecutar un comando al lanzarse: con uno de ellos como perfil predeterminado, una entrada de harness muestra un error con el nombre del perfil. Marca iTerm2, Terminal o WezTerm como Predeterminado en su lugar.

Gitbox busca harnesses en segundo plano poco después de abrirse la ventana, cada diez minutos y cuando la ventana recupera el foco, así que una herramienta recién instalada aparece sin reiniciar. Además de PATH, busca en `~/.local/bin`, en los prefijos de Homebrew en macOS y en carpetas de instalación propias de cada herramienta, como `%LOCALAPPDATA%\agy\bin` para Antigravity en Windows. Los harnesses encontrados van a `global.ai_harnesses`, cada uno con un `name`, un `command` y un array opcional `args` para los flags del harness, como `["--model", "sonnet-4.6"]`.

Cuando desinstalas un harness, desaparece del menú pero se queda en la config, así que reinstalarlo, incluso en otro sitio del disco, lo devuelve con tus `args`. Las entradas que añades a mano se ocultan igual cuando su `command` deja de resolverse, pero Gitbox nunca las reescribe. La lista de herramientas que busca Gitbox se describe en [Directorio de herramientas agénticas](agentic-tools-directory.md).

### Crear repositorios

Haz clic en **+ Crear** en la tarjeta de la cuenta, o elige **Crear repo…** en el menú ⋯ de la cuenta, para crear un repositorio en el proveedor sin salir de Gitbox. El diálogo pide:

- **Propietario** — tu usuario personal más las organizaciones que informa la API del proveedor.
- **Nombre** — solo se permiten `a-z`, `A-Z`, `0-9`, `.`, `_` y `-`. Los demás caracteres se eliminan, y los espacios se convierten en guiones mientras escribes.
- **Descripción** — un resumen opcional de una línea.
- **Privado** — marcado por defecto. Desmárcalo para un repo público.
- **Clonar después de crear** — marcado por defecto. Gitbox añade entonces el repo a tu config y lo clona al momento, y el botón dice **Crear y clonar** en lugar de **Crear**.
- **Raíz de clonado** — aparece cuando tienes dos o más raíces de clonado y la casilla de clonar está marcada. Por defecto es la raíz de la cuenta.

La creación funciona con todos los proveedores y tipos de credencial, con el mismo token de API que usa discovery.

### Editar una cuenta

Haz clic en el nombre de la cuenta en su tarjeta, o elige **Editar cuenta…** en el menú ⋯ de la cuenta. Puedes cambiar:

- **Clave de cuenta** — al renombrarla, Gitbox conserva las carpetas de los clones, migra las credenciales administradas y actualiza las referencias a la cuenta. Si falla un paso, resuelvo el problema y hago clic en **Reintentar**.
- **Proveedor** — por si elegiste el equivocado.
- **Todos los demás campos** — URL, usuario, nombre, email, rama predeterminada.

### Gestionar credenciales

Elige **Cambiar credencial…** en el menú ⋯ de la cuenta para abrir la pantalla de credenciales. Cuando la credencial necesita un arreglo, el botón de la tarjeta de la cuenta y el de Requiere atención abren la misma pantalla; en Simple, el menú ofrece **Reconectar…** en su lugar, y solo cuando la credencial está rota. Para cada tipo de credencial y sus permisos, consulta [credentials.md](credentials.md).

#### Cambiar tipo de credencial

Elige GCM, Token o SSH en el desplegable y sigue los pasos de configuración. Gitbox verifica la credencial nueva, guarda el cambio, actualiza los clones existentes y elimina los artefactos sin uso del método anterior.

#### Eliminar una credencial

Mientras ves el tipo de credencial actual, haz clic en el botón rojo **Eliminar** para borrar todos los datos de autenticación guardados, por ejemplo para empezar limpio después de que caducara un token. La tarjeta se vuelve entonces roja e indica **Sin credencial configurada**; haz clic en **Configurar credencial** para empezar de nuevo.

### Transferir un repositorio entre cuentas o proveedores

Usa la opción de transferencia del proveedor cuando esté disponible. Para copiar refs a un repositorio nuevo en otro proveedor, revisa el destino, crea un clon de espejo con `git clone --mirror <source-url>` y ejecuta `git push --mirror <destination-url>` desde él. Usa un destino vacío, porque el push de espejo también elimina las refs del destino que faltan en el origen. Al terminar, importa el repositorio de destino desde la cuenta correspondiente en Gitbox.

### Eliminar repos y cuentas

Abre el menú ⋯ del proyecto y elige **Dejar de mostrar este proyecto** en cualquiera de los dos modos. Gitbox quita el proyecto de su lista y conserva la carpeta y su contenido en el disco. Para volver a gestionar el clon, abre **Clones sin ubicar**, selecciona **Mostrar ignorados** y deja de ignorarlo.

En Avanzado, el mismo menú ofrece **Eliminar carpeta del clon…**. Confirma la eliminación permanente para quitar tanto la carpeta como la entrada del proyecto. Gitbox conserva la entrada y muestra el error si no puede verificar o eliminar la carpeta. Un clon que contiene otro repo de Git o tiene árboles de trabajo vinculados debe gestionarse por separado. En Avanzado, el **icono de papelera** de la barra superior también muestra los controles para quitar proyectos, cuentas y mirrors.

En el menú ⋯ de la cuenta, elige **Eliminar cuenta** y escribe su nombre. La confirmación enumera los sources, proyectos y mirrors afectados. Gitbox elimina la cuenta, su token guardado y su clave SSH con su entrada `Host`, y conserva por defecto las carpetas de sus clones en el disco. En Avanzado, marca **Eliminar también del disco las carpetas de los clones gestionados** y confirma para eliminar también esas carpetas. Si alguna carpeta no se puede eliminar, Gitbox enumera los errores y conserva la cuenta para que puedas resolverlos y volver a intentarlo. Es posible que ya haya eliminado algunas carpetas.

En una ventana remota, estas acciones afectan a la cuenta, las entradas de los proyectos y las carpetas de los clones del host conectado.

## Mirrors

Los mirrors mantienen copias de seguridad de repos en otro proveedor, por ejemplo haciendo push desde un Forgejo de homelab a GitHub. Los proveedores replican los repos en el lado del servidor mediante sus APIs; nada se clona en local. Los mirrors son opcionales y están disponibles en Avanzado.

### La vista Mirrors

**Mirrors** bajo **Más** en la barra lateral abre la vista: una cabecera con los botones **Descubrir** y **Comprobar todo**, las tarjetas de grupos mirror y, debajo, la lista de detalle de mirrors.

### Tarjetas de mirror

Cada grupo mirror se muestra como un chip (punto de estado y par de cuentas) que se abre en una tarjeta; haz clic fuera de ella o pulsa **Escape** para volver a plegarla. La tarjeta muestra:

- **Punto de estado** — verde cuando todos los repos están activos, rojo cuando hay errores, ámbar en caso contrario.
- **Etiqueta MIRROR** con los mirrors `active/total` del grupo, con el color del punto.
- **Par de cuentas**, como `forgejo ↔ github`.
- **Comprobar** — verifica el estado de sincronización comparando los commits HEAD de los dos lados.

Un chip **+** después de las tarjetas crea un grupo mirror nuevo.

### Anillo de salud de mirror

Cuando hay mirrors configurados, la barra superior muestra un segundo anillo con un icono de dos flechas junto al anillo de repos. Se llena a medida que los mirrors pasan a activos y se vuelve rojo si algún mirror tiene errores. Su insignia cuenta los mirrors que no están activos, al pasar el ratón se ve la línea `active/total`, y al hacer clic se abre **Mirrors**.

### Acciones de mirror

- **Descubrir** — escanea todos los pares de cuentas en busca de mirrors existentes, con confianza decreciente: API de push mirror (confirmed), flag de pull mirror (likely) y coincidencia de nombre (possible). Una barra de progreso muestra el avance de cada cuenta. Los repos que ya están en tu config se marcan como **configurado** y aparecen atenuados. Añade los demás uno a uno con **+ Añadir**, o todos a la vez con **Aplicar a la configuración**.
- **Comprobar todo** — comprueba el estado de sincronización de cada grupo mirror.

### Lista de detalle de mirror

Bajo las tarjetas, cada grupo lista sus repos replicados con:

- La dirección, como `origin → backup (mirror)`.
- El estado de sincronización, como Sincronizado OK o La copia va por detrás del origen.
- Un icono de aviso cuando el repo de copia es público.
- **Configurar** para los repos que todavía no se han configurado mediante la API.
- **+ Repo** para añadir repos al grupo.

El escaneo ordinario de una raíz llega hasta ocho niveles de carpetas. Abre **Clones sin ubicar** para ver cuántas carpetas omitió Gitbox por profundidad o falta de acceso, incluso si no encontró clones sin asociar.

## Workspaces de VS Code y contenedores multi-repo

Un **contenedor multi-repo** es un clon que contiene otros clones en su árbol de trabajo, por ejemplo un repo de proyecto cuyo `.code-workspace` agrupa varios clones hermanos. Los workspaces viven en la lista de clones, en el contenedor que los contiene; no hay una vista de workspaces aparte. Gitbox nunca crea, edita ni borra archivos `.code-workspace`: son tuyos.

### Los archivos de workspace crean contenedores

Un clon con un archivo `.code-workspace` en su raíz se convierte en contenedor por su cuenta, y Gitbox registra ese archivo en `repo.workspace`. Cuando la raíz contiene dos o más, Gitbox pregunta con cuál se abre el contenedor. Hasta que eliges, la fila muestra un aviso **elegir workspace**, y **Elegir workspace…** en el menú ⋯ lo cambia más tarde. Gitbox recuerda tu elección en `repo.workspace_preferred`. Si falta el archivo preferido, otro puede ocupar su lugar. Gitbox olvida tu elección cuando dos lecturas correctas consecutivas de la raíz del clon confirman que falta el archivo. Si Gitbox no puede leer la raíz, conserva el último workspace conocido y atenúa el icono 📁. Pasa el ratón sobre el icono para ver el archivo de workspace y si está desactualizado.

Gitbox comprueba al arrancar, cuando la ventana recupera el foco, tras un Fetch de todo manual y después de que cambies las raíces de clonado, así que un archivo de workspace nuevo convierte su clon en contenedor la próxima vez que vuelvas a Gitbox. Si el archivo desaparece, Gitbox borra `repo.workspace` y mantiene el contenedor.

### Clones bajo un contenedor

Cada clon dentro del árbol de trabajo de un contenedor aparece debajo de él, sangrado, y se pliega con él. Gitbox los busca hasta la **profundidad de escaneo anidado** por debajo del contenedor, y hasta esa misma profundidad bajo cada carpeta que el workspace lista dentro del contenedor. Los clones anidados se unen a su cuenta y organización reales en su sitio, se guardan con una `clone_folder` absoluta dentro del contenedor y nunca se mueven. Una carpeta que el archivo de workspace lista dentro del contenedor y que todavía no está en disco aparece como una fila atenuada **sin clonar** con su ruta, salvo que esté dentro de otro clon, como un submódulo sin hacer checkout. Las carpetas que el workspace lista fuera del contenedor mantienen su sitio normal bajo su propia cuenta.

Si un enlace dentro del contenedor apunta fuera de él, Gitbox muestra una fila atenuada **Fuera del contenedor** y omite esa carpeta en la búsqueda automática. Pulsa **Buscar en disco** para buscar clones sin seguimiento en la carpeta de destino. Después elige una cuenta y confirma cada clon que quieras seguir. Gitbox lee hasta 1 MiB por archivo de workspace. Si no puede leer o interpretar el archivo, o si este supera el límite, pasa el ratón sobre el icono de workspace del contenedor para ver los detalles.

Los nombres de un contenedor con archivo de workspace, de los clones que ese archivo lista y de sus carpetas sin clonar aparecen en azul, así ves de un vistazo lo que abre VS Code. Un clon anidado que el workspace no lista mantiene el color normal.

### Abrir un contenedor

En un contenedor con archivo de workspace, el botón de editor abre el archivo `.code-workspace` en lugar de la carpeta cuando el editor es VS Code o un fork suyo (VS Code Insiders, VSCodium, Cursor, Windsurf). Los demás editores abren la carpeta. Los editores del menú ⋯ se comportan igual.

### Contenedores sin archivo de workspace

Puedes marcar un clon sin archivo de workspace desde el menú ⋯ de la fila del repo, bajo **Mantenimiento**: **Marcar como contenedor multi-repo**, **Desmarcar como contenedor multi-repo** y **Volver a buscar clones anidados** una vez que es contenedor. Un contenedor con archivo de workspace no tiene la entrada Desmarcar, porque el archivo es lo que lo convierte en contenedor. El diálogo **Raíces de clonado** de Ajustes fija la **profundidad de escaneo anidado**: cuántos niveles por debajo de un contenedor busca Gitbox, por defecto 1 (los hijos inmediatos del contenedor).

El layout estándar es `<root>/<account>/<org|user>/<repo>`. Un clon encontrado en ese sitio bajo cualquier raíz de clonado se adopta en su sitio bajo esa raíz. Un clon encontrado en otro lugar de una raíz también se añade donde está, con una `clone_folder` absoluta. Gitbox nunca mueve un clon.

## Ajustes y mantenimiento

### Panel de ajustes

Haz clic en el **icono de engranaje** para abrir el panel de ajustes:

- **Configuración** — la ruta a tu archivo de config, con un botón **Abrir en editor**.
- **Raíces de clonado** — las carpetas donde viven los proyectos, la predeterminada primero, con un botón **Cambiar** (consulta [Raíces de clonado](#raíces-de-clonado)).
- **Idioma** — English o Español. El cambio se aplica al momento.
- **Tema** — Sistema, Claro u Oscuro.
- **Comprobación periódica** (Simple: **Buscar cambios**) — elige No, 5m, 15m o 30m para comprobar repositorios, mirrors y pull requests. El intervalo de cada comprobación empieza cuando termina la anterior. En Avanzado, **Comprobación del sistema → Ejecutar** muestra el último resultado, si hay una comprobación en curso y su próxima hora programada. **Volver a comprobar** actualiza esos datos.
- **Ejecutar al inicio** — abre Gitbox al iniciar sesión, donde la plataforma lo permite.
- **PR / revisiones** — muestra las solicitudes de revisión en Simple y las insignias con los conteos de tus PR abiertos y de las solicitudes de revisión en Avanzado. Usa **Incluir borradores** para incluir los PR en borrador.
- **Gitignore global** — la comprobación automática de `~/.gitignore_global` al arrancar (consulta [Aviso de gitignore global](#aviso-de-gitignore-global)).
- **Comprobación del sistema** — **Ejecutar** abre un informe de cada herramienta externa que usa Gitbox (`git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add` y `wsl` en Windows): dónde está instalada, su versión y un comando de instalación para cualquier cosa ausente que tu config necesite. Cada herramienta se marca como ok, missing (requerida por tu config) u optional. El informe también lista los hallazgos de permisos del directorio y los archivos de credenciales, con la ruta, el problema y la acción sugerida. **Volver a comprobar** repite ambas comprobaciones sin cambiar permisos.
- **Terminales** — **Gestor** abre el editor de perfiles de terminal (consulta [Editores y perfiles de terminal](#editores-y-perfiles-de-terminal)).
- **Equipos remotos** — experimental, solo aparece en la ventana local con el modo remoto activado: **Gestor** abre la ventana Equipos remotos (consulta [Remotos (experimental)](#remotos-experimental)).
- **Versión** — la versión actual de la app.
- **Autor** — el autor del proyecto y un enlace al repositorio de GitHub.

En modo Avanzado, **Ajustes → Comprobación del sistema → Ejecutar** también muestra los milisegundos transcurridos desde que se abre la aplicación hasta cada etapa observada del arranque, incluidas la primera fila de repositorio, el primer estado, la pasada completa de estados y la primera tarea de red de la flota programada.

En una ventana local, elige **Ajustes → Comprobación del sistema → Ejecutar → Exportar diagnóstico**. Revisa la vista previa del JSON y selecciona **Guardar JSON…** para elegir un archivo en este ordenador. Gitbox guarda exactamente el contenido de la vista previa. El archivo incluye versiones, recuentos de configuración, códigos y tiempos de operaciones recientes, la profundidad de la cola de fetch, el estado del planificador y los tiempos de arranque. Gitbox identifica los repositorios en los registros de error con hashes cortos de sus rutas locales. Puedes adjuntar el archivo guardado a un informe de error.

Al arrancar, Gitbox comprueba los permisos de las credenciales sin leer el contenido de los tokens. Si detecta exposición, aparece un aviso sobre el panel, incluida la vista Compacta, que abre Comprobación del sistema. La auditoría informa de problemas y permisos que no puede verificar; nunca los repara automáticamente. Consulta [Permisos de los archivos de credenciales](credentials.md#permisos-de-los-archivos-de-credenciales).

Los flujos de añadir cuenta y cambiar credencial ejecutan la misma comprobación de herramientas: si eliges el tipo de credencial `gcm` en una máquina sin Git Credential Manager, un banner amarillo muestra el comando de instalación en lugar de un fallo de autenticación críptico más tarde.

### Raíces de clonado

Gitbox guarda los clones bajo una o más **raíces de clonado**, por ejemplo `~/00.git` para el trabajo diario y un disco más grande para proyectos grandes. Cada clon vive en `<root>/<account>/<org|user>/<repo>`. La primera raíz es la predeterminada, y las cuentas nuevas clonan en ella.

Abre **Ajustes** y luego **Cambiar** junto a **Raíces de clonado** para gestionarlas. Cada fila muestra la carpeta, cuántos repos viven bajo ella y una insignia **Predeterminada** en la predeterminada. Una raíz que no está en disco aparece tachada.

Si dos entradas registradas apuntan a la misma carpeta, **Requiere atención** muestra ambos nombres. Revisa sus carpetas de clonado y corrige la entrada en conflicto. Gitbox conserva ambos registros y sus rutas originales.

- **+ Añadir raíz…** — elige una carpeta; Gitbox la crea si no existe.
- **Hacer predeterminada** — las cuentas nuevas clonan en esta raíz a partir de ahora. Las cuentas que ya usan la predeterminada anterior siguen en ella, y ningún clon se mueve.
- **✕** — quita una raíz. Cuando todavía viven repos bajo ella, Gitbox pregunta si olvidarlos; sus archivos se quedan en disco.

Con dos o más raíces, un clonado explícito pregunta una vez adónde va: el **Clonar** de una fila (Simple: **Descargar**), **Buscar → Añadir y Pull** y los proyectos que eliges al añadir una cuenta. La opción por defecto es la raíz de la cuenta, y la cuenta la recuerda para su siguiente clonado. **Crear repositorio** muestra la misma opción como un campo. **Pull de todo** prescinde del selector de raíz. Los clones ausentes van a la raíz de su cuenta.

El mismo diálogo fija la **profundidad de escaneo anidado** para los [contenedores multi-repo](#contenedores-sin-archivo-de-workspace).

### Recetas de flota portátiles

En la ventana principal, abre **Ajustes → Receta de flota → Exportar receta**. Revisa el JSON exacto antes de guardarlo. La receta contiene servidores de proveedores, nombres de usuario, tipos de credenciales, proyectos registrados, definiciones de mirrors e identificadores lógicos de raíz. Gitbox omite las claves de cuenta locales, las identidades de autor de Git, las credenciales y las carpetas absolutas de clonado. Al importar, un clon con una carpeta específica de la máquina usa la estructura de carpetas habitual.

En la máquina de destino, configura sus raíces de clonado y elige **Importar receta**. Asigna cada identificador a una raíz local e introduce el nombre y el correo del autor de Git para cada cuenta nueva. Revisa la tabla antes de aplicar los cambios. Resuelve primero los destinos ocupados o las asignaciones ambiguas. Gitbox conserva las entradas existentes y solo añade configuración. Usa **Reparar credencial** para iniciar sesión y descarga los proyectos que quieras.

### Remotos (experimental)

El modo remoto gestiona los repositorios de otro ordenador por SSH. Viene desactivado: la opción `--experimental-remote` o `"experimental_remote": true` en `global` lo activan. Entonces la barra superior muestra el selector de equipo junto al título, y los Ajustes de la ventana local muestran **Equipos remotos**.

El selector de equipo dice qué ordenador muestra la ventana: **Este ordenador** en la ventana local, y el alias y el estado de la conexión en una ventana remota. Su menú lista este ordenador y los equipos ya añadidos, cada uno con su estado. Elegir un equipo abre su ventana, o la trae al frente si ya está abierta, sin una segunda conexión. Elegir **Este ordenador** en una ventana remota trae la ventana local al frente. **Gestionar equipos remotos…** abre la ventana Equipos remotos. El selector no añade equipos.

La ventana Equipos remotos es una ventana propia, que abren **Gestionar equipos remotos…** y **Ajustes → Equipos remotos → Gestor**. Se abre con los equipos ya añadidos y mientras tanto lee la configuración de SSH. Cerrarla solo la oculta, así que vuelve a abrirse en el acto; Gitbox la termina tras diez minutos oculta. Muestra, para el equipo elegido, su sistema, la versión de su servidor y su última conexión, que Gitbox registra cada vez que la ventana de ese equipo se conecta.

- **Añadir** — escribe el alias de `ssh_config` del host, o elige una línea de mi configuración de SSH, y, si su carpeta home no permite ejecutar programas, una carpeta del servidor. La ventana lista una línea por cada entrada `Host` de `~/.ssh/config` y de los archivos que incluye, con todos los nombres de la entrada, recortada para que nunca ocupe dos líneas. Elegir una línea rellena su primer nombre, el que va tras `Host`. Una entrada cuyo primer nombre es un patrón o ya está añadido no aparece. Al escribir, las líneas se filtran y se completa el primer alias que empieza por lo escrito: **Tab** o **→** lo aceptan. Gitbox solo lee la configuración de SSH, y comprueba el alias con `ssh -G`, que detecta una configuración de ssh rota pero no un host desconocido.
- **Conectar** — abre el remoto en una ventana propia, con el título `gitbox · <alias>`. Cuando esa ventana está abierta, el botón dice **Ir a su ventana** y la trae al frente. La primera conexión instala `gitbox-server` en el host. Hasta que el servidor responde, la ventana muestra la conexión, con **Desconectar**, **Ver registro** y **Desinstalar servidor**. Cuando se cae la conexión, por ejemplo al cerrar la tapa del portátil, el host sigue con su trabajo y la ventana vuelve a conectarse por sí sola, hasta tres veces, y después muestra lo que terminó mientras tanto (consulta [Cuando la conexión se cae durante una acción larga](remote-mode.md#cuando-la-conexión-se-cae-durante-una-acción-larga)).
- **Registro de conexión** — trae al frente la ventana abierta del equipo con su registro de conexión. El registro vive en esa ventana, así que el botón está desactivado mientras el equipo no tiene ninguna ventana abierta.
- **Desinstalar** — quita el servidor del host, tras una confirmación, y pregunta otra vez cuando otra ventana todavía lo usa.
- **Editor** — el editor en el que este remoto abre las carpetas: **Automático**, o uno de los editores configurados que abren carpetas remotas, como VS Code.
- **Quitar** — olvida el remoto en este ordenador.

En una ventana remota, el onboarding y **Raíces** aceptan una carpeta escrita para el host en lugar del diálogo de carpetas: la línea bajo el campo dice a qué la resuelve el host.

Una vez conectada, la ventana del remoto es la ventana completa de Gitbox, que muestra y cambia el host:

- **Qué funciona** — el estado, **Fetch de todo**, **Fetch**, los detalles del repositorio, las insignias de pull requests, el estado de los espejos, **Buscar proyectos**, las comprobaciones de credenciales y **Comprobación del sistema**, todo ejecutado en el host. También los cambios de la configuración del host: editar, renombrar y borrar cuentas, las raíces, los grupos de espejos, añadir repositorios descubiertos a la configuración, ignorar clones, los ajustes y los arreglos de la configuración de git del host, y reparar o restaurar su `gitbox.json`. Añadir una cuenta y configurar sus credenciales también se ejecutan allí: un token se guarda en el host, y una clave SSH se crea y se queda allí, con su clave pública copiada al portapapeles de este ordenador (consulta [Hosts remotos](credentials.md#hosts-remotos)). Clonar, actualizar, borrar clones y crear repositorios, la limpieza, la configuración de espejos, la adopción y la reconciliación también se ejecutan en el host. Su daemon mantiene en marcha las operaciones largas cuando la conexión se cae. Un enlace de PR se abre en el navegador de este ordenador.
- **Abrir carpetas** — VS Code, un perfil de terminal y un AI harness abren la carpeta en el host: VS Code con su extensión Remote-SSH, y el terminal y el harness en un terminal local que ejecuta `ssh -t` hasta la carpeta. En Windows un perfil de WezTerm o Alacritty abre el terminal en su propia app, y cualquier otro perfil en una consola nueva. Los demás editores aparecen atenuados con «Este editor no puede abrir carpetas en hosts remotos», y con un editor elegido para el remoto, el resto aparece atenuado con «Este remoto se abre en el editor elegido en la ventana Equipos remotos». **Mostrar en Finder** o **Mostrar en el Explorador**, la carpeta de la cuenta y **Abrir en editor** del archivo de configuración aparecen atenuados con «No disponible para hosts remotos».
- **Qué está desactivado** — la vista compacta y el arranque automático están apagados en esta ventana, con el aviso «No disponible en una ventana remota». GCM no se ofrece, porque un host lo rechaza. El idioma, el tema, Simple o Avanzado y la barra lateral son preferencias de este ordenador y siguen funcionando. La ventana local y las ventanas remotas las comparten: un cambio en una aparece en otra la siguiente vez que recupera el foco, y el guardado de ninguna ventana lo deshace.
- **El selector** — en una ventana remota el selector de equipo muestra el alias, el sistema del host (solo en Avanzado) y el estado de la conexión. Su menú también abre el registro de conexión. Un **!** rojo significa que el host envió una lista más larga de lo que la ventana muestra.
- **Reconectar** — cuando la conexión se cae, el último estado se queda en pantalla, atenuado, bajo un aviso **Desconectado de** el host con **Conectar**, que vuelve a conectar, borra cualquier error que dejó la desconexión y recarga todo. Las acciones vuelven cuando los clones del host están reconciliados y su estado actualizado.
- **Enlaces** — un enlace `https` se abre enseguida. Un enlace `http`, como el de una cuenta de Gitea en la red local, se abre tras un diálogo que muestra la dirección completa. Cualquier otro tipo de enlace se rechaza. Esto vale también para la ventana principal.

Antes, `ssh <alias>` tiene que entrar sin pedir nada, y el host necesita un `gitbox.json` con credenciales que funcionen. [Modo remoto](remote-mode.md) explica la configuración, qué se instala y dónde, qué funciona en un remoto, qué hace una conexión caída con un clon, el registro de conexión y cada código de error.

### Notificación de actualización

Gitbox comprueba si hay actualizaciones al arrancar y cada seis horas mientras está abierto. Cuando hay una versión más nueva, aparece una píldora ámbar a la derecha del footer con la nueva versión. Haz clic en ella para descargar y aplicar la actualización en su sitio. Gitbox verifica la firma y el checksum de la release y rechaza una actualización que falle cualquiera de las dos comprobaciones; consulta [Firma de releases](release-signing.md). Reemplaza solo lo que ya está instalado junto a la app (en macOS, el bundle `GitboxApp.app` completo) y se reinicia en la nueva versión. Si la nueva versión no puede arrancar por su cuenta, la píldora lo indica: haz clic en **Salir** y vuelve a abrir la app. La píldora aparece en cada arranque hasta que actualizas; la ✕ la oculta hasta el siguiente arranque.

Si el primer arranque se interrumpe, vuelve a abrir Gitbox. Elige **Volver a la versión anterior** para restaurarla o **Conservar esta versión** después de comprobar que funciona. Gitbox conserva la copia durante un máximo de siete días y comprueba su integridad antes de restaurarla.

La AppImage de Linux solo avisa: al hacer clic en la píldora se abre la página de la release, y tú descargas la nueva AppImage y sustituyes el archivo antiguo.

El actualizador sigue la release que GitHub marca como latest, así que una GUI v1 pasa a v2 de la misma forma.

### Aviso de identidad global

Si tu `~/.gitconfig` tiene un `user.name` o un `user.email` global, aparece un **banner naranja** en la parte superior del dashboard, porque una identidad global puede sobrescribir las identidades por repo que Gitbox configura para cada cuenta. Haz clic en **Quitar** para limpiar esas entradas, o descarta el banner con **✕**.

### Aviso de credential helper global

Cuando al menos una cuenta usa GCM, Gitbox comprueba que tu `~/.gitconfig` global tiene un `credential.helper` que resuelve a Git Credential Manager (`manager`, el heredado `manager-core` o una ruta absoluta al binario `git-credential-manager`) y `credential.credentialStore` fijado para tu SO (`keychain` en macOS, `wincredman` en Windows, `secretservice` en Linux). Si no, un segundo banner naranja nombra el problema. Sin un helper de GCM, el inicio de sesión cae en un prompt de terminal y falla en la app con `fatal: could not read Password ... Device not configured`.

Haz clic en **Configurar** para corregir las dos entradas en un paso. Gitbox también guarda los mismos valores por defecto en `gitbox.json`, así que la comprobación sigue pasando aunque `~/.gitconfig` se edite después. Descarta el banner con **✕** para gestionarlo a mano.

### Aviso de gitignore global

Gitbox muestra un banner cuando falta `~/.gitignore_global`, su bloque recomendado está desactualizado, hay patrones gestionados duplicados fuera del bloque o `core.excludesfile` no está definido. **Instalar** escribe un bloque seleccionado de patrones de basura del SO (`.DS_Store`, `Thumbs.db`, `*~`, …), apunta `core.excludesfile` a él y guarda una copia con marca de tiempo `.bak-YYYYMMDD-HHMMSS` de cualquier archivo existente, conservando las tres últimas. Los patrones y comentarios que añades fuera del bloque gestionado sobreviven a cada reinstalación.

**Ajustes → Gitignore global** activa o desactiva la comprobación automática al arrancar (`global.check_global_gitignore`). El botón **Instalar** siempre funciona, sea cual sea el ajuste. El formato del bloque gestionado está en [Arquitectura › pkg/gitignore](architecture.md#pkggitignore--autorreparación-del-gitignore-global).

## Consejos

- **Posición de la ventana** — Gitbox recuerda el tamaño y la posición de su ventana. Si la ventana fuera a abrirse fuera de la pantalla, por ejemplo después de desconectar un monitor, se centra en tu pantalla principal.
- **Ediciones externas** — cuando editas `gitbox.json` a mano, la app recoge los cambios cuando su ventana recupera el foco.
- **La misma config que v1** — la app lee `~/.config/gitbox/gitbox.json`, el mismo archivo que usaba v1. Las versiones de config anteriores migran a la versión 5 al cargarse, y el siguiente guardado escribe el formato nuevo.
- **Copias de seguridad automáticas** — cada guardado significativo deja una copia fechada como `gitbox-20260401-143025.json` junto a la config, y la pantalla de recuperación restaura cualquiera de ellas con un clic. Consulta [Formato de config](architecture.md#4-formato-de-config-v5).

## Ver también

- [Credenciales](credentials.md) — tipos de credencial, permisos y resolución de problemas
- [Referencia del archivo de config](architecture.md#4-formato-de-config-v5) — cada clave de `gitbox.json`
- [Arquitectura](architecture.md) — diseño técnico
