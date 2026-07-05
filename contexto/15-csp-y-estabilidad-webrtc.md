# Fecha

5 de julio de 2026

# Objetivo

Corregir el visor que podía permanecer en `Conectando` aunque la cámara funcionara correctamente en VLC, eliminar violaciones CSP y estabilizar la entrega WebRTC cuando se usa FFmpeg.

# Decisiones tomadas

- Mantener una CSP estricta, permitiendo únicamente atributos de estilo dinámicos necesarios para Bootstrap mediante `style-src-attr`.
- Permitir conexiones a jsDelivr exclusivamente para mapas de código solicitados por las herramientas de desarrollo.
- Eliminar el atributo `style` propio del visor y representar relaciones de aspecto mediante clases CSS.
- Usar FFmpeg para normalizar cualquier stream principal cuando esté disponible.
- No reenviar RTP de FFmpeg directamente a cada navegador.
- Reconstruir access units H.264 completas, detectar SPS, PPS e IDR y publicarlas en el hub.
- Entregar a cada visor el GOP almacenado desde el último fotograma clave.
- Mantener fallback automático a H.264 directo o substream si FFmpeg falla.

# Arquitectura actual

La cámara principal continúa alimentando la grabación original sin recomprimir. Para la vista web, FFmpeg opcional produce RTP H.264 Baseline; Fragata recompone NAL units y access units, conserva un GOP acotado y Pion genera un stream WebRTC nuevo para cada visor.

# Librerías usadas

No se agregaron dependencias. Se reutilizan Pion RTP, Pion WebRTC y las estructuras internas de stream.

# Archivos importantes modificados

- `internal/httpapi/server.go`
- `internal/httpapi/static/viewer.html`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/styles.css`
- `internal/camera/manager.go`
- `internal/transcode/ffmpeg.go`
- `internal/transcode/ffmpeg_test.go`
- `internal/live/webrtc.go`

# Problemas encontrados

- La CSP bloqueaba el atributo de estilo que definía la relación de aspecto.
- Bootstrap puede aplicar estilos de posición dinámicos que también requieren permiso para atributos de estilo.
- DevTools solicitaba mapas de código del CDN y `connect-src` los bloqueaba.
- El modo FFmpeg reenviaba RTP crudo; un navegador nuevo podía empezar a mitad del GOP y permanecer negro.

# Soluciones implementadas

- CSP compatible con Bootstrap sin permitir bloques `<style>` inline.
- Relación de aspecto mediante clases CSS.
- Access units H.264 reconstruidas desde NAL unit, STAP-A y FU-A.
- GOP transcodificado reutilizable para nuevos visores.
- SPS/PPS conservados e inyectados antes del primer IDR.
- Fallback automático cuando FFmpeg termina o no produce video.

# Pendientes

- Ejecutar `go mod tidy`, `go test ./...` y una prueba real en el equipo del usuario.
- Confirmar consumo de CPU al normalizar streams 3 MP o superiores.

# Próximos pasos

Validar en Chrome y Firefox que el visor sale de `Conectando`, revisar la consola sin violaciones CSP y comprobar reconexiones repetidas de la cámara y del navegador.
