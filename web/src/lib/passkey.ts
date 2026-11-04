import type { Passkey } from "@/types";
import { ApiError } from "@/lib/api";

export function b64urlEncode(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function b64urlDecode(value: string): Uint8Array {
  const b64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(b64.padEnd(b64.length + ((4 - (b64.length % 4)) % 4), "="));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

async function post<T>(path: string, body: unknown = {}): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = (await res.json().catch(() => ({}))) as { error?: string };
  if (!res.ok) throw new ApiError(res.status, data.error || res.statusText);
  return data as T;
}

interface BeginResponse {
  challenge_id: string;
  options: { publicKey: PublicKeyCredentialCreationOptions };
}

export function passkeysSupported(): boolean {
  return typeof window !== "undefined" && !!window.PublicKeyCredential;
}

export async function registerPasskey(name: string): Promise<Passkey> {
  const begin = await post<BeginResponse>("/api/passkeys/register/begin", {});
  const raw = begin.options.publicKey as unknown as {
    challenge: string;
    user: { id: string; [key: string]: unknown };
    excludeCredentials?: { id: string; [key: string]: unknown }[];
    [key: string]: unknown;
  };

  raw.challenge = b64urlDecode(raw.challenge) as unknown as string;
  raw.user.id = b64urlDecode(raw.user.id) as unknown as string;
  if (raw.excludeCredentials) {
    raw.excludeCredentials = raw.excludeCredentials.map((c) => ({
      ...c,
      id: b64urlDecode(c.id) as unknown as string,
    }));
  }

  const credential = (await navigator.credentials.create({
    publicKey: raw as unknown as PublicKeyCredentialCreationOptions,
  })) as PublicKeyCredential | null;
  if (!credential) throw new Error("O navegador não retornou uma credencial.");
  const response = credential.response as AuthenticatorAttestationResponse;

  const payload = {
    id: credential.id,
    rawId: b64urlEncode(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: b64urlEncode(response.clientDataJSON),
      attestationObject: b64urlEncode(response.attestationObject),
      transports: response.getTransports ? response.getTransports() : [],
    },
    clientExtensionResults: credential.getClientExtensionResults(),
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
  };

  const query = `cid=${encodeURIComponent(begin.challenge_id)}&name=${encodeURIComponent(name.trim())}`;
  return post<Passkey>(`/api/passkeys/register/finish?${query}`, payload);
}
