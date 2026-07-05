# Fecha

2026-07-05

# Objetivo

Evitar que el usuario vea errores técnicos, mensajes de recuperación o video negro mientras Fragata prepara y recupera la vista en vivo.

# Decisiones tomadas

- El video permanece oculto hasta que el navegador confirma un fotograma decodificado.
- El único texto visible durante preparación y recuperación es `Conectando`.
- La reconexión continúa siendo completamente automática y no depende de un botón manual.
- Los errores temporales de WebRTC no se muestran al usuario final.
- Los recursos embebidos del panel se sirven sin caché para evitar que una versión antigua del visor reaparezca después de actualizar el binario.

# Arquitectura actual

- `viewer.js` controla el estado visual mediante `is-loading` e `is-ready`.
- `requestVideoFrameCallback` confirma fotogramas cuando el navegador lo soporta.
- El respaldo valida `readyState`, `videoWidth`, `videoHeight` y avance temporal antes de revelar el elemento de video.
- La sesión WebRTC se recrea automáticamente mientras no haya video utilizable.
- El servidor HTTP aplica `Cache-Control: no-store` a HTML y recursos estáticos.

# Librerías usadas

No se agregaron dependencias. La animación utiliza CSS y Bootstrap Icons ya incorporado por CDN.

# Archivos importantes modificados

- `internal/httpapi/server.go`
- `internal/httpapi/static/viewer.html`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/styles.css`
- `internal/httpapi/static/layouts.js`
- `CHANGELOG.md`

# Problemas encontrados

- Un mensaje técnico antiguo podía reaparecer por caché del navegador.
- El elemento de video podía hacerse visible antes de confirmar una imagen decodificada en navegadores sin `requestVideoFrameCallback`.
- El botón Reconectar exponía al usuario una recuperación que debe manejar el sistema.

# Soluciones implementadas

- Animación de carga permanente hasta recibir video real.
- Video oculto durante conexión, reconexión y estancamiento.
- Reintentos silenciosos e indefinidos con backoff.
- Invalidación de caché mediante versión de recursos y encabezados HTTP `no-store`.

# Pendientes

- Validar visualmente en los navegadores y dispositivos usados por el usuario.
- Medir el tiempo de primer fotograma con cámaras H.264 y H.265.

# Próximos pasos

Ejecutar las pruebas y compilación en el entorno del usuario y verificar desconexiones reales de cámara, red y FFmpeg.
