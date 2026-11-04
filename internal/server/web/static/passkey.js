// Usernameless passkey sign-in for the server-rendered login page. The account
// page is a React route and uses src/lib/passkey.ts. No build step here.
(() => {
  const enc = (buf) => {
    const bytes = new Uint8Array(buf);
    let s = "";
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  };
  const dec = (s) => {
    const b64 = s.replace(/-/g, "+").replace(/_/g, "/");
    const raw = atob(b64.padEnd(b64.length + ((4 - (b64.length % 4)) % 4), "="));
    const out = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  };
  const el = (id) => document.getElementById(id);
  const say = (msg) => {
    const m = el("passkey-msg");
    if (m) m.textContent = msg;
  };

  async function post(path, body) {
    const r = await fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || r.statusText);
    return j;
  }

  const loginBtn = el("passkey-login");
  if (!loginBtn) return;

  if (!window.PublicKeyCredential) {
    loginBtn.disabled = true;
    say("Este navegador não suporta passkeys. Entre com sua senha.");
    return;
  }

  loginBtn.addEventListener("click", async () => {
    loginBtn.disabled = true;
    say("Aguardando a confirmação do dispositivo…");
    try {
      const { challenge_id, options } = await post("/api/passkeys/login/begin", {});
      const pk = options.publicKey;
      pk.challenge = dec(pk.challenge);
      if (pk.allowCredentials) {
        pk.allowCredentials.forEach((c) => {
          c.id = dec(c.id);
        });
      }
      const cred = await navigator.credentials.get({ publicKey: pk });
      if (!cred) throw new Error("nenhuma credencial retornada");
      const payload = {
        id: cred.id,
        rawId: enc(cred.rawId),
        type: cred.type,
        response: {
          clientDataJSON: enc(cred.response.clientDataJSON),
          authenticatorData: enc(cred.response.authenticatorData),
          signature: enc(cred.response.signature),
          userHandle: cred.response.userHandle ? enc(cred.response.userHandle) : null,
        },
        clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
      };
      const done = await post("/api/passkeys/login/finish?cid=" + encodeURIComponent(challenge_id), payload);
      say("Tudo certo. Entrando…");
      location.href = done.redirect || "/";
    } catch (e) {
      const msg =
        e && e.name === "NotAllowedError"
          ? "A confirmação foi cancelada ou expirou. Tente novamente."
          : e.message || "erro desconhecido";
      say("Não foi possível entrar: " + msg);
      loginBtn.disabled = false;
    }
  });
})();
