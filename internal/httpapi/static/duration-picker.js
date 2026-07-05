class FragataDurationPicker extends HTMLElement {
  static get observedAttributes() {
    return ['value-seconds', 'disabled'];
  }

  connectedCallback() {
    if (this.dataset.ready === 'true') return;
    this.dataset.ready = 'true';
    this.classList.add('duration-picker');
    this.innerHTML = `
      <input class="duration-picker-value form-control" type="number" inputmode="decimal" aria-label="Duración por archivo">
      <select class="duration-picker-unit form-select" aria-label="Unidad de duración">
        <option value="60">minutos</option>
        <option value="3600">horas</option>
      </select>
    `;
    this.input = this.querySelector('.duration-picker-value');
    this.unit = this.querySelector('.duration-picker-unit');
    this.input.addEventListener('change', () => this.commit());
    this.unit.addEventListener('change', () => {
      const previousMultiplier = this.lastMultiplier || 60;
      const currentSeconds = Number(this.input.value || 1) * previousMultiplier;
      this.lastMultiplier = Number(this.unit.value);
      this.input.value = String(currentSeconds / this.lastMultiplier);
      this.updateLimits();
      this.commit();
    });
    this.syncFromAttribute();
    this.syncDisabled();
  }

  attributeChangedCallback(name) {
    if (!this.input || !this.unit) return;
    if (name === 'value-seconds' && !this.syncing) this.syncFromAttribute();
    if (name === 'disabled') this.syncDisabled();
  }

  get valueSeconds() {
    const multiplier = Number(this.unit?.value || 60);
    const value = Number(this.input?.value || 1);
    return this.normalizeSeconds(value * multiplier);
  }

  set valueSeconds(value) {
    const seconds = this.normalizeSeconds(value);
    this.syncing = true;
    this.setAttribute('value-seconds', String(seconds));
    this.syncing = false;
    if (this.input && this.unit) this.syncFromSeconds(seconds);
  }

  get disabled() {
    return this.hasAttribute('disabled');
  }

  set disabled(value) {
    this.toggleAttribute('disabled', Boolean(value));
  }

  syncFromAttribute() {
    const seconds = this.normalizeSeconds(Number(this.getAttribute('value-seconds') || 300));
    this.syncFromSeconds(seconds);
  }

  syncFromSeconds(seconds) {
    const useHours = seconds >= 3600 && seconds % 3600 === 0;
    const multiplier = useHours ? 3600 : 60;
    this.unit.value = String(multiplier);
    this.lastMultiplier = multiplier;
    this.input.value = String(seconds / multiplier);
    this.updateLimits();
  }

  updateLimits() {
    const hours = Number(this.unit.value) === 3600;
    this.input.min = '1';
    this.input.max = hours ? '24' : '1440';
    this.input.step = '1';
  }

  syncDisabled() {
    const disabled = this.disabled;
    if (this.input) this.input.disabled = disabled;
    if (this.unit) this.unit.disabled = disabled;
  }

  commit() {
    let seconds = this.valueSeconds;
    const multiplier = Number(this.unit.value);
    let value = seconds / multiplier;
    if (multiplier === 3600 && seconds < 3600) {
      seconds = 3600;
      value = 1;
    }
    this.input.value = String(value);
    this.syncing = true;
    this.setAttribute('value-seconds', String(seconds));
    this.syncing = false;
    this.dispatchEvent(new CustomEvent('durationchange', {
      bubbles: true,
      detail: { seconds },
    }));
  }

  normalizeSeconds(value) {
    const numeric = Number.isFinite(Number(value)) ? Number(value) : 300;
    const roundedMinutes = Math.round(numeric / 60) * 60;
    return Math.min(86400, Math.max(60, roundedMinutes));
  }
}

if (!customElements.get('fragata-duration-picker')) {
  customElements.define('fragata-duration-picker', FragataDurationPicker);
}
