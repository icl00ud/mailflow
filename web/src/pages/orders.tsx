import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ChevronRight, Package, RefreshCw, Search, ShoppingBag } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspace } from "@/components/workspace";
import { useOrders } from "@/lib/queries";
import { CreateOrderDialog } from "@/components/orders/create-order-dialog";
import { OrderSheet } from "@/components/orders/order-sheet";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { OrderStatusBadge } from "@/components/status-badges";
import { formatDateTime, orderStatus, relativeTime, countLabel } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { OrderView } from "@/types";

const filters = [
  { value: "all", label: "Todos os status" },
  { value: "andamento", label: "Em andamento" },
  { value: "retido", label: "Retidos" },
  { value: "concluido", label: "Concluídos" },
  { value: "falhou", label: "Com falha" },
  { value: "cancelado", label: "Cancelados" },
];

function matchesFilter(order: OrderView, filter: string): boolean {
  const status = orderStatus(order);
  switch (filter) {
    case "andamento":
      return status.label === "Em andamento";
    case "retido":
      return status.label === "Retido";
    case "concluido":
      return status.label === "Concluído";
    case "falhou":
      return status.label === "Falhou";
    case "cancelado":
      return status.label === "Cancelado";
    default:
      return true;
  }
}

function progress(order: OrderView) {
  const jobs = order.jobs ?? [];
  const sent = jobs.filter((j) => j.status === "sent").length;
  const total = jobs.filter((j) => j.status !== "canceled").length;
  return { sent, total, pct: total ? Math.round((sent / total) * 100) : 0 };
}

function Progress({ order }: { order: OrderView }) {
  const { sent, total, pct } = progress(order);
  if (!total) return <span className="text-[11px] text-muted-foreground">—</span>;
  const status = orderStatus(order);
  const color =
    status.label === "Falhou"
      ? "bg-destructive"
      : status.label === "Retido"
        ? "bg-warning"
        : status.label === "Cancelado"
          ? "bg-muted-foreground/30"
          : "bg-success";
  return (
    <div
      className="flex items-center gap-2"
      role="progressbar"
      aria-label={`Progresso da jornada: ${sent} de ${total} envios`}
      aria-valuemin={0}
      aria-valuemax={total}
      aria-valuenow={sent}
    >
      <div className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full", color)} style={{ width: `${pct}%` }} />
      </div>
      <span className="text-[11px] tabular-nums text-muted-foreground">
        {sent}/{total}
      </span>
    </div>
  );
}

export function OrdersPage() {
  const { flow } = useWorkspace();
  const ordersQuery = useOrders(flow.store.id);
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");

  const selectedId = Number(params.get("order")) || null;
  const selected = (ordersQuery.data ?? []).find((o) => o.id === selectedId);

  const orders = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (ordersQuery.data ?? []).filter((o) => {
      if (!matchesFilter(o, filter)) return false;
      if (!q) return true;
      return [o.name, o.email, o.customer_name, o.tracking_number].some((v) => (v ?? "").toLowerCase().includes(q));
    });
  }, [ordersQuery.data, filter, search]);

  function selectOrder(id: number | null) {
    if (id === null) {
      setParams({});
      return;
    }
    setParams({ order: String(id) });
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <Package className="h-3.5 w-3.5" />
            Operação
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Pedidos</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {ordersQuery.data?.length ?? 0} pedidos recentes em {flow.store.name}. Abra um pedido para ver a jornada
            completa.
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void qc.invalidateQueries({ queryKey: ["orders"] });
              toast.success("Pedidos atualizados");
            }}
            disabled={ordersQuery.isFetching}
          >
            <RefreshCw className={cn("h-3.5 w-3.5", ordersQuery.isFetching && "animate-spin")} />
            Atualizar
          </Button>
          <CreateOrderDialog storeId={flow.store.id} />
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          <div className="flex flex-col gap-3 border-b p-3 sm:flex-row sm:items-center sm:p-4">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Buscar por pedido, cliente, e-mail ou rastreio…"
                className="pl-9"
              />
            </div>
            <Select value={filter} onValueChange={setFilter}>
              <SelectTrigger className="w-full sm:w-[190px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {filters.map((f) => (
                  <SelectItem key={f.value} value={f.value}>
                    {f.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {ordersQuery.isLoading ? (
            <div className="space-y-2 p-4">
              {[0, 1, 2, 3, 4].map((i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : orders.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
              <span className="flex h-12 w-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <ShoppingBag className="h-5 w-5" />
              </span>
              <p className="mt-3 text-sm font-medium">
                {search || filter !== "all" ? "Nenhum pedido encontrado" : "Nenhum pedido nesta loja"}
              </p>
              <p className="mt-1 max-w-sm text-xs text-muted-foreground">
                {search || filter !== "all"
                  ? "Tente outro termo de busca ou remova o filtro de status."
                  : "Crie um pedido de teste para visualizar a jornada completa sem depender da Shopify."}
              </p>
              {!search && filter === "all" && (
                <div className="mt-4">
                  <CreateOrderDialog storeId={flow.store.id} />
                </div>
              )}
            </div>
          ) : (
            <>
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="pl-4">Pedido</TableHead>
                      <TableHead>Cliente</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Progresso</TableHead>
                      <TableHead>Total</TableHead>
                      <TableHead className="text-right pr-4">Pago em</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {orders.map((order) => (
                      <TableRow key={order.id} className="cursor-pointer" onClick={() => selectOrder(order.id)}>
                        <TableCell className="pl-4">
                          <div className="flex items-center gap-2.5">
                            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted text-[11px] font-medium text-muted-foreground">
                              {order.country || "—"}
                            </span>
                            <div>
                              <p className="font-medium">{order.name}</p>
                              <p className="text-[11px] text-muted-foreground">
                                {order.items?.length ? countLabel(order.items.length, "item", "itens") : "sem itens"}
                              </p>
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          <p className="max-w-[220px] truncate text-sm">{order.customer_name || "—"}</p>
                          <p className="max-w-[220px] truncate text-[11px] text-muted-foreground">{order.email}</p>
                        </TableCell>
                        <TableCell>
                          <OrderStatusBadge order={order} />
                        </TableCell>
                        <TableCell>
                          <Progress order={order} />
                        </TableCell>
                        <TableCell className="text-sm tabular-nums">{order.total || "—"}</TableCell>
                        <TableCell className="pr-4 text-right">
                          <p className="text-xs">{relativeTime(order.paid_at)}</p>
                          <p className="text-[11px] text-muted-foreground">{formatDateTime(order.paid_at)}</p>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>

              <ul className="divide-y md:hidden">
                {orders.map((order) => (
                  <li key={order.id}>
                    <button
                      className="flex w-full items-center gap-3 px-4 py-3.5 text-left active:bg-muted/50"
                      onClick={() => selectOrder(order.id)}
                    >
                      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted text-[11px] font-medium text-muted-foreground">
                        {order.country || "—"}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-2">
                          <p className="truncate text-sm font-medium">{order.name}</p>
                          <OrderStatusBadge order={order} />
                        </div>
                        <p className="mt-0.5 truncate text-xs text-muted-foreground">{order.email}</p>
                        <div className="mt-1.5 flex items-center gap-2">
                          <Progress order={order} />
                          <span className="text-[11px] text-muted-foreground">· {relativeTime(order.paid_at)}</span>
                        </div>
                      </div>
                      <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/60" />
                    </button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </CardContent>
      </Card>

      <OrderSheet
        order={selected}
        steps={flow.steps}
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) selectOrder(null);
        }}
      />
    </div>
  );
}
