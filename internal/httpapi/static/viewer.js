let session = { csrf_token: 'anonymous', auth_enabled: false };
let camera = null;
let peer = null;
const cameraID = decodeURIComponent(location.pathname.split('/').filter(Boolean).at(-1) || '');
const q = (selector) => document.querySelector(selector);

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
  camera = await api(`/api/cameras/${encodeURIComponent(cameraID)}`);
  document.title = `${camera.name} · Fragata`;
  q('#cameraName').textContent = camera.name;
  q('#cameraSubtitle').textContent = `${camera.host} · ${camera.manufacturer || ''} ${camera.model || ''}`.trim();
  q('#primaryInfo').textContent = `${camera.codec || '—'} · ${camera.width && camera.height ? `${camera.width}×${camera.height}` : 'resolución pendiente'}`;
  q('#recordToggle').checked = camera.record;
  await refreshStatus();
  await startLive();
  setInterval(refreshStatus, 3000);
}

async function refreshStatus() {
  const statuses = await api('/api/status');
  const status = statuses.find((item) => item.camera_id === cameraID) || { state: 'starting' };
  const state = q('#viewerState');
  state.textContent = translateState(status.state);
  state.className = `state ${status.state || ''}`;
  q('#recordingState').textContent = status.recording_path ? 'Grabando ahora' : (camera.record ? 'Esperando video o fotograma clave' : 'Apagada');
  if (status.live_mode) q('#liveMode').textContent = liveModeLabel(status.live_mode);
}

function translateState(value) {
  return ({ online: 'en línea', starting: 'iniciando', connecting: 'conectando', reconnecting: 'reconectando' }[value] || value || 'desconocido');
}

function liveModeLabel(mode) {
  if (mode === 'direct') return 'H.264 principal directo';
  if (mode === 'ffmpeg') return 'Calidad principal convertida con FFmpeg';
  if (mode === 'substream') {
    const resolution = camera.live_width && camera.live_height ? ` · ${camera.live_width}×${camera.live_height}` : '';
    return `Substream H.264${resolution}`;
  }
  return mode || '—';
}

async function startLive() {
  stopLive();
  const overlay = q('#viewerOverlay');
  overlay.classList.remove('hidden', 'error');
  overlay.textContent = 'Preparando vista en vivo…';
  peer = new RTCPeerConnection();
  peer.addTransceiver('video', { direction: 'recvonly' });
  peer.ontrack = (event) => {
    q('#viewerVideo').srcObject = event.streams[0];
    overlay.classList.add('hidden');
  };
  peer.onconnectionstatechange = () => {
    if (peer && ['failed', 'closed', 'disconnected'].includes(peer.connectionState)) {
      overlay.classList.remove('hidden');
      overlay.textContent = 'La vista se desconectó. Pulsa Reconectar.';
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
    overlay.classList.remove('hidden');
    overlay.classList.add('error');
    overlay.textContent = error.message;
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
  if (peer) {
    peer.close();
    peer = null;
  }
  q('#viewerVideo').srcObject = null;
}

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
    alert(error.message);
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
    await startLive();
  } catch (error) {
    toggle.checked = previous;
    alert(error.message);
  } finally {
    toggle.disabled = false;
  }
});
window.addEventListener('beforeunload', stopLive);
init().catch((error) => {
  q('#viewerOverlay').classList.add('error');
  q('#viewerOverlay').textContent = error.message;
});
