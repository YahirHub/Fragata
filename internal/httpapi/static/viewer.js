let session = { csrf_token: 'anonymous', auth_enabled: false };
let camera = null;
let retryTimer = null;
let frameWatchTimer = null;
let initRetryTimer = null;
let statusTimer = null;
let healthTimer = null;
let liveGeneration = 0;
let retryAttempt = 0;
let lastFrameAt = 0;
let lastVideoTime = -1;
let liveStartedAt = 0;
let initialized = false;
let initInFlight = false;
let healthInFlight = false;
let serverOnline = true;
let shuttingDown = false;
let soundEnabled = false;
let streamHasAudio = false;
let wakeLock = null;
let monitorMode = localStorage.getItem('fragata.viewer.monitor-mode') !== 'false';

const reconnectDelays = [0, 750, 1500, 2500, 4000, 6000, 8000, 10000];
const apiTimeout = 15000;
const streamStartupTimeout = 45000;
const healthIntervalOnline = 8000;
const healthIntervalOffline = 2500;
const cameraID = decodeURIComponent(location.pathname.split('/').filter(Boolean).at(-1) || '');
const q = (selector) => document.querySelector(selector);
const notify = (message, type = 'primary') => window.FragataUI?.toast(message, type);

async function api(path, options = {}) {
  const { timeout = apiTimeout, ...fetchOptions } = options;
  const headers = { ...(fetchOptions.headers || {}) };
  if (fetchOptions.body) headers['Content-Type'] = 'application/json';
  if (fetchOptions.method && fetchOptions.method !== 'GET') headers['X-Fragata-CSRF'] = session.csrf_token;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);
  try {
    const response = await fetch(path, { ...fetchOptions, headers, signal: controller.signal, cache: 'no-store' });
    if (response.status === 401) {
      location.href = '/login';
      throw new Error('Sesión vencida');
    }
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(body.error || `HTTP ${response.status}`);
      error.httpStatus = response.status;
      throw error;
    }
    return body;
  } catch (error) {
    if (error?.name === 'AbortError') {
      const timeoutError = new Error('La solicitud agotó el tiempo de espera');
      timeoutError.networkFailure = true;
      throw timeoutError;
    }
    if (error instanceof TypeError) error.networkFailure = true;
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

async function init() {
  if (shuttingDown || initialized || initInFlight || !serverOnline) return;
  clearTimeout(initRetryTimer);
  initRetryTimer = null;
  initInFlight = true;
  showConnecting('Conectando con Fragata');

  try {
    session = await api('/api/session', { timeout: 10000 });
    q('fragata-app-layout')?.setSession(session);
    q('#ffmpegBadge')?.classList.toggle('hidden', !session.ffmpeg_available);
    camera = await api(`/api/cameras/${encodeURIComponent(cameraID)}`, { timeout: 10000 });
    document.title = `${camera.name} · Fragata`;
    q('#cameraName').textContent = camera.name;
    q('#cameraSubtitle').textContent = `${camera.host} · ${camera.manufacturer || ''} ${camera.model || ''}`.trim();
    q('#cameraSettingsButton').href = `/cameras/${encodeURIComponent(camera.id)}/settings`;
    q('fragata-app-layout')?.setSubtitle(`${camera.name} · ${camera.host}`);
    q('#primaryInfo').textContent = `${camera.codec || '—'} · ${camera.width && camera.height ? `${camera.width}×${camera.height}` : 'resolución pendiente'}`;
    q('#recordToggle').checked = camera.record;
    q('#segmentDurationPicker').valueSeconds = camera.segment_duration_seconds || session.default_segment_duration_seconds || 300;
    applyVideoAspect(camera.width, camera.height);
    updateAudioMetadata(camera);
    await Promise.all([refreshStatus(), refreshUploads()]);

    if (!session.ffmpeg_available) throw new Error('FFmpeg no está disponible para crear la transmisión web protegida');

    initialized = true;
    retryAttempt = 0;
    startLive();
    clearInterval(statusTimer);
    statusTimer = setInterval(() => {
      if (!document.hidden && serverOnline) refreshStatus().catch(() => {});
    }, 5000);
    updateMonitorControl();
    requestWakeLock();
  } catch (error) {
    initialized = false;
    if (isNetworkFailure(error)) markServerUnavailable();
    else showConnecting(error?.message || 'Esperando a la cámara');
  } finally {
    initInFlight = false;
  }

  if (!initialized && !shuttingDown) {
    initRetryTimer = setTimeout(init, serverOnline ? 2500 : 5000);
  }
}

function applyVideoAspect(width, height) {
  const stage = q('#viewerStage');
  if (!stage) return;
  stage.classList.remove('viewer-aspect-wide', 'viewer-aspect-landscape', 'viewer-aspect-square', 'viewer-aspect-portrait');
  const numericWidth = Number(width);
  const numericHeight = Number(height);
  if (!(numericWidth > 0 && numericHeight > 0)) {
    stage.classList.add('viewer-aspect-wide');
    return;
  }
  const ratio = numericWidth / numericHeight;
  if (ratio >= 1.6) stage.classList.add('viewer-aspect-wide');
  else if (ratio >= 1.15) stage.classList.add('viewer-aspect-landscape');
  else if (ratio >= 0.85) stage.classList.add('viewer-aspect-square');
  else stage.classList.add('viewer-aspect-portrait');
}

async function refreshStatus() {
  if (!camera) return;
  const statuses = await api('/api/status');
  const status = statuses.find((item) => item.camera_id === cameraID) || { state: 'starting' };
  const state = q('#viewerState');
  state.className = `camera-status ${status.state || 'starting'}`;
  state.innerHTML = `<span class="status-dot"></span>${translateState(status.state)}`;
  q('#recordingState').textContent = status.recording_path ? 'Grabando ahora' : (camera.record ? 'Esperando video o fotograma clave' : 'Apagada');
  if (status.audio_codec) {
    camera.audio_codec = status.audio_codec;
    camera.audio_sample_rate = status.audio_sample_rate;
    camera.audio_channels = status.audio_channels;
    updateAudioMetadata(camera);
  }
}

function updateAudioMetadata(source) {
  const hasAudio = Boolean(source?.audio_codec || streamHasAudio);
  const detail = source?.audio_codec
    ? `${source.audio_codec} · ${source.audio_sample_rate || '—'} Hz${source.audio_channels ? ` · ${source.audio_channels} canal${source.audio_channels === 1 ? '' : 'es'}` : ''}`
    : (streamHasAudio ? 'AAC compatible en la transmisión web' : 'Sin audio detectado');
  q('#audioInfo').textContent = detail;
  q('#audioButton').classList.toggle('hidden', !hasAudio);
  q('#audioStatus').classList.toggle('hidden', !hasAudio);
  if (!hasAudio) soundEnabled = false;
  updateAudioControl();
}

async function refreshUploads() {
  const jobs = await api('/api/uploads');
  const badge = q('#queueBadge');
  if (badge) badge.innerHTML = `<i class="bi bi-cloud-arrow-up me-1"></i>${jobs.length} subida${jobs.length === 1 ? '' : 's'}`;
}

function translateState(value) {
  return ({ online: 'en línea', starting: 'iniciando', connecting: 'conectando', reconnecting: 'reconectando' }[value] || value || 'desconocido');
}

function setViewerReady(ready) {
  const stage = q('#viewerStage');
  const overlay = q('#viewerOverlay');
  if (!stage || !overlay) return;
  stage.classList.toggle('is-ready', ready);
  stage.classList.toggle('is-loading', !ready);
  stage.setAttribute('aria-busy', ready ? 'false' : 'true');
  overlay.classList.toggle('hidden', ready);
}

function showConnecting(label = 'Conectando') {
  const loader = q('#viewerLoader');
  if (loader) loader.setAttribute('label', label);
  setViewerReady(false);
}

function isNetworkFailure(error) {
  return Boolean(error?.networkFailure || error instanceof TypeError);
}

function setRuntimeState(value, label) {
  const state = q('#viewerState');
  if (!state) return;
  state.className = `camera-status ${value}`;
  state.innerHTML = `<span class="status-dot"></span>${label}`;
}

function markServerUnavailable() {
  if (shuttingDown) return;
  const firstFailure = serverOnline;
  serverOnline = false;
  initialized = false;
  clearInterval(statusTimer);
  statusTimer = null;
  clearTimeout(retryTimer);
  retryTimer = null;
  liveGeneration++;
  disposeLiveStream();
  setRuntimeState('offline', 'servidor desconectado');
  showConnecting('Fragata no está disponible · reintentando');
  if (firstFailure) notify('Se perdió la conexión con Fragata. El visor se recuperará automáticamente.', 'warning');
}

async function checkServerHealth() {
  clearTimeout(healthTimer);
  healthTimer = null;
  if (shuttingDown || healthInFlight) return;
  healthInFlight = true;
  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 4000);
    let response;
    try {
      response = await fetch('/healthz', { cache: 'no-store', signal: controller.signal });
    } finally {
      clearTimeout(timer);
    }
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const recovered = !serverOnline;
    serverOnline = true;
    if (recovered) {
      notify('Fragata volvió a estar disponible. Recuperando la cámara…', 'success');
      initialized = false;
      retryAttempt = 0;
      await init();
    }
  } catch (_) {
    markServerUnavailable();
  } finally {
    healthInFlight = false;
    if (!shuttingDown) healthTimer = setTimeout(checkServerHealth, serverOnline ? healthIntervalOnline : healthIntervalOffline);
  }
}

async function startLive() {
  if (shuttingDown || !serverOnline || !initialized) return;
  clearTimeout(retryTimer);
  retryTimer = null;
  disposeLiveStream();
  showConnecting('Preparando transmisión protegida');

  const generation = ++liveGeneration;
  const video = q('#viewerVideo');
  liveStartedAt = Date.now();
  lastFrameAt = 0;
  lastVideoTime = -1;

  streamHasAudio = Boolean(camera?.audio_codec);
  updateAudioMetadata(camera);
  q('#liveMode').textContent = String(camera?.codec || '').toUpperCase() === 'H264'
    ? 'MP4 protegido · H.264 directo'
    : 'MP4 protegido · H.265→H.264 con FFmpeg';

  video.srcObject = null;
  video.muted = !soundEnabled;
  video.autoplay = true;
  video.playsInline = true;
  video.preload = 'auto';
  video.src = `/api/cameras/${encodeURIComponent(cameraID)}/live-stream?session=${generation}&_=${Date.now()}`;
  video.load();
  beginFrameWatch(generation);

  try {
    await video.play();
  } catch (error) {
    if (generation !== liveGeneration || shuttingDown) return;
    if (error?.name === 'NotAllowedError') {
      soundEnabled = false;
      video.muted = true;
      updateAudioControl();
      try {
        await video.play();
      } catch (_) {
        // loadeddata/playing o el vigilante de inicio decidirán si se reconecta.
      }
    }
  }
}

function beginFrameWatch(generation) {
  clearInterval(frameWatchTimer);
  lastFrameAt = 0;
  lastVideoTime = -1;
  liveStartedAt = Date.now();
  const video = q('#viewerVideo');
  frameWatchTimer = setInterval(() => {
    if (generation !== liveGeneration || shuttingDown || document.hidden) return;
    const currentTime = Number(video.currentTime);
    const hasImage = video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth > 0 && video.videoHeight > 0;
    const advanced = Number.isFinite(currentTime) && (lastVideoTime < 0 || currentTime > lastVideoTime + 0.001);
    if (hasImage && advanced) {
      lastVideoTime = currentTime;
      markDecodedFrame(generation);
    }
    if (!lastFrameAt && liveStartedAt > 0 && Date.now() - liveStartedAt > streamStartupTimeout) {
      showConnecting('La transmisión no entregó el primer fotograma · reintentando');
      scheduleReconnect(generation, true);
      return;
    }
    if (lastFrameAt > 0 && Date.now() - lastFrameAt > 12000) {
      scheduleReconnect(generation, true);
    }
  }, 1000);
}

function markDecodedFrame(generation) {
  if (generation !== liveGeneration || shuttingDown) return;
  lastFrameAt = Date.now();
  retryAttempt = 0;
  const video = q('#viewerVideo');
  if (video.videoWidth > 0 && video.videoHeight > 0) applyVideoAspect(video.videoWidth, video.videoHeight);
  setViewerReady(true);
}

function scheduleReconnect(generation, immediate = false) {
  if (shuttingDown || !serverOnline || generation !== liveGeneration) return;
  showConnecting(immediate ? 'Reconectando video' : 'Esperando video');
  disposeLiveStream();
  if (retryTimer) return;
  const delay = immediate ? 0 : reconnectDelays[Math.min(retryAttempt, reconnectDelays.length - 1)];
  retryAttempt = Math.min(retryAttempt + 1, reconnectDelays.length - 1);
  retryTimer = setTimeout(() => {
    retryTimer = null;
    if (!shuttingDown && serverOnline && generation === liveGeneration) startLive();
  }, delay);
}

function disposeLiveStream() {
  clearInterval(frameWatchTimer);
  frameWatchTimer = null;
  const video = q('#viewerVideo');
  video.pause();
  video.removeAttribute('src');
  video.srcObject = null;
  video.load();
  liveStartedAt = 0;
  lastFrameAt = 0;
  lastVideoTime = -1;
}

function keepNearLiveEdge() {
  const video = q('#viewerVideo');
  if (!video?.buffered?.length || video.seeking) return;
  try {
    const index = video.buffered.length - 1;
    const start = video.buffered.start(index);
    const end = video.buffered.end(index);
    if (!Number.isFinite(video.currentTime) || video.currentTime < start || end - video.currentTime > 6) {
      video.currentTime = Math.max(start, end - 0.75);
    }
  } catch (_) {
    // El rango puede cambiar mientras el navegador procesa un fragmento nuevo.
  }
}

function liveVideoErrorMessage() {
  const error = q('#viewerVideo')?.error;
  if (!error) return 'No se pudo abrir la transmisión en vivo';
  const messages = {
    1: 'La carga del video fue cancelada',
    2: 'Se interrumpió la transmisión de video',
    3: 'El navegador no pudo decodificar el video',
    4: 'El formato de la transmisión no es compatible',
  };
  return messages[error.code] || 'No se pudo abrir la transmisión en vivo';
}

function updateAudioControl() {
  const button = q('#audioButton');
  const status = q('#audioStatus');
  if (!button || button.classList.contains('hidden')) return;
  button.disabled = false;
  button.innerHTML = soundEnabled
    ? '<i class="bi bi-volume-up me-2"></i>Silenciar'
    : '<i class="bi bi-volume-mute me-2"></i>Activar sonido';
  if (status) {
    status.innerHTML = soundEnabled
      ? '<i class="bi bi-volume-up me-1"></i>Audio activo'
      : '<i class="bi bi-volume-mute me-1"></i>Audio silenciado';
  }
}

function updateMonitorControl() {
  const button = q('#monitorButton');
  if (!button) return;
  if (!('wakeLock' in navigator)) {
    button.classList.add('hidden');
    return;
  }
  button.classList.remove('hidden');
  button.classList.toggle('btn-outline-secondary', !monitorMode);
  button.classList.toggle('btn-outline-success', monitorMode);
  button.setAttribute('aria-pressed', monitorMode ? 'true' : 'false');
  button.innerHTML = monitorMode
    ? (wakeLock
      ? '<i class="bi bi-display-fill me-2"></i>Monitor activo'
      : '<i class="bi bi-display me-2"></i>Monitor preparado')
    : '<i class="bi bi-display me-2"></i>Activar monitor';
}

async function requestWakeLock() {
  if (shuttingDown || !monitorMode || document.hidden || !('wakeLock' in navigator) || wakeLock) return;
  try {
    wakeLock = await navigator.wakeLock.request('screen');
    wakeLock.addEventListener('release', () => {
      wakeLock = null;
      updateMonitorControl();
      if (!shuttingDown && monitorMode && !document.hidden) setTimeout(requestWakeLock, 1000);
    }, { once: true });
    updateMonitorControl();
  } catch (_) {
    wakeLock = null;
    updateMonitorControl();
  }
}

async function releaseWakeLock() {
  const current = wakeLock;
  wakeLock = null;
  if (current) {
    try { await current.release(); } catch (_) { /* Ya estaba liberado. */ }
  }
}

function stopLive() {
  shuttingDown = true;
  liveGeneration++;
  clearTimeout(retryTimer);
  clearTimeout(initRetryTimer);
  clearTimeout(healthTimer);
  clearInterval(statusTimer);
  retryTimer = null;
  initRetryTimer = null;
  healthTimer = null;
  statusTimer = null;
  disposeLiveStream();
  releaseWakeLock();
}

q('#viewerVideo').addEventListener('loadeddata', () => markDecodedFrame(liveGeneration));
q('#viewerVideo').addEventListener('playing', () => markDecodedFrame(liveGeneration));
q('#viewerVideo').addEventListener('error', () => {
  showConnecting(`${liveVideoErrorMessage()} · reintentando`);
  scheduleReconnect(liveGeneration, true);
});
q('#viewerVideo').addEventListener('stalled', () => {
  if (lastFrameAt && Date.now() - lastFrameAt > 5000) scheduleReconnect(liveGeneration, true);
});
q('#viewerVideo').addEventListener('waiting', () => {
  if (!lastFrameAt) showConnecting('Esperando el primer fotograma');
});
q('#viewerVideo').addEventListener('progress', keepNearLiveEdge);
q('#viewerVideo').addEventListener('timeupdate', keepNearLiveEdge);
q('#audioButton').addEventListener('click', async () => {
  const video = q('#viewerVideo');
  soundEnabled = !soundEnabled;
  video.muted = !soundEnabled;
  updateAudioControl();
  if (soundEnabled) {
    try {
      await video.play();
    } catch (_) {
      soundEnabled = false;
      video.muted = true;
      updateAudioControl();
      notify('El navegador bloqueó el audio. Vuelve a intentarlo después de tocar el reproductor.', 'warning');
    }
  }
});
q('#monitorButton')?.addEventListener('click', async () => {
  monitorMode = !monitorMode;
  localStorage.setItem('fragata.viewer.monitor-mode', monitorMode ? 'true' : 'false');
  updateMonitorControl();
  if (monitorMode) await requestWakeLock();
  else await releaseWakeLock();
});
q('#fullscreenButton').addEventListener('click', async () => {
  try {
    const stage = q('#viewerStage');
    if (document.fullscreenElement) await document.exitFullscreen();
    else if (stage.requestFullscreen) await stage.requestFullscreen();
    else throw new Error('Este navegador no permite pantalla completa desde la página.');
  } catch (error) {
    notify(error.message, 'danger');
  }
});
q('#recordToggle').addEventListener('change', async (event) => {
  const toggle = event.currentTarget;
  const previous = camera.record;
  toggle.disabled = true;
  try {
    camera = await api(`/api/cameras/${encodeURIComponent(cameraID)}`, {
      method: 'PATCH',
      body: JSON.stringify({ record: toggle.checked }),
    });
    q('#recordingState').textContent = camera.record ? 'Activándose…' : 'Apagada';
  } catch (error) {
    toggle.checked = previous;
    notify(error.message, 'danger');
  } finally {
    toggle.disabled = false;
  }
});
q('#segmentDurationPicker').addEventListener('durationchange', async (event) => {
  const picker = event.currentTarget;
  const previous = camera.segment_duration_seconds;
  picker.disabled = true;
  try {
    camera = await api(`/api/cameras/${encodeURIComponent(cameraID)}`, {
      method: 'PATCH',
      body: JSON.stringify({ segment_duration_seconds: event.detail.seconds }),
    });
    picker.valueSeconds = camera.segment_duration_seconds;
  } catch (error) {
    picker.valueSeconds = previous;
    notify(error.message, 'danger');
  } finally {
    picker.disabled = false;
  }
});
q('fragata-app-layout')?.addEventListener('fragata-logout', async () => {
  await api('/api/logout', { method: 'POST', body: '{}' });
  location.href = '/login';
});
window.addEventListener('online', () => {
  checkServerHealth();
  if (serverOnline && !lastFrameAt) {
    retryAttempt = 0;
    startLive();
  }
});
window.addEventListener('offline', markServerUnavailable);
window.addEventListener('pageshow', () => {
  if (!shuttingDown) checkServerHealth();
});
window.addEventListener('focus', () => {
  if (!shuttingDown && (!lastFrameAt || Date.now() - lastFrameAt > 5000)) checkServerHealth();
});
document.addEventListener('visibilitychange', () => {
  if (document.hidden) return;
  requestWakeLock();
  checkServerHealth();
  if (serverOnline && (!lastFrameAt || Date.now() - lastFrameAt > 5000)) {
    retryAttempt = 0;
    startLive();
  }
});
document.addEventListener('pointerdown', requestWakeLock, { once: true, passive: true });
window.addEventListener('beforeunload', stopLive);
updateMonitorControl();
showConnecting();
init();
checkServerHealth();
