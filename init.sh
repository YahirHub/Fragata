#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$PROJECT_DIR"

COMPOSE_FILE="docker-compose.yml"
DO_GIT_PULL=false
NO_CACHE=false
REPAIR_PERMISSIONS=false
FOLLOW_LOGS=false

usage() {
  cat <<'USAGE'
Uso: bash init.sh [opciones]

Prepara el host, valida .env, corrige permisos, construye la imagen y levanta Fragata.
Docker reutiliza su caché: después de copiar cambios o ejecutar git pull basta con
volver a ejecutar este script.

Opciones:
  --bridge               Usa docker-compose.bridge.yml.
  --git-pull             Ejecuta git pull --ff-only antes de construir.
  --no-cache             Fuerza una reconstrucción completa de la imagen.
  --repair-permissions   Repara recursivamente propietario/permisos de datos y MKV.
  --logs                 Sigue los logs después del despliegue.
  -h, --help             Muestra esta ayuda.
USAGE
}

log() { printf '[init] %s\n' "$*"; }
die() { printf '[init] ERROR: %s\n' "$*" >&2; exit 1; }

while (($#)); do
  case "$1" in
    --bridge) COMPOSE_FILE="docker-compose.bridge.yml" ;;
    --git-pull) DO_GIT_PULL=true ;;
    --no-cache) NO_CACHE=true ;;
    --repair-permissions) REPAIR_PERMISSIONS=true ;;
    --logs) FOLLOW_LOGS=true ;;
    -h|--help) usage; exit 0 ;;
    *) die "opción desconocida: $1" ;;
  esac
  shift
done

command -v docker >/dev/null 2>&1 || die "Docker no está instalado"
command -v realpath >/dev/null 2>&1 || die "se requiere realpath (coreutils)"
docker compose version >/dev/null 2>&1 || die "se requiere Docker Compose v2 (docker compose)"
docker info >/dev/null 2>&1 || die "no se puede acceder al daemon de Docker; inicia Docker o agrega tu usuario al grupo docker"

if "$DO_GIT_PULL"; then
  [ -d .git ] || die "--git-pull requiere un repositorio Git"
  if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
    die "hay cambios locales sin guardar; no se ejecutará git pull"
  fi
  log "actualizando el código con git pull --ff-only"
  git pull --ff-only
fi

read_env_value() {
  local key="$1" value=""
  [ -f .env ] || return 0
  value="$(sed -n "s/^${key}=//p" .env | tail -n 1 | tr -d '\r')"
  if [[ "$value" == \"*\" && "$value" == *\" ]]; then
    value="${value:1:${#value}-2}"
  elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
    value="${value:1:${#value}-2}"
  fi
  printf '%s' "$value"
}

set_env_value() {
  local key="$1" value="$2" tmp
  tmp="$(mktemp)"
  awk -v key="$key" -v value="$value" '
    BEGIN { written = 0 }
    $0 ~ "^" key "=" {
      if (!written) { print key "=" value; written = 1 }
      next
    }
    { print }
    END { if (!written) print key "=" value }
  ' .env > "$tmp"
  cat "$tmp" > .env
  rm -f "$tmp"
}

project_uid="${SUDO_UID:-$(id -u)}"
project_gid="${SUDO_GID:-$(id -g)}"
if [ "$project_uid" -eq 0 ]; then
  project_uid="$(stat -c '%u' "$PROJECT_DIR")"
  project_gid="$(stat -c '%g' "$PROJECT_DIR")"
fi
if [ "$project_uid" -eq 0 ]; then
  project_uid=1000
  project_gid=1000
fi

env_created=false
if [ ! -f .env ]; then
  cp .env.example .env
  env_created=true
  log "se creó .env a partir de .env.example"
fi
chmod 0600 .env

if "$env_created"; then
  set_env_value FRAGATA_UID "$project_uid"
  set_env_value FRAGATA_GID "$project_gid"
fi

run_uid="$(read_env_value FRAGATA_UID)"
run_gid="$(read_env_value FRAGATA_GID)"
run_uid="${run_uid:-$project_uid}"
run_gid="${run_gid:-$project_gid}"
[[ "$run_uid" =~ ^[0-9]+$ ]] && [ "$run_uid" -gt 0 ] && [ "$run_uid" -le 2147483647 ] || die "FRAGATA_UID debe ser un entero válido mayor que 0"
[[ "$run_gid" =~ ^[0-9]+$ ]] && [ "$run_gid" -gt 0 ] && [ "$run_gid" -le 2147483647 ] || die "FRAGATA_GID debe ser un entero válido mayor que 0"
set_env_value FRAGATA_UID "$run_uid"
set_env_value FRAGATA_GID "$run_gid"

admin_user="$(read_env_value FRAGATA_ADMIN_USER)"
if [ -z "$admin_user" ]; then
  admin_user=admin
  set_env_value FRAGATA_ADMIN_USER "$admin_user"
fi

admin_password="$(read_env_value FRAGATA_ADMIN_PASSWORD)"
if [ -z "$admin_password" ]; then
  if command -v openssl >/dev/null 2>&1; then
    admin_password="$(openssl rand -hex 18)"
  else
    admin_password="$(od -An -N18 -tx1 /dev/urandom | tr -d ' \n')"
  fi
  set_env_value FRAGATA_ADMIN_PASSWORD "$admin_password"
  printf '\n[init] Credenciales iniciales generadas:\n'
  printf '       Usuario: %s\n' "$admin_user"
  printf '       Contraseña: %s\n' "$admin_password"
  printf '       Guárdalas ahora; la contraseña queda protegida en .env (modo 0600).\n\n'
fi
[ "${#admin_password}" -ge 12 ] || die "FRAGATA_ADMIN_PASSWORD debe tener al menos 12 caracteres"

host_data_dir="$(read_env_value FRAGATA_HOST_DATA_DIR)"
host_recordings_dir="$(read_env_value FRAGATA_HOST_RECORDINGS_DIR)"
host_data_dir="${host_data_dir:-./data}"
host_recordings_dir="${host_recordings_dir:-./recordings}"

absolute_path() {
  local path="$1"
  if [[ "$path" = /* ]]; then
    realpath -m -- "$path"
  else
    realpath -m -- "$PROJECT_DIR/${path#./}"
  fi
}

validate_persistent_path() {
  local label="$1" path="$2"
  case "$path" in
    /|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/media|/mnt|/opt|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/usr/local|/var|/var/cache|/var/lib|/var/log)
      die "$label apunta a una ruta del sistema no permitida: $path"
      ;;
  esac
  [ "$path" != "$PROJECT_DIR" ] || die "$label no puede ser la raíz del proyecto"
}

data_dir_abs="$(absolute_path "$host_data_dir")"
recordings_dir_abs="$(absolute_path "$host_recordings_dir")"
validate_persistent_path FRAGATA_HOST_DATA_DIR "$data_dir_abs"
validate_persistent_path FRAGATA_HOST_RECORDINGS_DIR "$recordings_dir_abs"
[ "$data_dir_abs" != "$recordings_dir_abs" ] || die "datos y grabaciones deben usar carpetas distintas"

run_privileged() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    die "se necesita sudo para crear o corregir las carpetas persistentes"
  fi
}

run_privileged chown "${project_uid}:${project_gid}" .env
chmod 0600 .env

log "preparando carpetas persistentes"
run_privileged mkdir -p "$data_dir_abs" "$recordings_dir_abs" "$PROJECT_DIR/config"
run_privileged chown "${project_uid}:${project_gid}" "$PROJECT_DIR/config"
run_privileged chmod 0750 "$PROJECT_DIR/config"

permission_schema="v2:${run_uid}:${run_gid}"
marker_path="${data_dir_abs}/.fragata-permissions"
current_schema=""
[ -f "$marker_path" ] && current_schema="$(cat "$marker_path" 2>/dev/null || true)"

if "$REPAIR_PERMISSIONS" || [ "$current_schema" != "$permission_schema" ]; then
  log "reparando propietario recursivamente a ${run_uid}:${run_gid}"
  run_privileged chown -R "${run_uid}:${run_gid}" "$data_dir_abs" "$recordings_dir_abs"
else
  log "verificando carpetas de grabación sin recorrer todos los archivos"
  run_privileged chown "${run_uid}:${run_gid}" "$data_dir_abs" "$recordings_dir_abs"
  run_privileged find "$data_dir_abs" -mindepth 1 -maxdepth 5 -type d -exec chown "${run_uid}:${run_gid}" {} +
  run_privileged find "$recordings_dir_abs" -mindepth 1 -maxdepth 4 -type d -exec chown "${run_uid}:${run_gid}" {} +
fi

run_privileged chmod 0750 "$data_dir_abs" "$recordings_dir_abs"
run_privileged find "$data_dir_abs" -mindepth 1 -maxdepth 5 -type d -exec chmod 0750 {} +
run_privileged find "$recordings_dir_abs" -mindepth 1 -maxdepth 4 -type d -exec chmod 0750 {} +
printf '%s\n' "$permission_schema" | run_privileged tee "$marker_path" >/dev/null
run_privileged chown "${run_uid}:${run_gid}" "$marker_path"
run_privileged chmod 0600 "$marker_path"

log "validando Docker Compose"
docker compose -f "$COMPOSE_FILE" config >/dev/null

if "$NO_CACHE"; then
  log "construyendo sin caché"
  docker compose -f "$COMPOSE_FILE" build --no-cache
  docker compose -f "$COMPOSE_FILE" up -d --remove-orphans
else
  log "construyendo con caché inteligente y levantando servicios"
  docker compose -f "$COMPOSE_FILE" up -d --build --remove-orphans
fi

container_id="$(docker compose -f "$COMPOSE_FILE" ps -q fragata)"
[ -n "$container_id" ] || die "Docker Compose no creó el contenedor fragata"

log "esperando el healthcheck"
healthy=false
for _ in {1..45}; do
  state="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null || true)"
  case "$state" in
    healthy|running) healthy=true; break ;;
    unhealthy|exited|dead)
      docker compose -f "$COMPOSE_FILE" logs --tail=120 fragata || true
      die "el contenedor terminó en estado ${state}"
      ;;
  esac
  sleep 2
done

if ! "$healthy"; then
  docker compose -f "$COMPOSE_FILE" logs --tail=120 fragata || true
  die "Fragata no alcanzó el estado healthy dentro del tiempo esperado"
fi

docker compose -f "$COMPOSE_FILE" ps
log "despliegue completado. En futuras actualizaciones vuelve a ejecutar: bash init.sh"

if "$FOLLOW_LOGS"; then
  docker compose -f "$COMPOSE_FILE" logs -f fragata
fi
