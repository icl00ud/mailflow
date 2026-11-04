import { Badge } from "@/components/ui/badge";
import type { FlowStatus, JobStatus, OrderView } from "@/types";
import { flowStatusLabel, jobStatusLabel, orderStatus } from "@/lib/format";
import { cn } from "@/lib/utils";

const flowVariant: Record<FlowStatus, "success" | "muted" | "outline"> = {
  on: "success",
  off: "muted",
  draft: "outline",
};

export function FlowStatusBadge({ status, className }: { status: FlowStatus; className?: string }) {
  return (
    <Badge variant={flowVariant[status]} className={cn("gap-1", className)}>
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full",
          status === "on" && "bg-success",
          status === "off" && "bg-muted-foreground",
          status === "draft" && "border border-muted-foreground/70",
        )}
      />
      {flowStatusLabel[status]}
    </Badge>
  );
}

const jobVariant: Record<JobStatus, "success" | "warning" | "destructive" | "info" | "muted" | "outline"> = {
  pending: "info",
  sending: "info",
  sent: "success",
  skipped: "muted",
  failed: "destructive",
  canceled: "outline",
};

export function JobStatusBadge({ status, held }: { status: JobStatus; held?: boolean }) {
  if (status === "pending" && held) {
    return <Badge variant="warning">Retido</Badge>;
  }
  return <Badge variant={jobVariant[status]}>{jobStatusLabel[status]}</Badge>;
}

export function OrderStatusBadge({ order }: { order: OrderView }) {
  const status = orderStatus(order);
  const variant = {
    success: "success",
    warning: "warning",
    danger: "destructive",
    muted: "muted",
    info: "info",
  } as const;
  return <Badge variant={variant[status.tone]}>{status.label}</Badge>;
}
