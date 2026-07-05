# Fecha

2026-07-05

# Objetivo

Evitar que el visor quede conectado pero negro cuando la sesión WebRTC inicia entre fotogramas clave, la pista se interrumpe o el navegador no recibe un fotograma decodificable.

# Decisiones tomadas

- La interfaz mantiene un único estado visible: `Conectando`.
- El navegador confirma reproducción mediante fotogramas decodificados, no solamente mediante el estado `connected` de WebRTC.
- La sesión se recrea automáticamente ante timeout de fotogramas, pista terminada o silenciada, ICE fallido, video estancado y recuperación de red.
- Los reintentos usan un backoff corto y acotado para evitar bucles agresivos.
- El hub conserva temporalmente el GOP H.264 actual desde su último fotograma clave para que un visor nuevo pueda iniciar inmediatamente.
- El caché de GOP tiene límites de unidades y memoria; si un GOP excede esos límites se descarta y se espera el siguiente fotograma clave.
- Las suscripciones confiables usadas por el grabador no reciben datos históricos del caché.

# Arquitectura actual

- `viewer.js` administra una máquina de reconexión WebRTC basada en generaciones para ignorar callbacks de sesiones antiguas.
- `stream.Hub` conserva un GOP acotado y lo precarga únicamente en nuevas suscripciones no confiables destinadas a visualización.
- Una discontinuidad RTSP limpia el GOP para separar correctamente sesiones de cámara.

# Librerías usadas

No se agregaron dependencias. Se utiliza WebRTC del navegador y las estructuras existentes en Go.

# Archivos importantes modificados

- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/viewer.html`
- `internal/stream/hub.go`
- `internal/stream/hub_test.go`
- `internal/httpapi/static/layouts.js`
- `CHANGELOG.md`

# Problemas encontrados

- El visor podía suscribirse después de un IDR y esperar hasta el siguiente GOP.
- El estado WebRTC `connected` no garantiza que el navegador haya decodificado una imagen.
- Los errores temporales exigían pulsar manualmente Reconectar.

# Soluciones implementadas

- Caché temporal del GOP actual.
- Detección real de fotogramas decodificados.
- Reintento automático y limpieza completa de sesiones anteriores.
- Estado visual único durante todos los intentos.

# Pendientes

- Validar con distintos navegadores y cámaras los tiempos de recuperación.
- Evaluar métricas de reconexiones por cámara en una versión futura.

# Próximos pasos

Ejecutar `go mod tidy`, pruebas y compilación en el entorno del usuario, y comprobar desconexiones reales de cámara y red.
