#!/bin/bash

# ----------------------------------------------------------------------------------------
# git-config-repos.sh
# ----------------------------------------------------------------------------------------
# Autor: Luis Palacios
# Fecha: 21 de septiembre de 2024
#
# SCRIPT MULTIPLATAFORMA: Probado en Linux, MacOS y Windows con WSL2
#
# Nota previa solo para usuarios de Windows. Este script debe ser ejecutado desde WSL2,
# aunque las modificaciones que hace serán en el "C:\Users\..."
#
# Descripción:
#
# Este script permite configurar repositorios Git de forma automática en tu equipo,
# soportando múltiples cuentas con uno o más proveedores Git (GitHub, GitLab, Gitea, ...),
# y también definiendo con qué método quieres autenticarte en cada cuenta y repositorio.
#
# Soporta dos métodos. El primero es HTTPS + Git Credential Manager, muy útil y recomendado
# en entornos de desktop. El segundo es SSH multicuenta, óptimo para entornos
# "headless", servidores a los que nos conectamos en remoto vía (CLI o VSCode remote).
#
# El script lee un archivo JSON de configuración, que define parámetros globales
# y los específicos de cada cuenta y repositorio:
#
#  - Configura Git globalmente según los parámetros definidos en el archivo JSON.
#  - Clona repositorios si no existen en el sistema local.
#  - Configura las credenciales y parámetros específicos para cada repositorio.
#
# Ejecución:
#
# chmod +x git-config-repos.sh
# ./git-config-repos.sh
#
# Requisitos:
#
# - Git Credential Manager en Linux, MacOS o Windows (se instala en Windows, no en WSL2)
# - Cliente SSH instalado y configurado para autenticación SSH.
# - jq: Es necesario tener instalado jq para parsear el archivo JSON. En Windows este
#   comando debe estar instalado dentro de WSL2
# - Acceso de escritura a los directorios donde se clonarán los repositorios. En Windows,
#   aunque el script se ejecuta en WSL, usará git.exe para que su ejecución sea desde Windows
#   y no desde WSL2, para evitar el problema de lentitud de Git bajo WSL2.
# - Acceso a Internet para clonar los repositorios.
# - Permisos para configurar Git globalmente en el sistema.
#
# Riesgos:
#
# - Este script sobrescribirá configuraciones existentes de Git si los parámetros en el
#   archivo JSON difieren de los actuales. Asegúrese de revisar el archivo JSON antes de
#   ejecutar el script para evitar configuraciones no deseadas.
# - Si hay errores en el archivo JSON, el script puede fallar o no configurar los
#   repositorios correctamente.
#
# ----------------------------------------------------------------------------------------

# Capturo Ctrl-C y hago que llame al a función ctrl_c()
trap ctrl_c INT

# ----------------------------------------------------------------------------------------
# Argumentos de línea de comandos
# ----------------------------------------------------------------------------------------
DRY_RUN=false

usage() {
    cat <<'USAGE'
Uso: git-config-repos.sh [opciones]

Configura repositorios Git de forma automática, soportando múltiples cuentas
y proveedores (GitHub, GitLab, Gitea) con autenticación HTTPS+GCM o SSH.

Opciones:
  -h, --help       Muestra esta ayuda y sale
  -n, --dry-run    Muestra las acciones que se realizarían sin ejecutarlas

Configuración:
  El script lee el archivo JSON de configuración en:
    Linux/macOS/Git Bash: ~/.config/git-config-repos/git-config-repos.json
    WSL2:                 /mnt/c/Users/<user>/.config/git-config-repos/git-config-repos.json

Más información: https://github.com/LuisPalacios/gitbox
USAGE
    exit 0
}

for arg in "$@"; do
    case "$arg" in
    -h | --help)
        usage
        ;;
    -n | --dry-run)
        DRY_RUN=true
        ;;
    *)
        echo "Error: argumento desconocido '$arg'"
        echo "Usa -h o --help para ver las opciones disponibles."
        exit 1
        ;;
    esac
done

# Buffer para mensajes dry-run (se vacía en echo_status)
_dry_run_buffer=()

# Wrapper que respeta --dry-run: ejecuta el comando o solo lo muestra
run() {
    if [ "$DRY_RUN" = true ]; then
        _dry_run_buffer+=("  [dry-run] $*")
    else
        "$@"
    fi
}

if [ "$DRY_RUN" = true ]; then
    echo "=== MODO DRY-RUN: no se ejecutará ningún cambio ==="
    echo
fi

# ----------------------------------------------------------------------------------------
# Detección de plataforma — establece PLATFORM y cmdgit
# PLATFORM: wsl2 | gitbash | macos | linux
# ----------------------------------------------------------------------------------------
PLATFORM="linux"
cmdgit="git"

if [[ -n "${MSYSTEM:-}" ]]; then
    # Git Bash (MSYS2/MinGW): $MSYSTEM = MINGW64, MINGW32 o MSYS
    PLATFORM="gitbash"
elif [[ -r /proc/version ]] && grep -qEi "(Microsoft|WSL)" /proc/version 2>/dev/null; then
    # WSL2
    PLATFORM="wsl2"
    cmdgit="git.exe"
    # Para evitar warnings (cuando llamo a cmd.exe y git.exe) cambio a un
    # directorio windows. Obtengo la ruta USERPROFILE de Windows y elimino
    # el retorno de carro (\r).
    USERPROFILE=$(wslpath "$(cmd.exe /c echo %USERPROFILE% 2>/dev/null | tr -d '\r')")
    cd "$USERPROFILE"
elif [[ "$OSTYPE" == darwin* ]]; then
    # macOS
    PLATFORM="macos"
fi
# else: Linux nativo — valores por defecto ya establecidos

# En Git Bash, jq.exe (Windows) produce salidas con \r (CRLF).
# Envolver jq para limpiar \r automáticamente y evitar que las variables
# capturen caracteres basura que corrompen la salida y las consultas.
if [[ "$PLATFORM" == "gitbash" ]]; then
    jq() {
        local output rc
        output=$(command jq "$@")
        rc=$?
        [ -n "$output" ] && printf '%s\n' "$output" | tr -d '\r'
        return $rc
    }
fi

# ----------------------------------------------------------------------------------------
# Variables Globales
# ----------------------------------------------------------------------------------------
credential_ssh="false"
credential_gcm="true"

# ----------------------------------------------------------------------------------------
# Mostrar mensajes bonitos
# ----------------------------------------------------------------------------------------

# Colores para los menasjes de estado
COLOR_GREEN=$(tput setaf 2)
COLOR_YELLOW=$(tput setaf 3)
COLOR_RED=$(tput setaf 1)

# Ancho de la terminal
width=$(tput cols)
message_len=0

# Función para imprimir un mensaje
echo_message() {
    local message=$1
    message_len=${#message}
    printf "%s " "$message"
}

# Función para imprimir un mensaje de estado (OK, WARNING, ERROR) alineado a la derecha
echo_status() {
    local status=$1
    local status_msg
    local status_color

    case $status in
    ok)
        status_msg="OK"
        status_color=${COLOR_GREEN}
        ;;
    warning)
        status_msg="WARNING"
        status_color=${COLOR_YELLOW}
        ;;
    created)
        status_msg="CREADA"
        status_color=${COLOR_YELLOW}
        ;;
    error)
        status_msg="ERROR"
        status_color=${COLOR_RED}
        ;;
    *)
        status_msg="UNKNOWN"
        status_color=${COLOR_RED}
        ;;
    esac

    local status_len=${#status_msg}
    local spaces=$((width - message_len - status_len - 2))

    printf "%${spaces}s" "["
    printf "${status_color}${status_msg}\e[0m"
    echo "]"

    # Vaciar buffer de mensajes dry-run después del status
    if [ ${#_dry_run_buffer[@]} -gt 0 ]; then
        for _msg in "${_dry_run_buffer[@]}"; do
            echo "$_msg"
        done
        _dry_run_buffer=()
    fi
}

# ----------------------------------------------------------------------------------------
# Funciones de utilidad
# ----------------------------------------------------------------------------------------

# Esto se ejecuta cuando pulsan CTRL-C
function ctrl_c() {
    echo "** Abortado por CTRL-C"
    exit
}

# Función para convertir una ruta de WSL a una ruta de Windows
# Si no se puede convertir se sale del programa porque se considera
# un error en la configuración del archivo JSON y es grave
convert_wsl_to_windows_path() {
    local wsl_path="$1"

    # Comprobar si el path empieza con /mnt/
    if [[ "$wsl_path" =~ ^/mnt/([a-zA-Z])/ ]]; then
        # Extraer la letra de la unidad
        local drive_letter=$(echo "${BASH_REMATCH[1]}" | tr '[:lower:]' '[:upper:]')

        # Remover el prefijo /mnt/<unidad>/
        local path_without_prefix="${wsl_path#/mnt/${BASH_REMATCH[1]}/}"

        # Reemplazar las barras inclinadas (/) con barras invertidas (\)
        local windows_path=$(echo "$path_without_prefix" | sed 's|/|\\|g')

        # Formar la ruta final en el formato de Windows
        echo "${drive_letter}:\\${windows_path}"
    else
        echo "Error: La ruta $global_folder no está en el formato esperado de WSL2."
        echo "Revisa global.folder en el .json, asegúrate de que comience con /mnt/<unidad>/"
        exit 1
    fi
}

# Extraer protocolo://host de una URL (sin sed, puro bash)
url_credential_base() {
    local url="$1"
    local proto="${url%%://*}"
    local rest="${url#*://}"
    local host="${rest%%/*}"
    echo "${proto}://${host}"
}

# Cache de credenciales de Windows Credential Manager (se llena una sola vez)
_wcm_cache=""
_wcm_cache_loaded=false

# Cargar la lista de credenciales de Windows Credential Manager (una sola vez)
wcm_load_cache() {
    if [ "$_wcm_cache_loaded" = true ]; then
        return
    fi
    case "$PLATFORM" in
    wsl2)
        _wcm_cache=$(cmd.exe /c "cmdkey /list" < /dev/null | tr -d '\r')
        ;;
    gitbash)
        # MSYS_NO_PATHCONV=1 evita que MSYS2 convierta /list a una ruta de fichero
        _wcm_cache=$(MSYS_NO_PATHCONV=1 cmd.exe /c "cmdkey /list" < /dev/null 2>/dev/null | tr -d '\r')
        ;;
    esac
    _wcm_cache_loaded=true
}

# Buscar credenciales en el cache de Windows Credential Manager
wcm_search() {
    local target="$1"
    local user="$2"

    wcm_load_cache

    # Buscar el bloque que contiene el target y el usuario
    local match
    match=$(echo "$_wcm_cache" | awk -v tgt="$target" -v usr="$user" '
        $0 ~ "Target:" && $0 ~ tgt {found_tgt=1}
        found_tgt && $0 ~ "User:" && $0 ~ usr {found_usr=1}
        found_tgt && found_usr {print_block=1}
        print_block && $0 ~ /^$/ {exit}
        print_block {print}
    ')

    # Comprobar si se encontró el bloque
    [ -n "$match" ]
}

# Function to check if a credential is stored in the credential manager
check_credential_in_store() {
    local service_url="$1"
    local username="$2"

    case "$PLATFORM" in
    macos)
        # macOS Keychain
        security find-generic-password -s "git:${service_url}" -a "${username}" &>/dev/null
        return $?
        ;;
    wsl2 | gitbash)
        # Windows Credential Manager (cmdkey, cacheado — una sola llamada para todas las cuentas)
        wcm_search "git:${service_url}" "${username}"
        return $?
        ;;
    *)
        # Linux nativo — Secret Service
        local output
        output=$(secret-tool search service "git:${service_url}" account "${username}" 2>/dev/null)
        local line_count
        line_count=$(printf '%s' "$output" | wc -l)
        if [ "$line_count" -gt 1 ]; then
            return 0
        fi
        return 1
        ;;
    esac
}

# ----------------------------------------------------------------------------------------
# Dependencias del Script
# ----------------------------------------------------------------------------------------

# Comprobar si las dependencias necesarias están instaladas
check_installed_programs() {
    local programs=("jq")

    case "$PLATFORM" in
    wsl2)
        programs+=("wslpath" "cmd.exe" "git.exe")
        [[ "$credential_gcm" == "true" ]] && programs+=("git-credential-manager.exe")
        ;;
    *)
        programs+=("git")
        [[ "$credential_gcm" == "true" ]] && programs+=("git-credential-manager")
        ;;
    esac

    for program in "${programs[@]}"; do
        if ! command -v "$program" &>/dev/null; then
            echo
            echo "Error: '$program' no está instalado o no está en el PATH."
            echo
            echo "Instala las dependencias necesarias:"
            echo

            case "$PLATFORM" in
            wsl2)
                echo " WSL2 (Ubuntu/Debian):"
                echo "   sudo apt update && sudo apt install -y jq"
                echo
                echo "   Para cmd.exe y git.exe instala Git for Windows en el host Windows:"
                echo "   https://git-scm.com/download/win"
                echo "   Añade al PATH de WSL2 (~/.bashrc o ~/.profile):"
                echo "     export PATH=\"\$PATH:/mnt/c/Windows/System32\""
                echo "     export PATH=\"\$PATH:/mnt/c/Program Files/Git/mingw64/bin\""
                echo
                echo "   Para git-credential-manager.exe instala GCM para Windows:"
                echo "   https://github.com/git-ecosystem/git-credential-manager/releases"
                ;;
            gitbash)
                echo " Git Bash (Windows):"
                echo "   git y bash se incluyen con Git for Windows:"
                echo "   https://git-scm.com/download/win"
                echo
                echo "   Para jq, descarga jq.exe y colócalo en un directorio del PATH:"
                echo "   https://jqlang.github.io/jq/download/"
                echo
                echo "   git-credential-manager se incluye con Git for Windows >= 2.39:"
                echo "   https://github.com/git-ecosystem/git-credential-manager/releases"
                ;;
            macos)
                echo " macOS:"
                echo "   brew update && brew upgrade"
                echo "   brew install git jq"
                echo "   brew install --cask git-credential-manager"
                ;;
            *)
                echo " Linux:"
                echo "   sudo apt update && sudo apt install -y git jq"
                echo
                echo "   Para git-credential-manager:"
                echo "   https://github.com/git-ecosystem/git-credential-manager/releases"
                echo "   Ejemplo: sudo dpkg -i gcm-linux_amd64.2.5.1.deb"
                ;;
            esac

            echo
            exit 1
        fi
    done
}

# ----------------------------------------------------------------------------------------
# Main Script Execution
# ----------------------------------------------------------------------------------------

# Fichero de configuración JSON
case "$PLATFORM" in
wsl2)
    git_config_repos_json_file="${USERPROFILE}/.config/git-config-repos/git-config-repos.json"
    ;;
*)
    git_config_repos_json_file="${HOME}/.config/git-config-repos/git-config-repos.json"
    ;;
esac
git_command="$cmdgit"
echo_message "* Config $git_config_repos_json_file"
if [ ! -f "$git_config_repos_json_file" ]; then
    echo_status error
    echo "ERROR: El archivo de configuración $git_config_repos_json_file no existe."
    exit 1
fi

# Validar el archivo JSON con jq
jq '.' "$git_config_repos_json_file" >/dev/null 2>&1
if [ $? -ne 0 ]; then
    echo_status error
    echo "ERROR: El archivo JSON $git_config_repos_json_file contiene errores de sintaxis."
    exit 1
fi
echo_status ok

# ----------------------------------------------------------------------------------------
# Carga masiva de datos del JSON (3 llamadas a jq en lugar de ~50+)
# Los datos se almacenan como cadenas TSV y se iteran con "while read".
# Compatible con Bash 3.2+ (macOS) — no usa arrays asociativos.
# ----------------------------------------------------------------------------------------

# 1) Configuración global (1 llamada a jq)
IFS=$'\t' read -r global_folder credential_ssh ssh_folder credential_gcm credential_helper credential_store < <(
    jq -r '[
        (.global.folder // "null"),
        (.global.credential_ssh.enabled // "false"),
        (.global.credential_ssh.ssh_folder // "null"),
        (.global.credential_gcm.enabled // "true"),
        (.global.credential_gcm.helper // "null"),
        (.global.credential_gcm.credentialStore // "null")
    ] | @tsv' "$git_config_repos_json_file"
)

# En WSL2, ~ debe expandir al USERPROFILE de Windows (/mnt/c/Users/<user>/),
# no al HOME de Linux (/home/<user>/), porque usamos git.exe y rutas Windows.
if [[ "$PLATFORM" == "wsl2" ]]; then
    global_folder="${global_folder/#\~/$USERPROFILE}"
    ssh_folder="${ssh_folder/#\~/$USERPROFILE}"
else
    global_folder="${global_folder/#\~/$HOME}"
    ssh_folder="${ssh_folder/#\~/$HOME}"
fi

# 2) Datos de cuentas — TSV: key url username folder name email gcm_provider gcm_useHttpPath ssh_host ssh_hostname ssh_type
_acct_data=$(jq -r '.accounts | to_entries | sort_by(.key)[] | [
    .key,
    (.value.url // "null"),
    (.value.username // "null"),
    (.value.folder // "null"),
    (.value.name // "null"),
    (.value.email // "null"),
    (.value.gcm_provider // "null"),
    (.value.gcm_useHttpPath // "null"),
    (.value.ssh_host // "null"),
    (.value.ssh_hostname // "null"),
    (.value.ssh_type // "null")
] | @tsv' "$git_config_repos_json_file")

# 3) Datos de repositorios — TSV: account_key repo_key credential_type name email folder
_repo_data=$(jq -r '.accounts | to_entries | sort_by(.key)[] | .key as $acc |
    (.value.repos // {} | to_entries | sort_by(.key)[]) | [
        $acc,
        .key,
        (.value.credential_type // "null"),
        (.value.name // "null"),
        (.value.email // "null"),
        (.value.folder // "null")
    ] | @tsv' "$git_config_repos_json_file")

# Precalcular qué cuentas tienen repos con GCM (cadena con delimitadores para búsqueda bash puro)
_gcm_accounts="|"
while IFS=$'\t' read -r _acc _repo _ctype _rname _remail _rfolder; do
    [[ -z "$_acc" ]] && continue
    [[ "$_ctype" == "gcm" && "$_gcm_accounts" != *"|${_acc}|"* ]] && _gcm_accounts+="|${_acc}|"
done <<< "$_repo_data"

#
# COMPROBAR PROGRAMAS INSTALADOS
#
check_installed_programs

# DIRECTORIO GLOBAL
# Crear el directorio global de Git
echo_message "Directorio Git: $global_folder"
run mkdir -p "$global_folder" &>/dev/null
if [ $? -ne 0 ] && [ "$DRY_RUN" = false ]; then
    echo_status error
    echo "ERROR: No se ha podido crear $global_folder."
    exit 1
fi
echo_status ok

# DIRECTORIO SSH
if [ "$credential_ssh" == "true" ]; then
    echo_message "Directorio SSH: $ssh_folder"
    run mkdir -p "$ssh_folder" &>/dev/null
    if [ $? -ne 0 ] && [ "$DRY_RUN" = false ]; then
        echo_status error
        echo "ERROR: No se ha podido crear $ssh_folder."
        exit 1
    fi
    echo_status ok
fi

# CONFIGURACIÓN GLOBAL de GCM
# --
# Para HTTPS + Git Credential Manager
if [ "$credential_gcm" == "true" ]; then

    echo_message "Configuración de Git global"
    run $git_command config --global --replace-all credential.helper "$credential_helper"
    run $git_command config --global credential.credentialStore "$credential_store"
    while IFS=$'\t' read -r _key _url _username _folder _name _email _gcm_provider _gcm_useHttpPath _ssh_host _ssh_hostname _ssh_type; do
        [[ -z "$_key" ]] && continue
        account_credential_url=$(url_credential_base "$_url")
        # Configurar las credenciales globales para la cuenta (solo si el campo existe en el JSON)
        [[ "$_gcm_provider" != "null" ]] && \
            run $git_command config --global credential."$account_credential_url".provider "$_gcm_provider"
        [[ "$_gcm_useHttpPath" != "null" ]] && \
            run $git_command config --global credential."$account_credential_url".useHttpPath "$_gcm_useHttpPath"
    done <<< "$_acct_data"
    echo_status ok

    # CREDENCIALES
    # Iterar sobre las cuentas para los CREDENCIALES de GCM
    # En esta seccion se configuran las credenciales en el almacen de credenciales, mediante
    # el proceso de ir a la URL de la cuenta y autenticarse con el navegador
    # Solo procesar cuentas que tienen al menos un repositorio usando GCM
    while IFS=$'\t' read -r _key _url _username _folder _name _email _gcm_provider _gcm_useHttpPath _ssh_host _ssh_hostname _ssh_type; do
        [[ -z "$_key" ]] && continue
        # Verificar si esta cuenta tiene repositorios que usan GCM
        if [[ "$_gcm_accounts" == *"|${_key}|"* ]]; then
            account_credential_url=$(url_credential_base "$_url")

            # Avisar al usuario para que prepare el navegador para que se autentique
            echo_message "Comprobando credenciales de $_key > $_username"
            check_credential_in_store "$account_credential_url" "$_username"
            if [ $? -eq 0 ]; then
                echo_status ok
            elif [ "$DRY_RUN" = true ]; then
                echo_status warning
                echo "  [dry-run] Solicitaría autenticación de $_key > $_username"
            else
                echo_status warning
                read -p "Preapara tu navegador para autenticar a $_key > $_username - (Enter/Ctrl-C)." confirm
                credenciales="/tmp/tmp-credenciales"
                (
                    echo url="$account_credential_url"
                    echo "username=$_username"
                    echo
                ) | $git_command credential fill >$credenciales 2>/dev/null
                if [ -f $credenciales ] && [ ! -s $credenciales ]; then
                    # No deberia entrar por aquí
                    echo "    Ya se habían configurado las credenciales en el pasado"
                    echo "$_key/$_username ya tiene los credenciales configurados en el almacen"
                else
                    echo_message "    Añado las credenciales al almacen de credenciales"
                    cat $credenciales | $git_command credential approve
                    echo_status ok
                fi
            fi
        else
            # Esta cuenta no tiene repositorios usando GCM, saltarla
            echo_message "Saltando $_key (solo usa SSH)"
            echo_status ok
        fi
    done <<< "$_acct_data"
fi

# CONFIGURACIÓN GLOBAL de SSH
# --
# Para SSH configuro las Keys
if [ "$credential_ssh" == "true" ]; then

    ssh_config_file="$ssh_folder/config"
    git_config_comment="# Configuración para git-config-repos.sh"
    git_include_line="Include $ssh_folder/git-config-repos"
    ssh_config_file_git_config_repos="$ssh_folder/git-config-repos"

    # Comprueba si las líneas ya existen
    echo_message "Configuración Hosts SSH en $ssh_config_file"
    if ! grep -qF "$git_config_comment" "$ssh_config_file" && ! grep -qF "$git_include_line" "$ssh_config_file"; then
        if [ "$DRY_RUN" = true ]; then
            echo_status warning
            echo "  [dry-run] Añadiría Include en $ssh_config_file"
        else
            # Añade las líneas al principio del archivo
            {
                echo ""
                echo "$git_config_comment"
                echo "$git_include_line"
                echo ""
                cat "$ssh_config_file"
            } > "$ssh_config_file.tmp" && mv "$ssh_config_file.tmp" "$ssh_config_file"
            echo_status created
        fi
    else
        echo_status ok
    fi

    # Verificar y/o crear las KEYS por cuenta, añadir al ssh-agent y configurar el archivo de configuración
    # de SSH para que use las claves SSH correctas
    if [ "$DRY_RUN" = true ]; then
        echo "  [dry-run] Regeneraría $ssh_config_file_git_config_repos"
    else
        echo "# Configuración para git-config-repos.sh" > "$ssh_config_file_git_config_repos"
    fi
    while IFS=$'\t' read -r _key _url _username _folder _name _email _gcm_provider _gcm_useHttpPath _ssh_host _ssh_hostname _ssh_type; do
        [[ -z "$_key" ]] && continue
        echo_message "Claves SSH para $_key"

        if [ "$_ssh_host" == "" ] || [ "$_ssh_host" == "null" ]; then
            echo "Error: La cuenta $_key no tiene definida el prefijo del nombre de clave SSH (ssh_host)."
            echo_status error
            exit 1
        fi
        if [ "$_ssh_hostname" == "" ] || [ "$_ssh_hostname" == "null" ]; then
            echo "Error: La cuenta $_key no tiene definida el Hostname SSH (ssh_hostname)."
            echo_status error
            exit 1
        fi
        if [ "$_ssh_type" == "" ] || [ "$_ssh_type" == "null" ]; then
            echo_status error
            echo "Error: La cuenta $_key no tiene definido el tipo de clave SSH (ssh_type)."
            exit 1
        fi
        account_ssh_key="$ssh_folder/$_ssh_host-sshkey"

        # Comprobar si la clave SSH existe, si no, crearla
        if [ ! -f "$account_ssh_key" ] || [ ! -f "$account_ssh_key.pub" ]; then
            if [ "$DRY_RUN" = true ]; then
                echo_status warning
                echo "  [dry-run] Generaría clave SSH $account_ssh_key ($_ssh_type)"
            else
                account_ssh_comment="$(whoami)@$(hostname) $_username $_url"
                if $(echo "y" | ssh-keygen -q -t "$_ssh_type" -f "$account_ssh_key" -C "$account_ssh_comment" -N "" &>/dev/null); then
                    echo_status created
                else
                    echo_status error
                    echo "Error creando la clave SSH para $_key" >&2
                    exit 1
                fi
            fi
        else
            echo_status ok
        fi

        # Añadir el host al fichero de configuración SSH git-config-repos
        echo_message "Configuración Host SSH para $_key"
        if [ "$DRY_RUN" = true ]; then
            echo_status ok
            echo "  [dry-run] Añadiría Host $_ssh_host → $_ssh_hostname en $ssh_config_file_git_config_repos"
        else
            echo "Host $_ssh_host" >>"$ssh_config_file_git_config_repos"
            echo "    HostName $_ssh_hostname" >>"$ssh_config_file_git_config_repos"
            echo "    User git" >>"$ssh_config_file_git_config_repos"
            echo "    IdentityFile $account_ssh_key" >>"$ssh_config_file_git_config_repos"
            echo "    IdentitiesOnly yes" >>"$ssh_config_file_git_config_repos"
            echo_status ok
        fi
    done <<< "$_acct_data"

    # Añadirlas al ssh-agent
    if [ "$DRY_RUN" = true ]; then
        echo "  [dry-run] Recargaría claves SSH en ssh-agent"
    else
        ssh-add -D &>/dev/null
        while IFS=$'\t' read -r _key _url _username _folder _name _email _gcm_provider _gcm_useHttpPath _ssh_host _ssh_hostname _ssh_type; do
            [[ -z "$_key" ]] && continue
            account_ssh_key="$ssh_folder/$_ssh_host-sshkey"
            echo_message "Añadiendo clave SSH a ssh-agent para $_key"
            ssh-add "$account_ssh_key" &>/dev/null
            echo_status ok
        done <<< "$_acct_data"
    fi
fi

# REPOSITORIOS
# Iterar sobre las cuentas para los REPOSITORIOS
# En esta sección se clonan los repositorios y se configuran los repositorios locales
while IFS=$'\t' read -r _key _url _username _folder _name _email _gcm_provider _gcm_useHttpPath _ssh_host _ssh_hostname _ssh_type; do
    [[ -z "$_key" ]] && continue

    account="$_key"
    account_url="$_url"
    account_username="$_username"
    account_folder="$_folder"
    account_user_name="$_name"
    account_user_email="$_email"

    # Crear el directorio para la cuenta
    echo_message "  $account_folder"
    run mkdir -p "$global_folder/$account_folder"
    if [ $? -ne 0 ] && [ "$DRY_RUN" = false ]; then
        echo_status error
        echo "ERROR: No se ha podido crear $global_folder/$account_folder."
        exit 1
    fi
    echo_status ok

    # Iterar sobre los repositorios de la cuenta
    while IFS=$'\t' read -r _racc _repo _cred_type _rname _remail _rfolder; do
        [[ -z "$_racc" ]] && continue
        [[ "$_racc" != "$account" ]] && continue

        # Si tengo nombre y email del usuario a nivel de repositorio, los pillo
        if [ "$_rname" != "" ] && [ "$_rname" != "null" ]; then
            account_user_name="$_rname"
        else
            account_user_name="$_name"
        fi
        if [ "$_remail" != "" ] && [ "$_remail" != "null" ]; then
            account_user_email="$_remail"
        else
            account_user_email="$_email"
        fi

        # Averiguo el tipo de credencial para el repositorio
        repo_credential_type="$_cred_type"

        # Preparo variables si el repo tiene credenciales vía SSH
        if [[ "${repo_credential_type}" == "ssh" ]]; then
            ssh_host="$_ssh_host"
            account_ssh_key="$ssh_folder/$ssh_host-sshkey"
            # Extraigo el nombre de la cuenta de la URL
            git_account_user="${account_url##*/}"
            # Reconstruyo la URL de clonación usando el ssh_host y el usuario
            account_clone_url="$ssh_host:$git_account_user"
            remote_origin_url=$account_clone_url
        elif [[ "${repo_credential_type}" == "gcm" ]]; then
            # Preparo variables si la cuenta tiene credenciales vía GCM
            account_clone_url="https://${account_username}@${account_url#https://}"
            account_credential_url=$(url_credential_base "$account_url")
            remote_origin_url=$account_url
        fi

        # Construyo la ruta del repositorio
        repo_folder="$_rfolder"
        if [ "$repo_folder" != "" ] && [ "$repo_folder" != "null" ]; then
            # Expandir ~ al HOME (o USERPROFILE en WSL2)
            if [[ "$PLATFORM" == "wsl2" ]]; then
                repo_folder="${repo_folder/#\~/$USERPROFILE}"
            else
                repo_folder="${repo_folder/#\~/$HOME}"
            fi
            if [ "${repo_folder:0:1}" == "/" ]; then
                repo_path="$repo_folder"
            else
                repo_path="$global_folder/$account_folder/$repo_folder"
            fi
        else
            repo_path="$global_folder/$account_folder/$_repo"
        fi

        # Si el repositorio no existe, clonarlo
        if [ ! -d "$repo_path" ]; then
            echo "   - $repo_path"
            echo_message "    ⬇ $repo_path"

            # En WSL2 convertir la ruta de destino del clone a formato C:\.. para git.exe
            case "$PLATFORM" in
            wsl2)
                destination_directory=$(convert_wsl_to_windows_path "$repo_path")
                ;;
            *)
                destination_directory="$repo_path"
                ;;
            esac

            # Clonar el repo
            run $git_command clone "$account_clone_url/$_repo.git" "$destination_directory" &>/dev/null
            if [ $? -eq 0 ] || [ "$DRY_RUN" = true ]; then
                echo_status ok
            else
                echo_status error
                continue
            fi

        else
            echo_message "   - $repo_path"
            echo_status ok
        fi

        # Configurar el repositorio local (.git/config)
        if [ "$DRY_RUN" = true ]; then
            if [ -d "$repo_path" ]; then
                echo "  [dry-run] Configuraría $repo_path (remote, user.name, user.email)"
            else
                echo "  [dry-run] Configuraría $repo_path tras clonarlo"
            fi
        else
            cd "$repo_path" || continue
            $git_command remote set-url origin "$remote_origin_url/$_repo.git"
            $git_command remote set-url --push origin "$remote_origin_url/$_repo.git"
            if [ "$account_user_name" != "" ] && [ "$account_user_name" != "null" ]; then
                $git_command config user.name "$account_user_name"
            fi
            if [ "$account_user_email" != "" ] && [ "$account_user_email" != "null" ]; then
                $git_command config user.email "$account_user_email"
            fi
            if [ "$credential_gcm" == "true" ]; then
                $git_command config credential."$account_credential_url".username "$account_username"
            fi
        fi

    done <<< "$_repo_data"
done <<< "$_acct_data"

# ----------------------------------------------------------------------------------------
