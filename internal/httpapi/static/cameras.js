(() => {
  const { q, api, initLayout, escapeHTML: esc, statusLabel, statusClass, formatDuration, notify } = window.Fragata;
  let cameras = [];
  let statuses = [];

  function statusFor(camera) {
    if (!camera.enabled) return { camera_id: camera.id, state: 'disabled' };
    return statuses.find((item) => item.camera_id === camera.id) || { camera_id: camera.id, state: 'offline' };
  }

  function filteredCameras() {
    const query = q('#cameraSearch').value.trim().toLocaleLowerCase('es-MX');
    const filter = q('#cameraStatusFilter').value;
    return cameras.filter((camera) => {
      const status = statusFor(camera);
      const haystack = [camera.name, camera.host, camera.folder_name, camera.manufacturer, camera.model].join(' ').toLocaleLowerCase('es-MX');
      if (query && !haystack.includes(query)) return false;
      if (filter === 'online' && status.state !== 'online') return false;
      if (filter === 'recording' && !status.recording_path) return false;
      if (filter === 'offline' && ['online', 'disabled'].includes(status.state)) return false;
      if (filter === 'disabled' && camera.enabled) return false;
      return true;
    });
  }

  function render() {
    const visible = filteredCameras();
    const body = q('#cameraRows');
    body.innerHTML = '';
    q('#cameraCount').textContent = `${visible.length} de ${cameras.length} cámara${cameras.length === 1 ? '' : 's'}`;
    q('#cameraEmpty').classList.toggle('hidden', visible.length > 0);
    q('.camera-table-wrap').classList.toggle('hidden', visible.length === 0);
    q('#cameraEmptyMessage').textContent = cameras.length ? 'No hay resultados para los filtros seleccionados.' : 'Agrega la primera cámara para comenzar.';
    q('#cameraEmptyAction').classList.toggle('hidden', cameras.length > 0);

    for (const camera of visible) {
      const status = statusFor(camera);
      const resolution = camera.width && camera.height ? `${camera.width}×${camera.height}` : 'Pendiente';
      const row = document.createElement('tr');
      row.dataset.cameraId = camera.id;
      row.innerHTML = `
        <td><div class="camera-cell"><span class="camera-cell-icon"><i class="bi bi-camera-video"></i></span><div><strong>${esc(camera.name)}</strong><small>${esc([camera.manufacturer, camera.model].filter(Boolean).join(' ') || 'Cámara IP')}</small></div></div></td>
        <td><strong class="table-main">${esc(camera.host)}</strong><small class="table-sub"><i class="bi bi-person me-1"></i>${esc(camera.username || 'Sin usuario')}</small></td>
        <td><strong class="table-main">${esc(camera.codec || '—')} · ${esc(resolution)}</strong><small class="table-sub">${camera.live_codec ? `Vista ${esc(camera.live_codec)}` : 'Vista según stream principal'}</small></td>
        <td><code class="folder-code">${esc(camera.folder_name || camera.id)}</code><small class="table-sub">Grabaciones futuras</small></td>
        <td><strong class="table-main">${status.recording_path ? 'Grabando ahora' : (camera.record ? 'Activada' : 'Desactivada')}</strong><small class="table-sub">${esc(formatDuration(camera.segment_duration_seconds))} por archivo</small></td>
        <td><span class="badge bg-${statusClass(status.state)}-subtle border border-${statusClass(status.state)}-subtle text-${statusClass(status.state)}-emphasis"><span class="status-dot me-1"></span>${esc(statusLabel(status.state))}</span>${status.last_error ? `<small class="table-error" title="${esc(status.last_error)}">${esc(status.last_error)}</small>` : ''}</td>
        <td class="text-end">
          <div class="dropdown">
            <button class="btn btn-sm btn-light action-menu-button" type="button" data-bs-toggle="dropdown" data-bs-boundary="viewport" aria-expanded="false" aria-label="Acciones de ${esc(camera.name)}"><i class="bi bi-three-dots-vertical"></i></button>
            <ul class="dropdown-menu dropdown-menu-end shadow-sm">
              <li><a class="dropdown-item" href="/camera/${encodeURIComponent(camera.id)}"><i class="bi bi-eye me-2"></i>Ver cámara</a></li>
              <li><a class="dropdown-item" href="/cameras/${encodeURIComponent(camera.id)}/settings"><i class="bi bi-sliders me-2"></i>Ajustes</a></li>
              <li><a class="dropdown-item" href="/events?camera_id=${encodeURIComponent(camera.id)}"><i class="bi bi-activity me-2"></i>Ver eventos</a></li>
              <li><button class="dropdown-item" type="button" data-action="redetect"><i class="bi bi-arrow-repeat me-2"></i>Redetectar calidad</button></li>
              <li><button class="dropdown-item" type="button" data-action="record"><i class="bi ${camera.record ? 'bi-stop-circle' : 'bi-record-circle'} me-2"></i>${camera.record ? 'Detener grabación' : 'Iniciar grabación'}</button></li>
              <li><hr class="dropdown-divider"></li>
              <li><button class="dropdown-item text-danger" type="button" data-action="delete"><i class="bi bi-trash3 me-2"></i>Eliminar cámara</button></li>
            </ul>
          </div>
        </td>`;
      body.append(row);
    }
  }

  async function refresh() {
    [cameras, statuses] = await Promise.all([api('/api/cameras'), api('/api/status')]);
    render();
  }

  async function redetect(camera, button) {
    button.disabled = true;
    notify(`Redetectando la mejor calidad de ${camera.name}…`);
    try {
      const result = await api(`/api/cameras/${encodeURIComponent(camera.id)}/redetect`, { method: 'POST', body: '{}' });
      notify(`Calidad actualizada: ${result.camera.codec || 'video'} ${result.camera.width && result.camera.height ? `${result.camera.width}×${result.camera.height}` : ''}`.trim(), 'success');
      await refresh();
    } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  }

  async function toggleRecording(camera, button) {
    button.disabled = true;
    try {
      await api(`/api/cameras/${encodeURIComponent(camera.id)}`, { method: 'PATCH', body: JSON.stringify({ record: !camera.record }) });
      notify(camera.record ? 'Grabación detenida correctamente.' : 'Grabación activada correctamente.', 'success');
      await refresh();
    } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  }

  async function deleteCamera(camera, button) {
    if (!confirm(`¿Eliminar ${camera.name}? Las grabaciones existentes no se borrarán.`)) return;
    button.disabled = true;
    try {
      await api(`/api/cameras/${encodeURIComponent(camera.id)}`, { method: 'DELETE' });
      notify('Cámara eliminada.', 'success');
      await refresh();
    } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  }

  q('#cameraRows').addEventListener('click', (event) => {
    const button = event.target.closest('[data-action]');
    if (!button) return;
    const row = button.closest('[data-camera-id]');
    const camera = cameras.find((item) => item.id === row?.dataset.cameraId);
    if (!camera) return;
    if (button.dataset.action === 'redetect') redetect(camera, button);
    if (button.dataset.action === 'record') toggleRecording(camera, button);
    if (button.dataset.action === 'delete') deleteCamera(camera, button);
  });
  q('#cameraSearch').addEventListener('input', render);
  q('#cameraStatusFilter').addEventListener('change', render);
  q('#refreshButton').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    button.disabled = true;
    try { await refresh(); notify('Listado actualizado.', 'success'); } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  });

  async function init() {
    await initLayout('Administración y configuración de dispositivos');
    const params = new URLSearchParams(location.search);
    if (params.has('created')) notify('Cámara agregada correctamente.', 'success');
    if (params.has('updated')) notify('Ajustes guardados correctamente.', 'success');
    await refresh();
    setInterval(async () => { statuses = await api('/api/status'); render(); }, 4000);
  }

  init().catch((error) => notify(error.message, 'danger'));
})();
