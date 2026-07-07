# 26. Navegación del sidebar y cabeceras según contexto seguro

## Problema observado

En el drawer móvil los enlaces del sidebar se generaban con `data-bs-dismiss="offcanvas"`. Bootstrap trata ese atributo como una acción de cierre. Cuando se coloca sobre un elemento `<a>`, su manejador de cierre puede cancelar la navegación normal del clic izquierdo. Por eso el menú parecía fallar de forma intermitente: ocurría en el sidebar móvil, mientras que el sidebar fijo de escritorio y la opción **Abrir en una nueva pestaña** seguían funcionando.

En accesos directos mediante `http://IP:puerto`, el servidor enviaba además `Cross-Origin-Opener-Policy: same-origin`. Los navegadores ignoran COOP en orígenes HTTP públicos porque no son contextos potencialmente confiables. No era la causa del fallo de navegación, pero producía una advertencia legítima.

Bootstrap y Bootstrap Icons incluyen referencias a archivos `.map`. La CSP permitía jsDelivr para scripts, estilos y fuentes, pero no para `connect-src`, de modo que DevTools bloqueaba esos mapas y mostraba advertencias adicionales. Los archivos principales sí cargaban; el bloqueo afectaba únicamente la depuración.

## Corrección

- Los enlaces de navegación ya no incluyen `data-bs-dismiss="offcanvas"`.
- El botón de cierre del drawer conserva `data-bs-dismiss="offcanvas"`, porque sí es una acción exclusiva de cierre.
- El clic izquierdo sobre una sección usa navegación HTML normal y no depende de JavaScript para cambiar de página.
- COOP se envía solo cuando la solicitud llega por HTTPS, cuando el host es localhost/loopback o cuando un proxy local confiable informa `X-Forwarded-Proto: https`.
- Las cabeceras reenviadas solo se confían si la conexión inmediata procede de loopback.
- HSTS se envía únicamente cuando la solicitud se reconoce realmente como HTTPS.
- `connect-src` permite `https://cdn.jsdelivr.net` para que los mapas de código de las dependencias fijadas no generen bloqueos CSP.

## Seguridad

No se acepta `X-Forwarded-Proto` de clientes remotos. Esto evita que una conexión HTTP directa pueda fingir HTTPS y provocar cookies o políticas de transporte incoherentes. En producción sigue siendo obligatorio publicar Fragata mediante un proxy HTTPS y configurar `FRAGATA_SECURE_COOKIES=true`.

## Pruebas añadidas

- HTTP remoto por IP no recibe COOP ni HSTS.
- localhost por HTTP recibe COOP, pero no HSTS.
- un proxy local con `X-Forwarded-Proto: https` recibe COOP y HSTS.
- un cliente remoto no puede falsificar `X-Forwarded-Proto`.
- la CSP contiene jsDelivr dentro de `connect-src`.
