(() => {
  const { q, api, initLayout, notify, escapeHTML: esc } = window.Fragata;
  let cameras = [];
  let events = [];

  function formatDate(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'Fecha desconocida';
    return new Intl.DateTimeFormat('es-MX', { dateStyle: 'medium', timeStyle: 'medium' }).format(date);
  }

  function render() {
    const type = q('#typeFilter').value;
    const filtered = type ? events.filter((event) => event.type === type) : events;
    q('#eventsCount strong').textContent = String(filtered.length);
    q('#eventsLoading').classList.add('hidden');
    q('#eventsEmpty').classList.toggle('hidden', filtered.length !== 0);
    q('#eventsGrid').innerHTML = filtered.map((event) => {
      const isPerson = event.type === 'person';
      const label = isPerson ? 'Persona' : 'Movimiento';
      const icon = isPerson ? 'bi-person-bounding-box' : 'bi-broadcast-pin';
      const badge = isPerson ? 'text-bg-primary' : 'text-bg-warning';
      const confidence = isPerson && event.confidence ? `<span><i class="bi bi-bullseye me-1"></i>${Math.round(event.confidence * 100)}% confianza</span>` : '';
      const dimensions = event.snapshot_width && event.snapshot_height
        ? ` width="${Number(event.snapshot_width)}" height="${Number(event.snapshot_height)}"`
        : '';
      let recordingBadge = '<span class="badge text-bg-secondary-subtle text-secondary-emphasis"><i class="bi bi-camera-reels me-1"></i>Sin video</span>';
      if (event.recording_pending) recordingBadge = '<span class="badge text-bg-info-subtle text-info-emphasis"><i class="bi bi-hourglass-split me-1"></i>Finalizando</span>';
      else if (event.recording_available) recordingBadge = '<span class="badge text-bg-success-subtle text-success-emphasis"><i class="bi bi-play-circle me-1"></i>Video disponible</span>';
      return `
        <div class="col-12 col-md-6 col-xxl-4">
          <article class="card dashboard-card event-card h-100">
            <div class="event-preview">
              ${event.snapshot_url ? `<img src="${esc(event.snapshot_url)}" alt="Captura de ${esc(event.camera_name)}" loading="lazy" decoding="async"${dimensions}>` : '<div class="event-preview-empty"><i class="bi bi-image"></i></div>'}
              <span class="badge ${badge} event-type-badge"><i class="bi ${icon} me-1"></i>${label}</span>
            </div>
            <div class="card-body">
              <div class="d-flex align-items-start justify-content-between gap-3 mb-2"><div><h3 class="h6 mb-1">${esc(event.camera_name || 'Cámara')}</h3><small class="text-body-secondary">${formatDate(event.created_at)}</small></div>${recordingBadge}</div>
              <div class="event-metrics"><span><i class="bi bi-activity me-1"></i>${Math.round((event.motion_score || 0) * 1000) / 10}% movimiento</span>${confidence}</div>
              <div class="d-grid mt-3"><a class="btn btn-outline-primary btn-sm" href="${esc(event.detail_url)}"><i class="bi bi-play-btn me-2"></i>Ver evento</a></div>
            </div>
          </article>
        </div>`;
    }).join('');
  }

  async function loadEvents() {
    q('#eventsLoading').classList.remove('hidden');
    q('#eventsEmpty').classList.add('hidden');
    q('#eventsGrid').innerHTML = '';
    const cameraID = q('#cameraFilter').value;
    const limit = q('#limitFilter').value;
    const params = new URLSearchParams({ limit });
    if (cameraID) params.set('camera_id', cameraID);
    events = await api(`/api/events?${params.toString()}`);
    render();
  }

  async function init() {
    await initLayout('Detección ligera en Go puro');
    cameras = await api('/api/cameras');
    q('#cameraFilter').innerHTML = '<option value="">Todas las cámaras</option>' + cameras.map((camera) => `<option value="${esc(camera.id)}">${esc(camera.name)}</option>`).join('');
    const requestedCamera = new URLSearchParams(location.search).get('camera_id');
    if (requestedCamera && cameras.some((camera) => camera.id === requestedCamera)) q('#cameraFilter').value = requestedCamera;
    q('#cameraFilter').addEventListener('change', loadEvents);
    q('#limitFilter').addEventListener('change', loadEvents);
    q('#typeFilter').addEventListener('change', render);
    q('#refreshEvents').addEventListener('click', async (event) => {
      event.currentTarget.disabled = true;
      try { await loadEvents(); } catch (error) { notify(error.message, 'danger'); }
      finally { event.currentTarget.disabled = false; }
    });
    await loadEvents();
  }

  init().catch((error) => { q('#eventsLoading').classList.add('hidden'); notify(error.message, 'danger'); });
})();
