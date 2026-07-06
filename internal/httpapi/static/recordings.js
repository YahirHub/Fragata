(() => {
  const { q, api, initLayout, notify, escapeHTML: esc } = window.Fragata;
  const TIMELINE_HEIGHT = 1536;
  let sources = [];
  let recordings = [];
  let events = [];
  let activeRecording = null;
  let playbackOffset = 0;
  let requestVersion = 0;

  const player = q('#recordingPlayer');

  function localDateValue(date = new Date()) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
  }

  function parseInputDate(value) {
    const [year, month, day] = String(value).split('-').map(Number);
    return new Date(year, month - 1, day, 12, 0, 0, 0);
  }

  function moveDay(delta) {
    const date = parseInputDate(q('#recordingDate').value || localDateValue());
    date.setDate(date.getDate() + delta);
    q('#recordingDate').value = localDateValue(date);
    loadRecordings();
  }

  function formatDate(value, options = { dateStyle: 'medium', timeStyle: 'medium' }) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'Fecha desconocida';
    return new Intl.DateTimeFormat('es-MX', options).format(date);
  }

  function formatTime(value, includeSeconds = true) {
    return formatDate(value, { hour: '2-digit', minute: '2-digit', second: includeSeconds ? '2-digit' : undefined });
  }

  function formatBytes(value) {
    let bytes = Number(value || 0);
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let unit = 0;
    while (bytes >= 1024 && unit < units.length - 1) {
      bytes /= 1024;
      unit += 1;
    }
    return `${bytes >= 10 || unit === 0 ? bytes.toFixed(0) : bytes.toFixed(1)} ${units[unit]}`;
  }

  function formatDuration(seconds, compact = false) {
    const total = Math.max(0, Math.round(Number(seconds || 0)));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    const secs = total % 60;
    if (compact) {
      if (hours) return `${hours} h ${minutes} min`;
      if (minutes) return `${minutes} min ${secs ? `${secs} s` : ''}`.trim();
      return `${secs} s`;
    }
    return [hours, minutes, secs].map((part) => String(part).padStart(2, '0')).join(':');
  }

  function secondsOfDay(value) {
    const date = new Date(value);
    return date.getHours() * 3600 + date.getMinutes() * 60 + date.getSeconds() + date.getMilliseconds() / 1000;
  }

  function sourceName(id) {
    return sources.find((source) => source.id === id)?.name || 'Cámara';
  }

  function updateURL() {
    const params = new URLSearchParams();
    const camera = q('#recordingCamera').value;
    const date = q('#recordingDate').value;
    if (camera) params.set('camera_id', camera);
    if (date) params.set('date', date);
    history.replaceState(null, '', `/recordings${params.size ? `?${params}` : ''}`);
  }

  function renderStats(response) {
    q('#recordingCount').textContent = String(response.recordings.length);
    q('#recordingDuration').textContent = formatDuration(response.total_duration_seconds, true);
    q('#recordingSize').textContent = formatBytes(response.total_size);
    q('#recordingEventCount').textContent = String(response.events.length);
  }

  function renderDays(days) {
    const selected = q('#recordingDate').value;
    const container = q('#recordingDays');
    if (!days.length) {
      container.innerHTML = '<span class="small text-body-secondary"><i class="bi bi-calendar-x me-1"></i>No se encontraron días grabados para este filtro.</span>';
      return;
    }
    container.innerHTML = days.slice(0, 14).map((day) => {
      const date = parseInputDate(day.date);
      const label = new Intl.DateTimeFormat('es-MX', { day: '2-digit', month: 'short' }).format(date);
      return `<button class="recording-day-chip ${day.date === selected ? 'active' : ''}" type="button" data-date="${esc(day.date)}" title="${day.count} videos · ${formatBytes(day.size)}"><strong>${esc(label)}</strong><span>${day.count}</span></button>`;
    }).join('');
    container.querySelectorAll('[data-date]').forEach((button) => button.addEventListener('click', () => {
      q('#recordingDate').value = button.dataset.date;
      loadRecordings();
    }));
  }

  async function loadDays() {
    const params = new URLSearchParams({ limit: '90' });
    const camera = q('#recordingCamera').value;
    if (camera) params.set('camera_id', camera);
    const days = await api(`/api/recordings/days?${params}`);
    renderDays(days);
  }

  function hourRows() {
    return Array.from({ length: 25 }, (_, hour) => {
      const top = (hour / 24) * TIMELINE_HEIGHT;
      return `<div class="timeline-hour" style="top:${top}px"><span>${String(hour).padStart(2, '0')}:00</span><i></i></div>`;
    }).join('');
  }

  function renderTimeline() {
    const timeline = q('#recordingTimeline');
    timeline.style.height = `${TIMELINE_HEIGHT}px`;
    const sorted = [...recordings].sort((a, b) => new Date(a.started_at) - new Date(b.started_at));
    const recordingBars = sorted.map((recording) => {
      const start = secondsOfDay(recording.started_at);
      const duration = Math.max(1, Math.min(Number(recording.duration_seconds || 0), 86400 - start));
      const top = (start / 86400) * TIMELINE_HEIGHT;
      const height = Math.max(9, (duration / 86400) * TIMELINE_HEIGHT);
      const classes = ['timeline-recording'];
      if (recording.pending) classes.push('pending');
      if (recording.recovered) classes.push('recovered');
      if (activeRecording?.id === recording.id) classes.push('active');
      const title = `${recording.camera_name} · ${formatTime(recording.started_at)}–${formatTime(recording.ended_at)} · ${formatDuration(recording.duration_seconds)}`;
      return `<button class="${classes.join(' ')}" style="top:${top}px;height:${height}px" type="button" data-recording-id="${esc(recording.id)}" title="${esc(title)}" aria-label="Reproducir ${esc(title)}"><span>${esc(formatTime(recording.started_at, false))}</span></button>`;
    }).join('');
    const markers = events.map((event) => {
      const top = (secondsOfDay(event.created_at) / 86400) * TIMELINE_HEIGHT;
      const person = event.type === 'person';
      return `<button class="timeline-event ${person ? 'person' : 'motion'}" style="top:${top}px" type="button" data-event-id="${esc(event.id)}" data-recording-id="${esc(event.recording_id || '')}" data-offset="${Number(event.offset_seconds || 0)}" title="${person ? 'Persona' : 'Movimiento'} · ${esc(formatTime(event.created_at))}" aria-label="${person ? 'Persona' : 'Movimiento'} a las ${esc(formatTime(event.created_at))}"><i class="bi ${person ? 'bi-person-fill' : 'bi-activity'}"></i></button>`;
    }).join('');
    timeline.innerHTML = hourRows() + recordingBars + markers;
    timeline.querySelectorAll('[data-recording-id].timeline-recording').forEach((button) => button.addEventListener('click', () => {
      const recording = recordings.find((item) => item.id === button.dataset.recordingId);
      if (recording) playRecording(recording, 0);
    }));
    timeline.querySelectorAll('.timeline-event').forEach((button) => button.addEventListener('click', () => {
      const recording = recordings.find((item) => item.id === button.dataset.recordingId);
      if (recording) playRecording(recording, Math.max(0, Number(button.dataset.offset || 0) - 5));
      else location.href = `/events/${encodeURIComponent(button.dataset.eventId)}`;
    }));
  }

  function renderList() {
    q('#recordingList').innerHTML = recordings.map((recording) => {
      const status = recording.pending
        ? '<span class="badge text-bg-info-subtle text-info-emphasis"><i class="bi bi-record-circle me-1"></i>Grabando</span>'
        : recording.recovered
          ? '<span class="badge text-bg-warning-subtle text-warning-emphasis"><i class="bi bi-tools me-1"></i>Recuperado</span>'
          : '<span class="badge text-bg-success-subtle text-success-emphasis"><i class="bi bi-check-circle me-1"></i>Finalizado</span>';
      const active = activeRecording?.id === recording.id ? 'active' : '';
      return `<article class="recording-list-item ${active}" data-list-recording="${esc(recording.id)}">
        <button class="recording-list-main" type="button" data-play-recording="${esc(recording.id)}" ${recording.pending ? 'disabled' : ''}>
          <span class="recording-list-icon"><i class="bi ${recording.pending ? 'bi-record-circle' : 'bi-play-fill'}"></i></span>
          <span class="recording-list-copy"><strong>${esc(formatTime(recording.started_at))}</strong><small>${esc(recording.camera_name)} · ${formatDuration(recording.duration_seconds)} · ${formatBytes(recording.size)}</small></span>
        </button>
        <div class="recording-list-actions">${status}${recording.event_count ? `<span class="badge text-bg-primary-subtle text-primary-emphasis"><i class="bi bi-activity me-1"></i>${recording.event_count}</span>` : ''}${recording.download_url ? `<a class="btn btn-sm btn-light" href="${esc(recording.download_url)}" title="Descargar MKV"><i class="bi bi-download"></i></a>` : ''}</div>
      </article>`;
    }).join('');
    q('#recordingList').querySelectorAll('[data-play-recording]').forEach((button) => button.addEventListener('click', () => {
      const recording = recordings.find((item) => item.id === button.dataset.playRecording);
      if (recording) playRecording(recording, 0);
    }));
  }

  function setActiveRecording(recording) {
    activeRecording = recording;
    document.querySelectorAll('.timeline-recording.active, .recording-list-item.active').forEach((element) => element.classList.remove('active'));
    document.querySelector(`.timeline-recording[data-recording-id="${CSS.escape(recording.id)}"]`)?.classList.add('active');
    document.querySelector(`[data-list-recording="${CSS.escape(recording.id)}"]`)?.classList.add('active');
  }

  function playRecording(recording, offset = 0) {
    if (recording.pending) {
      notify('Este segmento todavía se está grabando.', 'info');
      return;
    }
    if (!recording.playback_supported || !recording.playback_url) {
      notify('La reproducción web requiere FFmpeg. El MKV original sí puede descargarse.', 'warning');
      return;
    }
    playbackOffset = Math.max(0, Math.min(Number(offset || 0), Math.max(0, Number(recording.duration_seconds || 0) - 0.1)));
    setActiveRecording(recording);
    q('#recordingPlayerCard').classList.remove('hidden');
    q('#recordingPlayerTitle').textContent = `${recording.camera_name} · ${formatDate(recording.started_at)}`;
    q('#recordingDownload').href = recording.download_url || '#';
    q('#recordingDownload').classList.toggle('hidden', !recording.download_url);
    q('#recordingPlayerLoading').classList.remove('hidden');
    const actualStart = new Date(new Date(recording.started_at).getTime() + playbackOffset * 1000);
    q('#recordingPlayerMeta').textContent = `Desde ${formatTime(actualStart)} · Segmento ${formatDuration(recording.duration_seconds)} · ${formatBytes(recording.size)}`;
    player.src = `${recording.playback_url}?start=${encodeURIComponent(playbackOffset.toFixed(3))}&_=${Date.now()}`;
    player.load();
    player.play().catch(() => {});
    renderTimeline();
    renderList();
    q('#recordingPlayerCard').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  function jumpPlayback(delta) {
    if (!activeRecording) return;
    const target = playbackOffset + (Number.isFinite(player.currentTime) ? player.currentTime : 0) + delta;
    playRecording(activeRecording, target);
  }

  function scrollTimelineToRecordings() {
    if (!recordings.length) return;
    const earliest = recordings.reduce((best, item) => secondsOfDay(item.started_at) < best ? secondsOfDay(item.started_at) : best, 86400);
    q('#timelineScroll').scrollTop = Math.max(0, (earliest / 86400) * TIMELINE_HEIGHT - 100);
  }

  function renderResponse(response) {
    recordings = response.recordings || [];
    events = response.events || [];
    if (activeRecording && !recordings.some((recording) => recording.id === activeRecording.id)) closePlayer();
    renderStats(response);
    q('#recordingsLoading').classList.add('hidden');
    q('#recordingsEmpty').classList.toggle('hidden', recordings.length !== 0);
    q('#recordingsContent').classList.toggle('hidden', recordings.length === 0);
    if (!recordings.length) return;
    renderTimeline();
    renderList();
    requestAnimationFrame(scrollTimelineToRecordings);
  }

  async function loadRecordings() {
    const version = ++requestVersion;
    updateURL();
    q('#recordingsLoading').classList.remove('hidden');
    q('#recordingsEmpty').classList.add('hidden');
    q('#recordingsContent').classList.add('hidden');
    const params = new URLSearchParams({ date: q('#recordingDate').value || localDateValue() });
    const camera = q('#recordingCamera').value;
    if (camera) params.set('camera_id', camera);
    try {
      const [response] = await Promise.all([api(`/api/recordings?${params}`), loadDays()]);
      if (version !== requestVersion) return;
      renderResponse(response);
      const today = localDateValue();
      q('#nextDay').disabled = q('#recordingDate').value >= today;
    } catch (error) {
      if (version !== requestVersion) return;
      q('#recordingsLoading').classList.add('hidden');
      notify(error.message, 'danger');
    }
  }

  function closePlayer() {
    player.pause();
    player.removeAttribute('src');
    player.load();
    activeRecording = null;
    playbackOffset = 0;
    q('#recordingPlayerCard').classList.add('hidden');
    renderTimeline();
    renderList();
  }

  async function init() {
    await initLayout('Historial local y línea de tiempo diaria');
    sources = await api('/api/recordings/sources');
    q('#recordingCamera').innerHTML = '<option value="">Todas las cámaras</option>' + sources.map((source) => `<option value="${esc(source.id)}">${esc(source.name)}</option>`).join('');
    const params = new URLSearchParams(location.search);
    const requestedCamera = params.get('camera_id');
    if (requestedCamera && sources.some((source) => source.id === requestedCamera)) q('#recordingCamera').value = requestedCamera;
    else if (sources.length === 1) q('#recordingCamera').value = sources[0].id;
    q('#recordingDate').value = /^\d{4}-\d{2}-\d{2}$/.test(params.get('date') || '') ? params.get('date') : localDateValue();

    q('#recordingCamera').addEventListener('change', loadRecordings);
    q('#recordingDate').addEventListener('change', loadRecordings);
    q('#previousDay').addEventListener('click', () => moveDay(-1));
    q('#nextDay').addEventListener('click', () => moveDay(1));
    q('#todayButton').addEventListener('click', () => { q('#recordingDate').value = localDateValue(); loadRecordings(); });
    q('#refreshRecordings').addEventListener('click', async (event) => {
      event.currentTarget.disabled = true;
      await loadRecordings();
      event.currentTarget.disabled = false;
    });
    q('#closeRecordingPlayer').addEventListener('click', closePlayer);
    q('#rewindRecording').addEventListener('click', () => jumpPlayback(-10));
    q('#forwardRecording').addEventListener('click', () => jumpPlayback(10));
    player.addEventListener('playing', () => q('#recordingPlayerLoading').classList.add('hidden'));
    player.addEventListener('canplay', () => q('#recordingPlayerLoading').classList.add('hidden'));
    player.addEventListener('waiting', () => q('#recordingPlayerLoading').classList.remove('hidden'));
    player.addEventListener('error', () => {
      q('#recordingPlayerLoading').classList.add('hidden');
      if (player.error) notify('No se pudo reproducir este segmento. Revise FFmpeg y los logs del servidor.', 'danger');
    });
    await loadRecordings();
  }

  init().catch((error) => {
    q('#recordingsLoading').classList.add('hidden');
    notify(error.message, 'danger');
  });
})();
