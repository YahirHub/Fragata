(() => {
  const { q, qa, api, initLayout, slugFolder, escapeHTML: esc, notify } = window.Fragata;
  let folderEdited = false;

  function formData() {
    const form = q('#cameraForm');
    const data = Object.fromEntries(new FormData(form));
    data.name = String(data.name || '').trim();
    data.folder_name = String(data.folder_name || '').trim();
    data.host = String(data.host || '').trim();
    data.username = String(data.username || '').trim();
    data.password = String(data.password || '');
    data.rtsp_url = String(data.rtsp_url || '').trim();
    data.enabled = true;
    data.record = false;
    data.segment_duration_seconds = q('#newCameraSegmentDuration').valueSeconds;
    data.upload = form.elements.upload.checked;
    data.sftp_profile_id = q('#sftpProfile').value;
    return data;
  }

  async function loadSFTPProfiles() {
    const profiles = await api('/api/sftp-profiles');
    const select = q('#sftpProfile');
    select.innerHTML = '<option value="">Sin servidor seleccionado</option>' + profiles.filter((profile) => profile.enabled).map((profile) => `<option value="${esc(profile.id)}">${esc(profile.name)} · ${esc(profile.host)}:${profile.port}</option>`).join('');
    q('#uploadSwitch').disabled = profiles.filter((profile) => profile.enabled).length === 0;
    if (q('#uploadSwitch').disabled) q('#uploadSwitch').checked = false;
  }

  function setStatus(message, type = 'light') {
    const status = q('#formStatus');
    status.className = `alert alert-${type} border small mb-3`;
    status.textContent = message;
  }

  function diagnosticTarget(data) {
    if (data.host) return data.host;
    try { return new URL(data.rtsp_url).hostname; } catch (_) { return data.rtsp_url; }
  }

  q('#cameraName').addEventListener('input', (event) => {
    if (!folderEdited) q('#cameraFolder').value = slugFolder(event.currentTarget.value);
  });
  q('#cameraFolder').addEventListener('input', (event) => {
    folderEdited = true;
    const cursor = event.currentTarget.selectionStart;
    event.currentTarget.value = slugFolder(event.currentTarget.value);
    event.currentTarget.setSelectionRange(cursor, cursor);
  });

  qa('[data-password-toggle]').forEach((button) => button.addEventListener('click', () => {
    const input = q(button.dataset.passwordToggle);
    const visible = input.type === 'text';
    input.type = visible ? 'password' : 'text';
    button.innerHTML = `<i class="bi ${visible ? 'bi-eye' : 'bi-eye-slash'}"></i>`;
  }));

  q('#probeRTSPButton').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    const data = formData();
    if (!data.rtsp_url) return setStatus('Introduce una URL RTSP para probarla.', 'warning');
    button.disabled = true;
    setStatus('Comprobando conexión RTSP y recepción de video…', 'info');
    try {
      const probe = await api('/api/rtsp/probe', { method: 'POST', body: JSON.stringify({ host: data.host, username: data.username, password: data.password, rtsp_url: data.rtsp_url }) });
      if (!q('#cameraHost').value.trim()) q('#cameraHost').value = probe.host;
      setStatus(`URL válida: ${probe.codec}${probe.width && probe.height ? ` · ${probe.width}×${probe.height}` : ''} por el puerto ${probe.port}.`, 'success');
    } catch (error) { setStatus(error.message, 'danger'); }
    finally { button.disabled = false; }
  });

  q('#networkDiagnosticButton').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    const box = q('#networkDiagnosticResults');
    const host = diagnosticTarget(formData());
    if (!host) { box.classList.remove('hidden'); box.textContent = 'Introduce la IP, dominio o URL RTSP de la cámara.'; return; }
    button.disabled = true;
    box.classList.remove('hidden');
    box.textContent = 'Comprobando la red desde Fragata…';
    try {
      const report = await api('/api/network/diagnose', { method: 'POST', body: JSON.stringify({ host }) });
      const ports = report.port_checks.map((item) => `<span class="port-state ${esc(item.state)}"><strong>${item.port}</strong> ${esc(item.state)} · ${item.elapsed_ms} ms</span>`).join('');
      box.innerHTML = `<strong>${esc(report.summary)}</strong><p>${esc(report.recommendation)}</p><div class="port-list">${ports}</div>`;
    } catch (error) { box.textContent = error.message; }
    finally { button.disabled = false; }
  });

  q('#discoverButton').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    const box = q('#discoveryResults');
    button.disabled = true;
    button.innerHTML = '<span class="spinner-border spinner-border-sm me-2"></span>Buscando…';
    try {
      const devices = await api('/api/discovery', { method: 'POST', body: '{}' });
      box.classList.remove('hidden');
      box.innerHTML = devices.length ? devices.map((device) => `<div class="device"><div><strong>${esc(device.remote_address)}</strong><br><small>${esc(device.xaddrs?.[0] || 'Dispositivo ONVIF')}</small></div><button class="btn btn-sm btn-outline-primary" type="button" data-ip="${esc(device.remote_address)}"><i class="bi bi-arrow-right-circle me-1"></i>Usar IP</button></div>`).join('') : 'No se encontraron cámaras ONVIF. Puedes introducir la IP o URL RTSP manualmente.';
      qa('[data-ip]', box).forEach((item) => item.addEventListener('click', () => { q('#cameraHost').value = item.dataset.ip; box.classList.add('hidden'); }));
    } catch (error) { box.classList.remove('hidden'); box.textContent = error.message; }
    finally { button.disabled = false; button.innerHTML = '<i class="bi bi-radar me-2"></i>Detectar en red'; }
  });

  q('#uploadSwitch').addEventListener('change', (event) => {
    if (event.currentTarget.checked && !q('#sftpProfile').value) {
      event.currentTarget.checked = false;
      notify('Selecciona primero un servidor SFTP global.', 'warning');
    }
  });

  q('#cameraForm').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    if (!form.reportValidity()) return;
    const data = formData();
    if (!data.host && !data.rtsp_url) return setStatus('Introduce la IP, dominio o una URL RTSP manual.', 'warning');
    const submit = form.querySelector('button[type="submit"]');
    submit.disabled = true;
    q('#probeRTSPButton').disabled = true;
    setStatus(data.rtsp_url ? 'Validando la URL RTSP y guardando…' : 'Detectando ONVIF, puertos y perfiles de video…', 'info');
    try {
      await api('/api/cameras', { method: 'POST', body: JSON.stringify(data) });
      location.href = '/cameras?created=1';
    } catch (error) { setStatus(error.message, 'danger'); notify(error.message, 'danger'); }
    finally { submit.disabled = false; q('#probeRTSPButton').disabled = false; }
  });

  async function init() {
    const session = await initLayout('Registro y detección inteligente de dispositivos');
    q('#newCameraSegmentDuration').valueSeconds = session.default_segment_duration_seconds || 300;
    await loadSFTPProfiles();
  }
  init().catch((error) => setStatus(error.message, 'danger'));
})();
