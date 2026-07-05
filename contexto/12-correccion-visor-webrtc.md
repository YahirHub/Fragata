# Fecha
2026-07-05

# Objetivo
Corregir el visor que podía establecer WebRTC pero permanecer negro y reorganizar la pantalla para mostrar primero el video completo, seguido de sus controles y opciones.

# Decisiones tomadas
- Los streams RTSP H.264 directos y substreams se publican a WebRTC desde access units completas.
- El visor espera un IDR antes de emitir video y antepone SPS/PPS cuando la cámara no los incluye en el fotograma clave.
- El perfil H.264 anunciado se deriva del SPS real.
- FFmpeg se usa también para H.264 Main/High Profile cuando está disponible, porque Baseline es el perfil WebRTC interoperable.
- El frontend solo oculta la capa de estado después de `loadeddata` o `playing`.
- El video usa su relación de aspecto detectada y `object-fit: contain`.

# Arquitectura actual
RTSP principal o substream H.264 -> decoder RTP existente -> access units -> empaquetado H.264 WebRTC -> navegador.

Para H.265 o H.264 no Baseline con FFmpeg disponible:
RTSP -> FFmpeg H.264 Baseline -> RTP -> WebRTC.

# Librerías usadas
- Pion WebRTC v4.
- Componentes RTSP/H.264 existentes de gortsplib.
- Bootstrap y Bootstrap Icons mediante CDN para la interfaz.

# Archivos importantes modificados
- `internal/live/webrtc.go`
- `internal/stream/hub.go`
- `internal/camera/manager.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/viewer.html`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/styles.css`

# Problemas encontrados
- El navegador podía incorporarse a mitad de un GOP y no recibir SPS/PPS o un IDR inicial.
- El track WebRTC anunciaba un perfil genérico diferente al perfil real de ciertas cámaras.
- La interfaz ocultaba la capa de carga al recibir el track, aunque todavía no hubiese un frame decodificado.
- El visor mantenía acciones antes del video y un alto máximo que podía resultar incómodo en algunas pantallas.

# Soluciones implementadas
- Espera de conexión WebRTC y del siguiente fotograma clave.
- Conversión de access units a Annex-B con parámetros de codec.
- Selección preferente de H.264 en el navegador.
- Timeout visual de frame y botón de reproducción para políticas de autoplay.
- Video al inicio y controles debajo, con diseño responsivo y sin recortes.

# Pendientes
- Ejecutar `go mod tidy`, `go test ./...` y prueba real con la cámara en el entorno del usuario.
- Validar H.264 High Profile directo en navegadores sin FFmpeg.

# Próximos pasos
Probar vista directa, substream y FFmpeg; confirmar reconexión y pantalla completa en escritorio y teléfono.
