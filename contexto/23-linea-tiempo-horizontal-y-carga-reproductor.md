# Línea de tiempo horizontal y carga única del reproductor

Fecha: 2026-07-05  
Versión: 0.9.1

## Problemas corregidos

La primera videoteca representaba las 24 horas en vertical dentro de una columna estrecha. Cuando existían muchos segmentos o varias cámaras, los bloques compartían el mismo espacio horizontal y podían quedar montados. Los eventos también se añadían uno por uno sin agrupación visual.

El reproductor mostraba al mismo tiempo el indicador nativo del elemento `video` y el spinner propio de Fragata, lo que producía dos círculos superpuestos durante la preparación del MP4.

## Decisiones

- La escala diaria ahora es horizontal y fija en 220 píxeles por hora.
- La pista completa mide 5,280 píxeles y se recorre mediante desplazamiento horizontal.
- Los segmentos reciben carriles mediante un algoritmo voraz que considera el ancho visual mínimo, evitando superposiciones.
- Los eventos se agrupan por intervalos de diez minutos. Esto limita la pista a 144 marcadores como máximo por día sin perder el total de eventos mostrado en el resumen.
- La pista representa como máximo 1,200 bloques de video. Cuando existen más, utiliza una selección uniforme que conserva una vista de todo el día y lo informa en el encabezado.
- La lista conserva acceso a todos los videos mediante paginación y renderiza como máximo 200 elementos por página para proteger memoria y fluidez del navegador.
- Durante la carga inicial, Fragata vuelve temporalmente transparente el elemento `video` y muestra una sola capa opaca con su spinner.
- Después de recibir datos reproducibles, se retira la capa propia. Si ocurre buffering posterior, se conserva el video visible y se deja actuar al control nativo sin duplicar animaciones.

## Archivos principales

- `internal/httpapi/static/recordings.html`
- `internal/httpapi/static/recordings.js`
- `internal/httpapi/static/styles.css`

## Límites de interfaz

- Bloques visuales de video en la pista: 1,200.
- Intervalo de agrupación de eventos: 10 minutos.
- Marcadores de evento máximos por día: 144.
- Elementos máximos de la lista por página: 200.

Estos límites afectan únicamente al DOM del navegador. La API y la lista siguen conservando las grabaciones devueltas para el día seleccionado.
