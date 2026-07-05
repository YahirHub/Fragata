# Fecha
2026-07-05

# Objetivo
Definir cómo una sola conexión de cámara alimenta grabación y vista en vivo con consumo reducido.

# Decisiones tomadas
- Cada cámara tiene un worker y una conexión RTSP supervisada.
- El worker publica RTP y unidades de acceso en un hub de memoria.
- WebRTC consume RTP H.264 sin recodificar y aplica un límite global de espectadores.
- El grabador consume unidades de acceso H.264/H.265.
- Los suscriptores lentos descartan paquetes en lugar de bloquear la ingestión.
- La reconexión utiliza backoff de 2 a 30 segundos y se reinicia después de recibir video nuevamente.

# Arquitectura actual
```text
Cámara RTSP
  -> decoder RTP
     -> RTP Hub -> WebRTC
     -> Access Units Hub -> MKV
```

# Librerías usadas
- `gortsplib/v5`
- `pion/rtp`
- `pion/webrtc/v4`

# Archivos importantes modificados
- `internal/rtsp/client.go`
- `internal/stream/hub.go`
- `internal/live/webrtc.go`
- `internal/camera/manager.go`

# Problemas encontrados
- Cerrar una conexión WebRTC desde múltiples callbacks podía repetir la limpieza.
- El backoff podía mantenerse alto después de una conexión estable.

# Soluciones implementadas
- Limpieza WebRTC protegida por `sync.Once`.
- Reinicio del backoff cuando se recibió al menos un paquete.
- Conversión explícita de PTS desde unidades del reloj RTP a `time.Duration`.
- Actualización de SPS/PPS/VPS únicamente cuando los parámetros cambian.
- Límite configurable `FRAGATA_MAX_VIEWERS` para evitar conexiones WebRTC ilimitadas.
- Copia de paquetes y unidades de acceso al publicarlos para evitar aliasing.

# Pendientes
- Solicitar keyframes mediante RTCP cuando un espectador se conecta.
- Añadir métricas de paquetes descartados.
- Evaluar audio WebRTC.

# Próximos pasos
Probar navegadores Chromium y Firefox con perfiles H.264 Baseline/Main/High de cámaras reales.
