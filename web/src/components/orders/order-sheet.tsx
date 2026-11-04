import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Ban,
  Check,
  ExternalLink,
  Loader2,
  MapPin,
  Plus,
  Radar,
  RotateCcw,
  Truck,
} from "lucide-react";
import { toast } from "sonner";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { JobStatusBadge, OrderStatusBadge } from "@/components/status-badges";
import { api } from "@/lib/api";
import {
  countLabel,
  formatDateTime,
  trackingEventLabel,
} from "@/lib/format";
import { cn } from "@/lib/utils";
import type { OrderView, StepView } from "@/types";

const eventOptions = ["in_transit", "arrived_country", "out_for_delivery", "ready_for_pickup", "delivered", "exception"];

function Meta({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <p className="text-[11px] text-muted-foreground">{label}</p>
      <div className="mt-0.5 truncate text-sm">{value}</div>
    </div>
  );
}

export function OrderSheet({
  order,
  steps,
  open,
  onOpenChange,
}: {
  order: OrderView | undefined;
  steps: StepView[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [trackingOpen, setTrackingOpen] = useState(false);
  const [eventOpen, setEventOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [number, setNumber] = useState("");
  const [carrier, setCarrier] = useState("");
  const [url, setUrl] = useState("");
  const [event, setEvent] = useState("in_transit");
  const [eventLabel, setEventLabel] = useState("");

  useEffect(() => {
    if (trackingOpen && order) {
      setNumber(order.tracking_number ?? "");
      setCarrier(order.carrier ?? "");
      setUrl(order.tracking_url ?? "");
    }
  }, [trackingOpen, order]);

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ["orders"] });
    void qc.invalidateQueries({ queryKey: ["flows"] });
  };

  const trackingMutation = useMutation({
    mutationFn: () =>
      api.setTracking(order!.id, {
        number: number.trim(),
        carrier: carrier.trim() || undefined,
        url: url.trim() || undefined,
      }),
    onSuccess: () => {
      toast.success("Rastreio anexado", { description: "O código já está disponível para os passos retidos." });
      setTrackingOpen(false);
      invalidate();
    },
    onError: (e: Error) => toast.error("Não foi possível anexar", { description: e.message }),
  });

  const eventMutation = useMutation({
    mutationFn: () =>
      api.addEvent(order!.id, {
        event,
        label: eventLabel.trim() || trackingEventLabel[event] || event,
      }),
    onSuccess: () => {
      toast.success("Evento de rastreio registrado", { description: trackingEventLabel[event] });
      setEventOpen(false);
      setEventLabel("");
      invalidate();
    },
    onError: (e: Error) => toast.error("Não foi possível registrar", { description: e.message }),
  });

  const cancelMutation = useMutation({
    mutationFn: () => api.cancelOrder(order!.id),
    onSuccess: () => {
      toast.success("Jornada cancelada", { description: "Todos os envios pendentes foram cancelados." });
      setCancelOpen(false);
      invalidate();
    },
    onError: (e: Error) => toast.error("Não foi possível cancelar", { description: e.message }),
  });

  const retryMutation = useMutation({
    mutationFn: (jobId: number) => api.retryJob(jobId),
    onSuccess: () => {
      toast.success("Envio reenfileirado");
      invalidate();
    },
    onError: (e: Error) => toast.error("Não foi possível reenviar", { description: e.message }),
  });

  const stepOrder = useMemo(() => {
    const map = new Map<string, number>();
    steps.forEach((s, i) => map.set(s.id, i));
    return map;
  }, [steps]);

  const jobs = useMemo(() => {
    if (!order?.jobs) return [];
    return [...order.jobs].sort((a, b) => {
      const ai = stepOrder.get(a.step_id) ?? 99;
      const bi = stepOrder.get(b.step_id) ?? 99;
      if (ai !== bi) return ai - bi;
      return a.due_at.localeCompare(b.due_at);
    });
  }, [order, stepOrder]);

  if (!order) return null;

  const stepTitle = (id: string) => steps.find((s) => s.id === id)?.title ?? `Passo ${id}`;

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent side="right" className="w-full p-0 sm:max-w-[570px]">
          <SheetHeader className="pr-12">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs text-muted-foreground">#{order.id}</span>
              <OrderStatusBadge order={order} />
              {order.canceled && <Badge variant="muted">Cancelado</Badge>}
            </div>
            <SheetTitle className="text-xl">{order.name}</SheetTitle>
            <SheetDescription className="leading-relaxed">
              {order.email} · {order.customer_name || "cliente sem nome"} · pago em {formatDateTime(order.paid_at)}
            </SheetDescription>
          </SheetHeader>

          <ScrollArea className="min-h-0 flex-1">
            <div className="space-y-6 px-6 py-5">
              <div className="grid grid-cols-2 gap-x-4 gap-y-3 rounded-xl border bg-muted/25 p-4">
                <Meta label="Total" value={order.total || "—"} />
                <Meta label="País" value={order.country || "—"} />
                <Meta
                  label="Itens"
                  value={
                    order.items?.length
                      ? order.items.map((i) => `${i.qty}× ${i.title}`).join(", ")
                      : "—"
                  }
                />
                <Meta label="Store" value={<span className="font-mono text-xs">{order.store_id}</span>} />
                <div className="col-span-2">
                  <p className="text-[11px] text-muted-foreground">Rastreio</p>
                  {order.tracking_number ? (
                    <div className="mt-0.5 flex flex-wrap items-center gap-2 text-sm">
                      <Truck className="h-3.5 w-3.5 text-success" />
                      <span className="font-mono">{order.tracking_number}</span>
                      {order.carrier && <Badge variant="muted">{order.carrier}</Badge>}
                      {order.tracking_url && (
                        <a
                          href={order.tracking_url}
                          target="_blank"
                          rel="noreferrer"
                          className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
                        >
                          abrir <ExternalLink className="h-3 w-3" />
                        </a>
                      )}
                    </div>
                  ) : (
                    <p className="mt-0.5 text-sm text-muted-foreground">Nenhum código anexado.</p>
                  )}
                </div>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" onClick={() => setTrackingOpen(true)}>
                  <Truck className="h-3.5 w-3.5" />
                  {order.tracking_number ? "Alterar rastreio" : "Anexar rastreio"}
                </Button>
                <Button size="sm" variant="outline" onClick={() => setEventOpen(true)}>
                  <Plus className="h-3.5 w-3.5" />
                  Evento de rastreio
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="text-destructive hover:text-destructive"
                  disabled={order.canceled}
                  onClick={() => setCancelOpen(true)}
                >
                  <Ban className="h-3.5 w-3.5" />
                  Cancelar jornada
                </Button>
              </div>

              <section>
                <div className="mb-3 flex items-center justify-between">
                  <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                    Linha do tempo da jornada
                  </h4>
                  <span className="text-[11px] text-muted-foreground">{countLabel(jobs.length, "envio", "envios")}</span>
                </div>
                {jobs.length === 0 ? (
                  <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
                    Nenhum envio agendado para este pedido.
                  </div>
                ) : (
                  <ol className="relative ml-1 space-y-4 border-l pl-4">
                    {jobs.map((job) => (
                      <li key={job.id} className="relative">
                        <span
                          className={cn(
                            "absolute -left-[21px] top-1.5 h-2.5 w-2.5 rounded-full border-2 border-background",
                            job.status === "sent" && "bg-success",
                            job.status === "pending" && !job.held && "bg-info",
                            job.status === "pending" && job.held && "bg-warning",
                            job.status === "failed" && "bg-destructive",
                            job.status === "skipped" && "bg-muted-foreground/40",
                            job.status === "canceled" && "bg-muted-foreground/30",
                            job.status === "sending" && "bg-info animate-pulse",
                          )}
                        />
                        <div className="flex flex-wrap items-start justify-between gap-2">
                          <div className="min-w-0">
                            <p className="text-sm font-medium">
                              <span className="mr-2 font-mono text-[11px] text-muted-foreground">{job.step_id}</span>
                              {stepTitle(job.step_id)}
                            </p>
                            <p className="mt-0.5 text-[11px] text-muted-foreground">
                              Vence {formatDateTime(job.due_at)}
                              {job.sent_at && ` · enviado ${formatDateTime(job.sent_at)}`}
                            </p>
                            {job.reason && (
                              <p
                                className={cn(
                                  "mt-1 text-[11px]",
                                  job.status === "failed" ? "text-destructive" : "text-muted-foreground",
                                )}
                              >
                                {job.reason}
                              </p>
                            )}
                          </div>
                          <div className="flex shrink-0 items-center gap-1.5">
                            <JobStatusBadge status={job.status} held={job.held} />
                            {job.status === "failed" && (
                              <Button
                                size="icon-sm"
                                variant="ghost"
                                title="Reenviar"
                                onClick={() => retryMutation.mutate(job.id)}
                                disabled={retryMutation.isPending && retryMutation.variables === job.id}
                              >
                                <RotateCcw className="h-3.5 w-3.5" />
                              </Button>
                            )}
                          </div>
                        </div>
                      </li>
                    ))}
                  </ol>
                )}
              </section>

              <Separator />

              <section>
                <div className="mb-3 flex items-center justify-between">
                  <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                    Eventos de rastreio
                  </h4>
                  <span className="text-[11px] text-muted-foreground">{order.events?.length ?? 0}</span>
                </div>
                {(order.events?.length ?? 0) === 0 ? (
                  <p className="rounded-lg border border-dashed px-4 py-6 text-center text-xs text-muted-foreground">
                    Nenhum evento recebido. Anexe um rastreio ou registre um evento manualmente.
                  </p>
                ) : (
                  <ul className="space-y-2">
                    {[...(order.events ?? [])]
                      .sort((a, b) => b.at.localeCompare(a.at))
                      .map((ev) => (
                        <li key={ev.id} className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2.5">
                          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-info/10 text-info">
                            <Radar className="h-3.5 w-3.5" />
                          </span>
                          <div className="min-w-0 flex-1">
                            <p className="text-sm font-medium">{trackingEventLabel[ev.event] ?? ev.event}</p>
                            <p className="truncate text-[11px] text-muted-foreground">
                              {ev.label || "sem descrição"} · via {ev.source}
                            </p>
                          </div>
                          <span className="shrink-0 text-[11px] text-muted-foreground">{formatDateTime(ev.at)}</span>
                        </li>
                      ))}
                  </ul>
                )}
              </section>
            </div>
          </ScrollArea>
        </SheetContent>
      </Sheet>

      <Dialog open={trackingOpen} onOpenChange={setTrackingOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Anexar código de rastreio</DialogTitle>
            <DialogDescription>
              Anexar um código acorda os envios retidos por “aguarda rastreio”. O e-mail de enviado sai na próxima
              varredura.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!number.trim()) return;
              trackingMutation.mutate();
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor="tracking-number">Código</Label>
              <Input
                id="tracking-number"
                value={number}
                onChange={(e) => setNumber(e.target.value)}
                placeholder="LP00123456789012"
                autoFocus
                required
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="tracking-carrier">Transportadora</Label>
                <Input
                  id="tracking-carrier"
                  value={carrier}
                  onChange={(e) => setCarrier(e.target.value)}
                  placeholder="YunExpress"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="tracking-url">URL de rastreio</Label>
                <Input
                  id="tracking-url"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="https://…"
                />
              </div>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setTrackingOpen(false)}>
                Cancelar
              </Button>
              <Button type="submit" disabled={trackingMutation.isPending || !number.trim()}>
                {trackingMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                Anexar rastreio
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={eventOpen} onOpenChange={setEventOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Registrar evento de rastreio</DialogTitle>
            <DialogDescription>
              Simula um evento da transportadora. Os passos ligados a esse evento são agendados agora.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              eventMutation.mutate();
            }}
          >
            <div className="space-y-1.5">
              <Label>Evento</Label>
              <Select value={event} onValueChange={setEvent}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {eventOptions.map((e) => (
                    <SelectItem key={e} value={e}>
                      {trackingEventLabel[e]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="event-label">Descrição do carrier (opcional)</Label>
              <Input
                id="event-label"
                value={eventLabel}
                onChange={(e) => setEventLabel(e.target.value)}
                placeholder="Ex.: Arrived at destination country"
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEventOpen(false)}>
                Cancelar
              </Button>
              <Button type="submit" disabled={eventMutation.isPending}>
                {eventMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <MapPin className="h-4 w-4" />}
                Registrar evento
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={cancelOpen} onOpenChange={setCancelOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Cancelar a jornada deste pedido?</DialogTitle>
            <DialogDescription>
              Todos os envios pendentes de <span className="font-medium text-foreground">{order.name}</span> serão
              cancelados. Os e-mails já enviados não podem ser revertidos.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCancelOpen(false)}>
              Voltar
            </Button>
            <Button variant="destructive" onClick={() => cancelMutation.mutate()} disabled={cancelMutation.isPending}>
              {cancelMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Ban className="h-4 w-4" />}
              Cancelar jornada
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
