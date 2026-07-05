(() => {
  class FragataRetentionPicker extends HTMLElement {
    connectedCallback() {
      if (this.dataset.ready === 'true') return;
      this.dataset.ready = 'true';
      this.innerHTML = `
        <div class="retention-picker">
          <input class="form-control" type="number" min="1" max="3650" value="30" aria-label="Cantidad de retención">
          <select class="form-select" aria-label="Unidad de retención">
            <option value="days">Días</option>
            <option value="months">Meses</option>
            <option value="years">Años</option>
          </select>
        </div>`;
      this.querySelector('select').addEventListener('change', () => this.syncLimit());
      this.syncLimit();
    }

    syncLimit() {
      const input = this.querySelector('input');
      const unit = this.querySelector('select')?.value;
      input.max = unit === 'years' ? '10' : unit === 'months' ? '120' : '3650';
      if (Number(input.value) > Number(input.max)) input.value = input.max;
    }

    get value() {
      return { value: Number(this.querySelector('input')?.value || 1), unit: this.querySelector('select')?.value || 'days' };
    }

    set value(policy) {
      if (!this.dataset.ready) this.connectedCallback();
      this.querySelector('select').value = policy?.unit || 'days';
      this.syncLimit();
      this.querySelector('input').value = String(policy?.value || 30);
    }
  }
  if (!customElements.get('fragata-retention-picker')) customElements.define('fragata-retention-picker', FragataRetentionPicker);
})();
