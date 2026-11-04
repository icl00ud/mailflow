import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  Clock3,
  Info,
  Mail,
  Package,
  RefreshCw,
  RotateCcw,
  ShoppingBag,
} from "lucide-react";
import { toast } from "sonner";
import { useWorkspace } from "@/components/workspace";
import { useOrders } from "@/lib/queries";
import { api } from "@/lib/api";
import { totalCounts, orderStatus, relativeTime, formatDateTime, countLabel } from "@/lib/format";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { BadgeProps } from "@/components/ui/badge";
import type { OrderView } from "@/types";

function KpiCard({
  label,
  value,
  hint,
  icon: Icon,
  tone,
}: {
  label: string;
  value: string | number;
  hint: string;
  icon: typeof Mail;
  tone: "primary" | "success" | "warning" | "danger";
}) {
  const tint = {
    primary: "bg-primary/10 text-primary",
    success: "bg-success/10 text-success",
    warning: "bg-warning/10 text-warning",
    danger: "bg-destructive/10 text-destructive",
  }[tone];
  return (
    <Card className="relative overflow-hidden">
      <div
        className={cn(
          "absolute inset-x-0 top-0 h-px",
          tone === "primary" && "bg-gradient-to-r from-transparent via-primary/50 to-transparent",
          tone === "success" && "bg-gradient-to-r from-transparent via-success/50 to-transparent",
          tone === "warning" && "bg-gradient-to-r from-transparent via-warning/50 to-transparent",
          tone === "danger" && "bg-gradient-to-r from-transparent via-destructive/50 to-transparent",
        )}
      />
      <CardContent className="p-4">
        <div className="flex items-start justify-between gap-3">
          <p className="text-xs font-medium text-muted-foreground">{label}</p>
          <span className={cn("flex h-7 w-7 shrink-0 items-center justify-center rounded-lg", tint)}>
            <Icon className="h-3.5 w-3.5" />
          </span>
        </div>
        <p className="stat-value mt-2.5">{value}</p>
        <p className="mt-0.5 text-[11px] text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  );
}

function OrderStatusBadge({ order }: { order: OrderView }) {
  const status = orderStatus(order);
  const variant = (
    {
      success: "success",
      warning: "warning",
      danger: "destructive",
      muted: "muted",
      info: "info",
    } as Record<string, BadgeProps["variant"]>
  )[status.tone];
  return <Badge variant={variant}>{status.label}</Badge>;
}

function FailedJobs() {
  const { flow } = useWorkspace();
  const ordersQuery = useOrders(flow.store.id);
  const qc = useQueryClient();
  const retry = useMutation({
    mutationFn: (id: number) => api.retryJob(id),
    onSuccess: () => {
      toast.success("Envio reenfileirado", {
        description: "O job volta para a fila e será processado na próxima varredura.",
      });
      void qc.invalidateQueries({ queryKey: ["orders"] });
      void qc.invalidateQueries({ queryKey: ["flows"] });
    },
    onError: (e: Error) => toast.error("Não foi possível reenviar", { description: e.message }),
  });

  const failed = useMemo(() => {
    const rows: { order: OrderView; job: NonNullable<OrderView["jobs"]>[number]; title: string }[] = [];
    for (const order of ordersQuery.data ?? []) {
      for (const job of order.jobs ?? []) {
        if (job.status !== "failed") continue;
        const title = flow.steps.find((s) => s.id === job.step_id)?.title ?? `Passo ${job.step_id}`;
        rows.push({ order, job, title });
      }
    }
    return rows;
  }, [ordersQuery.data, flow.steps]);

  return (
    <Card className="flex h-full min-w-0 flex-col">
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-2">
        <div>
          <CardTitle className="text-base">Precisa de atenção</CardTitle>
          <CardDescription>Envios que falharam e esperam uma decisão humana.</CardDescription>
        </div>
        <Badge variant={failed.length ? "destructive" : "success"}>{failed.length}</Badge>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-1 flex-col">
        {ordersQuery.isLoading ? (
          <div className="space-y-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : failed.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed px-6 py-8 text-center">
            <span className="flex h-10 w-10 items-center justify-center rounded-full bg-success/10 text-success">
              <CheckCircle2 className="h-5 w-5" />
            </span>
            <p className="mt-3 text-sm font-medium">Nenhuma falha na fila</p>
            <p className="mt-1 max-w-sm text-xs text-muted-foreground">
              Todos os envios desta loja estão agendados, enviados ou pulados por condição.
            </p>
          </div>
        ) : (
          <ul className="divide-y">
            {failed.slice(0, 4).map(({ order, job, title }) => (
              <li key={job.id} className="flex flex-col gap-2 py-2.5 sm:flex-row sm:items-center">
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-destructive/10 text-destructive">
                  <AlertTriangle className="h-4 w-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <p className="text-sm font-medium">{title}</p>
                    <span className="text-xs text-muted-foreground">· {order.name}</span>
                  </div>
                  <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
                    {order.email} · {job.reason || "erro não informado"}
                    {job.attempts > 0 && ` · ${countLabel(job.attempts, "tentativa", "tentativas")}`}
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  className="shrink-0"
                  onClick={() => retry.mutate(job.id)}
                  disabled={retry.isPending && retry.variables === job.id}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  Reenviar
                </Button>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-auto flex items-start gap-2 border-t pt-3 text-[11px] leading-relaxed text-muted-foreground">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          Reenviar devolve o job para a fila; o motor tenta novamente na próxima varredura, sem duplicar um e-mail já
          entregue.
        </div>
      </CardContent>
    </Card>
  );
}

function StatusStrip() {
  const { flow } = useWorkspace();
  const totals = totalCounts(flow.steps);
  const rows = [
    { key: "scheduled", label: "Agendados", short: "AGEND.", value: totals.pending - totals.held, color: "bg-info", text: "text-info" },
    { key: "sent", label: "Enviados", short: "ENV.", value: totals.sent, color: "bg-success", text: "text-success" },
    { key: "held", label: "Retidos", short: "RET.", value: totals.held, color: "bg-warning", text: "text-warning" },
    { key: "skipped", label: "Pulados", short: "PUL.", value: totals.skipped, color: "bg-muted-foreground/40", text: "text-muted-foreground" },
    { key: "failed", label: "Falhas", short: "FALHAS", value: totals.failed, color: "bg-destructive", text: "text-destructive" },
  ];
  return (
    <Card className="min-w-0">
      <CardContent className="grid grid-cols-5 divide-x p-0">
        {rows.map((row) => (
          <div key={row.key} className="min-w-0 px-1.5 py-3 text-center sm:px-4 sm:text-left">
            <p className="truncate text-[9px] font-medium uppercase tracking-wide text-muted-foreground sm:text-[10px]" title={row.label}>
              <span className="sm:hidden">{row.short}</span>
              <span className="hidden sm:inline">{row.label}</span>
            </p>
            <p className={cn("mt-0.5 text-xl font-semibold tabular-nums", row.value ? row.text : "text-muted-foreground/40")}>
              {row.value}
            </p>
            <span className={cn("mt-1.5 block h-1 w-full overflow-hidden rounded-full bg-muted")}>
              <span
                className={cn("block h-full rounded-full", row.color)}
                style={{ width: `${Math.min(100, row.value === 0 ? 0 : Math.max(8, (row.value / Math.max(1, totals.sent)) * 100))}%` }}
              />
            </span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function RecentOrders() {
  const { flow } = useWorkspace();
  const ordersQuery = useOrders(flow.store.id);
  const navigate = useNavigate();
  const orders = (ordersQuery.data ?? []).slice(0, 5);

  return (
    <Card className="h-full min-w-0">
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-2">
        <div>
          <CardTitle className="text-base">Pedidos recentes</CardTitle>
          <CardDescription>Últimas compras na jornada.</CardDescription>
        </div>
        <Button asChild variant="ghost" size="sm" className="h-7">
          <Link to="/orders">
            Ver todos <ArrowRight className="h-3.5 w-3.5" />
          </Link>
        </Button>
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {ordersQuery.isLoading ? (
          <div className="space-y-2 px-5 pb-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-11 w-full" />
            ))}
          </div>
        ) : orders.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-6 pb-8 pt-4 text-center">
            <ShoppingBag className="h-5 w-5 text-muted-foreground" />
            <p className="mt-2 text-sm font-medium">Nenhum pedido ainda</p>
            <p className="mt-1 text-xs text-muted-foreground">Crie um pedido de teste na página Pedidos.</p>
          </div>
        ) : (
          <ul className="divide-y">
            {orders.map((order) => (
              <li key={order.id}>
                <button
                  onClick={() => navigate(`/orders?order=${order.id}`)}
                  className="flex w-full items-center gap-2 px-4 py-2.5 text-left transition-colors hover:bg-muted/40"
                >
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">
                      {order.name}
                      <span className="ml-1.5 font-normal text-muted-foreground">{order.total}</span>
                    </p>
                    <p className="truncate text-[11px] text-muted-foreground">{order.email}</p>
                  </div>
                  <div className="flex shrink-0 flex-col items-end gap-1">
                    <OrderStatusBadge order={order} />
                    <span className="text-[10px] text-muted-foreground">{relativeTime(order.paid_at)}</span>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

export function DashboardPage() {
  const { flow, me } = useWorkspace();
  const ordersQuery = useOrders(flow.store.id);
  const qc = useQueryClient();
  const totals = totalCounts(flow.steps);

  const inJourney = (ordersQuery.data ?? []).filter(
    (o) => !o.canceled && (o.jobs ?? []).some((j) => j.status === "pending" || j.status === "sending"),
  ).length;
  const scheduled = totals.pending - totals.held;
  const firstName = me.email.split("@")[0];

  function refresh() {
    void qc.invalidateQueries({ queryKey: ["orders"] });
    void qc.invalidateQueries({ queryKey: ["flows"] });
    toast.success("Dados atualizados");
  }

  return (
    <div className="min-w-0 space-y-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <div className="mb-1 flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success/60" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
            </span>
            Jornada ao vivo · {flow.store.name}
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Olá, {firstName}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {scheduled > 0
              ? `${countLabel(scheduled, "envio agendado", "envios agendados")} e ${countLabel(totals.held, "envio retido", "envios retidos")}.`
              : "Nenhum envio agendado no momento."}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={refresh} disabled={ordersQuery.isFetching}>
            <RefreshCw className={cn("h-3.5 w-3.5", ordersQuery.isFetching && "animate-spin")} />
            Atualizar
          </Button>
          <Button asChild size="sm">
            <Link to="/flow">
              Abrir fluxo <ArrowRight className="h-3.5 w-3.5" />
            </Link>
          </Button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KpiCard
          label="Pedidos na jornada"
          value={inJourney}
          hint={`${ordersQuery.data?.length ?? 0} pedidos recentes na loja`}
          icon={Package}
          tone="primary"
        />
        <KpiCard
          label="E-mails enviados"
          value={totals.sent}
          hint="Somando todos os passos do fluxo"
          icon={Mail}
          tone="success"
        />
        <KpiCard
          label="Retidos e agendados"
          value={totals.held + scheduled}
          hint={`${countLabel(totals.held, "retido", "retidos")} · ${countLabel(scheduled, "agendado", "agendados")}`}
          icon={Clock3}
          tone="warning"
        />
        <KpiCard
          label="Falhas a tratar"
          value={totals.failed}
          hint={totals.failed ? "Revise e reenvie abaixo" : "Fila saudável"}
          icon={AlertTriangle}
          tone="danger"
        />
      </div>

      <StatusStrip />

      <div className="grid min-w-0 gap-4 lg:grid-cols-3">
        <div className="min-w-0 lg:col-span-2">
          <FailedJobs />
        </div>
        <RecentOrders />
      </div>

      <p className="pb-2 text-center text-[11px] text-muted-foreground">
        Fuso {flow.store.timezone} · remetente {flow.store.from} · atualizado {formatDateTime(new Date().toISOString())}
      </p>
    </div>
  );
}
