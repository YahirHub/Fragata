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
  q('#ffmpegBadge').classList.toggle('hidden', !session.ffmpeg_available);
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
  for (const id of Array.from(peers.keys())) stopLive(id);
  q('#cameraGrid').innerHTML = '';
  q('#emptyState').classList.toggle('hidden', cameras.length > 0);
  for (const camera of cameras) {
    const card = document.createElement('article');
    card.className = 'camera-card';
    card.id = `camera-${camera.id}`;
    const primaryResolution = camera.width && camera.height ? `${camera.width}×${camera.height}` : 'Resolución pendiente';
    const previewResolution = camera.live_width && camera.live_height ? `${camera.live_width}×${camera.live_height}` : '';
    const previewText = camera.codec === 'H264'
      ? 'Vista directa del stream principal'
      : (camera.live_codec === 'H264' ? `Vista alternativa H.264${previewResolution ? ` · ${previewResolution}` : ''}` : 'Vista H.265 mediante FFmpeg cuando esté disponible');
    card.innerHTML = `<div class="video-wrap"><video id="video-${camera.id}" autoplay muted playsinline></video><div class="video-placeholder" id="placeholder-${camera.id}">Vista detenida</div></div><div class="camera-content"><div class="camera-line"><h3>${esc(camera.name)}</h3><span class="state" data-state>iniciando</span></div><div class="meta"><span>${esc(camera.host)}</span><span data-codec>${esc(camera.codec || '—')}</span><span>${esc(primaryResolution)} · calidad principal</span><span data-live-mode>${esc(previewText)}</span><span data-record>${camera.record ? 'MKV activo' : 'Grabación apagada'}</span></div><div class="record-control"><span><strong>Grabación</strong><small>Conserva el stream principal sin recomprimir</small></span><label class="switch"><input type="checkbox" data-record-toggle aria-label="Activar grabación de ${esc(camera.name)}" ${camera.record ? 'checked' : ''}><span class="switch-slider"></span></label></div><div class="error" data-error></div><div class="card-actions"><button class="secondary" data-live>Vista rápida</button><a class="button-link primary" href="/camera/${encodeURIComponent(camera.id)}">Abrir cámara</a><button class="ghost" data-redetect>Redetectar calidad</button><button class="ghost danger" data-delete>Eliminar</button></div></div>`;
    card.querySelector('[data-live]').addEventListener('click', () => toggleLive(camera.id));
    card.querySelector('[data-record-toggle]').addEventListener('change', (event) => setRecording(camera.id, event.currentTarget));
    card.querySelector('[data-redetect]').addEventListener('click', (event) => redetectCamera(camera.id, event.currentTarget));
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
  card.querySelector('[data-record]').textContent = status.recording_path ? 'Grabando MKV' : (camera.record ? 'Esperando fotograma clave' : 'Grabación apagada');
  const toggle = card.querySelector('[data-record-toggle]');
  if (!toggle.disabled) toggle.checked = camera.record;
  const liveMode = card.querySelector('[data-live-mode]');
  if (status.live_mode) liveMode.textContent = liveModeLabel(status.live_mode, camera);
  const liveButton = card.querySelector('[data-live]');
  liveButton.disabled = status.state !== 'online' && !peers.has(camera.id);
  liveButton.textContent = peers.has(camera.id) ? 'Detener vista' : 'Ver en vivo';
}

function liveModeLabel(mode, camera) {
  if (mode === 'direct') return 'Vista directa del stream principal';
  if (mode === 'ffmpeg') return 'Vista del stream principal transcodificada con FFmpeg';
  if (mode === 'substream') {
    const resolution = camera.live_width && camera.live_height ? ` · ${camera.live_width}×${camera.live_height}` : '';
    return `Vista mediante substream H.264${resolution}`;
  }
  return mode;
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
    const camera = cameras.find((item) => item.id === id);
    if (camera) {
      const card = q(`#camera-${CSS.escape(id)}`);
      const liveMode = card?.querySelector('[data-live-mode]');
      if (liveMode && answer.mode) liveMode.textContent = liveModeLabel(answer.mode, camera);
    }
    if (camera) updateCard(camera);
  } catch (error) {
    stopLive(id);
    alert(error.message);
  }
}

async function redetectCamera(id, button) {
  const camera = cameras.find((item) => item.id === id);
  if (!camera) return;
  button.disabled = true;
  const original = button.textContent;
  button.textContent = 'Detectando…';
  stopLive(id);
  try {
    const result = await api(`/api/cameras/${encodeURIComponent(id)}/redetect`, {
      method: 'POST',
      body: '{}',
    });
    const updated = result.camera;
    alert(`Calidad actualizada: ${updated.codec || 'video'} ${updated.width && updated.height ? `${updated.width}×${updated.height}` : ''}`.trim());
    await refreshAll();
  } catch (error) {
    alert(error.message);
  } finally {
    button.disabled = false;
    button.textContent = original;
  }
}

async function setRecording(id, toggle) {
  const camera = cameras.find((item) => item.id === id);
  if (!camera) return;
  const previous = camera.record;
  toggle.disabled = true;
  try {
    const updated = await api(`/api/cameras/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ record: toggle.checked }),
    });
    Object.assign(camera, updated);
    await refreshAll();
  } catch (error) {
    toggle.checked = previous;
    alert(error.message);
  } finally {
    toggle.disabled = false;
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
    q(`#placeholder-${CSS.escape(id)}`)?.classList.remove('hidden');
  }
  const camera = cameras.find((item) => item.id === id);
  if (camera) updateCard(camera);
}

function cameraFormData() {
  const form = q('#cameraForm');
  const data = Object.fromEntries(new FormData(form));
  data.record = false;
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
    const resolution = probe.width && probe.height ? ` · ${probe.width}×${probe.height}` : '';
    status.textContent = `URL válida: ${probe.codec}${resolution} por el puerto ${probe.port}. Ya puedes guardarla.`;
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

function diagnosticTarget(data) {
  if (data.host) return data.host;
  if (!data.rtsp_url) return '';
  try {
    return new URL(data.rtsp_url).hostname;
  } catch (_) {
    return data.rtsp_url;
  }
}

function portStateLabel(state) {
  return ({
    open: 'abierto',
    timeout: 'sin respuesta',
    refused: 'rechazado',
    no_route: 'sin ruta',
    unreachable: 'inalcanzable',
    canceled: 'cancelado',
    error: 'error',
  }[state] || state);
}

q('#networkDiagnosticButton').addEventListener('click', async () => {
  const button = q('#networkDiagnosticButton');
  const box = q('#networkDiagnosticResults');
  const data = cameraFormData();
  const host = diagnosticTarget(data);
  if (!host) {
    box.classList.remove('hidden');
    box.textContent = 'Introduce la IP o la URL RTSP de la cámara.';
    return;
  }
  button.disabled = true;
  box.classList.remove('hidden');
  box.textContent = 'Comprobando la red desde el mismo proceso de Fragata…';
  try {
    const report = await api('/api/network/diagnose', {
      method: 'POST',
      body: JSON.stringify({ host }),
    });
    const ports = report.port_checks.map((item) => `<span class="port-state ${esc(item.state)}"><strong>${item.port}</strong> ${esc(portStateLabel(item.state))} · ${item.elapsed_ms} ms</span>`).join('');
    const addresses = report.local_addresses.length ? report.local_addresses.map(esc).join(', ') : 'no detectadas';
    box.innerHTML = `<strong>${esc(report.summary)}</strong><p>${esc(report.recommendation)}</p><div class="port-list">${ports}</div><small>Entorno: ${report.in_container ? 'contenedor' : 'host'} · Interfaces: ${addresses} · Misma subred local: ${report.same_local_subnet ? 'sí' : 'no'}</small>`;
  } catch (error) {
    box.textContent = error.message;
  } finally {
    button.disabled = false;
  }
});
