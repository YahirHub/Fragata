# 22. Videoteca, línea de tiempo y hardening de producción

Fecha: 2026-07-05  
Versión: 0.9.0

## Objetivo

Incorporar una videoteca local independiente de SFTP para consultar todas las grabaciones guardadas, filtrarlas por cámara y día, reproducirlas desde una línea de tiempo similar a las aplicaciones de cámaras y preparar el servicio para un despliegue Linux con Docker más seguro.

## Decisiones de arquitectura

- Los MKV continúan siendo la fuente de verdad y nunca se modifican para reproducirlos.
- La API indexa bajo demanda únicamente el día seleccionado, evitando cargar todo el historial en memoria.
- Los días disponibles se obtienen recorriendo la jerarquía `cámara/año/mes/día` de más reciente a más antiguo, deteniéndose al completar el límite solicitado y sin abrir los videos.
- El identificador público de una grabación es una ruta relativa codificada en Base64 URL-safe; antes de abrirla se comprueban contención, estructura, extensión, archivo regular y ausencia de enlaces simbólicos.
- Las carpetas que ya no pertenecen a una cámara configurada se muestran como fuentes archivadas para no ocultar grabaciones antiguas.
- FFprobe identifica el códec real del archivo. H.264 se remultiplexa y H.265 u otro códec se convierte a H.264 para el navegador.
- La reproducción se entrega como MP4 fragmentado y acepta un desplazamiento inicial en segundos. La línea de tiempo reinicia el stream desde el punto elegido.
- Un semáforo global limita procesos FFmpeg históricos mediante `FRAGATA_MAX_TRANSCODES`.

## Interfaz

Se añadió `/recordings` con:

- filtro por cámara;
- selector de fecha y navegación anterior/hoy/siguiente;
- accesos rápidos a días que contienen video;
- cantidad, duración, tamaño y eventos del día;
- reproductor con saltos de diez segundos;
- descarga del MKV original;
- línea de tiempo vertical de 24 horas;
- segmentos finalizados, parciales y recuperados;
- marcadores de persona y movimiento;
- lista cronológica adaptable a móvil y modo oscuro.

## Seguridad del login

El limitador anterior de cinco fallos por minuto y por IP se sustituyó por buckets simultáneos de IP y pareja IP/usuario, evitando que una dirección remota pueda bloquear globalmente la cuenta. Se incorporó:

- máximo configurable de intentos;
- ventana configurable;
- bloqueo temporal configurable;
- encabezado `Retry-After`;
- limpieza de entradas antiguas y límite de memoria;
- confianza en `X-Forwarded-For` únicamente cuando el peer es loopback y lectura desde el extremo añadido por el proxy;
- cuerpo de login limitado a 8 KiB;
- longitud máxima de usuario y contraseña;
- contraseña mínima de 12 caracteres al cargar configuración;
- comparación uniforme mediante SHA-256 y `ConstantTimeCompare`.

## Hardening HTTP

- `Cross-Origin-Opener-Policy: same-origin`.
- `Cross-Origin-Resource-Policy: same-origin`.
- CSP ampliada con `form-action`, `worker-src` y `connect-src` restringido.
- HSTS solo cuando `FRAGATA_SECURE_COOKIES=true`.
- Respuestas de login y API marcadas `no-store`.
- JSON mutable exige `Content-Type: application/json`, un único documento y tamaño acotado.

## Docker

La imagen `scratch` no podía incluir FFmpeg. La imagen final usa Alpine, instala FFmpeg/FFprobe y sigue ejecutando Fragata sin privilegios.

`docker-compose.yml` ahora:

- exige credenciales administrativas;
- monta `./data` en `/data`;
- monta `./recordings` en `/recordings`;
- monta `./config` en `/etc/fragata/config` como solo lectura;
- exige que las carpetas persistentes ya existan para no crearlas accidentalmente como `root`;
- permite adaptar UID/GID al usuario Linux;
- usa filesystem raíz de solo lectura;
- crea `/tmp` como `tmpfs` sin ejecución;
- elimina todas las capabilities;
- activa `no-new-privileges`;
- limita PIDs;
- protege los recursos CDN con SRI y una CSP limitada;
- conserva healthcheck y rotación de logs Docker.

## Corrección SFTP

Cuando `DeleteLocal` está habilitado, la cola ya no elimina el trabajo antes de borrar el archivo local. Si el borrado falla, el trabajo permanece y en el siguiente intento se verifica el remoto existente antes de reintentar la limpieza local.

## Archivos principales

- `internal/httpapi/recordings.go`
- `internal/httpapi/recording_stream.go`
- `internal/httpapi/login_limiter.go`
- `internal/httpapi/static/recordings.html`
- `internal/httpapi/static/recordings.js`
- `internal/httpapi/static/styles.css`
- `internal/auth/auth.go`
- `internal/config/config.go`
- `internal/upload/uploader.go`
- `Dockerfile`
- `docker-compose.yml`
- `docker-compose.bridge.yml`
