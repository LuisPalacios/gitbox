#!/usr/bin/env bash

# ----------------------------------------------------------------------------------------
# git-status-pull.sh
# ----------------------------------------------------------------------------------------
# Autor: Luis Palacios
# Fecha: 3 de octubre de 2024
#

# Este script verifica el estado de múltiples repositorios Git a partir del
# directorio actual. Su objetivo es informar al usuario sobre qué repositorios
# necesitan un pull para estar sincronizados con su upstream. Además, el script
# puede hacer pull automáticamente si se proporciona el argumento "pull".
#
# También es capaz de proporcionar información detallada sobre cada repositorio,
# cuando no se puede hacer pull automáticamente, informando de la razón por la que
# el repositorio no está limpio y necesita ser revisado. Soporta un modo verbose
# (-v) para dar dicha informacion más detallada
#

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
elif [[ "$OSTYPE" == darwin* ]]; then
    # macOS
    PLATFORM="macos"
fi
# else: Linux nativo — valores por defecto ya establecidos

#
# Variables globales
verbose_output=()
pull=false # Se establece en true si se usa el comando "pull"
prg=$(basename $0)
evaluated_repos=() # Lista de repositorios ya evaluados

# ----------------------------------------------------------------------------------------
# Mostrar mensajes en color y justificados a la derecha
# ----------------------------------------------------------------------------------------

# Si estoy bajo GitHub Actions no se usan colores, ya que el output es en texto plano
if [ -z "$GITHUB_ACTIONS" ]; then
    COLOR_RED=$(tput setaf 1)
    COLOR_GREEN=$(tput setaf 2)
    COLOR_YELLOW=$(tput setaf 3)
    COLOR_PURPLE=$(tput setaf 5)
    COLOR_CYAN=$(tput setaf 6)
    COLOR_RESET=$(tput sgr0)
    width=$(tput cols)
    message_len=0
else
    COLOR_GREEN=""
    COLOR_YELLOW=""
    COLOR_RED=""
    COLOR_PURPLE=""
    COLOR_CYAN=""
    COLOR_RESET=""
    width=0
    message_len=0
fi

# Función para imprimir un mensaje
echo_message() {
    local message=$1
    if [ -z "$GITHUB_ACTIONS" ]; then
        message_len=${#message}
        printf "%s " "$message"
    else
        echo "$message"
    fi
}

# Función para crear una cadena con un número especificado de espacios
generate_chars() {
    local char=$1
    local num_chars=$2
    local theString=""

    for ((i = 0; i < num_chars; i++)); do
        theString+="${char}"
    done
    echo "$theString"
}

# Función para imprimir un mensaje de estado personalizado justificado a la derecha
echo_custom_msg() {
    initial_msg=$1
    right_msg=$2
    right_color=$3

    local initial_message_len=${#initial_msg}
    local right_msg_len=${#right_msg}
    local spaces=$((40 - initial_message_len - right_msg_len - 2))
    spaces_literal=$(generate_chars " " "$spaces")
    verbose_output+=("${initial_msg}${spaces_literal}${right_color}${right_msg}${COLOR_RESET}")
}

# Función para imprimir la salida detallada acumulada
print_verbose_output() {
    for line in "${verbose_output[@]}"; do
        echo -e "$line"
    done
}

# Función para imprimir un mensaje de estado justificado a la derecha
echo_status() {
    local status=$1
    local status_msg
    local status_color

    case $status in
    clean)
        status_msg="LIMPIO"
        status_color=${COLOR_RESET}
        ;;
    clean_behind_main)
        status_msg="LIMPIO PERO ATRASADA RESPECTO A LA RAMA PRINCIPAL"
        status_color=${COLOR_YELLOW}
        ;;
    needspull)
        status_msg="NECESITA PULL"
        status_color=${COLOR_GREEN}
        ;;
    pull)
        status_msg="HACIENDO EL PULL DE ESTE REPOSITORIO"
        status_color=${COLOR_PURPLE}
        ;;
    review)
        status_msg="DEBES REVISARLO ANTES DE HACER PULL"
        status_color=${COLOR_YELLOW}
        ;;
    error)
        status_msg="ERROR"
        status_color=${COLOR_RED}
        ;;
    *)
        status_msg="DESCONOCIDO"
        status_color=${COLOR_RED}
        ;;
    esac

    local status_len=${#status_msg}
    local spaces=$((width - message_len - status_len - 3))

    if [ -z "$GITHUB_ACTIONS" ]; then
        if [ "$verbose" = "true" ] || [ "$status" != "clean" ]; then
            spaces_literal=$(generate_chars "-" "$spaces")
        else
            spaces_literal=$(generate_chars " " "$spaces")
        fi
        echo "${spaces_literal}[${status_color}${status_msg}${COLOR_RESET}]"
    else
        echo "  Acción: [$status_msg]"
    fi
}

# ----------------------------------------------------------------------------------------
# Funciones Utilitarias
# ----------------------------------------------------------------------------------------

# Función para mostrar el uso
show_usage() {
    echo "${prg} - LuisPa, 2024."
    echo "Uso: ${prg} [-v] [-h] [pull]"
    echo "  -v         Habilitar salida detallada"
    echo "  -h         Mostrar este mensaje de ayuda"
    echo "  pull       Hacer pull de los cambios automáticamente (si es seguro)"
    exit 0
}

# Función para verificar si un repositorio está dentro de otro ya evaluado
is_repo_inside_another() {
    local repo_path=$1

    for evaluated_repo in "${evaluated_repos[@]}"; do
        if [[ "$repo_path" == "$evaluated_repo"* ]]; then
            return 0 # Si el repositorio está dentro de otro, retornar true
        fi
    done
    return 1 # Si no está dentro de otro, retornar false
}

# Función para verificar si la rama actual está por detrás de main o master
# Devuelve 0 si está por detrás, 1 si no lo está
check_if_behind_main() {
    local current_branch=$1

    # Verificar si la rama actual no es main o master
    if [[ "$current_branch" != "main" && "$current_branch" != "master" ]]; then
        for main_branch in "main" "master"; do
            if $cmdgit show-ref --verify --quiet refs/heads/$main_branch; then
                # Obtener la fecha del último commit en la rama 'main' o 'master'
                MAIN_COMMIT_DATE=$($cmdgit log -1 --format=%ct "origin/$main_branch")
                # Obtener la fecha del último commit en la rama actual
                BRANCH_COMMIT_DATE=$($cmdgit log -1 --format=%ct "$current_branch")
                # Comparar las fechas
                if [ "$BRANCH_COMMIT_DATE" -lt "$MAIN_COMMIT_DATE" ]; then
                    #echo "El último commit en la rama 'feature' es más antiguo que el de 'main'."
                    return 0
                fi
            fi
        done
    fi
    return 1
}

# Función para verificar el estado de un repositorio git
check_git_status() {
    local repo_path=$1
    local behind=0
    local ahead=0
    local diverged=0
    local stashed=0
    local staged=0
    local untracked=0
    local modified=0
    local moved=0
    local pending_push=0
    local behind_main=0

    # Verificar si el repositorio ya está evaluado o si está dentro de otro
    if is_repo_inside_another "$repo_path"; then
        return
    fi

    # Añadir el repositorio a la lista de evaluados
    evaluated_repos+=("$repo_path")

    # Guardar el directorio actual
    local current_dir=$(pwd)

    # Moverse al repositorio
    cd "$repo_path"

    # Imprimir la ruta del repositorio
    if [[ "$repo_path" == "." ]]; then
        echo_message "$(basename "$PWD")"
    else
        echo_message "$repo_path"
    fi

    # Asegurarse de que el array esté vacío
    verbose_output=()

    # Verificar si la rama tiene un upstream configurado
    local upstream=$($cmdgit rev-parse --symbolic-full-name --abbrev-ref "@{u}" 2>/dev/null)
    if [ -z "$upstream" ]; then
        echo_custom_msg "  Upstream:" "Falta Upstream" "${COLOR_RED}"
        cd "$current_dir"
        return
    fi

    # Hacer fetch para saber si estamos atrasados
    $cmdgit fetch origin --quiet

    # Nombre de la rama
    branch_name=$($cmdgit rev-parse --abbrev-ref HEAD)
    if $verbose; then
        echo_custom_msg "  Rama:" "$branch_name" "${COLOR_CYAN}"
    fi

    # Información de Adelantado/Atrasado
    ahead=$($cmdgit rev-list --count @{u}..HEAD 2>/dev/null || echo "0")
    behind=$($cmdgit rev-list --count HEAD..@{u} 2>/dev/null || echo "0")
    if $verbose; then
        if [ "$ahead" -ne 0 ]; then
            echo_custom_msg "  Commits adelantados:" "$ahead" "${COLOR_RED}"
        fi
        if [ "$behind" -ne 0 ]; then
            echo_custom_msg "  Commits por detrás:" "$behind" "${COLOR_GREEN}"
        fi
    fi

    # Verificar si hay divergencia
    diverged=0
    if [ "$ahead" -ne 0 ] && [ "$behind" -ne 0 ]; then
        diverged=1
        if $verbose; then
            echo_custom_msg "  Divergencia:" "sí" "${COLOR_RED}"
        fi
    fi

    # Verificar stash
    stashed=$($cmdgit stash list | wc -l | xargs)
    if [ "$stashed" -ne 0 ]; then
        if $verbose; then
            echo_custom_msg "  Elementos en stash:" "$stashed" "${COLOR_RED}"
        fi
    fi

    # Verificar cambios en stage
    staged=$($cmdgit diff --cached --name-only | wc -l | xargs)
    if [ "$staged" -ne 0 ]; then
        if $verbose; then
            echo_custom_msg "  Archivos en stage:" "$staged" "${COLOR_RED}"
        fi
    fi

    # Verificar archivos no rastreados
    untracked=$($cmdgit ls-files --others --exclude-standard | wc -l | xargs)
    if [ "$untracked" -ne 0 ]; then
        if $verbose; then
            echo_custom_msg "  Archivos no rastreados:" "$untracked" "${COLOR_RED}"
        fi
    fi

    # Verificar archivos modificados
    modified=$($cmdgit ls-files -m | wc -l | xargs)
    if [ "$modified" -ne 0 ]; then
        if $verbose; then
            echo_custom_msg "  Archivos modificados:" "$modified" "${COLOR_RED}"
        fi
    fi

    # Verificar archivos renombrados/movidos
    moved=$($cmdgit diff --name-status | grep '^R' | wc -l | xargs)
    if [ "$moved" -ne 0 ]; then
        if $verbose; then
            echo_custom_msg "  Archivos movidos:" "$moved" "${COLOR_RED}"
        fi
    fi

    # Push pendiente (cualquier cosa en ahead pero no empujada)
    pending_push=$ahead
    if [ "$pending_push" -ne 0 ]; then
        if $verbose; then
            echo_custom_msg "  Push pendiente (commits):" "$pending_push" "${COLOR_RED}"
        fi
    fi

    # Verificar si es seguro hacer pull
    if [ "$ahead" -eq 0 ] && [ "$diverged" -eq 0 ] && [ "$stashed" -eq 0 ] && [ "$staged" -eq 0 ] && [ "$untracked" -eq 0 ] && [ "$modified" -eq 0 ] && [ "$moved" -eq 0 ] && [ "$pending_push" -eq 0 ]; then
        if [ "$behind" -eq 0 ]; then

            # El repo está limpio, hago una última comprobación por si acaso estoy en una rama distinta a la principal y si está por detrás de la principal
            behind_main=$(check_if_behind_main "$branch_name")
            if [ "$behind_main" -eq 0 ] 2>/dev/null; then
                echo_custom_msg "  Rama por detrás de la principal: " "yes" "${COLOR_RED}"
                echo_status "clean_behind_main"
                if [ "$verbose" = "true" ]; then
                    print_verbose_output
                fi
            else
                echo_status "clean"
            fi

        else
            if $pull; then
                echo_status "pull"
                $cmdgit pull --quiet
            else
                echo_status "needspull"
                if $verbose; then
                    print_verbose_output
                fi
            fi
        fi
    else
        echo_status "review"
        print_verbose_output
    fi

    # Volver al directorio original
    cd "$current_dir"
}

# ----------------------------------------------------------------------------------------
# Ejecución Principal del Script
# ----------------------------------------------------------------------------------------

# Variable para rastrear el modo detallado
verbose=false

# Procesar argumentos de línea de comandos
while getopts "vh" opt; do
    case ${opt} in
    v)
        verbose=true
        ;;
    h)
        show_usage
        ;;
    \?)
        show_usage
        ;;
    esac
done

# Verificar el comando "pull" como un argumento posicional
shift $((OPTIND - 1))
if [ "$1" == "pull" ]; then
    pull=true
fi

# Encontrar todos los repositorios git desde el directorio actual
echo
if $pull; then
    echo "Análisis de que repositorios git necesitan un pull."
    echo "Se hará pull automáticamente si es seguro."
else
    echo "Análisis de que repositorios git necesitan un pull"
fi
echo
# find . -type d -name ".git" | while read -r git_dir; do
#     repo_path=$(dirname "$git_dir")
#     check_git_status "$repo_path"
# done
repos=$(find . -type d -name ".git")
for git_dir in $repos; do
    repo_path=$(dirname "$git_dir")
    check_git_status "$repo_path"
done
echo
