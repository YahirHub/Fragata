(() => {
  const { q, api, initLayout, notify } = window.Fragata;
  async function init() {
    const session = await initLayout('Retención automática y registros del sistema');
    const logPath = q('#logPath');
    if (logPath) logPath.textContent = session.log_path || 'data/logs.txt';
    const policy = await api('/api/retention');
    q('#retentionEnabled').checked = Boolean(policy.enabled);
    q('#retentionPicker').value = policy;
  }
  q('#saveRetention').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    button.disabled = true;
    try {
      const selection = q('#retentionPicker').value;
      const response = await api('/api/retention', { method: 'PATCH', body: JSON.stringify({ enabled: q('#retentionEnabled').checked, ...selection }) });
      const deleted = Number(response.cleanup?.deleted || 0);
      notify(deleted ? `Política guardada. Se eliminaron ${deleted} grabaciones antiguas.` : 'Política de retención guardada y comprobada.', 'success');
    } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  });
  init().catch((error) => notify(error.message, 'danger'));
})();
