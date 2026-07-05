# Fecha

2026-07-05

# Objetivo

Vincular los eventos de movimiento y persona con la parte exacta de la grabación MKV, mejorar la presentación de snapshots y permitir reproducción histórica desde el navegador.

# Decisiones tomadas

- Guardar en cada evento la ruta relativa del segmento activo, su hora de inicio y el desplazamiento temporal.
- No modificar ni recomprimir el MKV archivado.
- Usar FFmpeg solo como adaptador opcional para reproducción web.
- Mantener H.264 mediante stream copy y convertir H.265 a H.264 sin cambiar la resolución.
- Comenzar la reproducción cinco segundos antes del evento para aportar contexto.
- Mantener una descarga directa del MKV original.
- Buscar grabaciones por cámara, fecha y hora para eventos anteriores sin vínculo persistido.
- Mostrar snapshots con dimensiones naturales y sin relación de aspecto forzada.

# Arquitectura actual

```text
Evento de detección
  ├── Snapshot original
  ├── Segmento MKV relativo
  ├── Inicio del segmento
  └── Offset exacto
       └── Página de evento
            ├── MP4 fragmentado temporal para navegador
            └── Descarga MKV original
```

# Librerías usadas

- Biblioteca estándar de Go para archivos, rutas, HTTP, procesos y tiempos.
- FFmpeg opcional ya detectado por Fragata; no se agregaron dependencias Go.

# Archivos importantes modificados

- `internal/model/model.go`
- `internal/detection/runner.go`
- `internal/camera/manager.go`
- `internal/httpapi/server.go`
- `internal/httpapi/event_playback.go`
- `internal/httpapi/static/events.js`
- `internal/httpapi/static/event-viewer.html`
- `internal/httpapi/static/event-viewer.js`
- `internal/httpapi/static/styles.css`

# Problemas encontrados

- Los eventos no almacenaban relación con el archivo que se estaba grabando.
- Los navegadores no reproducen de forma uniforme MKV con H.265 o audio G.711.
- Un evento puede ocurrir mientras el segmento todavía tiene extensión `.partial`.
- Las miniaturas se colocaban dentro de una caja 16:9 aunque el snapshot tuviera otra proporción.

# Soluciones implementadas

- Captura atómica del estado de grabación desde el worker de cámara.
- Resolución segura de archivos finales, parciales y recuperados.
- Espera automática del cierre del segmento desde la página del evento.
- Endpoint de MP4 fragmentado con cancelación al cerrar el navegador.
- Verificación estricta de que las rutas permanezcan dentro de `recordings/`.
- Vista responsiva con `object-fit: contain`, altura automática y snapshot original.

# Pendientes

- Crear un índice persistente de todos los segmentos para búsquedas históricas más amplias.
- Añadir una línea de tiempo diaria con múltiples eventos y grabaciones.
- Permitir seleccionar cuánto contexto anterior y posterior reproducir.

# Próximos pasos

1. Ejecutar `go mod tidy`, `go test ./...` y `go vet ./...` en el entorno del usuario.
2. Generar un evento mientras la grabación está activa.
3. Esperar el cierre del segmento y abrir el detalle del evento.
4. Validar reproducción H.264 y H.265 con FFmpeg disponible.
5. Confirmar que la descarga MKV conserva la calidad original.
