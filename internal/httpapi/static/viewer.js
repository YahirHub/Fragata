let session = { csrf_token: 'anonymous', auth_enabled: false };
let camera = null;
let peer = null;
let audioPeer = null;
let retryTimer = null;
let audioRetryTimer = null;
let frameDeadlineTimer = null;
let frameWatchTimer = null;
let disconnectTimer = null;
let frameCallbackID = null;
let liveGeneration = 0;
let audioGeneration = 0;
let retryAttempt = 0;
let audioRetryAttempt = 0;
let lastFrameAt = 0;
let lastVideoTime = -1;
let shuttingDown = false;
let initialized = false;
let initInFlight = false;
let initRetryTimer = null;
let statusTimer = null;
let healthTimer = null;
let healthInFlight = false;
let serverOnline = true;
let remoteStream = null;
let soundEnabled = false;
let wakeLock = null;
let monitorMode = localStorage.getItem('fragata.viewer.monitor-mode') !== 'false';

const reconnectDelays = [0, 750, 1500, 2500, 4000, 6000, 8000, 10000];
const audioReconnectDelays = [1000, 2500, 5000, 10000, 20000, 30000];
const apiTimeout = 15000;
const offerTimeout = 45000;
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
    updateAudioMetadata(camera);
    q('#recordToggle').checked = camera.record;
    q('#segmentDurationPicker').valueSeconds = camera.segment_duration_seconds || session.default_segment_duration_seconds || 300;
    applyVideoAspect(camera.live_width || camera.width, camera.live_height || camera.height);
    await Promise.all([refreshStatus(), refreshUploads()]);

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
    if (soundEnabled && lastFrameAt > 0) startAudio();
  }
  if (status.live_mode) q('#liveMode').textContent = liveModeLabel(status.live_mode);
}

function canPlayAudio(source = camera) {
  const codec = String(source?.audio_codec || '').toUpperCase();
  return ['PCMA', 'PCMU', 'OPUS'].includes(codec) || (codec === 'AAC' && session.ffmpeg_available);
}

function updateAudioMetadata(source) {
  const hasAudio = Boolean(source?.audio_codec);
  const playable = canPlayAudio(source);
  const detail = hasAudio
    ? `${source.audio_codec} · ${source.audio_sample_rate || '—'} Hz${source.audio_channels ? ` · ${source.audio_channels} canal${source.audio_channels === 1 ? '' : 'es'}` : ''}`
    : 'Sin audio compatible';
  q('#audioInfo').textContent = hasAudio && !playable ? `${detail} · vista requiere FFmpeg` : detail;
  q('#audioButton').classList.toggle('hidden', !playable);
  q('#audioStatus').classList.toggle('hidden', !playable);
  if (!playable && soundEnabled) {
    soundEnabled = false;
    disposeAudioPeer();
  }
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

function liveModeLabel(mode) {
  if (mode === 'direct') return 'H.264 directo y normalizado';
  if (mode === 'ffmpeg') return 'Convertido para navegador con FFmpeg';
  if (mode === 'substream') {
    const resolution = camera.live_width && camera.live_height ? ` · ${camera.live_width}×${camera.live_height}` : '';
    return `Substream H.264${resolution}`;
  }
  return mode || '—';
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
  disposePeer();
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
      audioRetryAttempt = 0;
      if (initInFlight) {
        clearTimeout(initRetryTimer);
        initRetryTimer = setTimeout(() => {
          if (serverOnline && !initialized) init();
        }, 300);
      } else {
        await init();
      }
    }
  } catch (_) {
    markServerUnavailable();
  } finally {
    healthInFlight = false;
    if (!shuttingDown) {
      healthTimer = setTimeout(checkServerHealth, serverOnline ? healthIntervalOnline : healthIntervalOffline);
    }
  }
}

function preferH264(transceiver) {
  try {
    const capabilities = RTCRtpReceiver.getCapabilities?.('video');
    const codecs = capabilities?.codecs?.filter((codec) => codec.mimeType.toLowerCase() === 'video/h264') || [];
    if (codecs.length && typeof transceiver.setCodecPreferences === 'function') transceiver.setCodecPreferences(codecs);
  } catch (_) {
    // El navegador negociará su codec por defecto.
  }
}

function ensureRemoteStream() {
  if (!remoteStream) remoteStream = new MediaStream();
  const video = q('#viewerVideo');
  if (video.srcObject !== remoteStream) video.srcObject = remoteStream;
  const hasLiveAudio = remoteStream.getAudioTracks().some((track) => track.readyState === 'live');
  video.muted = !soundEnabled || !hasLiveAudio;
  video.autoplay = true;
  video.playsInline = true;
  return remoteStream;
}

function attachVideoTrack(event, generation, connection) {
  if (!isCurrentLive(generation, connection) || event.track.kind !== 'video') return;
  const video = q('#viewerVideo');
  const stream = ensureRemoteStream();
  if (!stream.getVideoTracks().some((track) => track.id === event.track.id)) stream.addTrack(event.track);
  event.track.onended = () => scheduleReconnect(generation, true);
  event.track.onmute = () => {
    if (!isCurrentLive(generation, connection)) return;
    showConnecting();
    armDisconnectRetry(generation, connection, 4000);
  };
  event.track.onunmute = () => clearDisconnectTimer();
  video.onloadeddata = () => {
    if (!isCurrentLive(generation, connection)) return;
    if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth > 0 && video.videoHeight > 0) markDecodedFrame(generation, connection);
  };
  beginFrameWatch(generation, connection);
  video.play().catch(() => scheduleReconnect(generation));
}

function attachAudioTrack(event, generation, connection) {
  if (!isCurrentAudio(generation, connection) || event.track.kind !== 'audio') return;
  const video = q('#viewerVideo');
  const stream = ensureRemoteStream();
  stream.getAudioTracks().forEach((track) => {
    if (track.id !== event.track.id) {
      stream.removeTrack(track);
      track.stop();
    }
  });
  if (!stream.getAudioTracks().some((track) => track.id === event.track.id)) stream.addTrack(event.track);
  event.track.onended = () => scheduleAudioReconnect(generation);
  event.track.onmute = () => scheduleAudioReconnect(generation);
  event.track.onunmute = () => {
    audioRetryAttempt = 0;
    updateAudioControl();
  };
  video.muted = !soundEnabled;
  video.play().catch(() => {});
  audioRetryAttempt = 0;
  updateAudioControl();
}

function updateAudioControl() {
  const button = q('#audioButton');
  const status = q('#audioStatus');
  if (!button || button.classList.contains('hidden')) return;
  const hasTrack = Boolean(remoteStream?.getAudioTracks().some((track) => track.readyState === 'live'));
  button.disabled = false;
  button.innerHTML = soundEnabled ? '<i class="bi bi-volume-up me-2"></i>Silenciar' : '<i class="bi bi-volume-mute me-2"></i>Activar sonido';
  if (status) {
    if (hasTrack && soundEnabled) status.innerHTML = '<i class="bi bi-volume-up me-1"></i>Audio activo';
    else if (soundEnabled) status.innerHTML = '<i class="bi bi-hourglass-split me-1"></i>Conectando audio';
    else status.innerHTML = '<i class="bi bi-volume-mute me-1"></i>Audio silenciado';
  }
}

function beginFrameWatch(generation, connection) {
  clearFrameWatch();
  lastFrameAt = 0;
  lastVideoTime = -1;
  const video = q('#viewerVideo');
  armFrameDeadline(generation, connection);

  if (typeof video.requestVideoFrameCallback === 'function') {
    const onFrame = () => {
      if (!isCurrentLive(generation, connection)) return;
      markDecodedFrame(generation, connection);
      frameCallbackID = video.requestVideoFrameCallback(onFrame);
    };
    frameCallbackID = video.requestVideoFrameCallback(onFrame);
  }

  frameWatchTimer = setInterval(() => {
    if (!isCurrentLive(generation, connection) || document.hidden) return;
    const currentTime = Number(video.currentTime);
    const hasDecodedImage = video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth > 0 && video.videoHeight > 0;
    const advanced = Number.isFinite(currentTime) && (lastVideoTime < 0 || currentTime > lastVideoTime + 0.001);
    if (hasDecodedImage && advanced) {
      lastVideoTime = currentTime;
      markDecodedFrame(generation, connection);
    }
    if (lastFrameAt > 0 && Date.now() - lastFrameAt > 10000) scheduleReconnect(generation, true);
  }, 1000);
}

function armFrameDeadline(generation, connection) {
  clearTimeout(frameDeadlineTimer);
  frameDeadlineTimer = setTimeout(() => {
    if (!lastFrameAt && isCurrentLive(generation, connection)) scheduleReconnect(generation, true);
  }, 20000);
}

function markDecodedFrame(generation, connection) {
  if (generation !== liveGeneration || (connection && !isCurrentLive(generation, connection))) return;
  lastFrameAt = Date.now();
  retryAttempt = 0;
  clearTimeout(frameDeadlineTimer);
  frameDeadlineTimer = null;
  clearDisconnectTimer();
  const video = q('#viewerVideo');
  if (video?.videoWidth > 0 && video?.videoHeight > 0) applyVideoAspect(video.videoWidth, video.videoHeight);
  setViewerReady(true);
  if (soundEnabled) startAudio();
}

function armDisconnectRetry(generation, connection, delay = 3500) {
  clearDisconnectTimer();
  disconnectTimer = setTimeout(() => {
    if (isCurrentLive(generation, connection)) scheduleReconnect(generation, true);
  }, delay);
}

function clearDisconnectTimer() {
  clearTimeout(disconnectTimer);
  disconnectTimer = null;
}

function clearFrameWatch() {
  clearTimeout(frameDeadlineTimer);
  clearInterval(frameWatchTimer);
  frameDeadlineTimer = null;
  frameWatchTimer = null;
  const video = q('#viewerVideo');
  if (frameCallbackID !== null && typeof video?.cancelVideoFrameCallback === 'function') video.cancelVideoFrameCallback(frameCallbackID);
  frameCallbackID = null;
}

function isCurrentLive(generation, connection) {
  return !shuttingDown && generation === liveGeneration && peer === connection;
}

function isCurrentAudio(generation, connection) {
  return !shuttingDown && generation === audioGeneration && audioPeer === connection;
}

function disposeAudioPeer() {
  clearTimeout(audioRetryTimer);
  audioRetryTimer = null;
  audioGeneration++;
  const current = audioPeer;
  audioPeer = null;
  if (current) current.close();
  if (remoteStream) {
    remoteStream.getAudioTracks().forEach((track) => {
      remoteStream.removeTrack(track);
      track.stop();
    });
  }
  updateAudioControl();
}

function disposePeer() {
  clearFrameWatch();
  clearDisconnectTimer();
  disposeAudioPeer();
  const current = peer;
  peer = null;
  if (current) current.close();
  const video = q('#viewerVideo');
  if (remoteStream) remoteStream.getTracks().forEach((track) => track.stop());
  remoteStream = null;
  if (video) {
    video.pause();
    video.srcObject = null;
  }
  lastFrameAt = 0;
  lastVideoTime = -1;
}

function scheduleReconnect(generation, immediate = false) {
  if (shuttingDown || !serverOnline || generation !== liveGeneration) return;
  showConnecting();
  disposePeer();
  if (retryTimer) return;
  const offlineDelay = navigator.onLine === false ? 3000 : null;
  const delay = offlineDelay ?? (immediate ? 0 : reconnectDelays[Math.min(retryAttempt, reconnectDelays.length - 1)]);
  retryAttempt = Math.min(retryAttempt + 1, reconnectDelays.length - 1);
  retryTimer = setTimeout(() => {
    retryTimer = null;
    if (!shuttingDown && generation === liveGeneration) startLive();
  }, delay);
}

function scheduleAudioReconnect(generation, immediate = false) {
  if (shuttingDown || !soundEnabled || generation !== audioGeneration || lastFrameAt === 0) return;
  const current = audioPeer;
  audioPeer = null;
  if (current) current.close();
  if (remoteStream) {
    remoteStream.getAudioTracks().forEach((track) => {
      remoteStream.removeTrack(track);
      track.stop();
    });
  }
  updateAudioControl();
  if (audioRetryTimer) return;
  const delay = immediate ? 0 : audioReconnectDelays[Math.min(audioRetryAttempt, audioReconnectDelays.length - 1)];
  audioRetryAttempt = Math.min(audioRetryAttempt + 1, audioReconnectDelays.length - 1);
  audioRetryTimer = setTimeout(() => {
    audioRetryTimer = null;
    if (!shuttingDown && soundEnabled && lastFrameAt > 0) startAudio();
  }, delay);
}

async function startLive() {
  if (shuttingDown || !serverOnline || !initialized) return;
  clearTimeout(retryTimer);
  retryTimer = null;
  disposePeer();
  showConnecting();

  const generation = ++liveGeneration;
  const connection = new RTCPeerConnection();
  peer = connection;
  const transceiver = connection.addTransceiver('video', { direction: 'recvonly' });
  preferH264(transceiver);
  connection.ontrack = (event) => attachVideoTrack(event, generation, connection);
  connection.onconnectionstatechange = () => {
    if (!isCurrentLive(generation, connection)) return;
    switch (connection.connectionState) {
      case 'connected':
        clearDisconnectTimer();
        break;
      case 'disconnected':
        showConnecting();
        armDisconnectRetry(generation, connection);
        break;
      case 'failed':
      case 'closed':
        scheduleReconnect(generation, true);
        break;
      default:
        showConnecting();
    }
  };
  connection.oniceconnectionstatechange = () => {
    if (!isCurrentLive(generation, connection)) return;
    if (connection.iceConnectionState === 'failed') scheduleReconnect(generation, true);
    else if (connection.iceConnectionState === 'disconnected') {
      showConnecting();
      armDisconnectRetry(generation, connection);
    }
  };

  try {
    await connection.setLocalDescription(await connection.createOffer());
    await waitICE(connection, () => isCurrentLive(generation, connection));
    if (!isCurrentLive(generation, connection)) return;
    const answer = await api(`/api/cameras/${encodeURIComponent(cameraID)}/offer`, {
      method: 'POST',
      body: JSON.stringify({ sdp: connection.localDescription.sdp, media: 'video' }),
      timeout: offerTimeout,
    });
    if (!isCurrentLive(generation, connection)) return;
    await connection.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
    q('#liveMode').textContent = liveModeLabel(answer.mode);
    armFrameDeadline(generation, connection);
  } catch (error) {
    if (isNetworkFailure(error)) markServerUnavailable();
    else scheduleReconnect(generation);
  }
}

async function startAudio() {
  if (shuttingDown || !soundEnabled || lastFrameAt === 0 || !canPlayAudio() || audioPeer || audioRetryTimer) return;
  const generation = ++audioGeneration;
  const connection = new RTCPeerConnection();
  audioPeer = connection;
  connection.addTransceiver('audio', { direction: 'recvonly' });
  connection.ontrack = (event) => attachAudioTrack(event, generation, connection);
  connection.onconnectionstatechange = () => {
    if (!isCurrentAudio(generation, connection)) return;
    if (connection.connectionState === 'failed' || connection.connectionState === 'closed' || connection.connectionState === 'disconnected') {
      scheduleAudioReconnect(generation);
    }
  };
  connection.oniceconnectionstatechange = () => {
    if (!isCurrentAudio(generation, connection)) return;
    if (connection.iceConnectionState === 'failed' || connection.iceConnectionState === 'disconnected') scheduleAudioReconnect(generation);
  };

  try {
    await connection.setLocalDescription(await connection.createOffer());
    await waitICE(connection, () => isCurrentAudio(generation, connection));
    if (!isCurrentAudio(generation, connection)) return;
    const answer = await api(`/api/cameras/${encodeURIComponent(cameraID)}/offer`, {
      method: 'POST',
      body: JSON.stringify({ sdp: connection.localDescription.sdp, media: 'audio' }),
      timeout: offerTimeout,
    });
    if (!isCurrentAudio(generation, connection)) return;
    await connection.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
  } catch (error) {
    if (isNetworkFailure(error)) markServerUnavailable();
    else scheduleAudioReconnect(generation);
  }
}

function waitICE(connection, stillCurrent) {
  if (connection.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise((resolve) => {
    let finished = false;
    const finish = () => {
      if (finished) return;
      finished = true;
      connection.removeEventListener('icegatheringstatechange', changed);
      resolve();
    };
    const changed = () => {
      if (connection.iceGatheringState === 'complete' || !stillCurrent()) finish();
    };
    connection.addEventListener('icegatheringstatechange', changed);
    setTimeout(finish, 8000);
  });
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
  audioGeneration++;
  clearTimeout(retryTimer);
  clearTimeout(audioRetryTimer);
  clearTimeout(initRetryTimer);
  clearTimeout(healthTimer);
  clearInterval(statusTimer);
  retryTimer = null;
  audioRetryTimer = null;
  initRetryTimer = null;
  healthTimer = null;
  statusTimer = null;
  disposePeer();
  releaseWakeLock();
}

q('#viewerVideo').addEventListener('error', () => scheduleReconnect(liveGeneration, true));
q('#viewerVideo').addEventListener('stalled', () => {
  showConnecting();
  const generation = liveGeneration;
  const connection = peer;
  if (connection) armDisconnectRetry(generation, connection, 3000);
});
q('#viewerVideo').addEventListener('waiting', () => {
  if (!lastFrameAt) showConnecting();
});
q('#audioButton').addEventListener('click', async () => {
  const video = q('#viewerVideo');
  if (soundEnabled) {
    soundEnabled = false;
    video.muted = true;
    disposeAudioPeer();
    updateAudioControl();
    return;
  }
  soundEnabled = true;
  const hasLiveAudio = Boolean(remoteStream?.getAudioTracks().some((track) => track.readyState === 'live'));
  video.muted = !hasLiveAudio;
  updateAudioControl();
  startAudio();
  try {
    await video.play();
  } catch (_) {
    soundEnabled = false;
    video.muted = true;
    disposeAudioPeer();
    updateAudioControl();
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
