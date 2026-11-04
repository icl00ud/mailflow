import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  ExternalLink,
  Info,
  MailCheck,
  Power,
  Send,
  Timer,
} from "lucide-react";
import { toast } from "sonner";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { FlowStatusBadge } from "@/components/status-badges";
import { api } from "@/lib/api";
import {
  conditionInfo,
  delayLabel,
  scheduledOf,
  timingLabel,
  trackingEventLabel,
  triggerLabel,
} from "@/lib/format";
import { cn } from "@/lib/utils";
import type { StepView } from "@/types";

const countCards = [
  { key: "scheduled", label: "Agendados", singular: "Agendado", className: "text-info" },
  { key: "sent", label: "Enviados", singular: "Enviado", className: "text-success" },
  { key: "held", label: "Retidos", singular: "Retido", className: "text-warning" },
  { key: "skipped", label: "Pulados", singular: "Pulado", className: "text-muted-foreground" },
  { key: "failed", label: "Falhas", singular: "Falhou", className: "text-destructive" },
] as const;

export function StepSheet({
  step,
  storeId,
  sender,
  open,
  onOpenChange,
}: {
  step: StepView | undefined;
  storeId: string;
  sender: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [frameKey, setFrameKey] = useState(0);

  useEffect(() => {
    if (open) {
      setEmail("");
      setFrameKey((k) => k + 1);
    }
  }, [open, step?.id]);

  const statusMutation = useMutation({
    mutationFn: (status: "on" | "off") => api.setStepStatus(storeId, step!.id, status),
    onSuccess: (_, status) => {
      toast.success(status === "on" ? "Passo ativado" : "Passo pausado", {
        description: `Passo ${step?.id} — ${step?.title}`,
      });
      void qc.invalidateQueries({ queryKey: ["flows"] });
    },
    onError: (e: Error) => toast.error("Não foi possível alterar o passo", { description: e.message }),
  });

  const testMutation = useMutation({
    mutationFn: ({ to }: { to: string }) => api.testSend(storeId, step!.id, to),
    onSuccess: (res) => {
      toast.success("E-mail de teste enviado", { description: `Para ${res.sent_to} via ${res.via}` });
      setEmail("");
    },
    onError: (e: Error) => toast.error("Falha ao enviar teste", { description: e.message }),
  });

  if (!step) return null;

  const counts = {
    scheduled: scheduledOf(step),
    sent: step.counts?.sent ?? 0,
    held: step.held ?? 0,
    skipped: step.counts?.skipped ?? 0,
    failed: step.counts?.failed ?? 0,
  };
  const isDraft = step.effective === "draft";
  const previewUrl = api.previewUrl(storeId, step.id);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full p-0 sm:max-w-[560px]">
        <SheetHeader className="pr-12">
          <div className="flex items-center gap-2">
            <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] font-medium text-muted-foreground">
              Passo {step.id}
            </span>
            <FlowStatusBadge status={step.effective} />
          </div>
          <SheetTitle className="text-xl leading-snug">{step.title}</SheetTitle>
          <SheetDescription className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="inline-flex items-center gap-1">
              <Clock3 className="h-3.5 w-3.5" />
              {timingLabel(step)}
            </span>
            <span className="inline-flex items-center gap-1">
              <Send className="h-3.5 w-3.5" />
              {triggerLabel(step.trigger)}
            </span>
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-6 px-6 py-5">
            <div
              className={cn(
                "flex items-start justify-between gap-4 rounded-xl border p-4",
                isDraft ? "bg-muted/40" : step.effective === "on" ? "bg-success/5" : "bg-muted/30",
              )}
            >
              <div className="flex gap-3">
                <span
                  className={cn(
                    "flex h-9 w-9 shrink-0 items-center justify-center rounded-lg",
                    isDraft
                      ? "bg-muted text-muted-foreground"
                      : step.effective === "on"
                        ? "bg-success/10 text-success"
                        : "bg-muted text-muted-foreground",
                  )}
                >
                  {isDraft ? <AlertCircle className="h-4 w-4" /> : <Power className="h-4 w-4" />}
                </span>
                <div>
                  <p className="text-sm font-medium">
                    {isDraft ? "Rascunho — nunca envia" : step.effective === "on" ? "Envio ativo" : "Envio pausado"}
                  </p>
                  <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                    {isDraft
                      ? "A copy deste passo ainda não foi escrita. Publique o template e mude o status no flow.yaml."
                      : step.effective === "on"
                        ? "Os jobs deste passo são processados normalmente na janela de envio da loja."
                        : "Jobs já agendados serão pulados enquanto o passo estiver pausado."}
                  </p>
                </div>
              </div>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    <Switch
                      checked={step.effective === "on"}
                      disabled={isDraft || statusMutation.isPending}
                      onCheckedChange={(checked) => statusMutation.mutate(checked ? "on" : "off")}
                      aria-label="Ativar passo"
                    />
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {isDraft ? "Passo em rascunho não pode ser ativado sem copy." : "Ativar ou pausar este passo."}
                </TooltipContent>
              </Tooltip>
            </div>

            <section className="space-y-2">
              <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Assunto</h4>
              <p className="rounded-lg border bg-muted/30 px-3 py-2.5 text-sm leading-relaxed">
                {step.subject || <span className="text-muted-foreground">Sem assunto (rascunho)</span>}
              </p>
              <div className="grid grid-cols-2 gap-3 pt-1 text-xs">
                <div>
                  <p className="text-muted-foreground">Template</p>
                  <p className="mt-0.5 font-mono text-foreground/90">{step.template || "—"}</p>
                </div>
                <div>
                  <p className="text-muted-foreground">Atraso</p>
                  <p className="mt-0.5 text-foreground/90">
                    {delayLabel(step)}
                    {step.immediate && " · ignora janela de envio"}
                  </p>
                </div>
                {step.trigger.on === "tracking" && (
                  <div className="col-span-2">
                    <p className="text-muted-foreground">Evento de rastreio</p>
                    <p className="mt-0.5 text-foreground/90">
                      {trackingEventLabel[step.trigger.event ?? ""] ?? step.trigger.event}
                    </p>
                  </div>
                )}
              </div>
            </section>

            {(step.conditions?.length ?? 0) > 0 && (
              <section className="space-y-2">
                <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Condições</h4>
                <div className="flex flex-col gap-2">
                  {step.conditions?.map((c) => {
                    const info = conditionInfo(c, step);
                    return (
                      <Tooltip key={c}>
                        <TooltipTrigger asChild>
                          <div className="flex cursor-help items-start gap-2.5 rounded-lg border bg-card px-3 py-2 text-left">
                            <Badge variant={info.kind === "hold" ? "warning" : "muted"} className="mt-0.5 shrink-0">
                              {info.kind === "hold" ? "Aguarda" : "Pula"}
                            </Badge>
                            <div className="min-w-0">
                              <p className="text-sm font-medium">{info.label}</p>
                              <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{info.explanation}</p>
                            </div>
                            <Info className="ml-auto mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground/60" />
                          </div>
                        </TooltipTrigger>
                        <TooltipContent>{info.explanation}</TooltipContent>
                      </Tooltip>
                    );
                  })}
                </div>
              </section>
            )}

            {step.note && (
              <section className="rounded-lg border-l-2 border-primary/50 bg-primary/5 px-3.5 py-3">
                <p className="text-xs font-medium text-primary">Nota de operação</p>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{step.note}</p>
              </section>
            )}

            <section>
              <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                Envios neste passo
              </h4>
              <div className="grid grid-cols-5 divide-x rounded-lg border">
                {countCards.map((c) => (
                  <div key={c.key} className="px-2 py-2.5 text-center">
                    <p className={cn("text-lg font-semibold tabular-nums", counts[c.key] ? c.className : "text-muted-foreground/40")}>
                      {counts[c.key]}
                    </p>
                    <p className="mt-0.5 text-[10px] text-muted-foreground">
                      {counts[c.key] === 1 ? c.singular : c.label}
                    </p>
                  </div>
                ))}
              </div>
            </section>

            <Separator />

            <section className="space-y-2">
              <div className="flex items-center justify-between">
                <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Prévia do e-mail</h4>
                {!isDraft && (
                  <Button asChild variant="ghost" size="sm" className="h-7 text-xs">
                    <a href={previewUrl} target="_blank" rel="noreferrer">
                      Nova aba <ExternalLink className="h-3 w-3" />
                    </a>
                  </Button>
                )}
              </div>
              {isDraft ? (
                <div className="flex flex-col items-center justify-center rounded-xl border border-dashed px-6 py-12 text-center">
                  <MailCheck className="h-5 w-5 text-muted-foreground" />
                  <p className="mt-2 text-sm font-medium">Sem prévia</p>
                  <p className="mt-1 max-w-xs text-xs text-muted-foreground">
                    Este passo é um rascunho: o template ainda não foi escrito.
                  </p>
                </div>
              ) : (
                <div className="overflow-hidden rounded-xl border bg-white shadow-sm">
                  <div className="flex items-center gap-2 border-b bg-muted/40 px-3 py-2">
                    <span className="flex gap-1">
                      <span className="h-2 w-2 rounded-full bg-destructive/50" />
                      <span className="h-2 w-2 rounded-full bg-warning/60" />
                      <span className="h-2 w-2 rounded-full bg-success/50" />
                    </span>
                    <p className="truncate text-[11px] text-muted-foreground">De: {sender}</p>
                  </div>
                  <iframe
                    key={frameKey}
                    title={`Prévia do passo ${step.id}`}
                    src={previewUrl}
                    sandbox=""
                    referrerPolicy="no-referrer"
                    loading="lazy"
                    className="h-[380px] w-full border-0 bg-white"
                  />
                </div>
              )}
              <p className="text-[11px] text-muted-foreground">
                HTML renderizado em iframe isolado (sandbox sem scripts). Nenhum código do template roda no painel.
              </p>
            </section>

            {!isDraft && (
              <section className="space-y-2 rounded-xl border bg-muted/25 p-4">
                <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  Enviar e-mail de teste
                </h4>
                <p className="text-xs text-muted-foreground">
                  Renderiza este passo com o pedido de exemplo e envia para o endereço abaixo.
                </p>
                <form
                  className="flex flex-col gap-2 sm:flex-row"
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (!email.trim()) return;
                    testMutation.mutate({ to: email.trim() });
                  }}
                >
                  <Input
                    type="email"
                    required
                    placeholder="voce@exemplo.com"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    className="bg-background"
                  />
                  <Button type="submit" disabled={testMutation.isPending || !email.trim()}>
                    {testMutation.isPending ? <Timer className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                    Enviar teste
                  </Button>
                </form>
              </section>
            )}

            {step.effective === "draft" && (
              <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-muted-foreground">
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                Para publicar: escreva o template, defina assunto e mude o status para "on" no flow.yaml.
              </div>
            )}
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}
