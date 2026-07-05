let session = { csrf_token: 'anonymous', auth_enabled: false };
let cameras = [];
let statuses = [];
const peers = new Map();

const q = (selector) => document.querySelector(selector);
const esc = (value) => String(value ?? '').replace(/[&<>'"]/g, (character) => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;',
}[character]));

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
  q('#logoutButton').classList.toggle('hidden', !session.auth_enabled);
  await refreshAll();
  setInterval(refreshStatus, 3000);
  setInterval(refreshUploads, 7000);
}

async function refreshAll() {
  [cameras, statuses] = await Promise.all([api('/api/cameras'), api('/api/status')]);
  renderCameras();
  await refreshUploads();
}

async function refreshStatus() {
  statuses = await api('/api/status');
  for (const camera of cameras) updateCard(camera);
  q('#cameraSummary').textContent = `${cameras.length} cámara${cameras.length === 1 ? '' : 's'} · ${statuses.filter((status) => status.state === 'online').length} en línea`;
}

async function refreshUploads() {
  const jobs = await api('/api/uploads');
  q('#queueBadge').textContent = `${jobs.length} subida${jobs.length === 1 ? '' : 's'}`;
}

function renderCameras() {
  q('#cameraGrid').innerHTML = '';
  q('#emptyState').classList.toggle('hidden', cameras.length > 0);
  for (const camera of cameras) {
    const card = document.createElement('article');
    card.className = 'camera-card';
    card.id = `camera-${camera.id}`;
    card.innerHTML = `<div class="video-wrap"><video id="video-${camera.id}" autoplay muted playsinline></video><div class="video-placeholder" id="placeholder-${camera.id}">Vista detenida</div></div><div class="camera-content"><div class="camera-line"><h3>${esc(camera.name)}</h3><span class="state" data-state>iniciando</span></div><div class="meta"><span>${esc(camera.host)}</span><span data-codec>${esc(camera.codec || '—')}</span><span>${camera.width && camera.height ? `${camera.width}×${camera.height}` : 'Resolución automática'}</span><span data-record>${camera.record ? 'MKV activo' : 'Sin grabación'}</span></div><div class="error" data-error></div><div class="card-actions"><button class="secondary" data-live>Ver en vivo</button><button class="ghost danger" data-delete>Eliminar</button></div></div>`;
    card.querySelector('[data-live]').addEventListener('click', () => toggleLive(camera.id));
    card.querySelector('[data-delete]').addEventListener('click', () => deleteCamera(camera.id, camera.name));
    q('#cameraGrid').append(card);
    updateCard(camera);
  }
  q('#cameraSummary').textContent = `${cameras.length} cámara${cameras.length === 1 ? '' : 's'}`;
}

function updateCard(camera) {
  const card = q(`#camera-${CSS.escape(camera.id)}`);
  if (!card) return;
  const status = statuses.find((item) => item.camera_id === camera.id) || { state: 'starting' };
  const state = card.querySelector('[data-state]');
  state.textContent = translateState(status.state);
  state.className = `state ${status.state}`;
  card.querySelector('[data-codec]').textContent = status.codec || camera.codec || '—';
  card.querySelector('[data-error]').textContent = status.last_error || '';
  card.querySelector('[data-record]').textContent = status.recording_path ? 'Grabando MKV' : (camera.record ? 'Esperando fotograma clave' : 'Sin grabación');
  const liveButton = card.querySelector('[data-live]');
  liveButton.disabled = status.state !== 'online' && !peers.has(camera.id);
  liveButton.textContent = peers.has(camera.id) ? 'Detener vista' : 'Ver en vivo';
}

function translateState(value) {
  return ({ online: 'en línea', starting: 'iniciando', connecting: 'conectando', reconnecting: 'reconectando' }[value] || value || 'desconocido');
}

async function toggleLive(id) {
  if (peers.has(id)) {
    stopLive(id);
    return;
  }
  const peer = new RTCPeerConnection();
  peers.set(id, peer);
  const video = q(`#video-${CSS.escape(id)}`);
  const placeholder = q(`#placeholder-${CSS.escape(id)}`);
  peer.addTransceiver('video', { direction: 'recvonly' });
  peer.ontrack = (event) => {
    video.srcObject = event.streams[0];
    placeholder.classList.add('hidden');
  };
  peer.onconnectionstatechange = () => {
    if (['failed', 'closed', 'disconnected'].includes(peer.connectionState)) stopLive(id);
  };
  try {
    await peer.setLocalDescription(await peer.createOffer());
    await waitICE(peer);
    const answer = await api(`/api/cameras/${encodeURIComponent(id)}/offer`, {
      method: 'POST', body: JSON.stringify({ sdp: peer.localDescription.sdp }),
    });
    await peer.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
    updateCard(cameras.find((camera) => camera.id === id));
  } catch (error) {
    stopLive(id);
    alert(error.message);
  }
}

function waitICE(peer) {
  if (peer.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise((resolve) => {
    const changed = () => {
      if (peer.iceGatheringState === 'complete') {
        peer.removeEventListener('icegatheringstatechange', changed);
        resolve();
      }
    };
    peer.addEventListener('icegatheringstatechange', changed);
    setTimeout(resolve, 8000);
  });
}

function stopLive(id) {
  const peer = peers.get(id);
  if (peer) {
    peer.close();
    peers.delete(id);
  }
  const video = q(`#video-${CSS.escape(id)}`);
  if (video) {
    video.srcObject = null;
    q(`#placeholder-${CSS.escape(id)}`).classList.remove('hidden');
  }
  const camera = cameras.find((item) => item.id === id);
  if (camera) updateCard(camera);
}

function cameraFormData() {
  const form = q('#cameraForm');
  const data = Object.fromEntries(new FormData(form));
  data.record = form.elements.record.checked;
  data.upload = form.elements.upload.checked;
  data.host = String(data.host || '').trim();
  data.rtsp_url = String(data.rtsp_url || '').trim();
  return data;
}

q('#probeRTSPButton').addEventListener('click', async () => {
  const button = q('#probeRTSPButton');
  const status = q('#formStatus');
  const data = cameraFormData();
  if (!data.rtsp_url) {
    status.textContent = 'Introduce una URL RTSP para probarla.';
    return;
  }
  button.disabled = true;
  status.textContent = 'Comprobando conexión RTSP y recepción de video…';
  try {
    const probe = await api('/api/rtsp/probe', {
      method: 'POST',
      body: JSON.stringify({
        host: data.host,
        username: data.username || '',
        password: data.password || '',
        rtsp_url: data.rtsp_url,
      }),
    });
    if (!q('#cameraForm').elements.host.value.trim()) q('#cameraForm').elements.host.value = probe.host;
    status.textContent = `URL válida: ${probe.codec} por el puerto ${probe.port}. Ya puedes guardarla.`;
  } catch (error) {
    status.textContent = error.message;
  } finally {
    button.disabled = false;
  }
});

q('#cameraForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  const status = q('#formStatus');
  const data = cameraFormData();
  if (!data.host && !data.rtsp_url) {
    status.textContent = 'Introduce la IP o una URL RTSP manual.';
    return;
  }
  button.disabled = true;
  q('#probeRTSPButton').disabled = true;
  status.textContent = data.rtsp_url ? 'Validando URL RTSP antes de guardarla…' : 'Consultando ONVIF, puertos y diccionario RTSP…';
  try {
    const result = await api('/api/cameras', { method: 'POST', body: JSON.stringify(data) });
    const openPorts = result.diagnostics?.open_ports?.length ? ` Puertos detectados: ${result.diagnostics.open_ports.join(', ')}.` : '';
    status.textContent = `Cámara agregada mediante ${friendlyMethod(result.detection_method)}.${openPorts}`;
    form.reset();
    form.elements.record.checked = true;
    await refreshAll();
  } catch (error) {
    status.textContent = error.message;
  } finally {
    button.disabled = false;
    q('#probeRTSPButton').disabled = false;
  }
});

function friendlyMethod(method) {
  return ({
    onvif: 'ONVIF',
    'rtsp-manual': 'URL RTSP manual',
    'rtsp-dictionary': 'diccionario RTSP',
  }[method] || method);
}

q('#discoverButton').addEventListener('click', async () => {
  const button = q('#discoverButton');
  const box = q('#discoveryResults');
  button.disabled = true;
  button.textContent = 'Buscando…';
  try {
    const devices = await api('/api/discovery', { method: 'POST', body: '{}' });
    box.classList.remove('hidden');
    box.innerHTML = devices.length ? devices.map((device) => `<div class="device"><div><strong>${esc(device.remote_address)}</strong><br><small>${esc(device.xaddrs?.[0] || 'ONVIF')}</small></div><button class="ghost" data-ip="${esc(device.remote_address)}">Usar IP</button></div>`).join('') : 'No se encontraron cámaras ONVIF. Puedes introducir la IP o una URL RTSP manualmente.';
    box.querySelectorAll('[data-ip]').forEach((element) => element.addEventListener('click', () => {
      q('#cameraForm').elements.host.value = element.dataset.ip;
      box.classList.add('hidden');
    }));
  } catch (error) {
    box.classList.remove('hidden');
    box.textContent = error.message;
  } finally {
    button.disabled = false;
    button.textContent = 'Detectar en red';
  }
});

async function deleteCamera(id, name) {
  if (!confirm(`¿Eliminar ${name}? Las grabaciones existentes no se borrarán.`)) return;
  stopLive(id);
  await api(`/api/cameras/${encodeURIComponent(id)}`, { method: 'DELETE' });
  await refreshAll();
}

q('#refreshButton').addEventListener('click', refreshAll);
q('#logoutButton').addEventListener('click', async () => {
  await api('/api/logout', { method: 'POST', body: '{}' });
  location.href = '/login';
});
window.addEventListener('beforeunload', () => peers.forEach((peer) => peer.close()));
init().catch((error) => { q('#cameraSummary').textContent = error.message; });
