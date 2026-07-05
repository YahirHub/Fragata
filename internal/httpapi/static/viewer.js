let session = { csrf_token: 'anonymous', auth_enabled: false };
let camera = null;
let peer = null;
let frameTimer = null;
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
  await startLive();
  setInterval(refreshStatus, 3000);
}

function applyVideoAspect(width, height) {
  if (Number(width) > 0 && Number(height) > 0) {
    q('#viewerStage').style.setProperty('--viewer-aspect', `${Number(width)} / ${Number(height)}`);
  }
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

function setOverlay(message, { visible = true, error = false, loading = false, play = false } = {}) {
  const overlay = q('#viewerOverlay');
  q('#viewerMessage').textContent = message;
  q('#viewerSpinner').classList.toggle('d-none', !loading);
  q('#viewerPlayButton').classList.toggle('d-none', !play);
  overlay.classList.toggle('hidden', !visible);
  overlay.classList.toggle('error', error);
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

function attachRemoteTrack(event) {
  const video = q('#viewerVideo');
  const stream = event.streams?.[0] || new MediaStream([event.track]);
  video.srcObject = stream;
  video.muted = true;
  video.playsInline = true;
  video.play().catch(() => {
    setOverlay('La cámara está lista. Pulsa Reproducir para iniciar el video.', { play: true });
  });
  armFrameTimeout();
}

function armFrameTimeout() {
  clearTimeout(frameTimer);
  frameTimer = setTimeout(() => {
    const video = q('#viewerVideo');
    if (!video || video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) {
      setOverlay('La conexión se estableció, pero todavía no llegó un fotograma decodificable. Pulsa Reconectar.', { error: true });
    }
  }, 12000);
}

function markVideoPlaying() {
  clearTimeout(frameTimer);
  setOverlay('', { visible: false });
}

async function startLive() {
  stopLive();
  setOverlay('Preparando vista en vivo…', { loading: true });
  peer = new RTCPeerConnection();
  const transceiver = peer.addTransceiver('video', { direction: 'recvonly' });
  preferH264(transceiver);
  peer.ontrack = attachRemoteTrack;
  peer.onconnectionstatechange = () => {
    if (!peer) return;
    if (peer.connectionState === 'failed' || peer.connectionState === 'closed') {
      setOverlay('La vista se desconectó. Pulsa Reconectar.', { error: true });
    } else if (peer.connectionState === 'disconnected') {
      setOverlay('La conexión de video se interrumpió. Intentando conservar la sesión…', { loading: true });
    }
  };
  try {
    await peer.setLocalDescription(await peer.createOffer());
    await waitICE(peer);
    const answer = await api(`/api/cameras/${encodeURIComponent(cameraID)}/offer`, {
      method: 'POST',
      body: JSON.stringify({ sdp: peer.localDescription.sdp }),
    });
    await peer.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
    q('#liveMode').textContent = liveModeLabel(answer.mode);
  } catch (error) {
    stopLive();
    setOverlay(error.message, { error: true });
  }
}

function waitICE(connection) {
  if (connection.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise((resolve) => {
    const changed = () => {
      if (connection.iceGatheringState === 'complete') {
        connection.removeEventListener('icegatheringstatechange', changed);
        resolve();
      }
    };
    connection.addEventListener('icegatheringstatechange', changed);
    setTimeout(resolve, 8000);
  });
}

function stopLive() {
  clearTimeout(frameTimer);
  frameTimer = null;
  if (peer) {
    peer.close();
    peer = null;
  }
  const video = q('#viewerVideo');
  if (video?.srcObject) {
    video.srcObject.getTracks().forEach((track) => track.stop());
  }
  if (video) video.srcObject = null;
}

q('#viewerVideo').addEventListener('playing', markVideoPlaying);
q('#viewerVideo').addEventListener('loadeddata', markVideoPlaying);
q('#viewerVideo').addEventListener('error', () => setOverlay('El navegador no pudo decodificar el video recibido.', { error: true }));
q('#viewerVideo').addEventListener('stalled', () => setOverlay('La transmisión se detuvo temporalmente…', { loading: true }));
q('#viewerPlayButton').addEventListener('click', async () => {
  try {
    await q('#viewerVideo').play();
    markVideoPlaying();
  } catch (error) {
    setOverlay(error.message, { error: true, play: true });
  }
});
q('#reconnectButton').addEventListener('click', startLive);
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
window.addEventListener('beforeunload', stopLive);
init().catch((error) => {
  setOverlay(error.message, { error: true });
});
