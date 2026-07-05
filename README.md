# Fragata

Fragata es un servidor de cámaras IP escrito en Go. Detecta dispositivos ONVIF, obtiene o prueba automáticamente la URL RTSP, guarda video H.264/H.265 en segmentos MKV, ofrece vista en vivo H.264 mediante WebRTC y puede subir grabaciones terminadas por SFTP.

El objetivo del proyecto es mantener una instalación simple: un único binario, frontend embebido, `CGO_ENABLED=0` y sin depender de FFmpeg durante la ejecución.

## Estado del MVP

Incluido:

- Descubrimiento ONVIF por WS-Discovery.
- Alta manual indicando únicamente IP, usuario y contraseña.
- Consulta ONVIF de información, perfiles y URL de transmisión.
- Fallback mediante diccionario RTSP integrado para Imou/Dahua, Hikvision, Reolink, Uniview, Axis, Vivotek, Hanwha y firmware genérico.
- Sondeo previo de puertos RTSP para no repetir timeouts por cada ruta.
- Diccionario local extensible sin descargar datos durante la ejecución.
- Prueba independiente de una URL RTSP manual antes de guardarla.
- Validación real del stream antes de guardar la cámara.
- Recepción RTSP H.264 y H.265 sin transcodificación.
- Grabación MKV segmentada y cierre atómico desde `.mkv.partial`.
- Recuperación conservadora de parciales después de un apagado inesperado.
- Vista en vivo WebRTC para perfiles H.264, con límite global configurable.
- Cola SFTP persistente, reintentos con backoff, `known_hosts`, archivo temporal remoto y checksum SHA-256.
- Login opcional definido en `.env`.
- Sesiones persistentes, CSRF, cookies `HttpOnly` y límite básico de intentos de acceso.
- Credenciales de cámaras cifradas con AES-256-GCM dentro del estado local.
- Panel web embebido y API HTTP.
- Docker, Compose, systemd y scripts de compilación estática.

No incluido todavía:

- Audio dentro del MKV.
- Transcodificación H.265 a H.264.
- Entrada mediante protocolo SRT. Las cámaras ONVIF normalmente entregan la transmisión por RTSP; SRT se añadirá como transporte independiente.
- Detección de personas, mascotas o movimiento.
- Reproducción histórica y línea de tiempo desde el panel.

## Requisitos

- Go 1.26.4 o compatible con la versión indicada en `go.mod`.
- Acceso inicial a internet para ejecutar `go mod tidy` y generar `go.sum`.
- Una cámara con RTSP H.264 o H.265.
- Para vista web, un perfil H.264. H.265 puede grabarse, pero no se transcodifica para el navegador.
- Acceso a la misma red local para WS-Discovery, salvo que se introduzca la IP manualmente.

## Inicio rápido

```bash
cp .env.example .env
go mod tidy
go test ./...
CGO_ENABLED=0 go build -trimpath -tags netgo,osusergo \
  -ldflags="-s -w -buildid=" \
  -o dist/fragata ./cmd/fragata
./dist/fragata -env .env
```

Abre:

```text
http://IP_DEL_SERVIDOR:8080
```

### Login opcional

Para proteger el panel:

```dotenv
FRAGATA_ADMIN_USER=admin
FRAGATA_ADMIN_PASSWORD=una-contraseña-larga
```

Las sesiones continúan siendo válidas después de reiniciar Fragata hasta que vencen. El navegador conserva el token aleatorio y Fragata guarda únicamente su hash SHA-256 en el archivo de estado. Si cualquiera de los dos valores queda vacío, la autenticación se deshabilita y el panel se abre directamente.

Cuando Fragata esté detrás de HTTPS:

```dotenv
FRAGATA_SECURE_COOKIES=true
```

## Agregar una cámara

### Detección automática por IP

En el panel introduce:

```text
IP: 192.168.1.100
Usuario: admin
Contraseña: contraseña del dispositivo
```

Fragata intenta, en orden:

1. Servicios ONVIF habituales.
2. Información y perfiles ONVIF.
3. `GetStreamUri` para obtener la dirección oficial entregada por la cámara.
4. Comprobación TCP de los puertos configurados en `FRAGATA_RTSP_PORTS`.
5. Diccionario RTSP integrado solamente sobre los puertos que respondieron.
6. Apertura real mediante RTSP sobre TCP para confirmar recepción de video H.264 o H.265.
7. Preferencia por H.264 para conservar la vista web; H.265 queda como fallback para grabación.

La detección no prueba contraseñas por fuerza bruta: usa únicamente las credenciales introducidas por el usuario. Tampoco existe una ruta RTSP universal para todas las marcas; ONVIF es la primera opción y el diccionario es un fallback acotado.

Para cámaras Imou/Dahua, entre las primeras rutas probadas se encuentran:

```text
/cam/realmonitor?channel=1&subtype=0
/cam/realmonitor?channel=1&subtype=1
```

### URL RTSP manual

El panel permite pegar una URL explícita, pulsar **Probar URL** y guardarla únicamente cuando Fragata confirma que recibe video H.264 o H.265.

```text
rtsp://192.168.10.50:554/cam/realmonitor?channel=1&subtype=0
```

Es preferible escribir usuario y contraseña en sus campos separados. Fragata también acepta una URL completa como esta y extrae las credenciales para guardarlas cifradas, eliminándolas de la URL persistida:

```text
rtsp://usuario:contraseña@192.168.10.50:554/cam/realmonitor?channel=1&subtype=0
```

### Diccionario RTSP local

Fragata incluye rutas comunes dentro del binario y permite anteponer rutas propias mediante un archivo local. No descarga bases de datos ni listas durante la ejecución.

```bash
cp config/rtsp-paths.example.txt config/rtsp-paths.txt
```

Configura:

```dotenv
FRAGATA_RTSP_DICTIONARY=./config/rtsp-paths.txt
```

Formatos admitidos:

```text
/ruta
8554|/ruta
Nombre de cámara|554|/ruta
```

`*` o `0` indican que la ruta debe probarse en todos los puertos configurados.

### Diagnóstico de timeouts

Un error como:

```text
dial tcp 192.168.10.50:554: i/o timeout
```

ocurre antes de comprobar la ruta: el servidor donde se ejecuta Fragata no pudo abrir el puerto 554. En ese caso revisa que RTSP esté habilitado en la cámara, que la IP sea correcta y que Docker, firewall, VLAN o rutas del host permitan alcanzar esa subred. El diccionario solo puede ayudar cuando algún puerto RTSP responde.

## Grabaciones MKV

Estructura:

```text
data/recordings/<camera-id>/2026/07/05/14-30-00.000.mkv
```

Cada segmento se escribe primero como:

```text
archivo.mkv.partial
```

Al terminar:

1. Se cierra el contenedor Matroska.
2. Se ejecuta `fsync` sobre el archivo.
3. Se renombra atómicamente a `.mkv`.
4. Se registra para SFTP, cuando corresponde.

La rotación ocurre en un fotograma clave después de `FRAGATA_SEGMENT_DURATION`. El valor mínimo es 10 segundos.

## SFTP

Fragata exige comprobar la identidad del servidor. Genera `known_hosts` y verifica la huella por un canal confiable antes de usarla:

```bash
mkdir -p data
ssh-keyscan -p 22 servidor.example.com > data/known_hosts
ssh-keygen -lf data/known_hosts
```

Configuración con llave:

```dotenv
FRAGATA_SFTP_ENABLED=true
FRAGATA_SFTP_HOST=servidor.example.com
FRAGATA_SFTP_PORT=22
FRAGATA_SFTP_USER=fragata
FRAGATA_SFTP_PRIVATE_KEY=/ruta/id_ed25519
FRAGATA_SFTP_KNOWN_HOSTS=./data/known_hosts
FRAGATA_SFTP_REMOTE_DIR=/grabaciones/fragata
FRAGATA_SFTP_WORKERS=1
FRAGATA_SFTP_DELETE_LOCAL=false
```

Flujo remoto:

```text
video.mkv.part -> comprobación de tamaño -> video.mkv -> video.mkv.sha256 atómico
```

Los trabajos permanecen en `data/state.json` y sobreviven a reinicios. Antes de omitir una subida existente, Fragata exige que coincidan tanto el tamaño como el SHA-256 remoto. Si una subida falla, se reintenta con backoff. El archivo local solo se elimina cuando `FRAGATA_SFTP_DELETE_LOCAL=true` y el remoto ya fue finalizado.

## Compilación estática

```bash
./scripts/build-static.sh
```

Para Linux ARM64:

```bash
GOARCH=arm64 ./scripts/build-static.sh
```

Validación en Linux:

```bash
file dist/fragata-linux-amd64
ldd dist/fragata-linux-amd64
readelf -d dist/fragata-linux-amd64
```

`ldd` debe responder `not a dynamic executable` o equivalente.

## Docker Compose

```bash
cp .env.example .env
docker compose build
docker compose up -d
docker compose logs -f fragata
```

El contenedor final usa `scratch`, ejecuta con UID/GID `65532` y persiste `/data` en un volumen.

## Servicio systemd

Ejemplo para Debian:

```bash
sudo useradd --system --home /var/lib/fragata --shell /usr/sbin/nologin fragata
sudo install -d -o fragata -g fragata -m 0750 /opt/fragata /var/lib/fragata
sudo install -d -o root -g fragata -m 0750 /etc/fragata
sudo install -m 0755 dist/fragata-linux-amd64 /opt/fragata/fragata
sudo install -m 0644 deploy/fragata.service /etc/systemd/system/fragata.service
sudo cp .env.example /etc/fragata/fragata.env
```

Configura en `/etc/fragata/fragata.env`:

```dotenv
FRAGATA_DATA_DIR=/var/lib/fragata
FRAGATA_RECORDINGS_DIR=/var/lib/fragata/recordings
```

Después:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now fragata
sudo systemctl status fragata
journalctl -u fragata -f
```

## API principal

| Método | Ruta | Función |
|---|---|---|
| `GET` | `/healthz` | Estado básico sin autenticación |
| `POST` | `/api/login` | Crear sesión |
| `POST` | `/api/logout` | Cerrar sesión |
| `GET` | `/api/session` | Estado de sesión y CSRF |
| `GET` | `/api/cameras` | Listar cámaras sin secretos |
| `POST` | `/api/cameras` | Detectar, validar y agregar cámara |
| `POST` | `/api/rtsp/probe` | Probar una URL RTSP sin guardarla |
| `DELETE` | `/api/cameras/{id}` | Eliminar configuración |
| `POST` | `/api/discovery` | WS-Discovery ONVIF |
| `GET` | `/api/status` | Estado de streams y grabación |
| `GET` | `/api/uploads` | Cola SFTP |
| `POST` | `/api/cameras/{id}/offer` | Negociar WebRTC |

Las operaciones mutables requieren el encabezado `X-Fragata-CSRF` cuando el login está habilitado.

## Seguridad y límites

- De forma predeterminada solo se aceptan IP privadas/locales para cámaras; los hosts devueltos por ONVIF se fijan a la IP introducida para reducir SSRF.
- La búsqueda RTSP está limitada por puertos, número de candidatos, tiempo y paralelismo; no realiza fuerza bruta de credenciales.
- Las contraseñas de las cámaras no se devuelven por API y se cifran en disco.
- La llave maestra se crea en `data/secret.key` con permisos `0600` si no se proporciona mediante entorno.
- No se usa `InsecureIgnoreHostKey` para SFTP.
- `insecure_tls` solo afecta una cámara ONVIF HTTPS concreta cuando se solicita desde la API.
- No publiques el puerto de Fragata directamente en Internet sin HTTPS, firewall y una contraseña robusta.
- La grabación actual es solo de video. El audio se ignora.
- Los clusters MKV se descargan al archivo aproximadamente cada 5 segundos para limitar memoria y pérdida ante cortes.
- `FRAGATA_MAX_VIEWERS` limita las conexiones WebRTC simultáneas; el valor predeterminado es 32.
- El escritor H.265 debe validarse con los modelos reales que se usarán antes de considerarlo producción estable.

## Pruebas

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
```

Smoke test con el servidor ejecutándose:

```bash
FRAGATA_BIN=./dist/fragata BASE_URL=http://127.0.0.1:8080 ./scripts/smoke-test.sh
```

Prueba real recomendada:

1. Agregar una cámara indicando solo IP y credenciales.
2. Confirmar estado `en línea`.
3. Abrir la vista en vivo con un perfil H.264.
4. Esperar el cierre de un segmento MKV.
5. Abrirlo en VLC o mpv.
6. Cortar la red de la cámara y confirmar reconexión.
7. Reiniciar Fragata durante un segmento y revisar recuperación del `.partial`.
8. Confirmar creación del MKV y `.sha256` remotos por SFTP.

## Estructura

```text
cmd/fragata/          punto de entrada
internal/auth/        sesiones persistentes, login y CSRF
internal/camera/      detección y supervisión de cámaras
internal/httpapi/     API y panel web embebido
internal/live/        puente RTP a WebRTC
internal/matroska/    escritor MKV sin CGO
internal/onvif/       WS-Discovery y SOAP ONVIF
internal/recording/   segmentación y recuperación
internal/rtsp/        conexión RTSP, sondeo de puertos y diccionario de rutas
internal/store/       estado JSON atómico y secretos cifrados
internal/stream/      distribución interna de RTP y access units
internal/upload/      cola y transferencia SFTP
contexto/             decisiones técnicas persistentes
```
