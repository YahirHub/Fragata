(() => {
  const STORAGE_KEY = 'fragata.theme';
  const THEMES = new Set(['light', 'dark']);
  const listeners = new Set();
  const media = window.matchMedia?.('(prefers-color-scheme: dark)');

  function storedTheme() {
    try {
      const value = localStorage.getItem(STORAGE_KEY);
      return THEMES.has(value) ? value : '';
    } catch {
      return '';
    }
  }

  function preferredTheme() {
    return media?.matches ? 'dark' : 'light';
  }

  function currentTheme() {
    const value = document.documentElement.getAttribute('data-bs-theme');
    return THEMES.has(value) ? value : preferredTheme();
  }

  function updateThemeColor(theme) {
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', theme === 'dark' ? '#111827' : '#4e73df');
  }

  function notify(theme) {
    listeners.forEach((listener) => {
      try {
        listener(theme);
      } catch (error) {
        console.error('No se pudo actualizar un control de tema', error);
      }
    });
    window.dispatchEvent(new CustomEvent('fragata-theme-change', { detail: { theme } }));
  }

  function apply(theme, { persist = false, announce = true } = {}) {
    const nextTheme = THEMES.has(theme) ? theme : preferredTheme();
    document.documentElement.setAttribute('data-bs-theme', nextTheme);
    document.documentElement.style.colorScheme = nextTheme;
    updateThemeColor(nextTheme);

    if (persist) {
      try {
        localStorage.setItem(STORAGE_KEY, nextTheme);
      } catch {
        // El tema sigue funcionando durante la sesión aunque el navegador bloquee storage.
      }
    }

    if (announce) notify(nextTheme);
    return nextTheme;
  }

  function toggle() {
    return apply(currentTheme() === 'dark' ? 'light' : 'dark', { persist: true });
  }

  function subscribe(listener) {
    if (typeof listener !== 'function') return () => {};
    listeners.add(listener);
    listener(currentTheme());
    return () => listeners.delete(listener);
  }

  window.FragataTheme = {
    get: currentTheme,
    set(theme) { return apply(theme, { persist: true }); },
    toggle,
    subscribe,
  };

  apply(storedTheme() || preferredTheme(), { announce: false });

  try {
    if (localStorage.getItem('fragata.sidebar.hidden') === 'true') {
      document.documentElement.classList.add('fragata-sidebar-hidden');
    }
  } catch {
    // La preferencia del sidebar es opcional.
  }

  media?.addEventListener?.('change', (event) => {
    if (!storedTheme()) apply(event.matches ? 'dark' : 'light');
  });

  window.addEventListener('storage', (event) => {
    if (event.key !== STORAGE_KEY) return;
    apply(THEMES.has(event.newValue) ? event.newValue : preferredTheme());
  });
})();
