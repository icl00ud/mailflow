import { useEffect, useState } from "react";
import { GitBranch, ListTree, Mail, Timer, Workflow } from "lucide-react";
import { useWorkspace } from "@/components/workspace";
import { JourneyGraph } from "@/components/flow/flow-graph";
import { FlowList } from "@/components/flow/flow-list";
import { StepSheet } from "@/components/flow/step-sheet";
import { Card, CardContent } from "@/components/ui/card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useMediaQuery } from "@/lib/use-media-query";
import { totalCounts, countLabel } from "@/lib/format";
import { cn } from "@/lib/utils";

export function FlowPage() {
  const { flow } = useWorkspace();
  const isMobile = useMediaQuery("(max-width: 767px)");
  const [view, setView] = useState<"graph" | "list">(() => (window.matchMedia("(max-width: 767px)").matches ? "list" : "graph"));
  const [stepId, setStepId] = useState<string | null>(null);
  useEffect(() => {
    if (isMobile) setView("list");
  }, [isMobile]);
  const selected = flow.steps.find((s) => s.id === stepId);
  const totals = totalCounts(flow.steps);
  const active = flow.steps.filter((s) => s.effective === "on").length;
  const drafts = flow.steps.filter((s) => s.effective === "draft").length;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <Workflow className="h-3.5 w-3.5" />
            Automação pós-compra
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Fluxo da jornada</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            {countLabel(flow.steps.length, "passo", "passos")} · {countLabel(active, "ativo", "ativos")}
            {drafts > 0 && ` · ${countLabel(drafts, "rascunho", "rascunhos")}`} ·{" "}
            {countLabel(totals.sent, "e-mail enviado", "e-mails enviados")}. Clique em um passo para ver a copy, as
            condições e os envios.
          </p>
        </div>
        <Tabs value={view} onValueChange={(v) => setView(v as "graph" | "list")}>
          <TabsList>
            <TabsTrigger value="graph">
              <GitBranch className="h-3.5 w-3.5" />
              Mapa
            </TabsTrigger>
            <TabsTrigger value="list">
              <ListTree className="h-3.5 w-3.5" />
              Lista
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-lg border bg-card px-4 py-3 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <Mail className="h-3.5 w-3.5" />
          <span className="text-foreground/80">{flow.store.from}</span>
        </span>
        <span className="hidden h-3 w-px bg-border sm:block" />
        <span className="inline-flex items-center gap-1.5">
          <Timer className="h-3.5 w-3.5" />
          Janela de envio {flow.store.send_window.start}h–{flow.store.send_window.end}h · {flow.store.timezone}
        </span>
        <span className="ml-auto hidden items-center gap-2 sm:flex">
          <Badge variant="success">Ativo envia</Badge>
          <Badge variant="muted">Pausado pula</Badge>
          <Badge variant="outline">Rascunho nunca envia</Badge>
        </span>
      </div>

      <div className="flex items-center gap-2 overflow-x-auto pb-1 text-[11px] text-muted-foreground sm:hidden">
        <span className="shrink-0 font-medium text-foreground/80">Legenda:</span>
        <Badge variant="success" className="shrink-0">Ativo</Badge>
        <Badge variant="muted" className="shrink-0">Pausado</Badge>
        <Badge variant="outline" className="shrink-0">Rascunho</Badge>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="warning" className="shrink-0 cursor-help">
              ⏸ Aguarda
            </Badge>
          </TooltipTrigger>
          <TooltipContent>Condição de espera: o envio fica retido até a condição ser satisfeita.</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="muted" className="shrink-0 cursor-help">
              ↷ Pula
            </Badge>
          </TooltipTrigger>
          <TooltipContent>Condição de exclusão: se ela for verdadeira, o envio é pulado definitivamente.</TooltipContent>
        </Tooltip>
      </div>

      <Card className="overflow-hidden">
        <CardContent className="p-0">
          {view === "graph" ? (
            <JourneyGraph
              steps={flow.steps}
              mode={isMobile ? "mobile" : "desktop"}
              height={isMobile ? "mobile" : "desktop"}
              onSelect={setStepId}
            />
          ) : (
            <FlowList steps={flow.steps} onSelect={setStepId} />
          )}
        </CardContent>
      </Card>

      <div className={cn("hidden items-center gap-5 text-[11px] text-muted-foreground sm:flex")}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex cursor-help items-center gap-1.5">
              <Badge variant="warning">⏸</Badge> Condição de espera (hold)
            </span>
          </TooltipTrigger>
          <TooltipContent>
            O job fica retido, mas não é perdido: o envio acontece quando a condição for satisfeita.
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex cursor-help items-center gap-1.5">
              <Badge variant="muted">↷</Badge> Condição de exclusão (skip)
            </span>
          </TooltipTrigger>
          <TooltipContent>
            Se a condição for verdadeira no momento do envio, o passo é pulado e a corrente segue para o próximo.
          </TooltipContent>
        </Tooltip>
        <span className="ml-auto">As setas seguem a dependência “após o passo” do flow.yaml.</span>
      </div>

      <StepSheet
        step={selected}
        storeId={flow.store.id}
        sender={flow.sender}
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) setStepId(null);
        }}
      />
    </div>
  );
}
