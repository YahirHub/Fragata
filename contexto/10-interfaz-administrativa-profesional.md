# Fecha

2026-07-05

# Objetivo

Modernizar la interfaz de Fragata con un panel administrativo profesional, layouts reutilizables y navegación completamente responsiva.

# Decisiones tomadas

- Se usa Bootstrap 5.3.8 mediante CDN versionado y con `integrity` para CSS y JavaScript.
- Se usa Bootstrap Icons 1.13.1 mediante CDN.
- Se mantiene frontend sin NPM, bundler ni compilación adicional.
- `fragata-app-layout` encapsula sidebar, topbar, usuario, footer y contenedor de notificaciones.
- `fragata-auth-layout` encapsula la presentación del login.
- El sidebar es fijo desde `lg` y usa `offcanvas` en móviles.
- El usuario mostrado es el administrador configurado o `Invitado` cuando el login está deshabilitado.
- El cierre de sesión solo aparece cuando la autenticación está activa.
- Dashboard y visor comparten el mismo layout de aplicación.
- La CSP admite exclusivamente `cdn.jsdelivr.net` para los recursos externos requeridos.

# Arquitectura actual

`HTML de página -> Web Component de layout -> Bootstrap + CSS de Fragata -> JavaScript funcional existente`

Los layouts usan Light DOM para que Bootstrap y el CSS local se apliquen sin Shadow DOM ni duplicación de estilos.

# Librerías usadas

- Bootstrap 5.3.8 por CDN.
- Bootstrap Icons 1.13.1 por CDN.
- Web Components nativos del navegador.
- JavaScript y CSS propios sin dependencias de build.

# Archivos importantes modificados

- `internal/httpapi/server.go`
- `internal/httpapi/static/layouts.js`
- `internal/httpapi/static/styles.css`
- `internal/httpapi/static/index.html`
- `internal/httpapi/static/login.html`
- `internal/httpapi/static/login.js`
- `internal/httpapi/static/viewer.html`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/app.js`
- `internal/httpapi/static/duration-picker.js`
- `README.md`
- `CHANGELOG.md`

# Problemas encontrados

La CSP anterior bloqueaba cualquier script, estilo o fuente CDN. Además, dashboard, login y visor duplicaban estructuras visuales y no tenían sidebar, footer ni un widget centralizado de usuario.

# Soluciones implementadas

Se añadieron layouts reutilizables, navegación adaptativa, dropdown de usuario, footer, tarjetas de métricas, estados visuales, toasts, formularios Bootstrap y una CSP limitada a jsDelivr.

# Pendientes

- Validar visualmente en Safari iOS y navegadores Android reales.
- Evaluar una copia local opcional de Bootstrap para instalaciones completamente aisladas de internet.
- Añadir tema oscuro configurable si se solicita.

# Próximos pasos

Ejecutar `go mod tidy`, compilar, abrir el panel en escritorio y móvil, y verificar login, modo Invitado, dropdown, sidebar, offcanvas, visor y pantalla completa.
