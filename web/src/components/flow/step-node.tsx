import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { ArrowDownRight, Clock3, Radar, ShoppingBag } from "lucide-react";
import type { StepView, Trigger } from "@/types";
import { FlowStatusBadge } from "@/components/status-badges";
import { conditionInfo, scheduledOf, timingLabel, triggerLabel } from "@/lib/format";
import { cn } from "@/lib/utils";

export interface StepNodeData extends Record<string, unknown> {
  step: StepView;
}

export interface LaneNodeData extends Record<string, unknown> {
  label: string;
  hint: string;
  icon: "order" | "tracking";
  count: number;
}

export type StepNodeType = Node<StepNodeData, "step">;
export type LaneNodeType = Node<LaneNodeData, "lane">;
export type JourneyNode = StepNodeType | LaneNodeType;

function TriggerIcon({ trigger, className }: { trigger: Trigger; className?: string }) {
  if (trigger.on === "order_paid") return <ShoppingBag className={className} />;
  if (trigger.on === "after_step") return <ArrowDownRight className={className} />;
  return <Radar className={className} />;
}

const countSpec = [
  { key: "scheduled", label: "agend.", singular: "agend.", dot: "bg-info" },
  { key: "sent", label: "enviados", singular: "enviado", dot: "bg-success" },
  { key: "held", label: "retidos", singular: "retido", dot: "bg-warning" },
  { key: "skipped", label: "pulados", singular: "pulado", dot: "bg-muted-foreground/40" },
  { key: "failed", label: "falhas", singular: "falha", dot: "bg-destructive" },
] as const;

export function StepNode({ data, selected }: NodeProps<StepNodeType>) {
  const step = data.step;
  const scheduled = scheduledOf(step);
  const counts = {
    scheduled,
    sent: step.counts?.sent ?? 0,
    held: step.held ?? 0,
    skipped: step.counts?.skipped ?? 0,
    failed: step.counts?.failed ?? 0,
  };
  const visible = countSpec.filter((c) => counts[c.key] > 0);
  const conditions = step.conditions ?? [];

  return (
    <div
      className={cn(
        "w-[252px] rounded-xl border bg-card p-3 shadow-sm transition-shadow",
        step.effective === "off" && "border-dashed bg-card/70",
        step.effective === "draft" && "border-dashed bg-muted/35",
        selected && "shadow-md ring-2 ring-primary/50",
      )}
    >
      <Handle type="target" position={Position.Top} id="top" className="!bg-muted-foreground/50" />
      <Handle type="target" position={Position.Left} id="left" className="!bg-muted-foreground/50" />

      <div className="flex items-center justify-between gap-2">
        <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground">
          {step.id}
        </span>
        <FlowStatusBadge status={step.effective} />
      </div>

      <h3
        className={cn(
          "mt-2 line-clamp-2 min-h-[34px] text-[13px] font-semibold leading-snug tracking-tight",
          step.effective === "off" && "text-muted-foreground",
        )}
      >
        {step.title}
      </h3>

      <div className="mt-1.5 flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Clock3 className="h-3 w-3 shrink-0" />
        <span className="truncate">{timingLabel(step)}</span>
      </div>
      <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <TriggerIcon trigger={step.trigger} className="h-3 w-3 shrink-0" />
        <span className="truncate">{triggerLabel(step.trigger)}</span>
      </div>

      <div className="mt-2 flex min-h-[22px] flex-wrap gap-1">
        {conditions.slice(0, 2).map((c) => {
          const info = conditionInfo(c, step);
          return (
            <span
              key={c}
              title={info.explanation}
              className={cn(
                "inline-flex items-center gap-1 rounded border px-1.5 py-px text-[10px] leading-4",
                info.kind === "hold"
                  ? "border-warning/30 bg-warning/10 text-warning"
                  : "border-border bg-muted/70 text-muted-foreground",
              )}
            >
              {info.kind === "hold" ? "⏸" : "↷"} {info.short}
            </span>
          );
        })}
        {conditions.length > 2 && (
          <span className="rounded border border-border px-1.5 py-px text-[10px] leading-4 text-muted-foreground">
            +{conditions.length - 2}
          </span>
        )}
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-x-2.5 gap-y-1 border-t pt-2 text-[10px] tabular-nums">
        {visible.length === 0 ? (
          <span className="text-muted-foreground/70">Sem envios ainda</span>
        ) : (
          visible.map((c) => (
            <span key={c.key} className="inline-flex items-center gap-1 text-muted-foreground" title={c.label}>
              <span className={cn("h-1.5 w-1.5 rounded-full", c.dot)} />
              <span className="font-medium text-foreground/85">{counts[c.key]}</span>
              {counts[c.key] === 1 ? c.singular : c.label}
            </span>
          ))
        )}
      </div>

      <Handle type="source" position={Position.Bottom} id="bottom" className="!bg-muted-foreground/50" />
      <Handle type="source" position={Position.Right} id="right" className="!bg-muted-foreground/50" />
      {/* Right-to-left links (the journey wraps onto a second row): source on the left, target on the right. */}
      <Handle type="source" position={Position.Left} id="left-out" className="!opacity-0" />
      <Handle type="target" position={Position.Right} id="right-in" className="!opacity-0" />
    </div>
  );
}

export function LaneNode({ data }: NodeProps<LaneNodeType>) {
  return (
    <div className="flex w-[860px] items-center gap-2 border-b border-dashed pb-2">
      <span className="flex h-6 w-6 items-center justify-center rounded-md bg-muted text-muted-foreground">
        {data.icon === "order" ? <ShoppingBag className="h-3.5 w-3.5" /> : <Radar className="h-3.5 w-3.5" />}
      </span>
      <div className="min-w-0">
        <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{data.label}</p>
        <p className="truncate text-[11px] text-muted-foreground/70">{data.hint}</p>
      </div>
      {data.count > 0 && (
        <span className="ml-auto rounded-full border bg-card px-2 py-0.5 text-[10px] text-muted-foreground">
          {data.count} {data.count === 1 ? "passo" : "passos"}
        </span>
      )}
    </div>
  );
}
