const form = document.querySelector('#loginForm');
const statusEl = document.querySelector('#loginStatus');
const togglePassword = document.querySelector('#togglePassword');
const passwordInput = document.querySelector('#loginPassword');

togglePassword.addEventListener('click', () => {
  const show = passwordInput.type === 'password';
  passwordInput.type = show ? 'text' : 'password';
  togglePassword.innerHTML = `<i class="bi ${show ? 'bi-eye-slash' : 'bi-eye'}"></i>`;
  togglePassword.setAttribute('aria-label', show ? 'Ocultar contraseña' : 'Mostrar contraseña');
});

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  if (!form.reportValidity()) return;
  const button = form.querySelector('button[type=submit]');
  button.disabled = true;
  button.querySelector('.button-label').classList.add('hidden');
  button.querySelector('.button-loading').classList.remove('hidden');
  statusEl.classList.add('hidden');
  const data = Object.fromEntries(new FormData(form));
  try {
    const response = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || 'No se pudo acceder');
    location.href = '/';
  } catch (error) {
    statusEl.textContent = error.message;
    statusEl.classList.remove('hidden');
    button.disabled = false;
    button.querySelector('.button-label').classList.remove('hidden');
    button.querySelector('.button-loading').classList.add('hidden');
  }
});
