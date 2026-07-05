(() => {
  const { q, qa, api, initLayout, slugFolder, notify } = window.Fragata;
  const segments = location.pathname.split('/').filter(Boolean);
  const cameraID = decodeURIComponent(segments.at(-2) || '');
  let camera = null;

  function setConnectionStatus(message, type = 'light') {
    const element = q('#connectionStatus');
    element.className = `alert alert-${type} border small mt-3 mb-0`;
    element.textContent = message;
  }

  function populate(value) {
    camera = value;
    document.title = `Ajustes de ${camera.name} · Fragata`;
    q('#settingsTitle').textContent = camera.name;
    q('#settingsSubtitle').textContent = `${camera.host} · ${[camera.manufacturer, camera.model].filter(Boolean).join(' ') || 'Cámara IP'}`;
    q('#cameraBreadcrumb').textContent = camera.name;
    q('#openCameraButton').href = `/camera/${encodeURIComponent(camera.id)}`;
    q('#cameraName').value = camera.name || '';
    q('#cameraFolder').value = camera.folder_name || camera.id;
    q('#cameraEnabled').checked = camera.enabled;
    q('#cameraHost').value = camera.host || '';
    q('#cameraUsername').value = camera.username || '';
    q('#cameraPassword').value = '';
    q('#cameraRTSP').value = camera.rtsp_url || '';
    q('#recordSwitch').checked = camera.record;
    q('#uploadSwitch').checked = camera.upload;
    q('#segmentDurationPicker').valueSeconds = camera.segment_duration_seconds || 300;
    q('#passwordState').textContent = camera.has_password ? 'Hay una contraseña cifrada configurada. Déjala vacía para conservarla.' : 'No hay una contraseña almacenada para esta cámara.';
    q('#deviceManufacturer').textContent = camera.manufacturer || 'No identificado';
    q('#deviceModel').textContent = camera.model || 'No identificado';
    q('#deviceSerial').textContent = camera.serial_number || 'No disponible';
    q('#deviceFirmware').textContent = camera.firmware_version || 'No disponible';
    q('#deviceStream').textContent = `${camera.codec || '—'}${camera.width && camera.height ? ` · ${camera.width}×${camera.height}` : ''}`;
    q('#deviceLiveStream').textContent = `${camera.live_codec || camera.codec || '—'}${camera.live_width && camera.live_height ? ` · ${camera.live_width}×${camera.live_height}` : ''}`;
    q('fragata-app-layout')?.setSubtitle(`${camera.name} · Configuración`);
  }

  function payload() {
    const data = {
      name: q('#cameraName').value.trim(),
      folder_name: q('#cameraFolder').value.trim(),
      enabled: q('#cameraEnabled').checked,
      host: q('#cameraHost').value.trim(),
      username: q('#cameraUsername').value.trim(),
      rtsp_url: q('#cameraRTSP').value.trim(),
      record: q('#recordSwitch').checked,
      upload: q('#uploadSwitch').checked,
      segment_duration_seconds: q('#segmentDurationPicker').valueSeconds,
    };
    const password = q('#cameraPassword').value;
    if (password) data.password = password;
    return data;
  }

  qa('[data-password-toggle]').forEach((button) => button.addEventListener('click', () => {
    const input = q(button.dataset.passwordToggle);
    const visible = input.type === 'text';
    input.type = visible ? 'password' : 'text';
    button.innerHTML = `<i class="bi ${visible ? 'bi-eye' : 'bi-eye-slash'}"></i>`;
  }));
  q('#cameraFolder').addEventListener('blur', (event) => { event.currentTarget.value = slugFolder(event.currentTarget.value || q('#cameraName').value); });

  q('#testConnectionButton').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    button.disabled = true;
    setConnectionStatus('Probando la configuración con las credenciales almacenadas o nuevas…', 'info');
    try {
      const result = await api(`/api/cameras/${encodeURIComponent(cameraID)}/probe-settings`, { method: 'POST', body: JSON.stringify(payload()) });
      const detected = result.camera;
      setConnectionStatus(`Conexión correcta mediante ${result.detection_method}. Stream ${detected.codec || 'video'}${detected.width && detected.height ? ` · ${detected.width}×${detected.height}` : ''}.`, 'success');
    } catch (error) { setConnectionStatus(error.message, 'danger'); }
    finally { button.disabled = false; }
  });

  q('#redetectButton').addEventListener('click', async (event) => {
    const button = event.currentTarget;
    button.disabled = true;
    button.innerHTML = '<span class="spinner-border spinner-border-sm me-2"></span>Redetectando…';
    try {
      const result = await api(`/api/cameras/${encodeURIComponent(cameraID)}/redetect`, { method: 'POST', body: '{}' });
      populate(result.camera);
      setConnectionStatus(`Calidad redetectada mediante ${result.detection_method}.`, 'success');
      notify('Calidad y perfiles actualizados.', 'success');
    } catch (error) { notify(error.message, 'danger'); }
    finally { button.disabled = false; button.innerHTML = '<i class="bi bi-arrow-repeat me-2"></i>Redetectar calidad'; }
  });

  q('#settingsForm').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    if (!form.reportValidity()) return;
    const button = q('#saveButton');
    button.disabled = true;
    q('#saveStatus').textContent = 'Validando y guardando los ajustes…';
    try {
      const updated = await api(`/api/cameras/${encodeURIComponent(cameraID)}`, { method: 'PATCH', body: JSON.stringify(payload()) });
      populate(updated);
      location.href = '/cameras?updated=1';
    } catch (error) { q('#saveStatus').textContent = error.message; notify(error.message, 'danger'); }
    finally { button.disabled = false; }
  });

  q('#deleteCameraButton').addEventListener('click', async (event) => {
    if (!confirm(`¿Eliminar ${camera.name}? Las grabaciones existentes no se borrarán.`)) return;
    const button = event.currentTarget;
    button.disabled = true;
    try {
      await api(`/api/cameras/${encodeURIComponent(cameraID)}`, { method: 'DELETE' });
      location.href = '/cameras';
    } catch (error) { notify(error.message, 'danger'); button.disabled = false; }
  });

  async function init() {
    await initLayout('Configuración avanzada del dispositivo');
    populate(await api(`/api/cameras/${encodeURIComponent(cameraID)}`));
  }
  init().catch((error) => { q('#saveStatus').textContent = error.message; notify(error.message, 'danger'); });
})();
