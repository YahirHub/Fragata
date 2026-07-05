# Fecha
2026-07-05

# Objetivo
Documentar el alcance del MVP inicial de Fragata y separar con claridad la revisión local de las pruebas que requieren dependencias, cámara física y servidor SFTP.

# Decisiones tomadas
- Mantener el proyecto sin `go.sum` para que el propietario ejecute `go mod tidy` en su entorno.
- No ejecutar `go mod download`, `go mod tidy`, `go test` ni `go build` en el entorno de entrega.
- No incluir binarios, módulos descargados, datos de ejecución ni credenciales.
- Esta decisión fue sustituida en 0.3.0 por `08-calidad-grabacion-y-visor.md`: la grabación selecciona máxima resolución aunque sea H.265.
- El binario no requiere FFmpeg; puede invocarlo externamente y bajo demanda solo para la vista web.

# Arquitectura actual

```text
ONVIF o IP manual -> validación RTSP -> worker supervisado
  -> RTP H.264 -> WebRTC
  -> access units H.264/H.265 -> MKV parcial -> MKV final
  -> cola persistente -> SFTP temporal -> MKV + SHA-256
```

# Librerías usadas
- `github.com/bluenviron/gortsplib/v5`.
- `github.com/pion/rtp`.
- `github.com/pion/webrtc/v4`.
- `github.com/pkg/sftp`.
- `golang.org/x/crypto`.

Las versiones exactas están declaradas en `go.mod` y deben resolverse mediante `go mod tidy` en el entorno del propietario.

# Archivos importantes modificados
- Proyecto inicial completo.
- Configuración `.env` con login opcional y sesiones persistentes.
- Descubrimiento ONVIF y detección RTSP por IP.
- Grabación Matroska segmentada.
- Vista en vivo mediante WebRTC.
- Cola de subida SFTP persistente.
- Panel web, Docker, Compose, systemd y scripts de build.

# Problemas encontrados
- No se dispone de cámara física ni servidor SFTP dentro del entorno de entrega.
- La compilación real depende de módulos externos todavía no descargados.
- La compatibilidad ONVIF y RTSP varía según fabricante y firmware.
- WebRTC requiere H.264. Desde 0.3.0 Fragata puede obtenerlo mediante FFmpeg externo o un substream H.264, sin alterar la grabación principal.

# Soluciones implementadas
- Fallback desde ONVIF hacia rutas RTSP comunes de Imou/Dahua, Hikvision, Reolink y cámaras genéricas.
- Validación de IP privada/local para reducir SSRF.
- Cifrado AES-256-GCM de contraseñas de cámaras en el estado local.
- Sesiones persistentes guardando solo el hash del token.
- Escritura MKV mediante archivo `.partial`, sincronización y renombrado atómico.
- Subida SFTP mediante archivo remoto `.part`, verificación de tamaño, checksum y renombrado final.

# Pendientes
- Ejecutar `go mod tidy`.
- Ejecutar `go test ./...` y `go vet ./...`.
- Compilar con `CGO_ENABLED=0`.
- Validar el descubrimiento y stream con la cámara Imou real.
- Reproducir MKV H.264/H.265 reales en VLC o mpv.
- Probar WebRTC en los navegadores objetivo.
- Probar SFTP contra el servidor real y simular reintentos.
- Simular reinicio abrupto, falta de red y disco lleno.

# Próximos pasos
Ejecutar los comandos de `README.md`, enviar cualquier error completo de `go mod tidy`, compilación o pruebas y corregirlo sobre esta misma base.
