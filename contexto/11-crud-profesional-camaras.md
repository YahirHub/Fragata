# Fecha

2026-07-05

# Objetivo

Separar la administración de cámaras del dashboard y construir un CRUD profesional con alta, listado, ajustes y visor independientes.

# Decisiones tomadas

- El dashboard queda dedicado a métricas y estado operativo.
- `/cameras` muestra todas las cámaras en una tabla responsiva con búsqueda, filtros y acciones.
- `/cameras/new` contiene el flujo de descubrimiento, diagnóstico, prueba RTSP y alta.
- `/cameras/<id>/settings` concentra identidad, carpeta, red, autenticación, grabación y SFTP.
- La contraseña no se devuelve al navegador; un campo vacío conserva la credencial cifrada actual.
- Cambios de IP, usuario, contraseña o URL se validan mediante detección antes de reemplazar la conexión almacenada.
- Cada cámara tiene una carpeta segura y única para grabaciones futuras.
- Cambiar la carpeta no mueve archivos anteriores y reinicia limpiamente el worker para cerrar el segmento activo.

# Arquitectura actual

- `internal/camera/settings.go` valida y aplica actualizaciones administrativas.
- `internal/httpapi/server.go` sirve las nuevas páginas y endpoints de prueba de ajustes.
- `internal/httpapi/static/core.js` centraliza sesión, API, CSRF, cola y utilidades de interfaz.
- Las páginas `cameras.html`, `camera-new.html` y `camera-settings.html` tienen scripts independientes.
- El recorder recibe `StorageFolder` y no depende únicamente del ID interno.

# Librerías usadas

- Biblioteca estándar de Go para validación, rutas, HTTP y persistencia.
- Bootstrap 5.3.8 y Bootstrap Icons 1.13.1 mediante CDN ya autorizado por CSP.

# Archivos importantes modificados

- `internal/model/model.go`
- `internal/camera/discover.go`
- `internal/camera/manager.go`
- `internal/camera/settings.go`
- `internal/recording/recorder.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/index.html`
- `internal/httpapi/static/cameras.html`
- `internal/httpapi/static/camera-new.html`
- `internal/httpapi/static/camera-settings.html`
- `internal/httpapi/static/core.js`
- `internal/httpapi/static/cameras.js`
- `internal/httpapi/static/camera-new.js`
- `internal/httpapi/static/camera-settings.js`
- `internal/httpapi/static/styles.css`

# Problemas encontrados

- El formulario de alta y las tarjetas de cámaras estaban mezclados dentro del dashboard.
- Solo podían modificarse grabación y duración; no existía edición completa de identidad, conexión o almacenamiento.
- Las grabaciones usaban el ID interno como carpeta, sin nombre configurable por el usuario.
- La lógica de API y sesión estaba duplicada entre páginas.

# Soluciones implementadas

- CRUD separado y navegación profesional.
- Actualización completa con validación previa de conexión.
- Carpeta normalizada, única y protegida contra separadores y rutas especiales.
- Estado `has_password` para informar que existe una contraseña sin exponerla.
- Componente común de API y sesión para nuevas páginas.
- Menú de acciones compacto y adecuado para tablas administrativas.

# Pendientes

- Ejecutar `go mod tidy`, `go test ./...`, `go vet ./...` y compilación en el entorno del usuario.
- Probar actualización real de credenciales y cambio de carpeta con una cámara activa.
- Considerar paginación cuando existan cientos de cámaras.

# Próximos pasos

Agregar explorador de grabaciones por cámara, retención local y línea de tiempo.
