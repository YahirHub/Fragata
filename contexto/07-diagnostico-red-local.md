# Fecha

2026-07-05

# Objetivo

Distinguir fallos de red, puertos cerrados y errores RTSP para evitar que el usuario cambie rutas o credenciales cuando Fragata todavía no puede abrir una conexión TCP hacia la cámara.

# Decisiones tomadas

- Añadir diagnóstico de red desde el mismo proceso de Fragata.
- Clasificar puertos como abiertos, rechazados, con timeout, sin ruta o inalcanzables.
- Comprobar el puerto antes de ejecutar DESCRIBE sobre una URL RTSP manual.
- Usar `network_mode: host` en Docker Compose para el despliegue Linux recomendado.
- Mantener un Compose bridge separado para entornos sin soporte de red host.
- No agregar herramientas externas como ping, nmap o netcat dentro del contenedor `scratch`.

# Arquitectura actual

El panel llama a `/api/network/diagnose`. El paquete `internal/networkdiag` obtiene las interfaces locales, detecta si Fragata corre dentro de un contenedor y usa el sondeo TCP de `internal/rtsp` para producir una recomendación segura.

El flujo RTSP manual ahora es:

1. Validar IP y URL.
2. Abrir TCP al puerto indicado.
3. Si responde, ejecutar DESCRIBE, SETUP y PLAY.
4. Confirmar recepción H.264/H.265.
5. Guardar la URL sin credenciales y cifrar la contraseña aparte.

# Librerías usadas

Solo biblioteca estándar para diagnóstico. No se agregaron dependencias.

# Archivos importantes modificados

- `internal/rtsp/dictionary.go`
- `internal/camera/discover.go`
- `internal/networkdiag/diagnose.go`
- `internal/httpapi/server.go`
- `internal/httpapi/static/index.html`
- `internal/httpapi/static/app.js`
- `internal/httpapi/static/styles.css`
- `docker-compose.yml`
- `docker-compose.bridge.yml`
- `README.md`

# Problemas encontrados

La cámara `192.168.10.234` agotó el tiempo tanto en RTSP `554` como en ONVIF, por lo que la falla ocurre antes de validar rutas o credenciales. El despliegue Compose anterior usaba bridge, que no es adecuado como valor predeterminado para WS-Discovery multicast y puede quedar bloqueado por reglas de forwarding.

# Soluciones implementadas

- Diagnóstico visible desde el panel.
- Errores RTSP manuales más precisos.
- Red host predeterminada para Linux.
- Documentación de VLAN, VPN, firewall, aislamiento Wi-Fi y rutas.

# Pendientes

- Probar en el servidor real con la cámara Imou.
- Confirmar si el host tiene una interfaz o ruta hacia `192.168.10.0/24`.
- Validar la URL Imou después de que el puerto `554` aparezca abierto.

# Próximos pasos

Ejecutar Fragata con red host, usar **Diagnosticar red** y enviar el resultado completo si `554` continúa apareciendo como `sin respuesta`.
