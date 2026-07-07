# AGENTS.md — memoria operativa de Fragata

Este archivo es el punto de entrada para continuar el desarrollo de Fragata sin perder decisiones importantes entre sesiones. Debe leerse antes de modificar el proyecto y actualizarse cuando cambie una decisión de arquitectura, producto, seguridad o despliegue.

No reemplaza el código ni la documentación detallada. El orden recomendado de lectura es:

1. `AGENTS.md` para recuperar el estado y las reglas vigentes.
2. Los documentos más recientes de `contexto/`, en orden numérico descendente.
3. `README.md`, `SECURITY.md` y `CHANGELOG.md`.
4. El código y las pruebas, que son la fuente de verdad final.

## Estado actual

- Versión de trabajo: **0.9.5**.
- Backend: Go, binario único con frontend embebido mediante `go:embed`.
- Persistencia principal: archivo de estado local; credenciales cifradas con AES-256-GCM.
- Video en vivo: RTSP de la cámara hacia WebRTC del navegador.
- Grabación: MKV H.264/H.265 sin recomprimir, con audio compatible cuando existe.
- Reproducción histórica: FFmpeg/FFprobe opcionales en ejecución nativa e incluidos en Docker.
- Eventos: **exclusivamente ONVIF PullPoint**. No reintroducir detección local por snapshots, HOG/SVM ni campos de URL de snapshot.
- Interfaz: HTML, CSS y JavaScript sin framework SPA; Bootstrap y Bootstrap Icons se cargan desde jsDelivr.
- Despliegue principal: Docker Compose mediante `bash init.sh`.

## Decisiones no negociables vigentes

### Eventos ONVIF

- Activar eventos debe requerir únicamente el interruptor correspondiente.
- Fragata crea y mantiene `CreatePullPointSubscription`, `PullMessages`, renovación y `Unsubscribe`.
- La sensibilidad, zonas y clasificación adicional se configuran en la propia cámara.
- Los eventos deben vincularse al segmento MKV y al segundo aproximado dentro del archivo.
- Conservar compatibilidad de lectura con eventos y capturas históricas antiguas.

### Grabaciones y reproducción

- El MKV original nunca se modifica ni se reemplaza por una versión transcodificada.
- El códec histórico se obtiene del archivo mediante FFprobe, no de la configuración actual de la cámara.
- La videoteca debe permitir filtro por cámara y día, orden cronológico, descarga y línea de tiempo horizontal.
- Limitar procesos FFmpeg simultáneos para proteger VPS pequeños.
- Las rutas de grabaciones deben validarse contra traversal, enlaces simbólicos y escape del directorio raíz.

### Docker y permisos

- El contenedor inicia temporalmente como root solo para preparar `/data` y `/recordings`.
- Después debe cambiar a `FRAGATA_UID:FRAGATA_GID`; Fragata no debe permanecer como root.
- `docker-entrypoint.sh` crea carpetas, corrige permisos cuando corresponde y verifica escritura real.
- El filesystem raíz permanece de solo lectura, `/tmp` usa `tmpfs`, se aplican capabilities mínimas y `no-new-privileges`.
- Los montajes persistentes son:
  - estado y logs: `/data`;
  - videos: `/recordings`;
  - configuración RTSP: `/etc/fragata/config`, solo lectura.
- Para instalar o actualizar en un VPS usar `bash init.sh`; con Git se permite `bash init.sh --git-pull`.

### Seguridad web

- Login opcional con contraseña mínima de 12 caracteres.
- Rate limit por IP y por pareja IP/usuario, bloqueo temporal y `Retry-After`.
- Cookies `HttpOnly`; usar `Secure` detrás de HTTPS.
- Todas las rutas mutables autenticadas requieren CSRF.
- No confiar en `X-Forwarded-*` de clientes remotos; solo aceptar proxy local confiable.
- COOP y HSTS se envían únicamente en contextos confiables/HTTPS.
- Mantener CSP restrictiva y SRI para recursos CDN.
- No exponer directamente el puerto HTTP a Internet; usar Caddy, Nginx o Traefik con HTTPS.

## Arquitectura relevante

```text
cmd/fragata/                  arranque y ensamblado
internal/auth/                sesiones y autenticación
internal/camera/              descubrimiento, configuración y workers
internal/httpapi/             API, páginas y frontend embebido
internal/live/                WebRTC
internal/matroska/            escritura MKV
internal/onvif/               ONVIF, digest, descubrimiento y eventos
internal/recording/           grabación y recuperación
internal/retention/           política de limpieza
internal/rtsp/                cliente, diccionario y dimensiones
internal/store/               persistencia y migraciones
internal/stream/              hub de medios
internal/transcode/           FFmpeg y audio
internal/upload/              SFTP y cola persistente
internal/httpapi/static/      HTML, CSS, JS y componentes web
contexto/                     historial técnico detallado y decisiones
```

## Convenciones del frontend

- Mantener páginas multipágina tradicionales; no convertir el proyecto en SPA sin una decisión explícita.
- Reutilizar `fragata-app-layout` y `fragata-auth-layout`.
- Reutilizar `fragata-loader` para cargas de página y reproductores.
- Cada loader genera una máscara SVG única, por lo que puede haber varias instancias en una página.
- Para cambiar el texto de un loader:

```js
const loader = document.querySelector('#miLoader');
loader?.setAttribute('label', 'Preparando video…');
```

- Ejemplo de uso:

```html
<fragata-loader id="miLoader" label="Cargando…" size="72"></fragata-loader>
```

- Respetar `prefers-reduced-motion`, modo oscuro, accesibilidad táctil y atributos ARIA.
- Incrementar la versión de caché `?v=` de recursos estáticos cuando cambie HTML, CSS o JavaScript.
- Evitar identificadores HTML duplicados, especialmente dentro de componentes reutilizables.

## Flujo de trabajo para cambios

1. Analizar el ZIP o árbol completo antes de editar.
2. Identificar archivos, dependencias, migraciones y riesgos.
3. Implementar sin dejar código antiguo paralelo que contradiga la decisión nueva.
4. Añadir o actualizar un documento numerado en `contexto/`.
5. Actualizar `AGENTS.md` solo si cambió una regla persistente o el estado global.
6. Actualizar `README.md`, `SECURITY.md`, `CHANGELOG.md` y avisos de terceros cuando corresponda.
7. Ejecutar las pruebas y validaciones disponibles.
8. Entregar un ZIP limpio y, cuando se conserve historial, otro ZIP con `.git`.

Cuando sea necesario eliminar archivos obsoletos, entregar primero un script Python seguro y explícito para realizar la eliminación. No borrar carpetas de datos, grabaciones ni eventos históricos de forma implícita.

## Validación mínima antes de entregar

```bash
go test ./...
go vet ./...
node --check internal/httpapi/static/*.js
bash -n init.sh
sh -n docker-entrypoint.sh
sh -n docker-healthcheck.sh
docker compose config
```

Si el entorno no permite ejecutar alguna validación, indicarlo con precisión y no afirmar que pasó.

Para cambios visuales comprobar además:

- ausencia de IDs duplicados;
- navegación móvil y escritorio;
- modo claro y oscuro;
- carga inicial, reconexión y errores;
- reproductores con un solo indicador de carga;
- funcionamiento con varias instancias de componentes en la misma página.

## Limitaciones conocidas

- El conjunto completo requiere la versión de Go indicada en `go.mod` y acceso a sus dependencias.
- H.265 requiere FFmpeg para reproducción web compatible.
- Bootstrap e iconos dependen actualmente de jsDelivr.
- ONVIF Events necesita que el puerto del servicio ONVIF sea alcanzable; exponer únicamente RTSP/554 no permite recibir eventos.
- La persistencia basada en archivo puede necesitar migración a SQLite cuando crezcan significativamente eventos, sesiones e índice de grabaciones.

## Formato de entrega

Toda entrega de desarrollo debe incluir al menos:

- resumen funcional;
- archivos modificados;
- validaciones ejecutadas y limitaciones reales;
- riesgos y siguientes pasos cuando existan;
- `Summary` y `Description` listos para pegar en GitHub Desktop, dentro de bloques de código y en español.

Los commits, documentación pública y código no deben mencionar herramientas o proveedores usados para producir los cambios.
