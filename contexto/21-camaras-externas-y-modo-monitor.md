# Cámaras externas, escucha configurable y modo monitor

Fecha: 2026-07-05
Versión: 0.8.3

## Objetivo

Permitir que Fragata se utilice como monitor continuo en una tablet y que las cámaras puedan localizarse mediante IP pública o dominio, sin limitar el alta a una dirección privada literal.

## Conectividad de cámaras

- El campo `host` admite IPv4, IPv6, dominio o CNAME.
- Puede incluir un puerto opcional, por ejemplo `camara.example.com:8554`.
- `FRAGATA_ALLOW_PUBLIC_CAMERAS` vale `true` por defecto.
- El valor `false` conserva un modo estricto: las IP deben ser privadas/locales y los dominios deben resolver únicamente hacia direcciones privadas/locales.
- Se rechazan destinos indefinidos o multicast como `0.0.0.0` para evitar configuraciones sin sentido.
- Las URL de snapshot obtenidas por ONVIF se reescriben hacia el host configurado antes de validarlas. Esto evita que una cámara registrada por dominio devuelva una IP interna distinta e inutilizable.

## Escucha HTTP

La dirección HTTP puede componerse con:

```dotenv
FRAGATA_LISTEN_HOST=0.0.0.0
FRAGATA_LISTEN_PORT=8080
```

`FRAGATA_LISTEN` se conserva como override compatible. `0.0.0.0` significa escuchar en todas las interfaces del servidor; no es un destino válido para una cámara.

## Recuperación del visor

El visor mantiene dos mecanismos independientes:

1. Reconexión WebRTC cuando una pista termina, se congela o ICE falla.
2. Supervisor HTTP de `/healthz` para detectar que el proceso Fragata completo dejó de responder.

Cuando el servidor desaparece:

- Se invalida la generación WebRTC actual.
- Se cierran video y audio sin abandonar la página.
- Se muestra `Fragata no está disponible · reintentando`.
- `/healthz` se consulta cada 2.5 segundos.

Cuando el servidor vuelve:

- Se recupera `/api/session`.
- Se vuelve a consultar la cámara guardada.
- Se actualizan estado, grabación y metadatos.
- Se negocia una sesión WebRTC nueva.

Las solicitudes normales tienen timeout de 15 segundos y las ofertas WebRTC de 45 segundos para tolerar el arranque de una cámara sin dejar peticiones colgadas indefinidamente.

## Modo monitor

El visor incorpora un botón persistente `Monitor activo`. Cuando Screen Wake Lock está disponible, solicita mantener la pantalla encendida y vuelve a adquirir el bloqueo al regresar a la pestaña. En navegadores o contextos que no ofrecen la API, el control se oculta y la reconexión continúa funcionando normalmente.

## Seguridad

Aceptar destinos públicos amplía deliberadamente la superficie SSRF del panel administrativo. Por ello, una instalación expuesta debe usar autenticación, HTTPS, firewall y acceso restringido. El modo privado continúa disponible para despliegues que no necesitan cámaras externas.
