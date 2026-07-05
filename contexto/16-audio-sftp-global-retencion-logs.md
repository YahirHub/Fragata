# Fecha

2026-07-05

# Objetivo

Agregar audio en vivo y grabado, registros locales rotativos, perfiles SFTP globales reutilizables y limpieza automática de grabaciones antiguas.

# Decisiones tomadas

- Mantener el video y audio originales en MKV sin transcodificar.
- Admitir PCMA, PCMU, Opus y AAC desde RTSP.
- Usar FFmpeg únicamente para convertir AAC a PCMU en la vista web cuando el navegador no puede recibir AAC por WebRTC.
- Mantener `logs.txt` como un único archivo de hasta 1 MiB, conservando primero las líneas recientes.
- Guardar múltiples perfiles SFTP cifrados y asignarlos por cámara.
- Aplicar una política global de retención en días, meses o años.
- Nunca eliminar parciales ni archivos presentes en la cola SFTP.

# Arquitectura actual

- `internal/rtsp` descubre y publica video y audio con timestamps.
- `internal/stream` distribuye access units de video y paquetes de audio.
- `internal/matroska` escribe pistas de video y audio en MKV.
- `internal/live` crea pistas WebRTC de video y audio.
- `internal/transcode` normaliza video y convierte AAC a PCMU solo para la vista.
- `internal/logging` mantiene el archivo rotativo.
- `internal/upload` resuelve perfiles SFTP por trabajo persistente.
- `internal/retention` elimina grabaciones finalizadas fuera de retención.

# Librerías usadas

- Biblioteca estándar de Go.
- `github.com/bluenviron/gortsplib/v5` para RTSP/RTP.
- `github.com/pion/webrtc/v4` y `github.com/pion/rtp` para WebRTC/RTP.
- `github.com/pkg/sftp` y `golang.org/x/crypto/ssh` para SFTP seguro.
- FFmpeg como proceso externo opcional para compatibilidad del visor.

# Archivos importantes modificados

- `cmd/fragata/main.go`
- `internal/rtsp/client.go`
- `internal/stream/hub.go`
- `internal/matroska/writer.go`
- `internal/recording/recorder.go`
- `internal/live/webrtc.go`
- `internal/transcode/ffmpeg.go`
- `internal/logging/rolling.go`
- `internal/retention/cleaner.go`
- `internal/upload/uploader.go`
- `internal/store/store.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/settings-sftp.*`
- `internal/httpapi/static/settings-storage.*`
- `internal/httpapi/static/viewer.*`

# Problemas encontrados

- AAC no puede anunciarse directamente como pista WebRTC compatible en todos los navegadores.
- FFmpeg reinicia el hub de video al comenzar y podía borrar temporalmente el metadato de audio replicado.
- Un perfil SFTP no debe desaparecer mientras una cámara o trabajo pendiente lo referencia.
- La limpieza por antigüedad puede causar pérdida si elimina un archivo aún pendiente de respaldo.

# Soluciones implementadas

- AAC se decodifica desde RTP a access units para conservarlo dentro del MKV.
- FFmpeg convierte AAC a PCMU únicamente para la vista web.
- El puente de audio repone metadatos después de cada reinicio del hub transcodificado.
- Las contraseñas SFTP se cifran con la misma clave maestra del estado.
- La eliminación de perfiles usados devuelve conflicto.
- La retención protege `.mkv.partial` y rutas presentes en la cola SFTP.
- `logs.txt` compacta líneas antiguas al alcanzar 1 MiB.

# Pendientes

- Validar audio real con modelos de cámaras que usen PCMA, AAC y Opus.
- Confirmar reproducción de cada combinación MKV con VLC y `ffprobe`.
- Ejecutar pruebas y compilación después de `go mod tidy` en el entorno del usuario.

# Próximos pasos

- Probar el visor con sonido activado desde una cámara real.
- Confirmar una subida con dos perfiles SFTP distintos.
- Probar retención con archivos simulados de distintas fechas.
