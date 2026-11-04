import { ArrowDownRight, Clock3, Radar, ShoppingBag } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { FlowStatusBadge } from "@/components/status-badges";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  conditionInfo,
  scheduledOf,
  timingLabel,
  trackingEventLabel,
  triggerLabel,
} from "@/lib/format";
import { cn } from "@/lib/utils";
import type { StepView, Trigger } from "@/types";

function TriggerIcon({ trigger }: { trigger: Trigger }) {
  const Icon = trigger.on === "order_paid" ? ShoppingBag : trigger.on === "after_step" ? ArrowDownRight : Radar;
  return <Icon className="h-3 w-3" />;
}

function CountPills({ step }: { step: StepView }) {
  const rows = [
    { label: "agend.", singular: "agend.", value: scheduledOf(step), dot: "bg-info" },
    { label: "enviados", singular: "enviado", value: step.counts?.sent ?? 0, dot: "bg-success" },
    { label: "retidos", singular: "retido", value: step.held ?? 0, dot: "bg-warning" },
    { label: "pulados", singular: "pulado", value: step.counts?.skipped ?? 0, dot: "bg-muted-foreground/40" },
    { label: "falhas", singular: "falha", value: step.counts?.failed ?? 0, dot: "bg-destructive" },
  ].filter((r) => r.value > 0);
  if (!rows.length) return <span className="text-[11px] text-muted-foreground">Sem envios</span>;
  return (
    <div className="flex flex-wrap justify-end gap-x-3 gap-y-1">
      {rows.map((r) => (
        <span key={r.label} className="inline-flex items-center gap-1 text-[11px] tabular-nums text-muted-foreground">
          <span className={cn("h-1.5 w-1.5 rounded-full", r.dot)} />
          <span className="font-medium text-foreground/90">{r.value}</span>
          {r.value === 1 ? r.singular : r.label}
        </span>
      ))}
    </div>
  );
}

function StepRow({
  step,
  last,
  onSelect,
}: {
  step: StepView;
  last: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      onClick={onSelect}
      className="group flex w-full gap-3 px-4 py-3.5 text-left transition-colors hover:bg-muted/40 sm:px-5"
    >
      <div className="flex shrink-0 flex-col items-center">
        <span
          className={cn(
            "flex h-7 w-7 items-center justify-center rounded-full border bg-card font-mono text-[11px] font-medium",
            step.effective === "on" && "border-primary/30 text-primary",
            step.effective === "off" && "text-muted-foreground",
            step.effective === "draft" && "border-dashed text-muted-foreground",
          )}
        >
          {step.id}
        </span>
        {!last && <span className="mt-1 w-px flex-1 bg-border" />}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={cn(
              "text-sm font-medium",
              step.effective === "off" && "text-muted-foreground",
              step.effective === "draft" && "text-muted-foreground",
            )}
          >
            {step.title}
          </span>
          <FlowStatusBadge status={step.effective} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <Clock3 className="h-3 w-3" />
            {timingLabel(step)}
          </span>
          <span className="inline-flex items-center gap-1">
            <TriggerIcon trigger={step.trigger} />
            {step.trigger.on === "tracking"
              ? trackingEventLabel[step.trigger.event ?? ""] ?? triggerLabel(step.trigger)
              : triggerLabel(step.trigger)}
          </span>
        </div>
        {(step.conditions?.length ?? 0) > 0 && (
          <div className="mt-1.5 flex flex-wrap gap-1">
            {step.conditions?.map((c) => {
              const info = conditionInfo(c, step);
              return (
                <Tooltip key={c}>
                  <TooltipTrigger asChild>
                    <Badge variant={info.kind === "hold" ? "warning" : "muted"} className="cursor-help font-normal">
                      {info.kind === "hold" ? "⏸" : "↷"} {info.short}
                    </Badge>
                  </TooltipTrigger>
                  <TooltipContent>{info.explanation}</TooltipContent>
                </Tooltip>
              );
            })}
          </div>
        )}
        <div className="mt-2 sm:hidden">
          <CountPills step={step} />
        </div>
      </div>

      <div className="hidden w-[250px] shrink-0 items-center justify-end sm:flex">
        <CountPills step={step} />
      </div>
    </button>
  );
}

export function FlowList({ steps, onSelect }: { steps: StepView[]; onSelect: (id: string) => void }) {
  const main = steps.filter((s) => s.trigger.on !== "tracking");
  const tracking = steps.filter((s) => s.trigger.on === "tracking");
  return (
    <div className="divide-y">
      <div>
        <div className="flex items-center gap-2 border-b bg-muted/25 px-4 py-2 sm:px-5">
          <span className="flex h-5 w-5 items-center justify-center rounded bg-muted text-muted-foreground">
            <ShoppingBag className="h-3 w-3" />
          </span>
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Jornada do pedido
          </span>
          <span className="ml-auto text-[10px] text-muted-foreground">{main.length} passos</span>
        </div>
        {main.map((step, i) => (
          <StepRow key={step.id} step={step} last={i === main.length - 1} onSelect={() => onSelect(step.id)} />
        ))}
      </div>
      <div>
        <div className="flex items-center gap-2 border-b bg-muted/25 px-4 py-2 sm:px-5">
          <span className="flex h-5 w-5 items-center justify-center rounded bg-muted text-muted-foreground">
            <Radar className="h-3 w-3" />
          </span>
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Eventos de rastreio
          </span>
          <span className="ml-auto text-[10px] text-muted-foreground">{tracking.length} entradas</span>
        </div>
        {tracking.map((step, i) => (
          <StepRow key={step.id} step={step} last={i === tracking.length - 1} onSelect={() => onSelect(step.id)} />
        ))}
      </div>
    </div>
  );
}
