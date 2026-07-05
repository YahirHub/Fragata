# Fecha

2026-07-05

# Objetivo

Agregar detección local y opcional de movimiento y personas a Fragata sin CGO, Python, OpenCV, ONNX Runtime, procesos auxiliares ni archivos de modelo externos.

# Decisiones tomadas

- Obtener imágenes mediante `GetSnapshotUri` de ONVIF para evitar decodificar H.264/H.265 en CPU.
- Permitir una URL HTTP(S) manual cuando la cámara no publica snapshots ONVIF.
- Restringir la URL de snapshot a la misma IP de la cámara para reducir SSRF.
- Ejecutar primero un detector de movimiento económico sobre una imagen de 160×90.
- Ejecutar la confirmación humana HOG/SVM únicamente cuando ya existe movimiento.
- Incrustar los coeficientes del detector humano dentro del binario.
- Considerar la confirmación humana una función beta por las limitaciones conocidas de HOG/SVM.
- Guardar miniaturas y metadatos de eventos separados de las grabaciones MKV.
- Cifrar en disco la URL de snapshot y ocultar parámetros sensibles en la API.
- Aplicar a las miniaturas la misma política global de retención.

# Arquitectura actual

```text
Cámara ONVIF/HTTP
  └── Snapshot JPEG/PNG
       ├── Reducción y zona de análisis
       ├── Detector de movimiento en Go
       └── HOG/SVM humano bajo demanda
            └── Evento + miniatura + estado en tiempo real
```

Cada worker de cámara mantiene un runner de detección independiente y cancelable mediante `context.Context`. El runner no modifica el stream RTSP, la grabación ni WebRTC.

# Librerías usadas

- Biblioteca estándar: `image`, `image/jpeg`, `image/png`, `net/http`, `math`, `context` y utilidades de archivos.
- Cliente ONVIF interno de Fragata para `GetSnapshotUri` y autenticación Basic/Digest.
- No se agregaron dependencias Go nuevas.
- Los coeficientes HOG/SVM predeterminados de OpenCV están embebidos como datos y documentados en `THIRD_PARTY_NOTICES.md`.

# Archivos importantes modificados

- `internal/detection/motion.go`
- `internal/detection/hog.go`
- `internal/detection/hog_weights.go`
- `internal/detection/runner.go`
- `internal/onvif/client.go`
- `internal/camera/discover.go`
- `internal/camera/settings.go`
- `internal/camera/manager.go`
- `internal/model/model.go`
- `internal/store/store.go`
- `internal/retention/cleaner.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/events.html`
- `internal/httpapi/static/events.js`
- `internal/httpapi/static/camera-settings.html`
- `internal/httpapi/static/camera-settings.js`

# Problemas encontrados

- Decodificar directamente el stream H.264/H.265 habría requerido un decoder complejo o dependencias nativas.
- Las cámaras pueden no exponer `GetSnapshotUri` aunque sí soporten RTSP.
- HOG/SVM tiene menor precisión que una red neuronal moderna con personas pequeñas, ocultas o en posturas no verticales.
- El ruido nocturno, cambios de luz, lluvia y vegetación pueden generar movimiento falso.
- El estado previo no tenía almacenamiento para eventos ni limpieza coordinada de miniaturas.

# Soluciones implementadas

- Capturas JPEG/PNG autenticadas mediante ONVIF y fallback manual validado.
- Límite de 8 MiB y 32 megapíxeles antes de decodificar cada snapshot.
- Compensación de cambios globales de iluminación y análisis por bloques.
- Confirmación en fotogramas consecutivos para reducir falsos positivos de movimiento.
- Zona rectangular configurable y normalizada.
- Detector HOG/SVM con pirámide, NMS y umbral de confianza configurable.
- Página protegida de eventos con filtros y miniaturas.
- Comprobación de contención de rutas al servir o eliminar miniaturas.
- Migración del estado persistente a versión 3 con mapa de eventos.

# Pendientes

- Validar sensibilidad y confianza con distintos modelos de cámara, iluminación diurna y visión nocturna.
- Añadir zonas poligonales si las zonas rectangulares resultan insuficientes.
- Añadir reproducción histórica y vinculación entre evento y segmento MKV.
- Evaluar un detector neuronal cuantizado en Go puro solo si existe una implementación estable y medible.
- Añadir clasificación de vehículos y mascotas en una etapa posterior.

# Próximos pasos

1. Ejecutar `go mod tidy`, `go test ./...` y `go vet ./...` en el entorno del usuario.
2. Probar `GetSnapshotUri` con una cámara Imou real.
3. Ajustar sensibilidad y confianza con eventos reales.
4. Medir CPU y memoria con varias cámaras activas.
5. Revisar falsos positivos antes de considerar la función estable.
