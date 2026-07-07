# Avisos de terceros

Fragata ya no incorpora coeficientes HOG/SVM, modelos de detección ni código derivado de OpenCV.

Las dependencias Go utilizadas por el proyecto y sus versiones se declaran en `go.mod` y `go.sum`. Los recursos web Bootstrap y Bootstrap Icons se cargan desde jsDelivr con versiones fijas y comprobación SRI.

## Loader visual

El componente `fragata-loader` adapta el concepto visual del snippet “loader” publicado en Uiverse.io por **andrew-manzyk**. La implementación de Fragata fue reescrita como componente web reutilizable, usa nombres aislados, genera una máscara SVG única por instancia, incorpora accesibilidad y respeta la preferencia de movimiento reducido.
