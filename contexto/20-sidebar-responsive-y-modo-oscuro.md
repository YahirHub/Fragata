# 20. Sidebar responsive y modo oscuro

Fecha: 2026-07-05
Versión: 0.8.2

## Objetivo

Permitir que la navegación pueda ocultarse en escritorio, funcione correctamente como drawer en dispositivos móviles y ofrecer un modo oscuro consistente en toda la interfaz administrativa.

## Decisiones

- Se conserva un único componente `fragata-app-layout` para todas las páginas.
- El botón del topbar cambia su comportamiento según el ancho disponible:
  - En escritorio oculta o muestra por completo el sidebar fijo.
  - En teléfono y tablet abre el `offcanvas` de Bootstrap.
- La preferencia del sidebar se guarda únicamente en `localStorage`; no forma parte del estado del servidor ni se sincroniza entre usuarios.
- El tema se aplica desde `theme.js` antes de cargar las hojas de estilo para evitar un destello de tema claro al abrir una página oscura.
- La primera visita respeta `prefers-color-scheme`; después de una selección manual se conserva `light` o `dark` en `localStorage`.
- Se mantiene `data-bs-theme` como fuente de verdad para aprovechar el soporte nativo de Bootstrap 5.3.
- La pantalla de login también incorpora selector de tema.
- El color de la barra del navegador se actualiza con el tema activo.

## Archivos principales

- `internal/httpapi/static/theme.js`
- `internal/httpapi/static/layouts.js`
- `internal/httpapi/static/styles.css`
- Todas las páginas HTML embebidas para cargar el tema antes del CSS.

## Accesibilidad y móviles

- Los botones exponen `aria-label`, `title`, `aria-expanded` y `aria-pressed` actualizados.
- El drawer usa overlay, cierre por botón y cierre al seleccionar una ruta.
- Se respetan áreas seguras mediante `env(safe-area-inset-*)`.
- Se usa `100dvh` para evitar saltos por la barra del navegador móvil.
- Las transiciones se desactivan cuando el sistema solicita movimiento reducido.
- Los controles táctiles mantienen al menos 40–44 px en pantallas pequeñas.

## Compatibilidad

La función requiere un navegador moderno con `matchMedia`, `localStorage` y soporte de Bootstrap 5.3. Si el almacenamiento local está bloqueado, el tema y el sidebar siguen funcionando durante la sesión, pero la preferencia no persiste.
