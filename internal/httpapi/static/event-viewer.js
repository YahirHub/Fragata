(() => {
  const { q, api, initLayout, notify } = window.Fragata;
  const eventID = decodeURIComponent(location.pathname.split('/').filter(Boolean).at(-1) || '');
  let eventData = null;
  let pendingTimer = null;

  function formatDate(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'Fecha desconocida';
    return new Intl.DateTimeFormat('es-MX', { dateStyle: 'long', timeStyle: 'medium' }).format(date);
  }

  function formatOffset(seconds) {
    const total = Math.max(0, Math.round(Number(seconds) || 0));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    const remaining = total % 60;
    return [hours, minutes, remaining].map((value) => String(value).padStart(2, '0')).join(':');
  }

  function renderMetadata(event) {
    const isPerson = event.type === 'person';
    const typeLabel = isPerson ? 'Persona' : 'Movimiento';
    const badge = q('#eventType');
    badge.className = `badge ${isPerson ? 'text-bg-primary' : 'text-bg-warning'} mb-2`;
    badge.innerHTML = `<i class="bi ${isPerson ? 'bi-person-bounding-box' : 'bi-broadcast-pin'} me-1"></i>${typeLabel}`;
    q('#eventTitle').textContent = `${typeLabel} en ${event.camera_name || 'cámara'}`;
    q('#eventSubtitle').textContent = formatDate(event.created_at);
    q('#detailCamera').textContent = event.camera_name || 'Cámara';
    q('#detailDate').textContent = formatDate(event.created_at);
    q('#detailType').textContent = typeLabel;
    q('#detailMotion').textContent = `${Math.round((event.motion_score || 0) * 1000) / 10}%`;
    q('#detailConfidence').textContent = isPerson && event.confidence ? `${Math.round(event.confidence * 100)}%` : 'No aplica';
    q('#detailResolution').textContent = event.snapshot_width && event.snapshot_height ? `${event.snapshot_width} × ${event.snapshot_height}` : 'Resolución original de la cámara';
    q('#openCamera').href = `/camera/${encodeURIComponent(event.camera_id)}`;
    document.title = `${typeLabel} · ${event.camera_name || 'Fragata'}`;

    const snapshot = q('#eventSnapshot');
    if (event.snapshot_url) {
      snapshot.src = event.snapshot_url;
      snapshot.width = Number(event.snapshot_width) || 0;
      snapshot.height = Number(event.snapshot_height) || 0;
      snapshot.alt = `Captura de ${event.camera_name || 'la cámara'}`;
      snapshot.classList.remove('hidden');
      q('#eventSnapshotEmpty').classList.add('hidden');
      q('#openSnapshotOriginal').href = event.snapshot_url;
      q('#openSnapshotOriginal').classList.remove('hidden');
    } else {
      snapshot.classList.add('hidden');
      q('#eventSnapshotEmpty').classList.remove('hidden');
      q('#openSnapshotOriginal').classList.add('hidden');
    }
  }

  function hideVideo() {
    const video = q('#eventVideo');
    video.pause();
    video.removeAttribute('src');
    video.load();
    q('#eventVideoStage').classList.add('hidden');
    q('#recordingActions').classList.add('hidden');
  }

  function showUnavailable(title, message) {
    hideVideo();
    q('#recordingUnavailableTitle').textContent = title;
    q('#recordingUnavailableMessage').textContent = message;
    q('#recordingUnavailable').classList.remove('hidden');
  }

  function renderRecording(event) {
    clearTimeout(pendingTimer);
    q('#recordingUnavailable').classList.add('hidden');
    const stage = q('#eventVideoStage');
    const video = q('#eventVideo');
    const loading = q('#eventVideoLoading');
    const message = q('#eventVideoMessage');

    if (event.recording_pending) {
      stage.classList.remove('hidden');
      stage.classList.add('is-loading');
      video.classList.add('hidden');
      loading.classList.remove('hidden');
      message.textContent = 'Finalizando la grabación…';
      q('#recordingDescription').textContent = 'El evento ya está vinculado. El video aparecerá automáticamente al cerrar el segmento actual.';
      q('#recordingActions').classList.add('hidden');
      pendingTimer = setTimeout(loadEvent, 5000);
      return;
    }

    if (!event.recording_available) {
      q('#recordingDescription').textContent = 'No se encontró un segmento de grabación para este instante.';
      showUnavailable('Sin grabación relacionada', 'La grabación estaba desactivada o el stream no estaba disponible cuando ocurrió este evento.');
      return;
    }

    q('#recordingActions').classList.remove('hidden');
    q('#downloadRecording').href = event.recording_url;
    q('#eventPosition').innerHTML = `<i class="bi bi-clock-history me-1"></i>Evento registrado en ${formatOffset(event.playback_offset_seconds)} del archivo original.`;

    if (!event.playback_supported || !event.playback_url) {
      q('#recordingDescription').textContent = 'La grabación original está disponible para descargar.';
      showUnavailable('Reproducción web no disponible', 'Puedes descargar el MKV original en máxima calidad desde el botón inferior.');
      q('#recordingActions').classList.remove('hidden');
      return;
    }

    stage.classList.remove('hidden');
    stage.classList.add('is-loading');
    video.classList.add('hidden');
    loading.classList.remove('hidden');
    message.textContent = 'Preparando video…';
    const context = Number(event.playback_context_seconds) || 0;
    q('#recordingDescription').textContent = context > 0
      ? `La reproducción comienza ${Math.round(context)} segundos antes del evento y conserva la resolución original.`
      : 'La reproducción comienza en el instante exacto del evento y conserva la resolución original.';

    if (video.dataset.source !== event.playback_url) {
      video.dataset.source = event.playback_url;
      video.src = `${event.playback_url}?v=${Date.now()}`;
      video.load();
    }
  }

  async function loadEvent() {
    try {
      eventData = await api(`/api/events/${encodeURIComponent(eventID)}`);
      renderMetadata(eventData);
      renderRecording(eventData);
    } catch (error) {
      clearTimeout(pendingTimer);
      showUnavailable('Evento no disponible', 'No fue posible cargar la información del evento.');
      notify(error.message, 'danger');
    }
  }

  async function init() {
    await initLayout('Grabación y captura asociadas');
    const video = q('#eventVideo');
    video.addEventListener('loadeddata', () => {
      q('#eventVideoStage').classList.remove('is-loading');
      video.classList.remove('hidden');
      q('#eventVideoLoading').classList.add('hidden');
      video.play().catch(() => {});
    });
    video.addEventListener('error', () => {
      q('#eventVideoStage').classList.add('is-loading');
      video.classList.add('hidden');
      q('#eventVideoLoading').classList.remove('hidden');
      q('#eventVideoMessage').textContent = 'La grabación original está disponible para descargar';
    });
    q('#eventFullscreen').addEventListener('click', () => {
      if (q('#eventVideoStage').requestFullscreen) q('#eventVideoStage').requestFullscreen();
    });
    await loadEvent();
  }

  window.addEventListener('beforeunload', () => clearTimeout(pendingTimer));
  init().catch((error) => notify(error.message, 'danger'));
})();
