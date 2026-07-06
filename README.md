# Fragata

Fragata es un servidor de cámaras IP escrito en Go. Detecta dispositivos ONVIF, elige el stream RTSP de mayor resolución para grabarlo sin recomprimir, guarda video H.264/H.265 y audio compatible en segmentos MKV, ofrece vista en vivo mediante WebRTC, puede subir grabaciones terminadas por SFTP y detecta movimiento y personas localmente mediante snapshots.

El núcleo sigue siendo un único binario compilable con `CGO_ENABLED=0` y frontend embebido. La detección utiliza código Go puro y pesos HOG/SVM embebidos: no requiere Python, OpenCV, ONNX Runtime, archivos de modelo ni servicios externos. FFmpeg sigue siendo opcional para el binario nativo, pero habilita la normalización de video, la conversión de audio y la reproducción histórica compatible con navegadores. El archivo MKV conserva siempre el video y audio originales de la cámara.

## Estado del MVP

Incluido:

- Descubrimiento ONVIF por WS-Discovery.
- Alta manual mediante IP privada, IP pública o dominio/CNAME, con puerto opcional, usuario y contraseña.
- Consulta ONVIF de información, perfiles y URL de transmisión.
- Fallback mediante diccionario RTSP integrado para Imou/Dahua, Hikvision, Reolink, Uniview, Axis, Vivotek, Hanwha y firmware genérico.
- Sondeo previo de puertos RTSP para no repetir timeouts por cada ruta.
- Diccionario local extensible sin descargar datos durante la ejecución.
- Prueba independiente de una URL RTSP manual antes de guardarla.
- Validación real del stream antes de guardar la cámara.
- Selección del perfil de mayor resolución sin preferir H.264 sobre H.265.
- Separación entre stream principal de grabación y stream de visualización.
- Recepción RTSP H.264 y H.265 sin recomprimir la grabación.
- Detección automática de FFmpeg en `PATH` para visualizar el stream H.265 principal manteniendo su resolución.
- Fallback a un substream H.264 cuando FFmpeg no está disponible o no puede iniciar.
- Grabación MKV continua con duración configurable por cámara, rotación sin huecos y cierre atómico desde `.mkv.partial`.
- Recuperación conservadora de parciales después de un apagado inesperado.
- Vista en vivo WebRTC con reconexión automática, supervisor de reinicio del servidor, modo monitor para tablets, arranque desde el GOP actual y pantalla completa.
- Grabación apagada al agregar una cámara, switch persistente y componente reutilizable para elegir entre 1 minuto y 24 horas por archivo.
- Audio en vivo y dentro del MKV para cámaras que entregan G.711 A-law, G.711 μ-law, Opus o AAC por RTSP.
- Cola SFTP persistente, reintentos con backoff, `known_hosts`, archivo temporal remoto y checksum SHA-256.
- Perfiles SFTP globales reutilizables, con múltiples servidores configurables desde el panel y selección independiente por cámara.
- Retención global configurable en días, meses o años, con protección de grabaciones abiertas y archivos pendientes de subir.
- Registro local rotativo en `logs.txt`, limitado a 1 MiB mediante eliminación de las líneas más antiguas.
- Login opcional definido en `.env`.
- Sesiones persistentes, CSRF, cookies `HttpOnly` y rate limit de login por IP y por pareja IP/usuario con bloqueo temporal configurable.
- Credenciales de cámaras cifradas con AES-256-GCM dentro del estado local.
- Panel web profesional y responsivo con dashboard, CRUD de cámaras, alta y ajustes en páginas independientes, sidebar, visor dedicado, Bootstrap e iconos por CDN.
- Layout de autenticación independiente, dropdown de usuario y modo Invitado cuando el login está deshabilitado.
- Carpeta de grabación configurable y única por cámara, con validación contra path traversal y nombres duplicados.
- API HTTP y frontend propio embebido en el binario; únicamente Bootstrap y Bootstrap Icons se cargan desde CDN.
- Docker, Compose con red LAN del host, systemd y scripts de compilación estática.
- Diagnóstico de puertos desde el mismo proceso que intenta abrir la cámara.
- Descubrimiento automático de `GetSnapshotUri` mediante ONVIF y validación de una URL HTTP(S) manual cuando la cámara no la publica.
- Detección de movimiento en Go puro mediante diferencia de imágenes pequeñas, compensación de iluminación y confirmación temporal.
- Confirmación humana beta mediante HOG/SVM embebido, ejecutada solo después de detectar movimiento para reducir consumo.
- Configuración por cámara de sensibilidad, intervalo, confianza humana, enfriamiento y zona rectangular de análisis.
- Página de eventos con miniaturas originales, filtros por cámara y tipo, vínculo al segmento MKV y reproducción desde el instante detectado.
- Videoteca local con filtro por cámara y día, días disponibles, lista cronológica, descarga del MKV original y línea de tiempo diaria con marcadores de eventos.
- Reproducción histórica desde cualquier segundo del segmento, detección del códec real mediante FFprobe y límite global de transcodificaciones.

No incluido todavía:

- Transcodificación general de codecs de audio distintos de G.711, Opus y AAC.
- Entrada mediante protocolo SRT. Las cámaras ONVIF normalmente entregan la transmisión por RTSP; SRT se añadirá como transporte independiente.
- Clasificación avanzada de mascotas, vehículos, rostros o personas pequeñas/ocultas mediante redes neuronales.

## Requisitos

- Go 1.26.4 o compatible con la versión indicada en `go.mod`.
- Acceso inicial a internet para ejecutar `go mod tidy` y generar `go.sum`.
- El navegador que abre el panel debe poder acceder a `cdn.jsdelivr.net` para cargar Bootstrap y Bootstrap Icons. El servidor Fragata no descarga esos archivos.
- Una cámara con RTSP H.264 o H.265.
- Para visualizar H.265 y reproducir grabaciones desde el navegador, una instalación de FFmpeg/FFprobe con el encoder `libx264`. La imagen Docker oficial del proyecto ya los incluye.
- Acceso a la misma red local para WS-Discovery. La alta manual también admite cámaras remotas mediante IP pública o dominio cuando sus puertos son alcanzables.

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

### Host de escucha y cámaras externas

El panel escucha por defecto en todas las interfaces mediante:

```dotenv
FRAGATA_LISTEN_HOST=0.0.0.0
FRAGATA_LISTEN_PORT=8080
```

`FRAGATA_LISTEN=0.0.0.0:8080` continúa disponible como formato heredado y tiene prioridad si se define. Para limitar el panel al propio equipo puede usarse `127.0.0.1`.

La alta de cámaras admite por defecto IP privadas, IP públicas, IPv4, IPv6 y dominios/CNAME:

```dotenv
FRAGATA_ALLOW_PUBLIC_CAMERAS=true
```

Para restaurar el modo estricto que solo permite destinos privados/locales:

```dotenv
FRAGATA_ALLOW_PUBLIC_CAMERAS=false
```

Un dominio debe resolver hacia un equipo que exponga realmente ONVIF/RTSP. Un proxy CDN HTTP tradicional no transporta RTSP por sí solo; puede utilizarse DNS directo, CNAME o un túnel que publique los puertos correspondientes.

### Login opcional

Para proteger el panel:

```dotenv
FRAGATA_ADMIN_USER=admin
FRAGATA_ADMIN_PASSWORD=una-contraseña-larga
```

La contraseña debe tener al menos 12 caracteres. Las sesiones continúan siendo válidas después de reiniciar Fragata hasta que vencen. El navegador conserva el token aleatorio y Fragata guarda únicamente su hash SHA-256 en el archivo de estado. El rate limit bloquea la IP y la pareja IP/usuario después de varios intentos fallidos y devuelve `Retry-After`; sus valores se ajustan con `FRAGATA_LOGIN_MAX_ATTEMPTS`, `FRAGATA_LOGIN_WINDOW` y `FRAGATA_LOGIN_BLOCK_DURATION`. Si cualquiera de los dos valores queda vacío, la autenticación se deshabilita para ejecución nativa, pero Docker Compose exige ambos valores.

Cuando Fragata esté detrás de HTTPS:

```dotenv
FRAGATA_SECURE_COOKIES=true
```


## Interfaz web

Fragata usa dos componentes de layout reutilizables:

- `fragata-auth-layout`: pantalla de inicio de sesión independiente.
- `fragata-app-layout`: sidebar, topbar, dropdown de usuario, contenido y footer compartidos por todas las páginas administrativas.

La administración está separada en rutas claras:

- `/`: dashboard operativo.
- `/cameras`: CRUD y tabla de cámaras con búsqueda, filtros y menú de acciones.
- `/cameras/new`: alta y detección de una cámara.
- `/cameras/<id>/settings`: identidad, carpeta, red, credenciales, grabación y SFTP.
- `/camera/<id>`: visor en vivo con audio opcional, recuperación tras reinicios, modo monitor y pantalla completa.
- `/events`: eventos de movimiento y persona con miniaturas y filtros.
- `/events/{id}`: detalle del evento, captura original y reproducción histórica vinculada.
- `/recordings`: videos locales por cámara y día, reproductor y línea de tiempo de 24 horas.
- `/settings/sftp`: servidores SFTP globales reutilizables.
- `/settings/storage`: política de retención y estado del registro local.

En escritorio, el sidebar permanece fijo a la izquierda y puede ocultarse o mostrarse desde la barra superior; la preferencia queda guardada en el navegador. En teléfonos y tablets, el mismo botón abre un drawer `offcanvas` con overlay y cierre automático al elegir una sección. Las tablas conservan desplazamiento horizontal controlado, los formularios se reorganizan y los controles mantienen áreas táctiles adecuadas.

La interfaz incluye modo claro y oscuro. En la primera visita adopta la preferencia del sistema operativo y, después de cambiarlo manualmente, conserva la selección en el navegador. El selector está disponible tanto en la barra superior como en la pantalla de acceso.

Cuando el login está configurado, el dropdown muestra el usuario administrador y permite cerrar sesión. Si `FRAGATA_ADMIN_USER` o `FRAGATA_ADMIN_PASSWORD` están vacíos, muestra `Invitado` y señala que la autenticación está desactivada.

Bootstrap 5.3.8 y Bootstrap Icons 1.13.1 se cargan desde jsDelivr con versión fija. La política CSP permite únicamente ese CDN para scripts, estilos y fuentes, manteniendo bloqueados otros orígenes.

## Administrar cámaras

El listado de cámaras usa un menú de tres puntos por fila para abrir el visor, consultar eventos, modificar ajustes, redetectar perfiles, iniciar o detener la grabación y eliminar el registro. La página de ajustes permite cambiar nombre, carpeta, IP o dominio, usuario, contraseña, URL RTSP, estado, duración, subida SFTP, servidor global asignado y parámetros de detección.

El visor comienza silenciado porque los navegadores bloquean la reproducción automática con sonido. Cuando la cámara ofrece audio compatible, aparece el botón **Activar sonido**; la acción del usuario habilita la pista sin reiniciar el video.

El supervisor del visor consulta `/healthz` de forma independiente. Si el proceso Go se detiene, mantiene la página abierta, muestra el estado desconectado y vuelve a cargar sesión, cámara y WebRTC cuando Fragata inicia otra vez. El botón **Monitor activo** conserva la preferencia y usa Screen Wake Lock cuando el navegador y el contexto seguro lo permiten, evitando que una tablet apague la pantalla mientras funciona como monitor.

Al cambiar IP, usuario, contraseña o URL RTSP, Fragata prueba la nueva configuración antes de guardarla. Una contraseña vacía conserva la credencial cifrada actual. Cambiar la carpeta afecta únicamente a nuevas grabaciones y no mueve los archivos existentes.

## Detección local de movimiento y personas

La detección es opcional y se configura por cámara desde **Ajustes → Detección**. Fragata intenta obtener automáticamente una URL JPEG mediante ONVIF `GetSnapshotUri`. Cuando una cámara no publica esa capacidad, puede introducirse manualmente una URL HTTP o HTTPS de snapshot perteneciente al mismo host configurado.

Flujo de análisis:

```text
Snapshot reducido a 160×90
        ↓
Movimiento por diferencia de imagen
        ↓ solo cuando existe actividad
Detector humano HOG/SVM sobre una imagen acotada
        ↓
Evento, confianza y miniatura
```

Parámetros disponibles:

- Activar o desactivar la detección sin afectar grabación ni vista en vivo.
- Detectar movimiento y, opcionalmente, confirmar persona.
- Sensibilidad de movimiento entre 1 y 100.
- Intervalo de análisis entre 1 y 60 segundos.
- Confianza humana entre 40 % y 95 %.
- Tiempo de enfriamiento entre eventos.
- Zona rectangular normalizada para ignorar áreas irrelevantes.

El detector humano está diseñado para cuerpos erguidos y visibles. Es una función beta: puede omitir personas pequeñas, parcialmente ocultas o tomadas desde ángulos extremos, y puede producir falsos positivos. La detección de movimiento continúa funcionando aunque la confirmación humana no encuentre una persona.

Para activarla en una cámara ya guardada:

1. Ejecuta **Redetectar calidad** para que Fragata vuelva a consultar perfiles y snapshot ONVIF.
2. Abre **Ajustes** y activa **Detección**.
3. Verifica o introduce la URL de snapshot.
4. Ajusta sensibilidad, intervalo, confianza, enfriamiento y zona.
5. Consulta los resultados en `/events`.

Las miniaturas se guardan en `data/events/` sin redimensionar ni recomprimir y se eliminan mediante la misma política global de retención. Fragata conserva las dimensiones y la proporción originales entregadas por la cámara y nunca devuelve la URL de snapshot con credenciales mediante la API.

Cuando la grabación está activa, el evento almacena el segmento MKV actual y el desplazamiento temporal exacto. Desde el detalle puede abrirse el video comenzando cinco segundos antes de la detección. Si el segmento todavía está abierto, la página espera a que se cierre y habilita la reproducción automáticamente. Los eventos anteriores intentan localizar su grabación por cámara, fecha y hora.

La reproducción histórica en navegador utiliza FFmpeg únicamente como adaptador HTTP opcional. Para H.264 conserva el video original sin recomprimir; para H.265 lo convierte a H.264 manteniendo las dimensiones originales. El MKV archivado nunca se modifica y siempre puede descargarse en su calidad original.

## Agregar una cámara

### Detección automática por IP o dominio

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
7. Comparación de todos los perfiles ONVIF válidos y selección del que tenga más píxeles, aunque sea H.265.
8. Búsqueda separada de un perfil H.264 secundario como respaldo para el navegador.

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

### Diagnóstico de red y timeouts

El panel incluye **Diagnosticar red**. La comprobación se ejecuta dentro del mismo proceso y espacio de red de Fragata, por lo que permite detectar diferencias entre el host y un contenedor.

Un error como:

```text
dial tcp 192.168.10.50:554: i/o timeout
```

ocurre antes de comprobar la ruta, el usuario o la contraseña. Significa que el entorno donde corre Fragata no completó la conexión TCP al puerto. El diagnóstico clasifica cada puerto como:

- `abierto`: ya puede probarse RTSP.
- `rechazado`: la IP responde, pero el servicio no escucha en ese puerto.
- `sin respuesta`: firewall, aislamiento, IP incorrecta o problema de red.
- `sin ruta` o `inalcanzable`: el servidor no tiene camino hacia la subred.

El diccionario de rutas solo puede ayudar después de que un puerto responda. Una contraseña con caracteres como `!` tampoco causa un timeout de conexión; la autenticación ocurre después de abrir el socket.

## Calidad principal y vista en vivo

Fragata guarda dos decisiones independientes por cámara:

```text
Stream principal de mayor resolución -> grabación MKV sin recomprimir
Stream de vista -> WebRTC directo, FFmpeg o substream H.264
```

La política de visualización es:

1. Si el stream principal es H.264, se envía directamente al navegador.
2. Si el principal es H.265 y Fragata detectó FFmpeg, toma ese stream de máxima resolución y lo recomprime a H.264 solo para WebRTC, conservando dimensiones y relación de aspecto.
3. Si FFmpeg no existe o falla, se utiliza el mejor substream H.264 detectado.
4. Si no existe ninguna opción compatible, la grabación H.265 continúa funcionando y el panel explica por qué no puede abrir la vista.

Fragata busca automáticamente `ffmpeg` o `ffmpeg.exe` en `PATH`. También puede indicarse una ruta explícita:

```dotenv
FRAGATA_FFMPEG_PATH=/usr/bin/ffmpeg
```

El proceso FFmpeg se inicia bajo demanda al abrir una cámara H.265; no se utiliza para grabar ni remultiplexar los MKV. Se detiene automáticamente cuando no quedan espectadores durante `FRAGATA_LIVE_IDLE_TIMEOUT` (30 segundos por defecto).

Cada cámara tiene una página dedicada en `/camera/<id>`, con reproducción automática, reconexión y pantalla completa. El video usa `object-fit: contain` para no deformar una imagen 16:9, 4:3 u otra relación de aspecto.

Las cámaras ya guardadas antes de esta versión pueden conservar la URL antigua. Usa **Redetectar calidad** en la tarjeta para volver a consultar perfiles y sustituirla sin perder nombre, credenciales ni configuración de grabación.

## Grabaciones MKV

La ruta `/recordings` consulta directamente el almacenamiento local y permite filtrar por cámara y día. Los segmentos se ordenan por fecha y hora, se muestran en una línea de tiempo de 24 horas y los eventos aparecen como marcadores. Al pulsar un segmento o evento, el navegador solicita MP4 fragmentado desde el segundo seleccionado; el MKV original permanece intacto y puede descargarse.

`FRAGATA_MAX_TRANSCODES` limita las reproducciones históricas simultáneas. Cuando todas las plazas están ocupadas la API responde `429` con `Retry-After`, evitando que varias pestañas agoten CPU y memoria. FFprobe identifica el códec del archivo histórico en lugar de asumir el códec actual de la cámara.

Estructura:

```text
data/recordings/<carpeta-de-la-camara>/2026/07/05/14-30-00.000.mkv
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

`FRAGATA_SEGMENT_DURATION` define únicamente el valor inicial para cámaras nuevas y la migración de cámaras antiguas. Después, cada cámara conserva su propia duración desde el panel, entre 1 minuto y 24 horas por archivo.

La rotación se realiza en el primer fotograma clave disponible al cumplir la duración. Fragata abre el MKV siguiente y escribe primero ese fotograma clave; el archivo anterior se sincroniza, cierra y renombra en segundo plano. Así, `fsync` no detiene el consumo del stream y no se descarta el fotograma de transición. Si no se puede crear el archivo siguiente, el actual continúa grabando en lugar de perder video.

Cuando la cámara se desconecta, Fragata finaliza el segmento de esa sesión. Al reconectar espera un nuevo fotograma clave y comienza otro MKV, evitando mezclar timestamps reiniciados dentro del mismo archivo. Activar, desactivar o cambiar la duración de grabación ya no reinicia la conexión RTSP. Una cámara nueva siempre se guarda con la grabación apagada y se activa después con su switch.

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

## Audio en vivo y grabado

Fragata detecta pistas de audio RTSP compatibles y las distribuye por el mismo hub que el video. Actualmente se admiten:

- G.711 A-law (`PCMA`), habitual en cámaras Imou/Dahua.
- G.711 μ-law (`PCMU`).
- Opus mono o estéreo.
- AAC transportado como MPEG-4 Audio.

El video y el audio originales se guardan sin recomprimir en el mismo MKV. Para la vista web, el video y el audio usan sesiones WebRTC independientes: una falla de audio nunca reinicia ni bloquea el video. G.711 y Opus se envían directamente; cuando la cámara usa AAC y FFmpeg está disponible, Fragata inicia una conversión auxiliar a PCMU únicamente después de que el usuario solicita sonido. La grabación conserva el AAC original.

El visor inicia siempre con una sesión de video sin audio y permanece en `Conectando` hasta confirmar un fotograma decodificado. El audio se negocia solo al pulsar **Activar sonido**, respetando las políticas de reproducción automática del navegador y evitando conexiones RTSP adicionales mientras no se necesitan.

## Servidores SFTP globales

La página **Configuración → Servidores SFTP** permite crear varios perfiles y reutilizarlos en distintas cámaras. Cada perfil contiene host, puerto, usuario, contraseña cifrada o ruta de llave privada, `known_hosts`, directorio remoto, timeout y la opción de eliminar la copia local después de verificar la subida.

El bloque `FRAGATA_SFTP_*` de `.env` sigue siendo compatible y aparece como un perfil global de solo lectura. Al agregar o editar una cámara se selecciona explícitamente qué perfil utilizar. La cola persistente guarda el identificador del perfil para que un archivo siempre se reintente contra el servidor que tenía asignado.

Un perfil no puede eliminarse mientras esté asignado a una cámara o tenga subidas pendientes. Fragata valida obligatoriamente la clave del host mediante `known_hosts`; no utiliza verificaciones inseguras.

## Retención automática y logs

Desde **Configuración → Almacenamiento** puede activarse una política global para conservar grabaciones durante una cantidad de días, meses o años. La política se ejecuta al iniciar, inmediatamente después de guardarla y luego según `FRAGATA_RETENTION_INTERVAL`.

La limpieza:

- Solo elimina archivos `.mkv` finalizados cuya fecha de modificación sea anterior al corte.
- Nunca elimina `.mkv.partial`.
- Nunca elimina archivos presentes en la cola SFTP.
- Elimina directorios vacíos después de completar el barrido.

Fragata escribe eventos tanto en la salida estándar como en `FRAGATA_LOG_PATH`. El archivo `logs.txt` se mantiene por debajo de 1 MiB; al alcanzar el límite conserva los registros recientes y elimina primero líneas completas antiguas. No registra contraseñas ni URLs con credenciales sin censurar.

## Docker Compose

En servidores Linux, `docker-compose.yml` usa `network_mode: host`. Fragata comparte la red del host para alcanzar cámaras LAN y recibir WS-Discovery multicast. No se declara `ports:` porque el servicio escucha directamente en el puerto configurado del host.

El Compose de producción exige usuario y contraseña, ejecuta con el UID/GID indicado, elimina todas las capabilities, activa `no-new-privileges`, usa raíz de solo lectura y limita procesos. La imagen final Alpine incluye FFmpeg y FFprobe. Solo quedan escribibles `/data`, `/recordings` y un `/tmp` temporal sin ejecución.

Prepara las carpetas visibles en el host con el mismo UID/GID definido en `.env`:

```bash
cp .env.example .env
# Edita .env y define una contraseña de al menos 12 caracteres.
mkdir -p data recordings
sudo chown -R "$(id -u):$(id -g)" data recordings
docker compose build
docker compose up -d
docker compose logs -f fragata
```

Montajes predeterminados:

```text
./data       -> /data        state.json, secret.key, logs.txt y eventos
./recordings -> /recordings  segmentos MKV visibles y respaldables desde Linux
```

Las rutas pueden cambiarse con `FRAGATA_HOST_DATA_DIR` y `FRAGATA_HOST_RECORDINGS_DIR`. Compose exige que ambas carpetas ya existan para evitar que Docker las cree como `root`. `./config` se monta como solo lectura en `/etc/fragata/config`; un diccionario personalizado debe usar, por ejemplo, `FRAGATA_RTSP_DICTIONARY=/etc/fragata/config/rtsp-paths.txt`. Para una llave SFTP, monta una carpeta de secretos como solo lectura y apunta `FRAGATA_SFTP_PRIVATE_KEY` al archivo dentro de `/run/secrets/fragata`.

Después abre:

```text
http://IP_DEL_SERVIDOR:8080
```

Si el entorno no admite red del host, usa:

```bash
docker compose -f docker-compose.bridge.yml up -d --build
```

Con bridge, el acceso manual por IP puede funcionar si el firewall permite forwarding, pero el descubrimiento multicast ONVIF puede no atravesar esa red.

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
| `GET` | `/api/cameras/{id}` | Consultar una cámara |
| `POST` | `/api/cameras` | Detectar, validar y agregar cámara |
| `PATCH` | `/api/cameras/{id}` | Activar o detener la grabación |
| `POST` | `/api/cameras/{id}/redetect` | Volver a elegir stream principal y vista |
| `POST` | `/api/rtsp/probe` | Probar una URL RTSP sin guardarla |
| `POST` | `/api/network/diagnose` | Diagnosticar puertos y alcance de red hacia una cámara |
| `DELETE` | `/api/cameras/{id}` | Eliminar configuración |
| `POST` | `/api/discovery` | WS-Discovery ONVIF |
| `GET` | `/api/status` | Estado de streams y grabación |
| `GET` | `/api/events` | Listar eventos de movimiento y persona |
| `GET` | `/api/events/{id}` | Consultar detalle, vínculo y estado de grabación del evento |
| `GET` | `/api/events/{id}/snapshot` | Servir de forma protegida la miniatura de un evento |
| `GET` | `/api/events/{id}/video` | Reproducir el segmento desde el instante del evento mediante MP4 fragmentado |
| `GET` | `/api/events/{id}/recording` | Descargar o abrir el MKV original relacionado |
| `GET` | `/api/recordings` | Listar videos por cámara y día con metadatos y eventos |
| `GET` | `/api/recordings/sources` | Listar cámaras y carpetas archivadas con grabaciones |
| `GET` | `/api/recordings/days` | Consultar días disponibles, cantidad y tamaño |
| `GET` | `/api/recordings/{id}/video` | Reproducir desde un segundo mediante MP4 fragmentado |
| `GET` | `/api/recordings/{id}/file` | Descargar el MKV original |
| `GET` | `/api/uploads` | Cola SFTP |
| `GET/POST` | `/api/sftp-profiles` | Listar o crear perfiles SFTP globales |
| `PATCH/DELETE` | `/api/sftp-profiles/{id}` | Modificar o eliminar un perfil global |
| `POST` | `/api/sftp-profiles/{id}/test` | Probar conexión y directorio remoto |
| `GET/PATCH` | `/api/retention` | Consultar o cambiar la política global de retención |
| `POST` | `/api/cameras/{id}/offer` | Negociar una sesión WebRTC explícita de `video` o `audio` |

Las operaciones mutables requieren el encabezado `X-Fragata-CSRF` cuando el login está habilitado.

## Seguridad y límites

Antes de publicar el panel, complete la lista de [`SECURITY.md`](SECURITY.md).

- De forma predeterminada se aceptan IP privadas, IP públicas y dominios/CNAME. Esta flexibilidad permite cámaras remotas, pero el panel debe protegerse con autenticación, HTTPS y firewall. `FRAGATA_ALLOW_PUBLIC_CAMERAS=false` restaura la restricción privada/local.
- La búsqueda RTSP está limitada por puertos, número de candidatos, tiempo y paralelismo; no realiza fuerza bruta de credenciales.
- Las contraseñas de las cámaras no se devuelven por API y se cifran en disco.
- Al usar FFmpeg externo, la URL RTSP con credenciales se entrega como argumento del proceso; ejecuta Fragata bajo un usuario dedicado y evita que otros usuarios del sistema puedan inspeccionar sus procesos.
- La llave maestra se crea en `data/secret.key` con permisos `0600` si no se proporciona mediante entorno.
- No se usa `InsecureIgnoreHostKey` para SFTP.
- `insecure_tls` solo afecta una cámara ONVIF HTTPS concreta cuando se solicita desde la API.
- No publiques el puerto de Fragata directamente en Internet sin HTTPS, firewall y una contraseña robusta.
- El audio PCMA, PCMU y Opus se reproduce directamente; AAC se conserva en MKV y puede convertirse de forma opcional para el navegador mediante FFmpeg sin afectar el video.
- Los clusters MKV se descargan al archivo aproximadamente cada 5 segundos para limitar memoria y pérdida ante cortes.
- `FRAGATA_MAX_VIEWERS` limita espectadores; internamente se reservan hasta dos sesiones WebRTC por visor, una de video y otra opcional de audio.
- `FRAGATA_MAX_TRANSCODES` limita procesos FFmpeg de reproducción histórica y devuelve `429` cuando el servidor está ocupado.
- Los identificadores de grabación codifican rutas relativas; el servidor valida contención, extensión, estructura y rechaza enlaces simbólicos antes de abrir un archivo.
- `FRAGATA_LIVE_IDLE_TIMEOUT` apaga FFmpeg o el substream de vista cuando ya no existen espectadores.
- El escritor H.265 debe validarse con los modelos reales que se usarán antes de considerarlo producción estable.
- La URL de snapshot se restringe a HTTP(S) y al mismo host configurado para la cámara; las URI ONVIF se normalizan hacia ese host, se cifran en el estado local y sus parámetros sensibles se ocultan en la API.
- Los snapshots se limitan a 8 MiB y 32 megapíxeles antes de decodificarlos para evitar consumo de memoria no acotado.
- Las miniaturas de eventos se sirven por una ruta autenticada con comprobación de contención para impedir path traversal.
- El detector humano no carga código nativo ni modelos externos: los pesos HOG/SVM están embebidos en el binario.

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

1. Agregar una cámara indicando IP o dominio y credenciales.
2. Confirmar estado `en línea`.
3. Confirmar que el panel muestra la resolución máxima esperada; si es una cámara existente, usar **Redetectar calidad**.
4. Abrir la página dedicada, probar pantalla completa y revisar si el modo es directo, FFmpeg o substream.
5. Activar el switch de grabación y esperar el cierre de un segmento MKV.
6. Abrirlo en VLC o mpv y verificar codec, ancho y alto con `ffprobe`.
7. Cortar la red de la cámara y confirmar reconexión automática.
8. Mantener abierto el visor, detener y volver a iniciar Fragata, y confirmar que la reproducción se recupera sin recargar la página; después revisar la recuperación del `.partial`.
9. Activar el sonido en el visor y confirmar que la pista se escucha cuando la cámara la ofrece.
10. Revisar con `ffprobe` que el MKV contiene una pista de audio compatible.
11. Crear dos perfiles SFTP, asignar uno a la cámara y confirmar creación del MKV y `.sha256` remotos.
12. Aplicar una retención corta sobre archivos de prueba y comprobar que no elimina `.partial` ni subidas pendientes.
13. Generar actividad y confirmar que `logs.txt` nunca supera 1 MiB.
14. Activar detección y grabación, caminar dentro de la zona configurada y comprobar el evento, su miniatura y el vínculo temporal al MKV en `/events`.
15. Cambiar la zona para excluir movimiento irrelevante y verificar que no se creen eventos fuera de ella.

## Estructura

```text
cmd/fragata/          punto de entrada
internal/auth/        sesiones persistentes, login y CSRF
internal/camera/      descubrimiento, configuración y supervisión de cámaras
internal/detection/   movimiento, HOG/SVM humano y generación de eventos en Go puro
internal/httpapi/     API y panel web embebido
internal/live/        access units H.264 normalizadas hacia WebRTC
internal/matroska/    escritor MKV sin CGO
internal/onvif/       WS-Discovery y SOAP ONVIF
internal/recording/   segmentación, audio y recuperación
internal/rtsp/        conexión RTSP, sondeo de puertos y diccionario de rutas
internal/logging/     logs.txt rotativo con límite estricto
internal/retention/   limpieza segura por antigüedad
internal/store/       estado JSON atómico y secretos cifrados
internal/stream/      distribución interna de RTP y access units
internal/transcode/   FFmpeg opcional y reconstrucción RTP/H.264 para WebRTC
internal/upload/      cola y transferencia SFTP
contexto/             decisiones técnicas persistentes
```

