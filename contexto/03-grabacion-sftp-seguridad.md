# Fecha
2026-07-05

# Objetivo
Garantizar que una interrupción o fallo de red no destruya grabaciones terminadas ni pierda la cola de respaldos.

# Decisiones tomadas
- Los segmentos se escriben como `.mkv.partial`.
- La rotación ocurre en un fotograma clave.
- Se ejecuta `fsync` y renombrado atómico antes de encolar SFTP.
- La cola de subida vive en `data/state.json`.
- El servidor SFTP se valida exclusivamente mediante `known_hosts`.
- El remoto se escribe como `.part`, se verifica por tamaño y se renombra.
- Se crea un archivo SHA-256 de forma atómica junto al MKV remoto.
- Una subida existente solo se acepta cuando coinciden tamaño y SHA-256.
- El archivo local no se borra salvo configuración explícita y subida completada.

# Arquitectura actual
```text
RTSP -> MKV.partial -> fsync -> MKV -> cola persistente -> SFTP .part -> MKV + SHA-256
```

# Librerías usadas
- Biblioteca estándar para Matroska, AES-GCM, SHA-256 y archivos.
- `pkg/sftp`.
- `golang.org/x/crypto/ssh`.

# Archivos importantes modificados
- `internal/matroska/writer.go`
- `internal/recording/recorder.go`
- `internal/recording/recover.go`
- `internal/upload/uploader.go`
- `internal/store/store.go`
- `internal/auth/auth.go`

# Problemas encontrados
- Un archivo abierto al perder energía puede quedar sin un cierre final.
- Ignorar la clave del host SFTP permitiría ataques de intermediario.
- Guardar contraseñas directamente en JSON expondría cámaras.

# Soluciones implementadas
- Segmentos cortos con clusters Matroska descargados aproximadamente cada 5 segundos.
- Recuperación de parciales no vacíos durante el inicio.
- Verificación estricta de `known_hosts`.
- Cifrado AES-256-GCM para credenciales.
- Tokens de sesión almacenados únicamente como hash SHA-256.
- Escritura del estado mediante archivo temporal, `fsync` y `rename`.

# Pendientes
- Validación real de recuperación ante corte eléctrico.
- Validación del contenedor H.265 con cámaras y reproductores reales.
- Política automática de retención y cuota de disco.
- Audio dentro del contenedor.

# Próximos pasos
Ejecutar pruebas de corte, corrupción y falta de espacio antes de usar borrado local automático.
