# Guía de usuario de Gitbox Desktop

[Read in English](../gui-guide.md)

<p align="center">
  <img src="../../assets/screenshot-gui.png" alt="Gitbox GUI" width="800" />
</p>

`GitboxApp` usa la misma configuración que la CLI. Puedes alternar entre GUI, TUI y CLI sin mantener estados separados.

## Requisitos previos

Necesitas `git` instalado. Según lo que uses, también puedes necesitar GCM, `ssh`, `ssh-agent`, WSL, tmuxinator o VS Code. Si algo falla, abre la CLI y ejecuta:

```bash
gitbox doctor
```

## Linux AppImage

En Linux, la AppImage puede requerir permisos de ejecución:

```bash
chmod +x GitboxApp.AppImage
./GitboxApp.AppImage
```

Si quieres integrarla en el menú del escritorio, usa el script de registro incluido en el repo o el paquete de release que corresponda.

## Paso 1: primer arranque

Al abrir `GitboxApp`, elige la carpeta raíz para tus clones. La app crea o reutiliza `~/.config/gitbox/gitbox.json`. Si ya configuraste la CLI, la GUI carga esa misma configuración.

En Ajustes puedes elegir `English` o `Español`. La selección se guarda en `global.language` y afecta a los textos humanos que pasan por el traductor. Los nombres de repos, cuentas, proveedores, comandos y estados internos permanecen sin traducir.

## Paso 2: añadir cuentas

Usa la pantalla de cuentas para añadir GitHub, GitLab, Gitea o Forgejo. Cada cuenta necesita:

- Un `account key` estable, por ejemplo `github-personal`.
- Un proveedor.
- Un usuario u organización principal.
- Un host si el proveedor es self-hosted.
- Un tipo de credencial por defecto: `gcm`, `ssh` o `token`.

Después de guardar, abre el menú de credenciales para preparar y verificar el acceso.

### Configurar credenciales

La GUI agrupa el setup de credenciales por cuenta. Para GCM, puede abrirse un navegador. Para SSH, la app comprueba la clave y el host configurado. Para token, revisa que el PAT exista y tenga permisos suficientes.

Si necesitas entender qué tipo usar, consulta [Credenciales](credentials.md).

## Paso 3: encontrar y añadir proyectos

Usa Discover para consultar el proveedor y seleccionar repos. La vista muestra los repos encontrados y permite añadirlos a la configuración sin editar JSON a mano.

Después de descubrir, usa Clone para traer los repos al disco. La GUI respeta la misma estructura de carpetas que la CLI.

## Paso 4: trabajo diario

### Entender las tarjetas de cuenta

Las tarjetas resumen cada cuenta: número de repos, clones presentes, estado de sincronización y problemas detectados. Selecciona una tarjeta para ver el detalle de sus repos debajo.

### Mantener proyectos sincronizados

La GUI puede comprobar estado, traer cambios y refrescar remotos. `Pull All` usa fast-forward y evita forzar repos con cambios locales o divergencias.

`Fetch All` actualiza referencias remotas sin modificar la rama local. Úsalo cuando quieras revisar estado sin tocar el árbol de trabajo.

El fetch periódico se configura en Ajustes. Mantén un intervalo razonable para no saturar proveedores ni redes privadas.

### Ver detalles

La lista de repos muestra ruta local, remoto, rama y estado. Si algo falla, abre el detalle del repo para ver el error concreto antes de reconfigurar la cuenta.

### Adoptar repos huérfanos

La GUI puede detectar repos que existen en la carpeta raíz pero no aparecen en `gitbox.json`. Si el remoto coincide con una cuenta configurada, puedes adoptarlos. Si hace falta mover carpetas, confirma la ruta antes de aceptar.

### Crear repositorios

Desde la GUI puedes crear repos cuando el proveedor y la credencial lo permiten. Si la acción falla por permisos, revisa el scope del token o la autorización de la organización.

### Editar una cuenta

Edita usuario, host, tipo de credencial y datos SSH desde la pantalla de cuenta. Cambiar una cuenta puede afectar a varios repos, así que verifica credenciales y remotos después.

### Gestionar credenciales

Puedes cambiar el tipo de credencial de una cuenta o eliminar una credencial guardada. Eliminarla no borra el repo local; solo obliga a configurar acceso de nuevo.

## Paso 5: mirrors opcionales

La pestaña Mirrors agrupa repos que se sincronizan entre proveedores.

### Pestañas Accounts y Mirrors

Accounts responde a "qué tengo clonado por cuenta". Mirrors responde a "qué repos se replican entre cuentas". Mantener esas vistas separadas evita mezclar estado local con configuración de sincronización entre proveedores.

### Tarjetas de mirror

Cada tarjeta resume un grupo: origen, destino, número de repos y estado general. Selecciónala para ver los repos del grupo.

### Anillo de salud

El indicador de salud resume mirrors correctos, pendientes o fallidos. Un fallo no implica que todos los repos estén rotos; entra en el detalle para ver cuál necesita acción.

### Acciones de mirror

Puedes descubrir mirrors existentes, añadir repos, ejecutar setup y revisar estado. Las acciones de setup pueden necesitar permisos API además de la credencial usada para clonar.

## Paso 6: workspaces opcionales

Workspaces agrupa repos para abrirlos juntos en VS Code o tmuxinator.

### Descubrimiento al iniciar

La app puede detectar workspaces ya presentes en la carpeta raíz y ofrecer adoptarlos. Esto ayuda cuando alguien deja un `.code-workspace` en disco antes de registrarlo en `gitbox`.

### Tmuxinator en Windows

En Windows, la integración con tmuxinator usa WSL. Si WSL no está disponible, la GUI debe mostrar la limitación en lugar de fallar de forma opaca.

## Vistas del dashboard

La vista completa muestra tarjetas, acciones y listas detalladas. La vista compacta reduce espacio visual para pantallas pequeñas o para mantener `GitboxApp` al lado del editor.

## Ajustes y mantenimiento

El panel de Ajustes controla carpeta raíz, idioma, fetch periódico y comandos externos como editor o terminal.

### Acciones sobre clones

Desde cada repo puedes abrir carpeta, terminal, editor, navegador o harness de IA cuando esté configurado. En Windows, abrir una terminal visible usa un wrapper que evita el flash de consola intermedio.

### Acciones sobre cuentas

Las acciones de cuenta afectan a todos los repos asociados: verificar credenciales, descubrir repos, clonar pendientes, editar o eliminar. Eliminar una cuenta bloqueada por sources o mirrors debe mostrar qué referencia impide borrarla.

### Abrir un perfil específico de Windows Terminal

Configura el comando externo con el perfil que quieras usar. Si el perfil no existe o Windows Terminal no está instalado, la GUI debe mostrar el error del sistema.

### Acciones de harness de IA

Si configuras herramientas de IA, la GUI puede abrir el repo en ese entorno. Estas acciones dependen de comandos externos, por lo que conviene probarlas desde Ajustes antes de usarlas en lote.

### Notificación de actualización

La GUI puede avisar cuando hay una versión nueva. La actualización real depende del sistema operativo, permisos del binario y método de instalación.

### Eliminar repos y cuentas

Eliminar un repo de la configuración no tiene por qué borrar la carpeta local. Revisa el texto de confirmación antes de aceptar. Para cuentas, la app debe impedir el borrado si todavía existen sources, repos o mirrors que la referencian.

### Aviso de identidad global

Si Git tiene `user.name` o `user.email` globales, `gitbox` puede avisar porque esos valores pueden mezclarse con identidades por cuenta. Revisa antes de eliminarlos.

### Aviso de credential helper global

Un helper global incompatible puede interferir con credenciales por cuenta. Usa `doctor` para ver el estado y corrige la configuración global solo si sabes qué flujo quieres conservar.

### Aviso de gitignore global

La GUI puede indicar si falta el bloque recomendado en `~/.gitignore_global`. Instalarlo añade marcadores gestionados y backups, igual que la CLI.

## Consejos

- Verifica credenciales antes de descubrir o clonar muchos repos.
- Usa `Fetch All` cuando quieras revisar cambios sin tocar ramas locales.
- Mantén los `account key` estables; aparecen en rutas, variables de entorno y configuración.
- Usa la CLI para diagnosticar errores complejos con `gitbox doctor` y `gitbox credential verify`.

## Ver también

- [Guía de la CLI](cli-guide.md)
- [Credenciales](credentials.md)
- [Referencia](reference.md)
