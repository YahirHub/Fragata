# Fecha

2026-07-05

# Objetivo

Mejorar el alta de cámaras cuando ONVIF no entrega una URL utilizable o cuando cada fabricante usa una ruta RTSP distinta. Permitir además probar y guardar una URL RTSP manual desde el panel.

# Decisiones tomadas

- ONVIF y `GetStreamUri` continúan siendo la primera estrategia.
- Antes de probar rutas se comprueba una lista acotada de puertos TCP para evitar repetir el mismo timeout por cada candidato.
- Se incorpora un diccionario RTSP integrado y extensible mediante un archivo local opcional.
- No se descargan diccionarios durante la ejecución.
- Las credenciales nunca se prueban por fuerza bruta; solo se usan las proporcionadas por el usuario.
- Las rutas personalizadas tienen prioridad sobre las integradas.
- La conexión RTSP se fuerza por TCP para reducir problemas con UDP dentro de Docker, VLAN y redes con NAT.
- Una URL manual se valida recibiendo paquetes H.264 o H.265 antes de guardarse.
- Si la URL manual contiene credenciales, se extraen y se almacenan cifradas; la URL persistida queda sin usuario ni contraseña.

# Arquitectura actual

Flujo automático:

1. Validar que la cámara use una IP permitida.
2. Consultar ONVIF.
3. Probar la URL entregada por ONVIF.
4. Comprobar puertos TCP configurados.
5. Expandir rutas del diccionario únicamente sobre puertos accesibles.
6. Probar candidatos en lotes pequeños.
7. Preferir H.264 y conservar H.265 como fallback.
8. Guardar la cámara solo después de recibir video.

Flujo manual:

1. El usuario pega una URL RTSP.
2. `POST /api/rtsp/probe` comprueba la URL sin persistirla.
3. Al guardar, Fragata vuelve a validarla para evitar guardar un resultado obsoleto.
4. Las credenciales se separan de la URL y se cifran en el store.

# Librerías usadas

No se agregaron dependencias. Se reutilizan `net`, `net/url`, `bufio`, `context` y `gortsplib` ya presente.

# Archivos importantes modificados

- `internal/rtsp/dictionary.go`
- `internal/rtsp/client.go`
- `internal/camera/discover.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/index.html`
- `internal/httpapi/static/app.js`
- `internal/httpapi/static/styles.css`
- `internal/config/config.go`
- `.env.example`
- `README.md`
- `config/rtsp-paths.example.txt`

# Problemas encontrados

El error `dial tcp <IP>:554: i/o timeout` se producía antes de comprobar la ruta RTSP. El flujo anterior repetía ese mismo timeout para múltiples rutas sobre el mismo puerto, lo que hacía lenta la detección y ocultaba que el problema era de conectividad o puerto.

También se detectó que una URL manual con credenciales embebidas podía validarse, pero después perderlas al persistir la URL saneada. Ahora se extraen antes de guardar.

# Soluciones implementadas

- Sondeo concurrente y limitado de puertos.
- Mensajes separados para puerto inaccesible, autenticación rechazada, ruta inexistente y stream sin video.
- Diccionario integrado de rutas de fabricantes comunes.
- Diccionario local opcional con máximo de 512 entradas.
- Límite configurable de candidatos y timeouts.
- Endpoint y botón para probar URL manual.
- Transporte RTSP sobre TCP.
- Pruebas unitarias preparadas para parser, expansión de rutas y extracción de credenciales.

# Pendientes

- Ejecutar `go mod tidy`, `go test ./...` y `go vet ./...` en el entorno del usuario.
- Validar contra la cámara Imou `192.168.10.50` y confirmar si el puerto 554 es alcanzable desde el host o contenedor.
- Ajustar firmas si una dependencia cambió su API.

# Próximos pasos

- Recibir los resultados de compilación y prueba real.
- Agregar edición de cámaras existentes.
- Mostrar diagnóstico de red más detallado desde el panel si resulta necesario.
