# Changelog

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
