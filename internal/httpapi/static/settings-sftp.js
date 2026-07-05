(() => {
  const { q, api, initLayout, notify, escapeHTML: esc } = window.Fragata;
  let profiles = [];
  const modalElement = q('#sftpModal');
  const modal = bootstrap.Modal.getOrCreateInstance(modalElement);

  function row(profile) {
    const status = profile.enabled ? '<span class="badge bg-success-subtle text-success-emphasis">Habilitado</span>' : '<span class="badge text-bg-light border">Deshabilitado</span>';
    const actions = profile.read_only ? `<button class="btn btn-sm btn-outline-primary" data-action="test" data-id="${esc(profile.id)}"><i class="bi bi-plug me-1"></i>Probar</button>` : `<div class="dropdown"><button class="btn action-menu-button" data-bs-toggle="dropdown" aria-label="Acciones"><i class="bi bi-three-dots-vertical"></i></button><ul class="dropdown-menu dropdown-menu-end shadow border-0"><li><button class="dropdown-item" data-action="edit" data-id="${esc(profile.id)}"><i class="bi bi-pencil me-2"></i>Editar</button></li><li><button class="dropdown-item" data-action="test" data-id="${esc(profile.id)}"><i class="bi bi-plug me-2"></i>Probar conexión</button></li><li><hr class="dropdown-divider"></li><li><button class="dropdown-item text-danger" data-action="delete" data-id="${esc(profile.id)}"><i class="bi bi-trash3 me-2"></i>Eliminar</button></li></ul></div>`;
    return `<tr><td><div class="camera-cell"><span class="camera-cell-icon"><i class="bi bi-server"></i></span><div><strong>${esc(profile.name)}</strong><small>${profile.read_only ? 'Configurado en .env' : 'Perfil global'}</small></div></div></td><td><span class="table-main">${esc(profile.host)}:${profile.port}</span></td><td>${esc(profile.user)}</td><td><code class="folder-code">${esc(profile.remote_base_dir)}</code></td><td>${status}</td><td class="text-end">${actions}</td></tr>`;
  }

  async function refresh() {
    profiles = await api('/api/sftp-profiles');
    q('#profilesTable').innerHTML = profiles.length ? profiles.map(row).join('') : '<tr><td colspan="6" class="text-center py-5 text-body-secondary">No hay servidores SFTP configurados.</td></tr>';
  }

  function resetForm(profile = null) {
    q('#sftpModalTitle').textContent = profile ? 'Editar servidor SFTP' : 'Agregar servidor SFTP';
    q('#profileID').value = profile?.id || '';
    q('#profileName').value = profile?.name || '';
    q('#profileEnabled').checked = profile?.enabled ?? true;
    q('#profileHost').value = profile?.host || '';
    q('#profilePort').value = profile?.port || 22;
    q('#profileUser').value = profile?.user || '';
    q('#profilePassword').value = '';
    q('#profileKey').value = profile?.private_key_path || '';
    q('#profileKnownHosts').value = profile?.known_hosts_path || '';
    q('#profileRemote').value = profile?.remote_base_dir || '/fragata';
    q('#profileTimeout').value = profile?.timeout_seconds || 30;
    q('#profileDeleteLocal').checked = Boolean(profile?.delete_local);
    q('#passwordHelp').textContent = profile?.has_password ? 'Hay una contraseña cifrada. Déjala vacía para conservarla.' : 'Obligatoria si no se usa llave privada.';
  }

  function payload() {
    return { name: q('#profileName').value.trim(), enabled: q('#profileEnabled').checked, host: q('#profileHost').value.trim(), port: Number(q('#profilePort').value), user: q('#profileUser').value.trim(), password: q('#profilePassword').value, private_key_path: q('#profileKey').value.trim(), known_hosts_path: q('#profileKnownHosts').value.trim(), remote_base_dir: q('#profileRemote').value.trim(), delete_local: q('#profileDeleteLocal').checked, timeout_seconds: Number(q('#profileTimeout').value) };
  }

  q('#newProfileButton').addEventListener('click', () => resetForm());
  q('#profilesTable').addEventListener('click', async (event) => {
    const button = event.target.closest('[data-action]'); if (!button) return;
    const profile = profiles.find((item) => item.id === button.dataset.id); if (!profile) return;
    if (button.dataset.action === 'edit') { resetForm(profile); modal.show(); return; }
    if (button.dataset.action === 'test') { button.disabled = true; try { await api(`/api/sftp-profiles/${encodeURIComponent(profile.id)}/test`, { method: 'POST', body: '{}' }); notify('Conexión SFTP correcta.', 'success'); } catch (error) { notify(error.message, 'danger'); } finally { button.disabled = false; } return; }
    if (button.dataset.action === 'delete' && confirm(`¿Eliminar el servidor ${profile.name}?`)) { try { await api(`/api/sftp-profiles/${encodeURIComponent(profile.id)}`, { method: 'DELETE' }); await refresh(); notify('Servidor SFTP eliminado.', 'success'); } catch (error) { notify(error.message, 'danger'); } }
  });
  q('#sftpForm').addEventListener('submit', async (event) => {
    event.preventDefault(); if (!event.currentTarget.reportValidity()) return;
    const id = q('#profileID').value; const button = q('#saveProfileButton'); button.disabled = true;
    try { await api(id ? `/api/sftp-profiles/${encodeURIComponent(id)}` : '/api/sftp-profiles', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(payload()) }); modal.hide(); await refresh(); notify('Servidor SFTP guardado.', 'success'); } catch (error) { notify(error.message, 'danger'); } finally { button.disabled = false; }
  });
  async function init() { await initLayout('Perfiles globales de respaldo remoto'); await refresh(); }
  init().catch((error) => notify(error.message, 'danger'));
})();
