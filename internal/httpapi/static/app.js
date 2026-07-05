(() => {
  const { q, api, initLayout, escapeHTML: esc, statusLabel, statusClass, formatDuration, notify } = window.Fragata;
  let session = null;

  async function refresh() {
    const [cameras, statuses, uploads] = await Promise.all([api('/api/cameras'), api('/api/status'), api('/api/uploads')]);
    const statusMap = new Map(statuses.map((status) => [status.camera_id, status]));
    q('#statsTotal').textContent = String(cameras.length);
    q('#statsOnline').textContent = String(statuses.filter((status) => status.state === 'online').length);
    q('#statsRecording').textContent = String(statuses.filter((status) => Boolean(status.recording_path)).length);
    q('#statsUploads').textContent = String(uploads.length);
    q('#cameraSummary').textContent = `${cameras.length} cámara${cameras.length === 1 ? '' : 's'} registrada${cameras.length === 1 ? '' : 's'}`;
    q('#uploadStatus').textContent = uploads.length ? `${uploads.length} archivo${uploads.length === 1 ? '' : 's'} pendiente${uploads.length === 1 ? '' : 's'} de subir` : 'No hay archivos pendientes';
    renderRows(cameras.slice(0, 8), statusMap);
  }

  function renderRows(cameras, statusMap) {
    const body = q('#dashboardCameraRows');
    body.innerHTML = '';
    q('#dashboardEmpty').classList.toggle('hidden', cameras.length > 0);
    if (!cameras.length) return;
    for (const camera of cameras) {
      const status = statusMap.get(camera.id) || { state: camera.enabled ? 'starting' : 'disabled' };
      const resolution = camera.width && camera.height ? `${camera.width}×${camera.height}` : 'Pendiente';
      const row = document.createElement('tr');
      row.innerHTML = `
        <td><div class="camera-cell"><span class="camera-cell-icon"><i class="bi bi-camera-video"></i></span><div><strong>${esc(camera.name)}</strong><small>${esc(camera.host)}</small></div></div></td>
        <td><span class="badge bg-${statusClass(status.state)}-subtle border border-${statusClass(status.state)}-subtle text-${statusClass(status.state)}-emphasis"><span class="status-dot me-1"></span>${esc(statusLabel(status.state))}</span></td>
        <td><strong class="table-main">${esc(camera.codec || '—')}</strong><small class="table-sub">${esc(resolution)}</small></td>
        <td><strong class="table-main">${status.recording_path ? 'Grabando' : (camera.record ? 'En espera' : 'Desactivada')}</strong><small class="table-sub">${esc(formatDuration(camera.segment_duration_seconds))} por archivo</small></td>
        <td class="text-end"><a class="btn btn-sm btn-outline-primary" href="/camera/${encodeURIComponent(camera.id)}"><i class="bi bi-eye me-1"></i>Ver</a></td>`;
      body.append(row);
    }
  }

  async function init() {
    session = await initLayout('Cámaras, grabaciones y respaldos');
    q('#authStatus').textContent = session.auth_enabled ? `Sesión activa como ${session.username}` : 'Acceso local sin autenticación';
    q('#ffmpegStatus').textContent = session.ffmpeg_available ? 'FFmpeg disponible para vista H.265' : 'FFmpeg no detectado; se usará H.264 cuando exista';
    await refresh();
    setInterval(refresh, 5000);
  }

  q('#refreshButton')?.addEventListener('click', async (event) => {
    const button = event.currentTarget;
    button.disabled = true;
    try { await refresh(); notify('Información actualizada.', 'success'); } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  });

  init().catch((error) => notify(error.message, 'danger'));
})();
