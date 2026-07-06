# Seguridad de despliegue

Fragata administra credenciales de cámaras, sesiones y archivos de video. El despliegue de producción debe cumplir esta lista antes de publicar el panel.

## Requisitos obligatorios

1. Configure `FRAGATA_ADMIN_USER` y una contraseña única de al menos 12 caracteres. Docker Compose no inicia si faltan.
2. Publique el panel mediante HTTPS y active `FRAGATA_SECURE_COOKIES=true`. No exponga directamente el puerto HTTP de Fragata a Internet.
3. Permita el acceso al puerto de Fragata únicamente desde el proxy inverso o desde la LAN mediante firewall.
4. Mantenga `data/`, `recordings/` y cualquier carpeta de secretos propiedad del UID/GID configurado, sin permisos de escritura para otros usuarios.
5. Para SFTP use una llave dedicada y un archivo `known_hosts` verificado. Nunca desactive la comprobación de identidad del servidor.
6. Cambie `FRAGATA_ALLOW_PUBLIC_CAMERAS=false` cuando todas las cámaras estén en redes privadas.
7. Respalde `data/state.json`, `data/secret.key` y las grabaciones. La pérdida de `secret.key` impide descifrar credenciales persistidas.

## Controles implementados

- Sesiones aleatorias persistidas únicamente como hash SHA-256.
- Cookies `HttpOnly`, `SameSite=Strict` y `Secure` cuando se configura HTTPS.
- CSRF obligatorio en todas las operaciones mutables autenticadas.
- Comparación uniforme de usuario y contraseña.
- Rate limit por IP y pareja IP/usuario, bloqueo temporal, `Retry-After` y memoria acotada.
- Cabeceras CSP, anti-framing, `nosniff`, política de permisos, aislamiento de origen y HSTS cuando se usan cookies seguras.
- Rutas de grabación confinadas al directorio configurado, sin traversal ni enlaces simbólicos.
- Límite global de procesos FFmpeg para reproducción histórica.
- Contenedor sin capabilities, con `no-new-privileges`, raíz de solo lectura y proceso sin privilegios.
- Verificación SFTP de host, tamaño y SHA-256 antes de considerar una subida terminada.

## Persistencia del contenedor

Solo deben ser escribibles y persistentes:

- `/data`: estado, clave de cifrado, capturas de eventos y `logs.txt`.
- `/recordings`: segmentos MKV locales.
- `/tmp`: almacenamiento temporal en memoria, sin ejecución y no persistente.

Las llaves SFTP se montan aparte como solo lectura en `/run/secrets/fragata`.

## Riesgos residuales conocidos

- Fragata no termina TLS por sí mismo; necesita Caddy, Nginx, Traefik u otro proxy HTTPS.
- El rate limit vive en memoria y se reinicia junto con el proceso. El servicio está diseñado para una sola instancia; varias réplicas necesitan un limitador compartido en el proxy.
- Bootstrap y Bootstrap Icons aún se obtienen desde jsDelivr. Sus hojas y scripts tienen SRI y la CSP limita el origen, pero una instalación completamente aislada debe empaquetar esos recursos localmente.
- H.265 se convierte a H.264 durante la reproducción web y puede consumir CPU. Ajuste `FRAGATA_MAX_TRANSCODES` al hardware disponible.
