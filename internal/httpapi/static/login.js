const form = document.querySelector('#loginForm');
const statusEl = document.querySelector('#loginStatus');
const togglePassword = document.querySelector('#togglePassword');
const passwordInput = document.querySelector('#loginPassword');
const submitButton = form.querySelector('button[type=submit]');
let lockTimer = null;

function setLoading(loading) {
  submitButton.disabled = loading;
  submitButton.querySelector('.button-label').classList.toggle('hidden', loading);
  submitButton.querySelector('.button-loading').classList.toggle('hidden', !loading);
}

function showLockout(message, retryAfter) {
  let remaining = Math.max(1, Number.parseInt(retryAfter || '60', 10) || 60);
  clearInterval(lockTimer);
  const render = () => {
    statusEl.textContent = `${message} Podrás intentarlo nuevamente en ${remaining} s.`;
    statusEl.classList.remove('hidden');
    submitButton.disabled = true;
  };
  render();
  lockTimer = setInterval(() => {
    remaining -= 1;
    if (remaining <= 0) {
      clearInterval(lockTimer);
      lockTimer = null;
      statusEl.classList.add('hidden');
      submitButton.disabled = false;
      submitButton.querySelector('.button-label').classList.remove('hidden');
      submitButton.querySelector('.button-loading').classList.add('hidden');
      return;
    }
    render();
  }, 1000);
}

togglePassword.addEventListener('click', () => {
  const show = passwordInput.type === 'password';
  passwordInput.type = show ? 'text' : 'password';
  togglePassword.innerHTML = `<i class="bi ${show ? 'bi-eye-slash' : 'bi-eye'}"></i>`;
  togglePassword.setAttribute('aria-label', show ? 'Ocultar contraseña' : 'Mostrar contraseña');
});

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  if (!form.reportValidity() || lockTimer) return;
  setLoading(true);
  statusEl.classList.add('hidden');
  const data = Object.fromEntries(new FormData(form));
  try {
    const response = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(body.error || 'No se pudo acceder');
      error.status = response.status;
      error.retryAfter = response.headers.get('Retry-After');
      throw error;
    }
    location.href = '/';
  } catch (error) {
    if (error.status === 429) {
      showLockout(error.message, error.retryAfter);
      return;
    }
    statusEl.textContent = error.message;
    statusEl.classList.remove('hidden');
    setLoading(false);
  }
});
