# Fecha
2026-07-05

# Objetivo
Corregir la selección de streams para que Fragata grabe siempre la máxima resolución, aunque sea H.265, y separar esa decisión de la compatibilidad de la vista web.

# Decisiones tomadas
- `RTSPURL`, `Codec`, `Width` y `Height` representan el stream principal de máxima calidad.
- `LiveRTSPURL` y sus metadatos representan únicamente un respaldo H.264 para el navegador.
- ONVIF prueba los perfiles válidos y compara su resolución real/declarada; H.265 gana solo cuando la resolución empata.
- El diccionario conserva el orden por fabricante, pero compara una ventana acotada de variantes principal/secundaria y H.264/H.265 antes de decidir.
- FFmpeg es opcional, se detecta en `PATH` o mediante `FRAGATA_FFMPEG_PATH`, nunca participa en la grabación y se apaga después de un periodo sin espectadores.
- Para H.265, la vista usa primero FFmpeg sobre el stream principal y cae al substream H.264 si FFmpeg falla.
- Las cámaras nuevas se guardan siempre con grabación desactivada; no existe activación dentro del formulario de alta.
- El switch de grabación persiste el cambio y reinicia el worker para abrir o cerrar el recorder limpiamente.
- Se agregó redetección manual para migrar cámaras almacenadas con un substream antiguo.

# Arquitectura actual
- Un worker RTSP principal alimenta el hub usado por el recorder.
- H.264 principal comparte ese hub con WebRTC.
- H.265 crea bajo demanda un hub de vista alimentado por FFmpeg o por un segundo RTSP H.264, que se cierra automáticamente cuando no quedan espectadores.
- La página `/camera/{id}` negocia WebRTC y conserva la relación de aspecto con `object-fit: contain`.

# Librerías usadas
No se agregaron módulos Go. FFmpeg es un ejecutable externo opcional. Continúan gortsplib, Pion WebRTC/RTP y las dependencias existentes.

# Archivos importantes modificados
- `internal/camera/discover.go`
- `internal/camera/manager.go`
- `internal/rtsp/client.go`
- `internal/rtsp/dictionary.go`
- `internal/rtsp/dimensions.go`
- `internal/transcode/ffmpeg.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/index.html`
- `internal/httpapi/static/app.js`
- `internal/httpapi/static/viewer.html`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/styles.css`
- `internal/model/model.go`

# Problemas encontrados
- La selección anterior prefería cualquier H.264 sobre un H.265 de mayor resolución.
- El fallback del diccionario devolvía H.264 aunque primero hubiera encontrado la ruta principal H.265.
- Cuando ONVIF no entregaba resolución, el source escribía 1920×1080 de forma arbitraria.
- Las cámaras nuevas comenzaban a grabar inmediatamente.
- Las cámaras ya persistidas no se actualizan solo por cambiar el algoritmo de detección.

# Soluciones implementadas
- Selección máxima por resolución después de comparar todos los perfiles ONVIF válidos y las variantes cercanas del diccionario.
- Parser SPS básico para obtener dimensiones H.264/H.265 sin CGO.
- Streams principal y de vista separados.
- Transcodificador FFmpeg bajo demanda con salida RTP local H.264.
- Switch persistente y API PATCH para grabación.
- Redetección sin borrar la cámara.
- Visor dedicado con pantalla completa.

# Pendientes
- Validar `libx264` y reproducción WebRTC con H.265 real en los sistemas objetivo.
- Añadir selección de encoder por hardware si se requiere reducir CPU.
- Añadir audio en vivo y en MKV.

# Próximos pasos
Ejecutar `go mod tidy`, `go test ./...`, compilar y probar con una cámara Imou usando el stream principal H.265/H.264, el switch de grabación y la página dedicada.
