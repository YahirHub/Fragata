# Fecha
2026-07-05

# Objetivo
Crear Fragata, un servidor ligero de cámaras IP en Go que pueda detectar cámaras, grabar sus transmisiones en MKV, ofrecer vista en vivo y respaldar archivos por SFTP.

# Decisiones tomadas
- Un único binario con `CGO_ENABLED=0`.
- Sin transcodificación obligatoria dentro del proceso; desde 0.3.0 puede invocar FFmpeg externo de forma opcional solo para vista H.265.
- ONVIF se usa para descubrimiento y obtención de perfiles; RTSP transporta el video.
- El login es opcional: solo se activa cuando usuario y contraseña están completos en `.env`.
- Las sesiones son persistentes y sobreviven reinicios.
- La configuración sensible de cámaras se cifra con AES-256-GCM.
- El panel web se incrusta con `go:embed`.

# Arquitectura actual
- `cmd/fragata`: arranque y apagado ordenado.
- `internal/camera`: detección y workers por cámara.
- `internal/stream`: hub interno para grabador y espectadores.
- `internal/recording`: segmentos MKV.
- `internal/live`: WebRTC.
- `internal/upload`: cola SFTP.
- `internal/store`: estado persistente JSON con escritura atómica.

# Librerías usadas
- `gortsplib/v5` para RTSP/RTP.
- `pion/webrtc/v4` para vista en vivo.
- `pion/rtp` para paquetes RTP.
- `pkg/sftp` y `x/crypto/ssh` para SFTP.
- Biblioteca estándar para HTTP, configuración, cifrado, logs y ONVIF SOAP.

# Archivos importantes modificados
Proyecto inicial completo.

# Problemas encontrados
- El binario estático no incluye ni requiere FFmpeg; si existe en el host, puede invocarlo como proceso externo opcional.
- H.265 no tiene compatibilidad web uniforme.
- La escritura MKV sin una biblioteca C requiere implementar el subconjunto Matroska usado por cámaras.

# Soluciones implementadas
- Remux directo de H.264/H.265 sin pérdida.
- WebRTC continúa transportando H.264; un stream H.265 puede visualizarse mediante FFmpeg externo o un substream H.264.
- Segmentación y cierre atómico de MKV.
- Fallback de rutas RTSP cuando ONVIF no funciona.

# Pendientes
- Pruebas contra cámaras físicas variadas.
- Audio en MKV.
- Entrada SRT.
- Línea de tiempo y reproducción histórica.

# Próximos pasos
Validar el flujo completo con una cámara Imou real y un servidor SFTP real.
