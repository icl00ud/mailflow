// Passkey ceremonies. Vanilla navigator.credentials with base64url <-> ArrayBuffer,
// no build step. Loaded by login.html (sign in) and account.html (register/list/delete).
(() => {
  const enc = buf => {
    const bytes = new Uint8Array(buf);
    let s = '';
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  };
  const dec = s => {
    const b64 = s.replace(/-/g, '+').replace(/_/g, '/');
    const raw = atob(b64.padEnd(b64.length + (4 - b64.length % 4) % 4, '='));
    const out = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  };
  const el = id => document.getElementById(id);
  const say = msg => { const m = el('passkey-msg'); if (m) m.textContent = msg; };

  async function post(path, body) {
    const r = await fetch(path, {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || r.statusText);
    return j;
  }

  // ---- sign in (usernameless / discoverable) ----
  const loginBtn = el('passkey-login');
  if (loginBtn) {
    if (!window.PublicKeyCredential) {
      loginBtn.disabled = true;
      say('Este navegador não suporta passkeys.');
    }
    loginBtn.addEventListener('click', async () => {
      loginBtn.disabled = true;
      try {
        const { challenge_id, options } = await post('/api/passkeys/login/begin', {});
        const pk = options.publicKey;
        pk.challenge = dec(pk.challenge);
        if (pk.allowCredentials) pk.allowCredentials.forEach(c => { c.id = dec(c.id); });
        const cred = await navigator.credentials.get({ publicKey: pk });
        const payload = {
          id: cred.id, rawId: enc(cred.rawId), type: cred.type,
          response: {
            clientDataJSON: enc(cred.response.clientDataJSON),
            authenticatorData: enc(cred.response.authenticatorData),
            signature: enc(cred.response.signature),
            userHandle: cred.response.userHandle ? enc(cred.response.userHandle) : null,
          },
          clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
        };
        const done = await post('/api/passkeys/login/finish?cid=' + encodeURIComponent(challenge_id), payload);
        location.href = done.redirect || '/';
      } catch (e) {
        say('Não foi possível entrar: ' + e.message);
        loginBtn.disabled = false;
      }
    });
  }

  // ---- account: list, add, delete ----
  const addBtn = el('passkey-add');
  if (!addBtn) return;

  async function render() {
    const list = el('passkey-list');
    try {
      const keys = await fetch('/api/passkeys', { credentials: 'same-origin' }).then(r => r.json());
      list.replaceChildren();
      if (!keys.length) { list.textContent = 'Nenhuma passkey cadastrada.'; return; }
      for (const k of keys) {
        const row = document.createElement('div');
        row.className = 'rowline';
        const label = document.createElement('span');
        label.textContent = k.name;
        const meta = document.createElement('span');
        meta.className = 'muted';
        meta.textContent = (k.last_used_at ? 'usada em ' : 'criada em ') +
          new Date(k.last_used_at || k.created_at).toLocaleDateString('pt-BR');
        const del = document.createElement('button');
        del.textContent = 'remover';
        del.addEventListener('click', async () => {
          if (!confirm('Remover esta passkey?')) return;
          try {
            await post('/api/passkeys/' + encodeURIComponent(k.id) + '/delete', {});
            await render();
            say('Passkey removida.');
          } catch (e) { say('Erro: ' + e.message); }
        });
        row.append(label, meta, del);
        list.append(row);
      }
    } catch (e) { list.textContent = 'Erro ao carregar: ' + e.message; }
  }

  addBtn.addEventListener('click', async () => {
    addBtn.disabled = true;
    try {
      const name = (el('passkey-name').value || '').trim();
      const { challenge_id, options } = await post('/api/passkeys/register/begin', {});
      const pk = options.publicKey;
      pk.challenge = dec(pk.challenge);
      pk.user.id = dec(pk.user.id);
      if (pk.excludeCredentials) pk.excludeCredentials.forEach(c => { c.id = dec(c.id); });
      const cred = await navigator.credentials.create({ publicKey: pk });
      const payload = {
        id: cred.id, rawId: enc(cred.rawId), type: cred.type,
        response: {
          clientDataJSON: enc(cred.response.clientDataJSON),
          attestationObject: enc(cred.response.attestationObject),
          transports: cred.response.getTransports ? cred.response.getTransports() : [],
        },
        clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
        authenticatorAttachment: cred.authenticatorAttachment || undefined,
      };
      await post('/api/passkeys/register/finish?cid=' + encodeURIComponent(challenge_id) +
                 '&name=' + encodeURIComponent(name), payload);
      el('passkey-name').value = '';
      await render();
      say('Passkey adicionada.');
    } catch (e) {
      say('Não foi possível adicionar: ' + e.message);
    } finally {
      addBtn.disabled = false;
    }
  });

  render();
})();
