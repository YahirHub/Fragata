(() => {
  const { q, api, initLayout, notify, escapeHTML: esc } = window.Fragata;
  const TIMELINE_HOUR_WIDTH = 220;
  const TIMELINE_WIDTH = TIMELINE_HOUR_WIDTH * 24;
  const TIMELINE_RULER_HEIGHT = 46;
  const TIMELINE_LANE_HEIGHT = 48;
  const TIMELINE_EVENT_TRACK_HEIGHT = 48;
  const TIMELINE_MIN_BAR_WIDTH = 30;
  const TIMELINE_BAR_GAP = 6;
  const TIMELINE_MAX_RECORDINGS = 1200;
  const TIMELINE_EVENT_BUCKET_SECONDS = 10 * 60;
  const RECORDING_LIST_PAGE_SIZE = 200;

  let sources = [];
  let recordings = [];
  let events = [];
  let activeRecording = null;
  let playbackOffset = 0;
  let requestVersion = 0;
  let currentListPage = 0;
  let playerHasStarted = false;

  const player = q('#recordingPlayer');
  const playerStage = q('.recording-player-stage');
  const playerLoading = q('#recordingPlayerLoading');

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

  function hourColumns(totalHeight) {
    return Array.from({ length: 25 }, (_, hour) => {
      const left = hour * TIMELINE_HOUR_WIDTH;
      const edgeClass = hour === 0 ? ' hour-start' : hour === 24 ? ' hour-end' : '';
      return `<div class="timeline-hour${edgeClass}" style="left:${left}px;height:${totalHeight}px"><span>${String(hour).padStart(2, '0')}:00</span><i></i></div>`;
    }).join('');
  }

  function limitTimelineRecordings(items) {
    if (items.length <= TIMELINE_MAX_RECORDINGS) return items;
    const limited = [];
    const step = items.length / TIMELINE_MAX_RECORDINGS;
    for (let index = 0; index < TIMELINE_MAX_RECORDINGS; index += 1) {
      limited.push(items[Math.min(items.length - 1, Math.floor(index * step))]);
    }
    if (activeRecording && !limited.some((item) => item.id === activeRecording.id)) limited[limited.length - 1] = activeRecording;
    return limited.sort((a, b) => new Date(a.started_at) - new Date(b.started_at));
  }

  function layoutRecordingBars(items) {
    const laneEnds = [];
    return items.map((recording) => {
      const startSeconds = Math.max(0, Math.min(86400, secondsOfDay(recording.started_at)));
      const duration = Math.max(1, Math.min(Number(recording.duration_seconds || 0), 86400 - startSeconds));
      let left = (startSeconds / 86400) * TIMELINE_WIDTH;
      const proportionalWidth = (duration / 86400) * TIMELINE_WIDTH;
      const width = Math.min(TIMELINE_WIDTH, Math.max(TIMELINE_MIN_BAR_WIDTH, proportionalWidth));
      if (left + width > TIMELINE_WIDTH) left = Math.max(0, TIMELINE_WIDTH - width);
      let lane = laneEnds.findIndex((end) => left >= end + TIMELINE_BAR_GAP);
      if (lane === -1) {
        lane = laneEnds.length;
        laneEnds.push(0);
      }
      laneEnds[lane] = left + width;
      return { recording, left, width, lane };
    });
  }

  function clusterTimelineEvents(items) {
    const buckets = new Map();
    items.forEach((event) => {
      const seconds = Math.max(0, Math.min(86399.999, secondsOfDay(event.created_at)));
      const bucket = Math.floor(seconds / TIMELINE_EVENT_BUCKET_SECONDS);
      if (!buckets.has(bucket)) buckets.set(bucket, []);
      buckets.get(bucket).push({ event, seconds });
    });
    return [...buckets.entries()].sort((a, b) => a[0] - b[0]).map(([, entries]) => {
      const seconds = entries.reduce((sum, entry) => sum + entry.seconds, 0) / entries.length;
      const kind = entries.some((entry) => entry.event.type === 'person')
        ? 'person'
        : entries.some((entry) => entry.event.type === 'motion') ? 'motion' : 'onvif';
      return { entries, seconds, kind, first: entries[0].event };
    });
  }

  function renderTimeline() {
    const timeline = q('#recordingTimeline');
    const sorted = [...recordings].sort((a, b) => new Date(a.started_at) - new Date(b.started_at));
    const visibleRecordings = limitTimelineRecordings(sorted);
    const bars = layoutRecordingBars(visibleRecordings);
    const laneCount = Math.max(1, bars.reduce((maximum, item) => Math.max(maximum, item.lane + 1), 0));
    const eventTrackTop = TIMELINE_RULER_HEIGHT + laneCount * TIMELINE_LANE_HEIGHT + 6;
    const totalHeight = eventTrackTop + TIMELINE_EVENT_TRACK_HEIGHT;
    const clusters = clusterTimelineEvents(events);

    timeline.style.width = `${TIMELINE_WIDTH}px`;
    timeline.style.height = `${totalHeight}px`;

    const recordingBars = bars.map(({ recording, left, width, lane }) => {
      const classes = ['timeline-recording'];
      if (recording.pending) classes.push('pending');
      if (recording.recovered) classes.push('recovered');
      if (activeRecording?.id === recording.id) classes.push('active');
      const top = TIMELINE_RULER_HEIGHT + lane * TIMELINE_LANE_HEIGHT + 7;
      const title = `${recording.camera_name} · ${formatTime(recording.started_at)}–${formatTime(recording.ended_at)} · ${formatDuration(recording.duration_seconds)}`;
      const label = width >= 76 ? `${formatTime(recording.started_at, false)} · ${recording.camera_name}` : width >= 45 ? formatTime(recording.started_at, false) : '';
      return `<button class="${classes.join(' ')}" style="left:${left}px;top:${top}px;width:${width}px" type="button" data-recording-id="${esc(recording.id)}" title="${esc(title)}" aria-label="Reproducir ${esc(title)}">${label ? `<span>${esc(label)}</span>` : '<i class="bi bi-play-fill" aria-hidden="true"></i>'}</button>`;
    }).join('');

    const markers = clusters.map((cluster) => {
      const left = Math.max(17, Math.min(TIMELINE_WIDTH - 17, (cluster.seconds / 86400) * TIMELINE_WIDTH));
      const count = cluster.entries.length;
      const firstTime = formatTime(cluster.first.created_at);
      const typeLabel = cluster.kind === 'person' ? 'Persona' : cluster.kind === 'motion' ? 'Movimiento' : 'Evento ONVIF';
      const typeIcon = cluster.kind === 'person' ? 'bi-person-fill' : cluster.kind === 'motion' ? 'bi-activity' : 'bi-shield-exclamation';
      const title = count > 1
        ? `${count} eventos entre ${firstTime} y ${formatTime(cluster.entries[cluster.entries.length - 1].event.created_at)}`
        : `${typeLabel} · ${firstTime}`;
      const contents = count > 1 ? `<span>${count > 99 ? '99+' : count}</span>` : `<i class="bi ${typeIcon}"></i>`;
      return `<button class="timeline-event ${cluster.kind} ${count > 1 ? 'cluster' : ''}" style="left:${left}px;top:${eventTrackTop + 8}px" type="button" data-event-id="${esc(cluster.first.id)}" data-recording-id="${esc(cluster.first.recording_id || '')}" data-offset="${Number(cluster.first.offset_seconds || 0)}" title="${esc(title)}" aria-label="${esc(title)}">${contents}</button>`;
    }).join('');

    timeline.innerHTML = `${hourColumns(totalHeight)}<div class="timeline-event-track" style="top:${eventTrackTop}px;width:${TIMELINE_WIDTH}px"><span>Eventos</span></div>${recordingBars}${markers}`;

    timeline.querySelectorAll('[data-recording-id].timeline-recording').forEach((button) => button.addEventListener('click', () => {
      const recording = recordings.find((item) => item.id === button.dataset.recordingId);
      if (recording) playRecording(recording, 0);
    }));
    timeline.querySelectorAll('.timeline-event').forEach((button) => button.addEventListener('click', () => {
      const recording = recordings.find((item) => item.id === button.dataset.recordingId);
      if (recording) playRecording(recording, Math.max(0, Number(button.dataset.offset || 0) - 5));
      else location.href = `/events/${encodeURIComponent(button.dataset.eventId)}`;
    }));

    const hiddenRecordings = Math.max(0, sorted.length - visibleRecordings.length);
    const groupedEvents = Math.max(0, events.length - clusters.length);
    const summary = [`${sorted.length} video${sorted.length === 1 ? '' : 's'}`, `${clusters.length} marcador${clusters.length === 1 ? '' : 'es'} de eventos`];
    if (hiddenRecordings) summary.push(`vista visual limitada a ${TIMELINE_MAX_RECORDINGS} bloques`);
    if (groupedEvents) summary.push(`${groupedEvents} eventos agrupados en intervalos de 10 min`);
    q('#timelineSummary').textContent = `${summary.join(' · ')}. Desplázate horizontalmente para recorrer el día.`;
  }

  function renderList() {
    const pageCount = Math.max(1, Math.ceil(recordings.length / RECORDING_LIST_PAGE_SIZE));
    currentListPage = Math.max(0, Math.min(currentListPage, pageCount - 1));
    const pageStart = currentListPage * RECORDING_LIST_PAGE_SIZE;
    const visible = recordings.slice(pageStart, pageStart + RECORDING_LIST_PAGE_SIZE);
    q('#recordingList').innerHTML = visible.map((recording) => {
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

    const first = recordings.length ? pageStart + 1 : 0;
    const last = pageStart + visible.length;
    q('#recordingListCounter').textContent = recordings.length ? `Mostrando ${first}–${last} de ${recordings.length}` : '';
    q('#recordingListFooter').classList.toggle('hidden', pageCount <= 1);
    q('#recordingListPage').textContent = `Página ${currentListPage + 1} de ${pageCount}`;
    q('#previousRecordingsPage').disabled = currentListPage === 0;
    q('#nextRecordingsPage').disabled = currentListPage >= pageCount - 1;
  }

  function setActiveRecording(recording) {
    activeRecording = recording;
    document.querySelectorAll('.timeline-recording.active, .recording-list-item.active').forEach((element) => element.classList.remove('active'));
    document.querySelector(`.timeline-recording[data-recording-id="${CSS.escape(recording.id)}"]`)?.classList.add('active');
    document.querySelector(`[data-list-recording="${CSS.escape(recording.id)}"]`)?.classList.add('active');
  }

  function setPlayerLoading(loading) {
    playerLoading.classList.toggle('hidden', !loading);
    playerStage.classList.toggle('is-preparing', loading && !playerHasStarted);
    playerStage.setAttribute('aria-busy', loading ? 'true' : 'false');
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
    playerHasStarted = false;
    const recordingIndex = recordings.findIndex((item) => item.id === recording.id);
    if (recordingIndex >= 0) currentListPage = Math.floor(recordingIndex / RECORDING_LIST_PAGE_SIZE);
    setActiveRecording(recording);
    q('#recordingPlayerCard').classList.remove('hidden');
    q('#recordingPlayerTitle').textContent = `${recording.camera_name} · ${formatDate(recording.started_at)}`;
    q('#recordingDownload').href = recording.download_url || '#';
    q('#recordingDownload').classList.toggle('hidden', !recording.download_url);
    setPlayerLoading(true);
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
    const earliest = recordings.reduce((best, item) => Math.min(best, secondsOfDay(item.started_at)), 86400);
    q('#timelineScroll').scrollLeft = Math.max(0, (earliest / 86400) * TIMELINE_WIDTH - 90);
  }

  function renderResponse(response) {
    recordings = response.recordings || [];
    events = response.events || [];
    currentListPage = 0;
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
    playerHasStarted = false;
    setPlayerLoading(false);
    activeRecording = null;
    playbackOffset = 0;
    q('#recordingPlayerCard').classList.add('hidden');
    if (recordings.length) {
      renderTimeline();
      renderList();
    }
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
    q('#previousRecordingsPage').addEventListener('click', () => {
      currentListPage = Math.max(0, currentListPage - 1);
      renderList();
      q('#recordingList').scrollTop = 0;
    });
    q('#nextRecordingsPage').addEventListener('click', () => {
      const lastPage = Math.max(0, Math.ceil(recordings.length / RECORDING_LIST_PAGE_SIZE) - 1);
      currentListPage = Math.min(lastPage, currentListPage + 1);
      renderList();
      q('#recordingList').scrollTop = 0;
    });
    q('#closeRecordingPlayer').addEventListener('click', closePlayer);
    q('#rewindRecording').addEventListener('click', () => jumpPlayback(-10));
    q('#forwardRecording').addEventListener('click', () => jumpPlayback(10));
    player.addEventListener('loadstart', () => {
      if (!playerHasStarted) setPlayerLoading(true);
    });
    player.addEventListener('loadeddata', () => {
      playerHasStarted = true;
      setPlayerLoading(false);
    });
    player.addEventListener('playing', () => {
      playerHasStarted = true;
      setPlayerLoading(false);
    });
    player.addEventListener('canplay', () => {
      playerHasStarted = true;
      setPlayerLoading(false);
    });
    player.addEventListener('waiting', () => {
      if (!playerHasStarted) setPlayerLoading(true);
    });
    player.addEventListener('error', () => {
      setPlayerLoading(false);
      if (player.error) notify('No se pudo reproducir este segmento. Revise FFmpeg y los logs del servidor.', 'danger');
    });
    await loadRecordings();
  }

  init().catch((error) => {
    q('#recordingsLoading').classList.add('hidden');
    notify(error.message, 'danger');
  });
})();
