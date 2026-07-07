# Seguridad de despliegue

Fragata administra credenciales de cámaras, sesiones y archivos de video. El despliegue de producción debe cumplir esta lista antes de publicar el panel.

## Requisitos obligatorios

1. Configure `FRAGATA_ADMIN_USER` y una contraseña única de al menos 12 caracteres. Docker Compose no inicia si faltan.
2. Publique el panel mediante HTTPS y active `FRAGATA_SECURE_COOKIES=true`. No exponga directamente el puerto HTTP de Fragata a Internet.
3. Permita el acceso al puerto de Fragata únicamente desde el proxy inverso o desde la LAN mediante firewall.
4. Mantenga `data/`, `recordings/` y cualquier carpeta de secretos sin permisos de escritura para otros usuarios. `init.sh` y el entrypoint corrigen el propietario al UID/GID configurado.
5. Para SFTP use una llave dedicada y un archivo `known_hosts` verificado. Nunca desactive la comprobación de identidad del servidor.
6. Cambie `FRAGATA_ALLOW_PUBLIC_CAMERAS=false` cuando todas las cámaras estén en redes privadas.
7. Respalde `data/state.json`, `data/secret.key` y las grabaciones. La pérdida de `secret.key` impide descifrar credenciales persistidas.

## Controles implementados

- Sesiones aleatorias persistidas únicamente como hash SHA-256.
- Cookies `HttpOnly`, `SameSite=Strict` y `Secure` cuando se configura HTTPS.
- CSRF obligatorio en todas las operaciones mutables autenticadas.
- Comparación uniforme de usuario y contraseña.
- Rate limit por IP y pareja IP/usuario, bloqueo temporal, `Retry-After` y memoria acotada.
- Cabeceras CSP, anti-framing, `nosniff` y política de permisos. El aislamiento COOP solo se activa en HTTPS, localhost o detrás de un proxy HTTPS local confiable; HSTS se envía únicamente cuando la solicitud se reconoce como HTTPS.
- Rutas de grabación confinadas al directorio configurado, sin traversal ni enlaces simbólicos.
- Suscripciones ONVIF PullPoint limitadas al host configurado para la cámara, con timeouts, cancelación y reconexión acotada.
- Límite global de procesos FFmpeg para reproducción histórica.
- Entrypoint root mínimo con solo `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `SETUID` y `SETGID`; después ejecuta `tini` y Fragata como UID/GID no root sin conservar capabilities ni dejar un proceso root residente.
- `no-new-privileges`, raíz de solo lectura y verificación real de escritura sobre los bind mounts antes de iniciar.
- Verificación SFTP de host, tamaño y SHA-256 antes de considerar una subida terminada.

## Modelo de privilegios del contenedor

El proceso principal no permanece como root. El entrypoint usa privilegios únicamente para preparar los bind mounts y luego hace `exec su-exec UID:GID tini -- /usr/local/bin/fragata`. Un marcador en `/data/.fragata-permissions` evita reparaciones recursivas innecesarias.

No elimine `cap_drop: ALL` ni añada capacidades distintas de las cinco declaradas sin una revisión de seguridad. Tampoco configure `FRAGATA_UID=0` o `FRAGATA_GID=0`; el entrypoint lo rechaza.

## Persistencia del contenedor

Solo deben ser escribibles y persistentes:

- `/data`: estado, clave de cifrado, miniaturas históricas de eventos y `logs.txt`.
- `/recordings`: segmentos MKV locales.
- `/tmp`: almacenamiento temporal en memoria, sin ejecución y no persistente.

Las llaves SFTP se montan aparte como solo lectura en `/run/secrets/fragata`.

## Riesgos residuales conocidos

- Fragata no termina TLS por sí mismo; necesita Caddy, Nginx, Traefik u otro proxy HTTPS.
- El rate limit vive en memoria y se reinicia junto con el proceso. El servicio está diseñado para una sola instancia; varias réplicas necesitan un limitador compartido en el proxy.
- Bootstrap y Bootstrap Icons aún se obtienen desde jsDelivr. Sus hojas y scripts tienen SRI y la CSP limita el origen, incluyendo únicamente sus mapas de código en `connect-src`; una instalación completamente aislada debe empaquetar esos recursos localmente.
- H.265 se convierte a H.264 durante la reproducción web y puede consumir CPU. Ajuste `FRAGATA_MAX_TRANSCODES` al hardware disponible.
- Fragata depende de las capacidades de eventos del firmware. La sensibilidad, zonas y clasificación se configuran en la cámara; un dispositivo sin ONVIF Events PullPoint no puede generar eventos en Fragata.
