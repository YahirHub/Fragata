let session = { csrf_token: 'anonymous', auth_enabled: false };
let camera = null;
let peer = null;
let retryTimer = null;
let frameDeadlineTimer = null;
let frameWatchTimer = null;
let disconnectTimer = null;
let frameCallbackID = null;
let liveGeneration = 0;
let retryAttempt = 0;
let lastFrameAt = 0;
let lastVideoTime = -1;
let shuttingDown = false;
let initialized = false;
let initRetryTimer = null;
let statusTimer = null;

const reconnectDelays = [0, 750, 1500, 2500, 4000, 6000, 8000, 10000];
const cameraID = decodeURIComponent(location.pathname.split('/').filter(Boolean).at(-1) || '');
const q = (selector) => document.querySelector(selector);
const notify = (message, type = 'primary') => window.FragataUI?.toast(message, type);

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body) headers['Content-Type'] = 'application/json';
  if (options.method && options.method !== 'GET') headers['X-Fragata-CSRF'] = session.csrf_token;
  const response = await fetch(path, { ...options, headers });
  if (response.status === 401) {
    location.href = '/login';
    throw new Error('Sesión vencida');
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
  return body;
}

async function init() {
  if (shuttingDown || initialized) return;
  clearTimeout(initRetryTimer);
  initRetryTimer = null;
  showConnecting();

  try {
    session = await api('/api/session');
    q('fragata-app-layout')?.setSession(session);
    q('#ffmpegBadge')?.classList.toggle('hidden', !session.ffmpeg_available);
    camera = await api(`/api/cameras/${encodeURIComponent(cameraID)}`);
    document.title = `${camera.name} · Fragata`;
    q('#cameraName').textContent = camera.name;
    q('#cameraSubtitle').textContent = `${camera.host} · ${camera.manufacturer || ''} ${camera.model || ''}`.trim();
    q('#cameraSettingsButton').href = `/cameras/${encodeURIComponent(camera.id)}/settings`;
    q('fragata-app-layout')?.setSubtitle(`${camera.name} · ${camera.host}`);
    q('#primaryInfo').textContent = `${camera.codec || '—'} · ${camera.width && camera.height ? `${camera.width}×${camera.height}` : 'resolución pendiente'}`;
    q('#recordToggle').checked = camera.record;
    q('#segmentDurationPicker').valueSeconds = camera.segment_duration_seconds || session.default_segment_duration_seconds || 300;
    applyVideoAspect(camera.live_width || camera.width, camera.live_height || camera.height);
    await Promise.all([refreshStatus(), refreshUploads()]);

    initialized = true;
    startLive();
    clearInterval(statusTimer);
    statusTimer = setInterval(refreshStatus, 3000);
  } catch (_) {
    showConnecting();
    initRetryTimer = setTimeout(init, 2500);
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
  const statuses = await api('/api/status');
  const status = statuses.find((item) => item.camera_id === cameraID) || { state: 'starting' };
  const state = q('#viewerState');
  state.className = `camera-status ${status.state || 'starting'}`;
  state.innerHTML = `<span class="status-dot"></span>${translateState(status.state)}`;
  q('#recordingState').textContent = status.recording_path ? 'Grabando ahora' : (camera.record ? 'Esperando video o fotograma clave' : 'Apagada');
  if (status.live_mode) q('#liveMode').textContent = liveModeLabel(status.live_mode);
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

function showConnecting() {
  const message = q('#viewerMessage');
  if (message) message.textContent = 'Conectando';
  setViewerReady(false);
}

function preferH264(transceiver) {
  try {
    const capabilities = RTCRtpReceiver.getCapabilities?.('video');
    const codecs = capabilities?.codecs?.filter((codec) => codec.mimeType.toLowerCase() === 'video/h264') || [];
    if (codecs.length && typeof transceiver.setCodecPreferences === 'function') {
      transceiver.setCodecPreferences(codecs);
    }
  } catch (_) {
    // El navegador negociará su codec por defecto.
  }
}

function attachRemoteTrack(event, generation, connection) {
  if (!isCurrentLive(generation, connection)) return;
  const video = q('#viewerVideo');
  const stream = event.streams?.[0] || new MediaStream([event.track]);
  video.srcObject = stream;
  video.muted = true;
  video.autoplay = true;
  video.playsInline = true;
  event.track.onended = () => scheduleReconnect(generation, true);
  event.track.onmute = () => {
    if (!isCurrentLive(generation, connection)) return;
    showConnecting();
    armDisconnectRetry(generation, connection, 4000);
  };
  event.track.onunmute = () => clearDisconnectTimer();
  video.onloadeddata = () => {
    if (!isCurrentLive(generation, connection)) return;
    if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth > 0 && video.videoHeight > 0) {
      markDecodedFrame(generation, connection);
    }
  };
  beginFrameWatch(generation, connection);
  video.play().catch(() => scheduleReconnect(generation));
}

function beginFrameWatch(generation, connection) {
  clearFrameWatch();
  lastFrameAt = 0;
  lastVideoTime = -1;
  const video = q('#viewerVideo');

  frameDeadlineTimer = setTimeout(() => {
    if (!lastFrameAt && isCurrentLive(generation, connection)) {
      scheduleReconnect(generation, true);
    }
  }, 15000);

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
    if (lastFrameAt > 0 && Date.now() - lastFrameAt > 10000) {
      scheduleReconnect(generation, true);
    }
  }, 1000);
}

function markDecodedFrame(generation, connection) {
  if (generation !== liveGeneration || (connection && !isCurrentLive(generation, connection))) return;
  lastFrameAt = Date.now();
  retryAttempt = 0;
  clearTimeout(frameDeadlineTimer);
  frameDeadlineTimer = null;
  clearDisconnectTimer();
  const video = q('#viewerVideo');
  if (video?.videoWidth > 0 && video?.videoHeight > 0) {
    applyVideoAspect(video.videoWidth, video.videoHeight);
  }
  setViewerReady(true);
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
  if (frameCallbackID !== null && typeof video?.cancelVideoFrameCallback === 'function') {
    video.cancelVideoFrameCallback(frameCallbackID);
  }
  frameCallbackID = null;
}

function isCurrentLive(generation, connection) {
  return !shuttingDown && generation === liveGeneration && peer === connection;
}

function disposePeer() {
  clearFrameWatch();
  clearDisconnectTimer();
  const current = peer;
  peer = null;
  if (current) current.close();
  const video = q('#viewerVideo');
  if (video?.srcObject) video.srcObject.getTracks().forEach((track) => track.stop());
  if (video) {
    video.pause();
    video.srcObject = null;
  }
  lastFrameAt = 0;
  lastVideoTime = -1;
}

function scheduleReconnect(generation, immediate = false) {
  if (shuttingDown || generation !== liveGeneration) return;
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

async function startLive() {
  if (shuttingDown) return;
  clearTimeout(retryTimer);
  retryTimer = null;
  disposePeer();
  showConnecting();

  const generation = ++liveGeneration;
  const connection = new RTCPeerConnection();
  peer = connection;
  const transceiver = connection.addTransceiver('video', { direction: 'recvonly' });
  preferH264(transceiver);
  connection.ontrack = (event) => attachRemoteTrack(event, generation, connection);
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
    if (connection.iceConnectionState === 'failed') {
      scheduleReconnect(generation, true);
    } else if (connection.iceConnectionState === 'disconnected') {
      showConnecting();
      armDisconnectRetry(generation, connection);
    }
  };

  try {
    await connection.setLocalDescription(await connection.createOffer());
    await waitICE(connection, generation);
    if (!isCurrentLive(generation, connection)) return;
    const answer = await api(`/api/cameras/${encodeURIComponent(cameraID)}/offer`, {
      method: 'POST',
      body: JSON.stringify({ sdp: connection.localDescription.sdp }),
    });
    if (!isCurrentLive(generation, connection)) return;
    await connection.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
    q('#liveMode').textContent = liveModeLabel(answer.mode);
    frameDeadlineTimer = setTimeout(() => {
      if (!lastFrameAt && isCurrentLive(generation, connection)) scheduleReconnect(generation, true);
    }, 15000);
  } catch (_) {
    scheduleReconnect(generation);
  }
}

function waitICE(connection, generation) {
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
      if (connection.iceGatheringState === 'complete' || generation !== liveGeneration) finish();
    };
    connection.addEventListener('icegatheringstatechange', changed);
    setTimeout(finish, 8000);
  });
}

function stopLive() {
  shuttingDown = true;
  liveGeneration++;
  clearTimeout(retryTimer);
  clearTimeout(initRetryTimer);
  clearInterval(statusTimer);
  retryTimer = null;
  initRetryTimer = null;
  statusTimer = null;
  disposePeer();
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
q('#fullscreenButton').addEventListener('click', async () => {
  try {
    const stage = q('#viewerStage');
    if (document.fullscreenElement) {
      await document.exitFullscreen();
    } else if (stage.requestFullscreen) {
      await stage.requestFullscreen();
    } else {
      throw new Error('Este navegador no permite pantalla completa desde la página.');
    }
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
  if (!lastFrameAt) {
    retryAttempt = 0;
    startLive();
  }
});
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && (!lastFrameAt || Date.now() - lastFrameAt > 5000)) {
    retryAttempt = 0;
    startLive();
  }
});
window.addEventListener('beforeunload', stopLive);
showConnecting();
init();
