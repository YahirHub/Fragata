(() => {
  let loaderSequence = 0;

  function normalizeSize(value) {
    const parsed = Number.parseFloat(value);
    if (!Number.isFinite(parsed)) return 72;
    return Math.min(140, Math.max(28, parsed));
  }

  class FragataLoader extends HTMLElement {
    static get observedAttributes() {
      return ['label', 'size'];
    }

    connectedCallback() {
      if (this.dataset.loaderReady === 'true') {
        this.syncAttributes();
        return;
      }

      this.dataset.loaderReady = 'true';
      const maskID = `fragata-loader-mask-${++loaderSequence}`;
      this.classList.add('fragata-loader-component');
      this.setAttribute('role', this.getAttribute('role') || 'status');
      this.setAttribute('aria-live', this.getAttribute('aria-live') || 'polite');
      this.setAttribute('aria-atomic', 'true');

      this.innerHTML = `
        <span class="fragata-loader-visual" aria-hidden="true">
          <span class="fragata-loader-animation">
            <svg width="100" height="100" viewBox="0 0 100 100" focusable="false">
              <defs>
                <mask id="${maskID}">
                  <polygon points="0,0 100,0 100,100 0,100" fill="black"></polygon>
                  <polygon points="25,25 75,25 50,75" fill="white"></polygon>
                  <polygon points="50,25 75,75 25,75" fill="white"></polygon>
                  <polygon points="35,35 65,35 50,65" fill="white"></polygon>
                  <polygon points="35,35 65,35 50,65" fill="white"></polygon>
                  <polygon points="35,35 65,35 50,65" fill="white"></polygon>
                  <polygon points="35,35 65,35 50,65" fill="white"></polygon>
                </mask>
              </defs>
            </svg>
            <span class="fragata-loader-box" style="mask: url(#${maskID}); -webkit-mask: url(#${maskID});"></span>
          </span>
        </span>
        <span class="fragata-loader-label"></span>
      `;

      this.syncAttributes();
    }

    attributeChangedCallback() {
      if (this.isConnected && this.dataset.loaderReady === 'true') this.syncAttributes();
    }

    syncAttributes() {
      const label = (this.getAttribute('label') || 'Cargando…').trim() || 'Cargando…';
      const size = normalizeSize(this.getAttribute('size'));
      const labelElement = this.querySelector('.fragata-loader-label');
      if (labelElement) labelElement.textContent = label;
      this.style.setProperty('--fragata-loader-size', `${size}px`);
      this.style.setProperty('--fragata-loader-scale', String(size / 100));
      this.setAttribute('aria-label', label);
    }

    get label() {
      return this.getAttribute('label') || '';
    }

    set label(value) {
      this.setAttribute('label', String(value || 'Cargando…'));
    }
  }

  if (!customElements.get('fragata-loader')) {
    customElements.define('fragata-loader', FragataLoader);
  }

  window.FragataLoader = Object.freeze({
    setLabel(target, label) {
      const element = typeof target === 'string' ? document.querySelector(target) : target;
      if (element) element.setAttribute('label', String(label || 'Cargando…'));
    },
  });
})();
