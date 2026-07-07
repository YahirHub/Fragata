# 25. Eventos ONVIF nativos

## Decisión

Fragata deja de descargar snapshots y de analizar movimiento o personas localmente. Desde la versión 0.9.3, el único origen de eventos nuevos es el servicio ONVIF Events publicado por cada cámara.

## Flujo

1. Se descubre el endpoint Events mediante `GetCapabilities` o `GetServices`.
2. Al activar el interruptor se prueba `CreatePullPointSubscription`.
3. El worker mantiene `PullMessages` con espera larga y un límite de mensajes.
4. Antes del vencimiento se cancela y crea una suscripción nueva.
5. Ante errores se aplica reconexión exponencial acotada.
6. Las notificaciones activas se guardan con tópico, hora, cámara y vínculo al segmento MKV actual.

## Compatibilidad

El campo JSON `detection_enabled` se conserva para no romper estados existentes, pero ahora significa “eventos ONVIF habilitados”. Los campos antiguos de snapshot, sensibilidad, zona y detector se ignoran al leer `state.json` y desaparecen la próxima vez que se persiste la cámara.

Los eventos históricos conservan `snapshot_path`, dimensiones y reproducción. No se elimina `data/events/`, porque puede contener miniaturas todavía válidas de versiones anteriores. Los eventos nuevos usan `source=onvif` y no crean imágenes.

## Archivos eliminados

- `internal/detection/` completo.

## Seguridad

- Los endpoints devueltos por la cámara se normalizan al host registrado.
- Las solicitudes usan timeouts y se cancelan al detener el worker.
- Las transiciones de inicialización e inactividad no generan eventos.
- Se deduplican notificaciones idénticas durante treinta segundos.
