# 28. Stream HTTP fMP4 para VPS

## Problema confirmado

El endpoint WebRTC devolvía una respuesta SDP válida, pero el único candidato ICE del servidor era una IP interna de Docker, por ejemplo `172.17.0.2:34633`. El navegador remoto podía completar la solicitud HTTP de señalización, pero no podía alcanzar el socket UDP del contenedor. El resultado visible era un loader permanente, `viewers: 0` y ofertas repetidas. FFmpeg sí estaba funcionando y la cámara continuaba grabando.

## Decisión vigente

La vista principal deja de depender de WebRTC. Se incorpora `internal/livestream`, que crea un MP4 fragmentado compatible con Media Source Extensions y lo transmite mediante `GET /api/cameras/{id}/live-stream`. La ruta está protegida por la misma sesión que el panel y usa exactamente el mismo puerto HTTP/HTTPS de Fragata.

Flujo:

```text
RTSP de la cámara
  -> FFmpeg compartido por cámara
  -> fMP4: ftyp + moov + moof/mdat
  -> protocolo binario en respuesta HTTP autenticada
  -> fetch ReadableStream
  -> MediaSource / SourceBuffer
  -> elemento video
```

## Compatibilidad y consumo

- H.264 se remultiplexa sin recomprimir.
- H.265 se convierte a H.264 mediante `libx264`, `ultrafast` y `zerolatency`.
- El audio disponible se normaliza a AAC.
- Todos los espectadores de la misma cámara comparten un solo FFmpeg.
- `FRAGATA_MAX_VIEWERS` limita clientes totales.
- `FRAGATA_MAX_LIVE_STREAMS` limita cámaras activas simultáneamente.
- El proceso termina después de `FRAGATA_LIVE_IDLE_TIMEOUT` sin espectadores.
- WebRTC y `/offer` permanecen como compatibilidad heredada, pero el visor ya no los usa.

## Seguridad

- La ruta está detrás de `auth.Require`.
- Las credenciales RTSP nunca se entregan al navegador.
- La respuesta usa `no-store`, `X-Accel-Buffering: no` y el mismo origen.
- Una URL copiada sin cookie de sesión válida responde `401`.
- En producción debe usarse HTTPS.

## Proxies

No se requieren puertos 8554, 8555 ni rangos UDP. El proxy debe permitir respuestas largas y desactivar el buffering. Fragata solicita esto mediante `X-Accel-Buffering: no`; en Nginx también conviene `proxy_buffering off` y un `proxy_read_timeout` amplio.

## Validaciones realizadas

- Parser de cajas MP4.
- Extracción dinámica del codec AVC desde `avcC`.
- Detección de audio AAC en `moov`.
- Generación real de fMP4 mediante FFmpeg con `frag_keyframe`, `empty_moov` y `default_base_moof`.
- Pruebas con detector de carreras del paquete `internal/livestream`.
- Sintaxis de JavaScript, YAML y scripts de despliegue.

La compilación integral sigue requiriendo Go 1.26.4 y dependencias externas disponibles en el VPS o CI.
