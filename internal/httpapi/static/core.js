(() => {
  const state = { session: { csrf_token: 'anonymous', auth_enabled: false } };
  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const escapeHTML = (value) => String(value ?? '').replace(/[&<>'"]/g, (character) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;',
  }[character]));

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (options.body) headers['Content-Type'] = 'application/json';
    if (options.method && options.method !== 'GET') headers['X-Fragata-CSRF'] = state.session.csrf_token;
    const response = await fetch(path, { ...options, headers });
    if (response.status === 401) {
      location.href = '/login';
      throw new Error('Sesión vencida');
    }
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
    return body;
  }

  async function initLayout(subtitle = '') {
    state.session = await api('/api/session');
    const layout = q('fragata-app-layout');
    layout?.setSession(state.session);
    if (subtitle) layout?.setSubtitle(subtitle);
    q('#ffmpegBadge')?.classList.toggle('hidden', !state.session.ffmpeg_available);
    layout?.addEventListener('fragata-logout', async () => {
      await api('/api/logout', { method: 'POST', body: '{}' });
      location.href = '/login';
    }, { once: true });
    await refreshQueueBadge();
    return state.session;
  }

  async function refreshQueueBadge() {
    const jobs = await api('/api/uploads');
    const badge = q('#queueBadge');
    if (badge) badge.innerHTML = `<i class="bi bi-cloud-arrow-up me-1"></i>${jobs.length} subida${jobs.length === 1 ? '' : 's'}`;
    return jobs;
  }

  function notify(message, type = 'primary') {
    window.FragataUI?.toast(message, type);
  }

  function statusLabel(value) {
    return ({
      online: 'En línea', starting: 'Iniciando', connecting: 'Conectando', reconnecting: 'Reconectando',
      disabled: 'Deshabilitada', offline: 'Sin conexión', error: 'Error',
    }[value] || value || 'Desconocido');
  }

  function statusClass(value) {
    if (value === 'online') return 'success';
    if (['starting', 'connecting', 'reconnecting'].includes(value)) return 'warning';
    if (value === 'disabled') return 'secondary';
    return 'danger';
  }

  function formatDuration(seconds) {
    const value = Number(seconds || 300);
    if (value % 3600 === 0) {
      const hours = value / 3600;
      return `${hours} hora${hours === 1 ? '' : 's'}`;
    }
    const minutes = Math.round(value / 60);
    return `${minutes} minuto${minutes === 1 ? '' : 's'}`;
  }

  function slugFolder(value) {
    return String(value || '').trim().toLocaleLowerCase('es-MX')
      .normalize('NFD').replace(/[\u0300-\u036f]/g, '')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 80);
  }

  window.Fragata = {
    state, q, qa, api, initLayout, refreshQueueBadge, notify, escapeHTML,
    statusLabel, statusClass, formatDuration, slugFolder,
  };
})();
