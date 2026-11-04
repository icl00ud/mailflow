import type { FlowView, Me, OrderView, Passkey } from "@/types";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

function redirectToLogin() {
  if (window.location.pathname.startsWith("/login")) return;
  const next = encodeURIComponent(window.location.pathname + window.location.search);
  window.location.assign(`/login?next=${next}`);
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(path, { credentials: "same-origin", ...init, headers });
  if (res.status === 401 && !path.startsWith("/api/passkeys/login")) {
    redirectToLogin();
    throw new ApiError(401, "Sessão expirada. Entre novamente.");
  }
  if (!res.ok) {
    let message = res.statusText || "Erro inesperado";
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(res.status, translateError(message, res.status));
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function translateError(message: string, status: number): string {
  const map: Record<string, string> = {
    unauthorized: "Sua sessão expirou. Entre novamente.",
    "unknown store": "Loja não encontrada.",
    "unknown order": "Pedido não encontrado.",
    "unknown job": "Envio não encontrado.",
    "unknown passkey": "Passkey não encontrada.",
  };
  if (map[message]) return map[message];
  if (status === 403) return "Ação bloqueada: requisição de outra origem.";
  if (status === 404) return "Não encontrado.";
  if (status === 429) return "Muitas tentativas. Tente novamente em alguns minutos.";
  return message;
}

export const api = {
  me: () => request<Me>("/api/me"),
  flows: () => request<FlowView[]>("/api/flows"),
  orders: (store?: string) =>
    request<OrderView[]>(`/api/orders${store ? `?store=${encodeURIComponent(store)}` : ""}`),
  setStepStatus: (store: string, step: string, status: "on" | "off") =>
    request<{ status: string }>(
      `/api/flows/${encodeURIComponent(store)}/steps/${encodeURIComponent(step)}/status`,
      { method: "POST", body: JSON.stringify({ status }) },
    ),
  previewUrl: (store: string, step: string) =>
    `/api/flows/${encodeURIComponent(store)}/steps/${encodeURIComponent(step)}/preview`,
  testSend: (store: string, step: string, to: string) =>
    request<{ sent_to: string; via: string }>(
      `/api/flows/${encodeURIComponent(store)}/steps/${encodeURIComponent(step)}/test`,
      { method: "POST", body: JSON.stringify({ to }) },
    ),
  createOrder: (input: {
    store: string;
    email: string;
    name?: string;
    customer?: string;
    item?: string;
    total?: string;
  }) => request<OrderView>("/api/orders", { method: "POST", body: JSON.stringify(input) }),
  setTracking: (orderId: number, input: { number: string; carrier?: string; url?: string }) =>
    request<{ ok: boolean }>(`/api/orders/${orderId}/tracking`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  addEvent: (orderId: number, input: { event: string; label: string }) =>
    request<{ ok: boolean }>(`/api/orders/${orderId}/events`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  cancelOrder: (orderId: number) =>
    request<{ ok: boolean }>(`/api/orders/${orderId}/cancel`, { method: "POST", body: "{}" }),
  retryJob: (jobId: number) =>
    request<{ ok: boolean }>(`/api/jobs/${jobId}/retry`, { method: "POST", body: "{}" }),
  passkeys: () => request<Passkey[]>("/api/passkeys"),
  deletePasskey: (id: string) =>
    request<{ ok: boolean }>(`/api/passkeys/${encodeURIComponent(id)}/delete`, {
      method: "POST",
      body: "{}",
    }),
};
