# 24. Entrypoint root y despliegue idempotente

Fecha: 2026-07-06  
Versión: 0.9.2

## Problema

Los bind mounts podían existir con propietario `root`, mientras Compose obligaba a iniciar el proceso directamente con UID/GID no privilegiado. Fragata no podía crear una carpeta de cámara como `/recordings/corredor` y terminaba con `permission denied`.

## Solución en el contenedor

- El contenedor inicia únicamente su entrypoint como root.
- Crea `/data`, `/recordings` y `/data/events`.
- Corrige propietario y permisos según `FRAGATA_UID` y `FRAGATA_GID`.
- Verifica con archivos temporales que el UID final pueda escribir en ambos volúmenes.
- Ejecuta `tini` y el binario mediante `su-exec`, por lo que no queda ningún proceso root residente y el servidor permanece sin privilegios durante toda su operación normal.
- Un marcador de esquema evita ejecutar `chown -R` sobre todos los MKV en cada reinicio.
- `FRAGATA_REPAIR_PERMISSIONS=always` permite forzar una reparación completa; `never` evita el recorrido recursivo.

Compose conserva `cap_drop: ALL` y añade solamente las capacidades necesarias durante el arranque: `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `SETUID` y `SETGID`. Al cambiar a UID no root, el proceso de Fragata no conserva esas capacidades.

## Script de host

`init.sh` es idempotente y está pensado para el flujo de copiar código al VPS o ejecutar `git pull`:

1. Comprueba Docker y Compose v2.
2. Crea `.env` cuando falta y genera una contraseña inicial segura si está vacía.
3. Detecta UID/GID del propietario del proyecto.
4. Crea y repara las carpetas persistentes del host.
5. Valida Compose.
6. Ejecuta `docker compose up -d --build --remove-orphans` usando caché.
7. Espera el healthcheck y muestra logs si el contenedor falla.

Opciones: `--git-pull`, `--bridge`, `--no-cache`, `--repair-permissions` y `--logs`.
