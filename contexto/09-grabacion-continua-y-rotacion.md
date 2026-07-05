# Fecha

2026-07-05

# Objetivo

Permitir configurar por cámara la duración de cada MKV y mantener grabación continua durante rotaciones, cambios de configuración y reconexiones RTSP.

# Decisiones tomadas

- Cada cámara persiste `segment_duration_seconds`.
- El rango permitido es de 60 a 86400 segundos y se guarda en minutos completos.
- `FRAGATA_SEGMENT_DURATION` queda como valor inicial y de migración.
- El selector de duración es un Web Component reutilizable en alta, tarjetas y visor.
- Activar o detener grabación no reinicia el stream RTSP.
- Cambiar la duración actualiza un valor atómico que el grabador consulta en cada fotograma clave.
- La rotación abre y escribe el archivo nuevo antes de finalizar el anterior.
- La finalización anterior se ejecuta en una cola separada para que `fsync` no bloquee el stream.
- Cada sesión RTSP lleva una generación; una desconexión finaliza el MKV y la reconexión inicia otro desde un fotograma clave.

# Arquitectura actual

`worker RTSP -> Hub -> suscripción confiable -> Recorder -> MKV actual`

En una rotación: `crear siguiente -> escribir keyframe -> publicar siguiente como activo -> finalizar anterior en segundo plano`.

# Librerías usadas

No se agregaron dependencias. Se usa la biblioteca estándar y las dependencias ya existentes.

# Archivos importantes modificados

- `internal/model/model.go`
- `internal/camera/manager.go`
- `internal/recording/recorder.go`
- `internal/stream/hub.go`
- `internal/rtsp/client.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/duration-picker.js`
- `internal/httpapi/static/app.js`
- `internal/httpapi/static/viewer.js`

# Problemas encontrados

La implementación anterior cerraba y sincronizaba el MKV dentro del mismo bucle que consumía video. Un disco lento podía llenar el buffer y provocar pérdida de access units. Además, activar o detener grabación reiniciaba el worker RTSP, y una reconexión podía mezclar timestamps reiniciados en el mismo archivo.

# Soluciones implementadas

Rotación solapada, finalización asíncrona, control independiente del grabador, duración atómica por cámara y marcadores de discontinuidad por generación RTSP.

# Pendientes

- Validar con cámaras reales de distintos GOP y discos lentos.
- Añadir métricas explícitas de backlog y latencia de escritura.

# Próximos pasos

Ejecutar `go mod tidy`, pruebas y una grabación real de al menos dos rotaciones por cámara.
