# Changelog

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
