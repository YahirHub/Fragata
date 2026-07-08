## Sin versión — corrección de visor VPS en prueba

- Sustituye el transporte principal WebRTC por fMP4/MSE autenticado sobre el mismo puerto HTTP/HTTPS.
- Comparte un proceso FFmpeg por cámara entre todos los espectadores.
- Evita candidatos ICE con IP interna de Docker como `172.17.0.2`.
- Añade límites globales de espectadores y cámaras transmitidas.
- Conserva el endpoint WebRTC anterior solo como compatibilidad.
- No crea commit ni incrementa versión hasta validar la corrección en el VPS.

# Changelog

## 0.9.5 - 2026-07-07

- Añade `AGENTS.md` en la raíz como memoria operativa persistente y punto de entrada para continuar el desarrollo.
- Define el orden de lectura entre `AGENTS.md`, `contexto/`, documentación y código.
- Documenta decisiones vigentes sobre eventos ONVIF, grabaciones, seguridad, Docker, frontend, pruebas y entregas.
- Añade el componente web reutilizable `fragata-loader` con etiqueta y tamaño configurables.
- Genera una máscara SVG única por instancia para permitir múltiples loaders en la misma página sin colisiones.
- Sustituye la animación anterior del visor en vivo por el nuevo componente.
- Aplica el mismo loader al reproductor de eventos, reproductor histórico y cargas de las páginas de eventos y grabaciones.
- Conserva un único indicador de carga por reproductor y actualiza dinámicamente sus mensajes.
- Respeta `prefers-reduced-motion`, modo oscuro, ARIA y tamaños adaptables.
- Registra la atribución del concepto visual original en los avisos de terceros.

## 0.9.4 - 2026-07-07

- Corrige la navegación del sidebar móvil: los enlaces ya no se usan como controles `data-bs-dismiss`, por lo que el clic izquierdo navega normalmente en lugar de limitarse a cerrar el drawer.
- Conserva el cierre natural del menú al cambiar de página y mantiene el botón independiente para cerrar el `offcanvas`.
- Envía `Cross-Origin-Opener-Policy` únicamente cuando el origen es HTTPS, localhost o un proxy HTTPS local confiable, evitando advertencias en accesos HTTP directos por IP.
- Reconoce `X-Forwarded-Proto: https` solo cuando la conexión inmediata procede de loopback, evitando que un cliente remoto falsifique el contexto seguro.
- Restaura `https://cdn.jsdelivr.net` en `connect-src` para permitir los mapas de código de Bootstrap y eliminar bloqueos CSP de `.map` en la consola.
- Añade pruebas de cabeceras para HTTP remoto, localhost, proxy HTTPS local y cabeceras reenviadas falsificadas.

## 0.9.3 - 2026-07-07

- Elimina por completo el detector local basado en snapshots, diferencia de imágenes y HOG/SVM.
- Sustituye la detección local por suscripciones nativas ONVIF Events mediante `CreatePullPointSubscription`, `PullMessages` y `Unsubscribe`.
- Reduce la configuración a un único interruptor **Activar eventos ONVIF**, sin URL, sensibilidad, intervalo, confianza, enfriamiento ni zona.
- Valida la compatibilidad PullPoint antes de guardar la activación y muestra un error claro cuando la cámara no soporta el servicio.
- Renueva y reconecta automáticamente las suscripciones, con backoff y deduplicación de notificaciones repetidas.
- Conserva el tópico ONVIF, clasifica movimiento/persona cuando es posible y registra eventos específicos del fabricante como `onvif`.
- Mantiene compatibles los eventos y snapshots históricos creados por versiones anteriores, sin generar nuevas capturas locales.
- Migra `state.json` a la versión 4 y elimina al reescribir los campos obsoletos del detector local sin borrar cámaras, credenciales ni historial.
- Elimina `internal/detection` y los avisos de terceros del antiguo detector HOG/SVM.

## 0.9.2 - 2026-07-06

- Corrige `permission denied` en `/recordings` mediante un entrypoint que prepara volúmenes como root y ejecuta Fragata después con UID/GID no privilegiado.
- Añade reparación automática e idempotente de permisos, marcador de esquema y verificación real de escritura antes de iniciar el servidor.
- Incorpora `init.sh` para crear carpetas del host, preparar `.env`, construir con caché, levantar Compose y esperar el healthcheck.
- Permite actualizar un VPS repitiendo `bash init.sh` después de copiar código o usando `bash init.sh --git-pull`.
- Ajusta Docker Compose para crear bind mounts ausentes y conservar solo las capacidades mínimas necesarias durante el entrypoint.
- Añade modos `auto`, `always` y `never` mediante `FRAGATA_REPAIR_PERMISSIONS`.

## 0.9.1 - 2026-07-05

- Convierte la línea de tiempo de grabaciones a una pista horizontal de 24 horas con escala amplia y desplazamiento lateral.
- Distribuye automáticamente videos superpuestos en carriles independientes para que ningún segmento quede montado sobre otro.
- Mantiene un ancho mínimo legible para segmentos cortos y muestra la línea de tiempo a ancho completo sobre la lista cronológica.
- Agrupa eventos en intervalos de diez minutos, con un máximo visual de 144 marcadores diarios, evitando acumulaciones ilegibles.
- Limita la representación visual de la línea de tiempo a 1,200 bloques y conserva todos los videos accesibles desde la lista.
- Pagina la lista cronológica en bloques de 200 elementos, evitando miles de nodos simultáneos en el navegador.
- Corrige el indicador doble al preparar un video ocultando el cargador nativo bajo una única pantalla de carga opaca.
- Evita volver a mostrar la pantalla completa de preparación cuando un video ya iniciado entra temporalmente en buffering.

## 0.9.0 - 2026-07-05

- Añade el apartado **Grabaciones** con filtro por cámara y día, días disponibles, resumen de espacio y duración, lista cronológica y línea de tiempo diaria de 24 horas.
- Permite reproducir cualquier segmento MKV desde un segundo específico y saltar desde marcadores de movimiento o persona sin modificar el archivo original.
- Agrega descarga protegida del MKV, soporte para carpetas de cámaras eliminadas y validación reforzada contra path traversal y enlaces simbólicos.
- Detecta el códec real de cada archivo histórico con FFprobe y solo conserva H.264 cuando es compatible; H.265 u otros formatos se convierten a H.264 para el navegador.
- Limita las transcodificaciones simultáneas mediante `FRAGATA_MAX_TRANSCODES` y responde `429` con `Retry-After` al alcanzar el límite.
- Sustituye el límite básico de login por rate limit por IP y pareja IP/usuario, ventana y bloqueo configurables, limpieza de buckets y soporte seguro para proxies locales sin bloqueo global de cuenta.
- Exige contraseñas administrativas de al menos 12 caracteres, compara credenciales mediante hashes de tamaño uniforme y endurece cabeceras HTTP/CSP.
- Corrige la cola SFTP para no retirar un trabajo hasta que la eliminación local solicitada haya terminado correctamente.
- Cambia la imagen final a Alpine con FFmpeg/FFprobe, usuario no privilegiado y soporte real de reproducción histórica dentro de Docker.
- Endurece Docker Compose con autenticación obligatoria, bind mounts separados para estado, grabaciones y configuración de solo lectura, raíz de solo lectura, `no-new-privileges`, capabilities eliminadas, `tmpfs` y límite de procesos.
- Corrige la confirmación de movimiento consecutivo y protege los recursos CDN mediante SRI.

## 0.8.3 - 2026-07-05

- Permite agregar cámaras mediante IP privada, IP pública, IPv4, IPv6 o dominio/CNAME con puerto opcional.
- Activa cámaras externas por defecto y conserva `FRAGATA_ALLOW_PUBLIC_CAMERAS=false` como modo restringido.
- Añade `FRAGATA_LISTEN_HOST` y `FRAGATA_LISTEN_PORT`, con `0.0.0.0:8080` como escucha predeterminada y compatibilidad con `FRAGATA_LISTEN`.
- Normaliza las URL de snapshot ONVIF hacia el dominio o host configurado para la cámara.
- Agrega un supervisor de `/healthz` al visor que detecta la caída y el regreso del proceso Go.
- Reconstruye automáticamente sesión, metadatos y WebRTC después de reiniciar Fragata sin recargar la página.
- Añade timeouts explícitos a las solicitudes del visor para evitar conexiones detenidas indefinidamente.
- Incorpora un modo monitor persistente para tablets mediante Screen Wake Lock cuando el navegador lo permite.
- Actualiza formularios, Docker Compose, documentación y pruebas para hosts externos y escucha configurable.

## 0.8.2 - 2026-07-05

- Añade un control unificado para ocultar o mostrar el sidebar en escritorio y abrir el drawer de navegación en teléfonos y tablets.
- Conserva la preferencia del sidebar en el navegador y evita saltos visibles al cargar la interfaz.
- Implementa modo oscuro completo, persistente y compatible con la preferencia del sistema operativo.
- Añade un selector de tema en la barra superior y en la pantalla de inicio de sesión.
- Mejora el comportamiento móvil del topbar, menús, modales, tablas y áreas táctiles.
- Adapta tarjetas, formularios, tablas, visor, eventos, ajustes, dropdowns y modales al tema oscuro.
- Respeta `prefers-reduced-motion` y actualiza el color del navegador según el tema activo.

## 0.8.1 - 2026-07-05

- Vincula cada evento nuevo con el segmento MKV activo y guarda el desplazamiento exacto desde el inicio del archivo.
- Localiza de forma compatible las grabaciones de eventos creados antes de esta versión mediante cámara, fecha, nombre del segmento y hora de finalización.
- Agrega una página de detalle por evento con reproducción desde cinco segundos antes de la detección, captura original y metadatos.
- Usa FFmpeg opcional para servir MP4 fragmentado compatible con navegador; conserva H.264 sin recomprimir y mantiene la resolución original al convertir H.265.
- Permite descargar el MKV original cuando la reproducción web no está disponible.
- Espera automáticamente a que finalice el segmento cuando el evento pertenece a una grabación todavía abierta.
- Elimina la relación de aspecto 16:9 forzada en las miniaturas y muestra snapshots con sus dimensiones y proporción naturales.
- Añade validación de contención de rutas para impedir acceso a archivos fuera del directorio de grabaciones.

## 0.8.0 - 2026-07-05

- Añade detección opcional de movimiento mediante diferencia de snapshots, compensación de iluminación y confirmación temporal en Go puro.
- Incorpora confirmación humana beta mediante HOG/SVM con coeficientes embebidos, sin CGO, Python, OpenCV, ONNX Runtime ni archivos externos.
- Obtiene automáticamente `GetSnapshotUri` mediante ONVIF y permite configurar una URL HTTP(S) manual restringida a la IP de la cámara.
- Permite configurar sensibilidad, intervalo, confianza humana, enfriamiento y zona rectangular por cámara.
- Agrega una página de eventos con miniaturas protegidas, filtros por cámara y tipo, confianza y enlace al visor.
- Persiste eventos en el estado local y aplica a sus miniaturas la política global de retención.
- Cifra la URL de snapshot en el estado local, oculta parámetros sensibles en la API y limita tamaño y dimensiones antes de decodificar imágenes.
- Añade pruebas mínimas para movimiento, detector humano vacío, normalización de zonas, persistencia de eventos, validación de snapshots y limpieza.
- Documenta los coeficientes HOG/SVM de terceros y las limitaciones de la detección humana beta.

## 0.7.1 - 2026-07-05

- Separa video y audio en sesiones WebRTC independientes para que un fallo de sonido no bloquee la imagen.
- Restaura el proceso FFmpeg de video aislado y mueve la conversión AAC a un proceso auxiliar independiente.
- Mantiene el visor en `Conectando` únicamente hasta confirmar un fotograma de video decodificado.
- Evita que los reintentos de audio reinicien o destruyan una sesión de video saludable.
- Reduce la consulta de estado a cada cinco segundos y la pausa cuando la pestaña está oculta.
- Añade el tipo de medio a la oferta WebRTC para negociar video y audio de forma explícita.

## 0.7.0 - 2026-07-05

- Detecta audio RTSP G.711 A-law, G.711 μ-law, Opus y AAC junto con el stream de video.
- Añade pista de audio al visor WebRTC con activación explícita desde la interfaz y conversión AAC a PCMU mediante FFmpeg cuando se necesita.
- Guarda el audio compatible dentro del mismo MKV sin recomprimir y conserva la grabación de video cuando no existe audio compatible.
- Agrega `logs.txt` rotativo con límite estricto de 1 MiB y eliminación de las líneas más antiguas.
- Implementa perfiles SFTP globales reutilizables, múltiples servidores, prueba de conexión, credenciales cifradas y selección por cámara.
- Conserva el perfil SFTP dentro de cada trabajo de la cola persistente para reintentar en el destino correcto.
- Añade una página global de almacenamiento y una política de retención configurable por días, meses o años.
- Ejecuta la retención al iniciar, al guardar la política y periódicamente, sin eliminar archivos parciales ni subidas pendientes.
- Añade pruebas mínimas para rotación de logs, cifrado SFTP, pista de audio MKV, limpieza de metadatos y protección de grabaciones.

## 0.6.4 - 2026-07-05

- Corrige la política CSP que bloqueaba estilos dinámicos requeridos por Bootstrap y el ajuste del visor.
- Elimina el estilo inline usado para la relación de aspecto y lo sustituye por clases CSS seguras.
- Permite las solicitudes de mapas de código de jsDelivr para evitar advertencias falsas en la consola de desarrollo.
- Usa FFmpeg para normalizar cualquier stream principal cuando está disponible, incluyendo H.264 Baseline irregular.
- Reconstruye el RTP H.264 generado por FFmpeg en access units completas antes de enviarlo por WebRTC.
- Conserva el GOP transcodificado desde el último fotograma clave para que los visores no se incorporen a mitad del video.
- Inyecta SPS/PPS al comienzo de cada sesión y mantiene el reintento automático detrás del estado `Conectando`.
- Agrega respaldo automático al stream H.264 directo o secundario cuando FFmpeg falla.

## 0.6.3 - 2026-07-05

- Oculta completamente el elemento de video hasta confirmar un fotograma realmente decodificado.
- Sustituye mensajes técnicos y recuperación manual por un único estado visual `Conectando`.
- Añade una animación de carga profesional mientras WebRTC negocia, espera un fotograma clave o reintenta.
- Mantiene los reintentos automáticos ante timeout, pista detenida, ICE fallido, video estancado y recuperación de red.
- Exige datos de imagen y dimensiones reales antes de revelar el video en navegadores sin `requestVideoFrameCallback`.
- Ajusta la relación de aspecto con las dimensiones decodificadas del stream.
- Elimina el botón manual Reconectar del visor.
- Desactiva la caché de HTML, JavaScript y CSS embebidos para impedir que el navegador reutilice interfaces antiguas.

## 0.6.2 - 2026-07-05

- Reintenta automáticamente la vista WebRTC cuando no llega un fotograma decodificable.
- Mantiene el overlay en un único estado `Conectando` durante preparación, reconexión y recuperación.
- Maneja fallos de conexión, ICE desconectado, pistas terminadas o silenciadas, video estancado y cambios de red.
- Comprueba fotogramas realmente decodificados mediante `requestVideoFrameCallback` y avance temporal como respaldo.
- Recrea la sesión con backoff acotado y reinicio inmediato cuando el navegador vuelve a primer plano o recupera red.
- Conserva en memoria un GOP H.264 acotado desde el último fotograma clave para iniciar visores sin esperar al siguiente IDR.
- Limpia el GOP almacenado al reconectar la cámara para no entregar video obsoleto de una sesión anterior.
- Mantiene las suscripciones confiables del grabador sin reproducción del GOP almacenado.

## 0.6.1 - 2026-07-05

- Corrige el visor WebRTC que podía conectarse sin mostrar imagen y quedar completamente negro.
- Reconstruye el video H.264 desde access units completas en lugar de reenviar RTP de cámara sin normalizar.
- Espera un fotograma clave antes de iniciar cada visor e inyecta SPS/PPS en el arranque y tras reconexiones.
- Publica el perfil H.264 obtenido del SPS para evitar discrepancias de codec entre cámara y navegador.
- Usa FFmpeg automáticamente para normalizar H.264 Main/High Profile cuando está disponible, además de H.265.
- Prioriza H.264 en la negociación del navegador y no oculta el estado de carga hasta reproducir un fotograma real.
- Añade diagnóstico visual cuando existe conexión WebRTC pero todavía no llega video decodificable.
- Reorganiza la página con el video arriba y todas las acciones y opciones debajo.
- Mantiene la relación de aspecto real, usa `object-fit: contain` y evita recortes en escritorio, móvil y pantalla completa.
- Registra correctamente espectadores que consumen access units para conservar el apagado inteligente por inactividad.

## 0.6.0 - 2026-07-05

- Separa el dashboard, listado de cámaras, alta y ajustes en páginas independientes.
- Implementa un CRUD profesional de cámaras con tabla responsiva, búsqueda y filtros por estado.
- Añade menú de acciones de tres puntos para ver, ajustar, redetectar, iniciar o detener grabación y eliminar.
- Incorpora una página de alta dedicada con descubrimiento ONVIF, diagnóstico de red y prueba RTSP.
- Añade una página de ajustes por cámara para renombrar, habilitar, cambiar IP, usuario, contraseña y URL RTSP.
- Permite definir una carpeta de grabación segura y única por cámara.
- Valida los cambios de conexión antes de sustituir el stream activo y conserva la contraseña cuando el campo queda vacío.
- Permite configurar grabación, duración de segmentos y subida SFTP desde los ajustes.
- Migra cámaras existentes a una carpeta compatible basada en su identificador sin mover grabaciones anteriores.
- Mejora dashboard, tablas, formularios, estados vacíos, navegación y experiencia móvil.

## 0.5.0 - 2026-07-05

- Rediseña la interfaz con una estética administrativa profesional inspirada en SB Admin.
- Añade un layout reutilizable de aplicación con sidebar, topbar, footer y navegación responsiva.
- Añade un layout reutilizable de autenticación para el inicio de sesión.
- Incorpora Bootstrap 5.3.8 y Bootstrap Icons 1.13.1 mediante CDN oficial de jsDelivr.
- Implementa sidebar fijo en escritorio y menú offcanvas en teléfonos y tabletas.
- Añade dropdown de usuario con cierre de sesión y modo Invitado cuando la autenticación está deshabilitada.
- Agrega tarjetas de resumen para cámaras, dispositivos en línea, grabaciones y subidas pendientes.
- Rediseña tarjetas de cámaras, formularios, diagnóstico de red, visor y controles de grabación.
- Añade notificaciones toast y estados de carga visuales.
- Actualiza la política CSP para permitir exclusivamente los recursos CDN necesarios.

## 0.4.0 - 2026-07-05

- Añade duración de archivo configurable por cámara entre 1 minuto y 24 horas.
- Incorpora un componente web reutilizable para seleccionar minutos u horas.
- Permite cambiar la duración mientras se graba sin reiniciar RTSP.
- Inicia y detiene el grabador sin reiniciar el worker de la cámara.
- Abre y escribe el segmento siguiente antes de finalizar el anterior.
- Finaliza los MKV anteriores en segundo plano para evitar pausas por `fsync`.
- Mantiene el archivo actual si falla la creación del siguiente segmento.
- Detecta desconexiones RTSP y separa cada sesión en un MKV independiente.
- Añade una suscripción confiable de access units para el grabador.
- Migra cámaras existentes al valor predeterminado configurado en `.env`.

## 0.3.0 - 2026-07-05

- Selecciona el stream de mayor resolución aunque use H.265.
- Separa el stream principal de grabación y el stream H.264 alternativo para vista.
- Detecta FFmpeg y convierte H.265 a H.264 únicamente para WebRTC.
- Usa un substream H.264 como fallback si FFmpeg no está disponible o falla.
- Extrae dimensiones desde SPS H.264/H.265 cuando ONVIF no las entrega.
- Evita escribir 1920×1080 arbitrario en los metadatos MKV.
- Desactiva por defecto la grabación al agregar una cámara.
- Añade switch persistente para iniciar o detener la grabación por cámara.
- Añade redetección de calidad para cámaras existentes.
- Incorpora una página dedicada de cámara con reconexión y pantalla completa.
- Detiene FFmpeg o el substream de vista después de un periodo sin espectadores.
- Fuerza que toda cámara nueva se guarde con grabación apagada; el switch se activa después desde el panel.

## 0.2.1 - 2026-07-05

- Añade diagnóstico de conectividad desde el mismo proceso de Fragata.
- Distingue puerto abierto, conexión rechazada, timeout, host inalcanzable y ausencia de ruta.
- Evita sugerir cambios de URL o contraseña cuando el socket TCP todavía no responde.
- Comprueba el puerto antes de validar una URL RTSP manual.
- Aumenta el timeout TCP predeterminado de 1.2 a 3 segundos para cámaras lentas.
- Configura Docker Compose con red del host por defecto para servidores Linux y cámaras LAN.
- Añade `docker-compose.bridge.yml` como alternativa explícita.
- Muestra interfaces locales, entorno de ejecución y relación de subred desde el panel.

## 0.2.0 - 2026-07-05

- Agrega sondeo previo de puertos RTSP para evitar timeouts repetidos.
- Incorpora un diccionario RTSP integrado con rutas de fabricantes comunes.
- Permite ampliar el diccionario mediante un archivo local configurable.
- Añade prueba manual de URL RTSP desde el panel antes de guardarla.
- Corrige la persistencia de credenciales incluidas dentro de una URL RTSP.
- Fuerza el transporte RTSP sobre TCP para mejorar estabilidad en redes y contenedores.
- Mejora los errores de conectividad, autenticación, rutas inexistentes y streams sin video.
- Añade pruebas unitarias preparadas para el parser del diccionario y credenciales.

## 0.1.0 - 2026-07-05

- Implementa login opcional y sesiones persistentes.
- Agrega descubrimiento ONVIF y alta manual por IP.
- Detecta perfiles y URL RTSP mediante ONVIF.
- Recibe H.264/H.265 por RTSP.
- Graba segmentos MKV sin transcodificación.
- Agrega vista en vivo H.264 mediante WebRTC.
- Incorpora cola SFTP persistente.
- Añade compilación estática, Docker, systemd, pruebas preparadas y contexto técnico.
