#!/bin/sh
set -eu

log() {
  printf '%s %s\n' '[fragata-entrypoint]' "$*"
}

fail() {
  printf '%s %s\n' '[fragata-entrypoint] ERROR:' "$*" >&2
  exit 1
}

validate_id() {
  name="$1"
  value="$2"
  case "$value" in
    ''|*[!0-9]*) fail "$name debe ser un entero positivo" ;;
  esac
  [ "$value" -gt 0 ] || fail "$name no puede ser 0; Fragata debe ejecutarse sin privilegios"
  [ "$value" -le 2147483647 ] || fail "$name está fuera de rango"
}

run_uid="${FRAGATA_UID:-65532}"
run_gid="${FRAGATA_GID:-65532}"
data_dir="${FRAGATA_DATA_DIR:-/data}"
recordings_dir="${FRAGATA_RECORDINGS_DIR:-/recordings}"
repair_mode="${FRAGATA_REPAIR_PERMISSIONS:-auto}"
permission_schema="v2:${run_uid}:${run_gid}"

validate_id FRAGATA_UID "$run_uid"
validate_id FRAGATA_GID "$run_gid"

case "$repair_mode" in
  auto|always|never) ;;
  *) fail "FRAGATA_REPAIR_PERMISSIONS debe ser auto, always o never" ;;
esac

if [ "$#" -eq 0 ]; then
  set -- /usr/local/bin/fragata
fi

if [ "$(id -u)" -ne 0 ]; then
  log "el contenedor no inició como root; se omite la preparación de permisos"
  if command -v tini >/dev/null 2>&1; then
    exec tini -- "$@"
  fi
  exec "$@"
fi

command -v su-exec >/dev/null 2>&1 || fail "su-exec no está instalado"
command -v tini >/dev/null 2>&1 || fail "tini no está instalado"

umask 027
mkdir -p "$data_dir" "$recordings_dir"
data_dir="$(readlink -f "$data_dir")"
recordings_dir="$(readlink -f "$recordings_dir")"
[ -n "$data_dir" ] && [ "$data_dir" != "/" ] || fail "FRAGATA_DATA_DIR no puede apuntar a la raíz"
[ -n "$recordings_dir" ] && [ "$recordings_dir" != "/" ] || fail "FRAGATA_RECORDINGS_DIR no puede apuntar a la raíz"
[ "$data_dir" != "$recordings_dir" ] || fail "datos y grabaciones deben usar directorios distintos"
marker_path="${data_dir}/.fragata-permissions"
mkdir -p "$data_dir/events"

current_schema=""
if [ -f "$marker_path" ]; then
  current_schema="$(cat "$marker_path" 2>/dev/null || true)"
fi

full_repair=false
case "$repair_mode" in
  always) full_repair=true ;;
  auto)
    if [ "$current_schema" != "$permission_schema" ]; then
      full_repair=true
    fi
    ;;
  never) ;;
esac

if [ "$full_repair" = true ]; then
  log "ajustando propietario de datos y grabaciones a ${run_uid}:${run_gid}; puede tardar en instalaciones grandes"
  chown -R "${run_uid}:${run_gid}" "$data_dir" "$recordings_dir"
else
  # En reinicios normales solo se corrigen las carpetas. Esto evita recorrer y
  # cambiar de propietario todos los MKV cada vez que arranca el contenedor.
  chown "${run_uid}:${run_gid}" "$data_dir" "$recordings_dir" "$data_dir/events"
  find "$data_dir" -mindepth 1 -maxdepth 5 -type d -exec chown "${run_uid}:${run_gid}" {} +
  find "$recordings_dir" -mindepth 1 -maxdepth 4 -type d -exec chown "${run_uid}:${run_gid}" {} +
fi

chmod 0750 "$data_dir" "$recordings_dir" "$data_dir/events"
find "$data_dir" -mindepth 1 -maxdepth 5 -type d -exec chmod 0750 {} +
find "$recordings_dir" -mindepth 1 -maxdepth 4 -type d -exec chmod 0750 {} +

verify_write_access() {
  target_dir="$1"
  probe="${target_dir}/.fragata-write-test.$$"
  if ! su-exec "${run_uid}:${run_gid}" sh -c 'umask 077; : > "$1" && rm -f "$1"' sh "$probe"; then
    fail "el usuario ${run_uid}:${run_gid} no puede escribir en ${target_dir}"
  fi
}

verify_write_access "$data_dir"
verify_write_access "$recordings_dir"

printf '%s\n' "$permission_schema" > "$marker_path"
chown "${run_uid}:${run_gid}" "$marker_path"
chmod 0600 "$marker_path"

log "permisos preparados; iniciando tini y Fragata como ${run_uid}:${run_gid}"
exec su-exec "${run_uid}:${run_gid}" tini -- "$@"
