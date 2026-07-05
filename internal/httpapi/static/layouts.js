(() => {
  const navItems = [
    { key: 'dashboard', href: '/', icon: 'bi-speedometer2', label: 'Dashboard' },
    { key: 'cameras', href: '/cameras', icon: 'bi-camera-video-fill', label: 'Cámaras' },
    { key: 'add-camera', href: '/cameras/new', icon: 'bi-plus-square-fill', label: 'Agregar cámara' },
  ];

  function navMarkup(active, mobile = false) {
    return navItems.map((item) => `
      <a class="app-nav-link ${active === item.key ? 'active' : ''}" href="${item.href}" ${mobile ? 'data-bs-dismiss="offcanvas"' : ''}>
        <i class="bi ${item.icon}" aria-hidden="true"></i>
        <span>${item.label}</span>
      </a>
    `).join('');
  }

  class FragataAppLayout extends HTMLElement {
    connectedCallback() {
      if (this.dataset.ready === 'true') return;
      this.dataset.ready = 'true';
      const content = this.innerHTML;
      const pageTitle = this.getAttribute('page-title') || 'Panel de control';
      const active = this.getAttribute('active') || 'dashboard';
      const pageIcon = this.getAttribute('page-icon') || 'bi-speedometer2';

      this.innerHTML = `
        <div class="app-shell">
          <aside class="app-sidebar d-none d-lg-flex" aria-label="Navegación principal">
            <a class="sidebar-brand" href="/" aria-label="Ir al dashboard de Fragata">
              <span class="brand-symbol"><i class="bi bi-camera-reels-fill"></i></span>
              <span><strong>Fragata</strong><small>Servidor de cámaras</small></span>
            </a>
            <div class="sidebar-section-label">Administración</div>
            <nav class="app-nav">${navMarkup(active)}</nav>
            <div class="sidebar-spacer"></div>
            <div class="sidebar-status">
              <span class="status-pulse"></span>
              <span><strong>Servicio activo</strong><small>Monitoreo local</small></span>
            </div>
          </aside>

          <div class="offcanvas offcanvas-start app-offcanvas" tabindex="-1" id="fragataSidebar" aria-labelledby="fragataSidebarLabel">
            <div class="offcanvas-header">
              <a class="sidebar-brand" href="/" id="fragataSidebarLabel">
                <span class="brand-symbol"><i class="bi bi-camera-reels-fill"></i></span>
                <span><strong>Fragata</strong><small>Servidor de cámaras</small></span>
              </a>
              <button type="button" class="btn-close btn-close-white" data-bs-dismiss="offcanvas" aria-label="Cerrar"></button>
            </div>
            <div class="offcanvas-body">
              <div class="sidebar-section-label">Administración</div>
              <nav class="app-nav">${navMarkup(active, true)}</nav>
            </div>
          </div>

          <div class="app-workspace">
            <header class="app-topbar">
              <div class="d-flex align-items-center gap-3 min-w-0">
                <button class="btn sidebar-toggle d-lg-none" type="button" data-bs-toggle="offcanvas" data-bs-target="#fragataSidebar" aria-controls="fragataSidebar" aria-label="Abrir menú">
                  <i class="bi bi-list"></i>
                </button>
                <div class="page-title-wrap min-w-0">
                  <span class="page-title-icon"><i class="bi ${pageIcon}"></i></span>
                  <div class="min-w-0"><h1>${pageTitle}</h1><span id="layoutSubtitle">Administración de Fragata</span></div>
                </div>
              </div>
              <div class="topbar-tools">
                <span id="ffmpegBadge" class="badge rounded-pill bg-success-subtle border border-success-subtle text-success-emphasis hidden"><i class="bi bi-cpu me-1"></i>FFmpeg</span>
                <span id="queueBadge" class="badge rounded-pill text-bg-light border"><i class="bi bi-cloud-arrow-up me-1"></i>0 subidas</span>
                <div class="dropdown">
                  <button class="user-dropdown" type="button" data-bs-toggle="dropdown" aria-expanded="false">
                    <span class="user-avatar"><i class="bi bi-person-fill"></i></span>
                    <span class="user-copy d-none d-sm-grid"><strong id="layoutUsername">Cargando…</strong><small id="layoutUserRole">Usuario</small></span>
                    <i class="bi bi-chevron-down small"></i>
                  </button>
                  <ul class="dropdown-menu dropdown-menu-end shadow border-0 mt-2">
                    <li><h6 class="dropdown-header">Cuenta</h6></li>
                    <li id="layoutGuestStatus" class="hidden"><span class="dropdown-item-text small text-body-secondary"><i class="bi bi-shield-slash me-2"></i>Autenticación desactivada</span></li>
                    <li id="layoutLogoutItem"><button id="layoutLogoutButton" class="dropdown-item text-danger" type="button"><i class="bi bi-box-arrow-right me-2"></i>Cerrar sesión</button></li>
                  </ul>
                </div>
              </div>
            </header>

            <main class="app-content">${content}</main>
            <footer class="app-footer">
              <span>Fragata <strong>v0.6.1</strong></span>
              <span>Servidor NVR ligero · Go</span>
            </footer>
          </div>
        </div>
        <div class="toast-container position-fixed bottom-0 end-0 p-3" id="fragataToasts" aria-live="polite" aria-atomic="true"></div>
      `;

      this.querySelector('#layoutLogoutButton')?.addEventListener('click', () => {
        this.dispatchEvent(new CustomEvent('fragata-logout', { bubbles: true }));
      });
    }

    setSession(session) {
      const authenticated = Boolean(session?.auth_enabled);
      const username = authenticated && session?.username ? session.username : 'Invitado';
      const usernameEl = this.querySelector('#layoutUsername');
      const roleEl = this.querySelector('#layoutUserRole');
      if (usernameEl) usernameEl.textContent = username;
      if (roleEl) roleEl.textContent = authenticated ? 'Administrador' : 'Acceso local';
      this.querySelector('#layoutLogoutItem')?.classList.toggle('hidden', !authenticated);
      this.querySelector('#layoutGuestStatus')?.classList.toggle('hidden', authenticated);
    }

    setSubtitle(value) {
      const element = this.querySelector('#layoutSubtitle');
      if (element && value) element.textContent = value;
    }
  }

  class FragataAuthLayout extends HTMLElement {
    connectedCallback() {
      if (this.dataset.ready === 'true') return;
      this.dataset.ready = 'true';
      const content = this.innerHTML;
      this.innerHTML = `
        <main class="auth-layout">
          <section class="auth-showcase d-none d-lg-flex">
            <div class="auth-brand">
              <span class="brand-symbol brand-symbol-lg"><i class="bi bi-camera-reels-fill"></i></span>
              <div><strong>Fragata</strong><span>Servidor de cámaras</span></div>
            </div>
            <div class="auth-message">
              <span class="eyebrow"><i class="bi bi-shield-check me-2"></i>Acceso seguro</span>
              <h1>Controla tus cámaras desde un solo lugar.</h1>
              <p>Supervisa transmisiones, grabaciones y respaldos con una interfaz rápida, privada y adaptable.</p>
            </div>
            <div class="auth-features">
              <span><i class="bi bi-broadcast-pin"></i> Vista en tiempo real</span>
              <span><i class="bi bi-device-ssd"></i> Grabación continua</span>
              <span><i class="bi bi-cloud-check"></i> Respaldo SFTP</span>
            </div>
          </section>
          <section class="auth-panel">
            <div class="auth-mobile-brand d-lg-none">
              <span class="brand-symbol"><i class="bi bi-camera-reels-fill"></i></span>
              <span><strong>Fragata</strong><small>Servidor de cámaras</small></span>
            </div>
            <div class="auth-card">${content}</div>
            <footer class="auth-footer">Fragata v0.6.1 · Servidor NVR ligero</footer>
          </section>
        </main>
      `;
    }
  }

  const escapeHTML = (value) => String(value ?? '').replace(/[&<>'"]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[character]));

  window.FragataUI = {
    toast(message, type = 'primary') {
      const container = document.querySelector('#fragataToasts');
      if (!container || !window.bootstrap?.Toast) {
        console[type === 'danger' ? 'error' : 'log'](message);
        return;
      }
      const element = document.createElement('div');
      const icon = type === 'danger' ? 'bi-exclamation-triangle-fill' : type === 'success' ? 'bi-check-circle-fill' : 'bi-info-circle-fill';
      element.className = `toast border-0 text-bg-${type}`;
      element.setAttribute('role', 'status');
      element.innerHTML = `<div class="d-flex"><div class="toast-body"><i class="bi ${icon} me-2"></i>${escapeHTML(message)}</div><button type="button" class="btn-close btn-close-white me-2 m-auto" data-bs-dismiss="toast" aria-label="Cerrar"></button></div>`;
      container.append(element);
      element.addEventListener('hidden.bs.toast', () => element.remove());
      bootstrap.Toast.getOrCreateInstance(element, { delay: 4200 }).show();
    },
  };

  if (!customElements.get('fragata-app-layout')) customElements.define('fragata-app-layout', FragataAppLayout);
  if (!customElements.get('fragata-auth-layout')) customElements.define('fragata-auth-layout', FragataAuthLayout);
})();
