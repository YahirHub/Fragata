# Fecha
2026-07-05

# Objetivo
Corregir el visor que permanecía en `Conectando` y repetía ofertas WebRTC después de incorporar audio.

# Decisiones tomadas
- Separar video y audio en sesiones WebRTC independientes.
- La sesión de video se negocia primero y es el único requisito para revelar el visor.
- El audio se negocia únicamente cuando el usuario pulsa **Activar sonido**.
- Un error, timeout o codec de audio incompatible no reinicia la sesión de video.
- La conversión AAC usa un proceso FFmpeg auxiliar iniciado bajo demanda.
- El proceso FFmpeg principal vuelve a generar exclusivamente video H.264.
- La consulta `/api/status` se realiza cada cinco segundos y se pausa con la pestaña oculta.

# Arquitectura actual
- `POST /api/cameras/{id}/offer` recibe `media: video|audio`.
- `internal/live` crea peers separados para video y audio.
- `internal/camera` mantiene el hub de video y activa el convertidor AAC solo cuando se solicita audio.
- G.711 y Opus se puentean al hub de vista sin abrir otra conexión RTSP.
- AAC abre una conexión auxiliar únicamente durante el uso de audio en vivo.

# Librerías usadas
- Pion WebRTC v4.
- FFmpeg opcional como proceso externo.
- Biblioteca estándar de Go para contexto, sincronización y procesos.

# Archivos importantes modificados
- `internal/live/webrtc.go`
- `internal/camera/manager.go`
- `internal/transcode/ffmpeg.go`
- `internal/transcode/audio.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/viewer.html`
- `README.md`
- `CHANGELOG.md`

# Problemas encontrados
- Video y audio compartían una sola negociación; un problema en la pista de audio podía impedir confirmar fotogramas.
- FFmpeg generaba video y audio en el mismo proceso; un fallo de mapeo de audio terminaba también el video.
- El visor solicitaba audio automáticamente, creando ofertas y conexiones adicionales aunque el usuario no hubiera activado sonido.

# Soluciones implementadas
- Peer de video aislado y estable.
- Peer de audio opcional e independiente.
- FFmpeg de video sin salidas de audio.
- FFmpeg de audio AAC independiente y bajo demanda.
- Reintentos de audio silenciosos sin modificar el estado visual del video.
- Menor frecuencia de polling y pausa cuando la página no está visible.

# Pendientes
- Validar audio AAC, PCMA, PCMU y Opus con cámaras reales de distintos fabricantes.
- Confirmar el límite de conexiones RTSP simultáneas de cada modelo.

# Próximos pasos
- Ejecutar `go mod tidy`, `go test ./...`, `go vet ./...` y compilar en el entorno del usuario.
- Probar primero video sin activar sonido y después habilitar audio desde la interfaz.
