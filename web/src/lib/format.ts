import type { FlowStatus, JobStatus, StepView, Trigger } from "@/types";

export const dateTimeFmt = new Intl.DateTimeFormat("pt-BR", {
  day: "2-digit",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});

export const dateFmt = new Intl.DateTimeFormat("pt-BR", {
  day: "2-digit",
  month: "short",
  year: "numeric",
});

const relativeFmt = new Intl.RelativeTimeFormat("pt-BR", { numeric: "auto" });

export function formatDateTime(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return dateTimeFmt.format(d);
}

export function formatDate(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return dateFmt.format(d);
}

export function relativeTime(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const diffMs = d.getTime() - Date.now();
  const abs = Math.abs(diffMs);
  const minute = 60_000;
  const hour = 3_600_000;
  const day = 86_400_000;
  if (abs < minute) return "agora";
  if (abs < hour) return relativeFmt.format(Math.round(diffMs / minute), "minute");
  if (abs < day) return relativeFmt.format(Math.round(diffMs / hour), "hour");
  if (abs < 30 * day) return relativeFmt.format(Math.round(diffMs / day), "day");
  return formatDate(iso);
}

export const flowStatusLabel: Record<FlowStatus, string> = {
  on: "Ativo",
  off: "Pausado",
  draft: "Rascunho",
};

export const jobStatusLabel: Record<JobStatus, string> = {
  pending: "Agendado",
  sending: "Enviando",
  sent: "Enviado",
  skipped: "Pulado",
  failed: "Falhou",
  canceled: "Cancelado",
};

export const jobStatusOrder: JobStatus[] = ["pending", "sent", "skipped", "failed", "canceled"];

export function countLabel(n: number, singular: string, plural: string): string {
  return `${n} ${n === 1 ? singular : plural}`;
}

export function totalCounts(steps: StepView[]) {
  const acc = { pending: 0, sent: 0, skipped: 0, failed: 0, sending: 0, canceled: 0, held: 0 };
  for (const s of steps) {
    const c = s.counts ?? {};
    acc.pending += c.pending ?? 0;
    acc.sent += c.sent ?? 0;
    acc.skipped += c.skipped ?? 0;
    acc.failed += c.failed ?? 0;
    acc.sending += c.sending ?? 0;
    acc.canceled += c.canceled ?? 0;
    acc.held += s.held ?? 0;
  }
  return acc;
}

export function scheduledOf(step: StepView): number {
  return Math.max(0, (step.counts?.pending ?? 0) - (step.held ?? 0));
}

export interface ConditionMeta {
  label: string;
  short: string;
  kind: "hold" | "skip";
  explanation: string;
}

export const conditionMeta: Record<string, ConditionMeta> = {
  has_tracking: {
    label: "Aguarda código de rastreio",
    short: "Aguarda código de rastreio",
    kind: "hold",
    explanation:
      "Segura o envio até existir um código de rastreio real. Não pula: o e-mail sai assim que o código for anexado.",
  },
  no_carrier_movement: {
    label: "Sem movimento do carrier",
    short: "Sem movimento",
    kind: "skip",
    explanation:
      "Pula o envio se a transportadora já registrou qualquer movimentação. Evita dizer que o pacote está parado quando ele já andou.",
  },
  not_arrived: {
    label: "Ainda não chegou ao país",
    short: "Não chegou",
    kind: "skip",
    explanation:
      "Pula o envio se o pacote já chegou ao país de destino. Evita um e-mail desatualizado.",
  },
  not_delivered: {
    label: "Ainda não entregue",
    short: "Não entregue",
    kind: "skip",
    explanation:
      "Pula o envio se o pacote já foi entregue. O pedido é encerrado antes de um e-mail fora de contexto.",
  },
  within_window: {
    label: "Dentro da janela",
    short: "Na janela",
    kind: "skip",
    explanation:
      "Pula o envio depois do prazo configurado (contado a partir do despacho). Fora da janela o e-mail perde o sentido.",
  },
};

export function conditionInfo(id: string, step?: StepView): ConditionMeta {
  if (id === "within_window" && step?.window_days) {
    return {
      ...conditionMeta.within_window,
      label: `Dentro de ${step.window_days} dias`,
      short: `${step.window_days} dias`,
    };
  }
  return (
    conditionMeta[id] ?? {
      label: id,
      short: id,
      kind: "skip",
      explanation: id,
    }
  );
}

export const trackingEventLabel: Record<string, string> = {
  in_transit: "Em trânsito",
  arrived_country: "Chegou ao país",
  out_for_delivery: "Saiu para entrega",
  ready_for_pickup: "Aguardando retirada",
  delivered: "Entregue",
  exception: "Exceção na entrega",
};

export function triggerLabel(trigger: Trigger): string {
  switch (trigger.on) {
    case "order_paid":
      return "Pedido pago";
    case "after_step":
      return `Após o passo ${trigger.step ?? "?"}`;
    case "tracking":
      return `Rastreio · ${trackingEventLabel[trigger.event ?? ""] ?? trigger.event ?? "evento"}`;
    default:
      return trigger.on;
  }
}

export function delayLabel(step: StepView): string {
  const h = step.delay?.hours ?? 0;
  if (h <= 0) return "imediato";
  const unit = step.delay?.business ? "h úteis" : "h";
  const value = Number.isInteger(h) ? String(h) : String(h).replace(".", ",");
  return `${value} ${unit}`;
}

/** Human Portuguese timing, replacing the server's English timing string. */
export function timingLabel(step: StepView): string {
  const h = step.delay?.hours ?? 0;
  const wait = h > 0 ? delayLabel(step) : "";
  switch (step.trigger.on) {
    case "order_paid":
      return wait ? `${wait} após a compra` : "imediato após a compra";
    case "after_step":
      return wait ? `${wait} após o passo ${step.trigger.step}` : `logo após o passo ${step.trigger.step}`;
    case "tracking":
      return wait
        ? `${wait} após ${trackingEventLabel[step.trigger.event ?? ""] ?? "o evento"}`
        : "imediato no evento";
    default:
      return step.timing;
  }
}

export function orderStatus(order: {
  canceled: boolean;
  jobs: { status: JobStatus; held: boolean; due_at: string }[] | null;
}): { label: string; tone: "success" | "warning" | "danger" | "muted" | "info" } {
  if (order.canceled) return { label: "Cancelado", tone: "muted" };
  const jobs = order.jobs ?? [];
  if (jobs.some((j) => j.status === "failed")) return { label: "Falhou", tone: "danger" };
  if (jobs.some((j) => j.status === "pending" && j.held)) return { label: "Retido", tone: "warning" };
  if (jobs.some((j) => j.status === "pending" || j.status === "sending"))
    return { label: "Em andamento", tone: "info" };
  if (jobs.some((j) => j.status === "sent")) return { label: "Concluído", tone: "success" };
  return { label: "Sem envios", tone: "muted" };
}

export function currency(value: string): string {
  return value || "—";
}

/** Initials from the local part: "demo" -> D, "israel.schroeder" -> IS. */
export function initials(email: string): string {
  const parts = email.split("@")[0].split(/[._-]/).filter(Boolean);
  const value = ((parts[0]?.[0] ?? "") + (parts[1]?.[0] ?? "")).toUpperCase();
  return value || "MF";
}
