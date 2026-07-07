# 27. AGENTS.md y loader reutilizable

Fecha: 2026-07-07  
Versión: 0.9.5

## Objetivo

Resolver dos necesidades de continuidad y experiencia visual:

1. conservar una memoria operativa breve cuando el desarrollo se continúa desde otra sesión;
2. sustituir indicadores de carga inconsistentes por un componente reutilizable en páginas y reproductores.

## Memoria operativa

Se añadió `AGENTS.md` en la raíz. No duplica íntegramente `contexto/`: funciona como índice de decisiones vigentes y explica qué documentos deben leerse después.

Incluye:

- estado actual y versión;
- decisiones no negociables;
- eventos exclusivamente ONVIF PullPoint;
- reglas de grabación y reproducción histórica;
- arranque temporal como root y descenso a usuario no privilegiado;
- seguridad HTTP, login, proxy y CSRF;
- mapa de directorios;
- convenciones del frontend;
- flujo de implementación y documentación;
- validación mínima;
- limitaciones conocidas;
- formato esperado de cada entrega.

El código y las pruebas continúan siendo la fuente final. `AGENTS.md` debe actualizarse solamente cuando cambie una decisión persistente o el estado global; los detalles de cada implementación siguen en `contexto/`.

## Componente `fragata-loader`

Se añadió `internal/httpapi/static/loader.js`, que registra el elemento:

```html
<fragata-loader label="Cargando…" size="72"></fragata-loader>
```

Atributos:

- `label`: mensaje accesible y visible;
- `size`: diámetro visual entre 28 y 140 píxeles;
- `compact`: reduce el espacio entre animación y etiqueta.

Cada instancia genera un identificador propio para la máscara SVG. Esto evita el error del snippet original cuando se colocan varios loaders con el mismo `id="clipping"` en una página.

El mensaje puede cambiar durante la ejecución:

```js
document.querySelector('#viewerLoader')
  ?.setAttribute('label', 'Reconectando con la cámara…');
```

## Integraciones

El nuevo componente reemplaza:

- la animación circular anterior del visor WebRTC;
- el spinner del video asociado a un evento;
- el spinner del reproductor de la videoteca;
- el spinner de carga de la lista de eventos;
- el spinner de consulta de grabaciones.

Los reproductores conservan una única capa de carga. El elemento `<video>` permanece oculto solamente durante la preparación inicial y se revela al recibir datos reproducibles.

## Accesibilidad y rendimiento

- `role="status"`, `aria-live="polite"` y `aria-atomic="true"`;
- etiqueta textual visible;
- con `prefers-reduced-motion: reduce` se eliminan las deformaciones intensas y se conserva un giro lento para que el estado de carga nunca parezca congelado;
- sin dependencias externas nuevas;
- componente embebido junto con el resto del frontend;
- colores adaptados a la identidad visual de Fragata;
- tamaño limitado para impedir valores extremos.

## Atribución

El concepto visual fue aportado por el usuario a partir de un snippet de Uiverse.io publicado por `andrew-manzyk`. La implementación se reescribió y adaptó; la atribución se conserva en `THIRD_PARTY_NOTICES.md` y en un comentario del CSS.

## Archivos principales

- `AGENTS.md`
- `internal/httpapi/static/loader.js`
- `internal/httpapi/static/styles.css`
- `internal/httpapi/static/viewer.html`
- `internal/httpapi/static/viewer.js`
- `internal/httpapi/static/event-viewer.html`
- `internal/httpapi/static/event-viewer.js`
- `internal/httpapi/static/events.html`
- `internal/httpapi/static/recordings.html`
- `README.md`
- `CHANGELOG.md`
- `THIRD_PARTY_NOTICES.md`
