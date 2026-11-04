import { useMemo } from "react";
import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  ReactFlow,
  type Edge,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { StepView } from "@/types";
import { LaneNode, StepNode, type JourneyNode } from "@/components/flow/step-node";

const nodeTypes: NodeTypes = { step: StepNode, lane: LaneNode };

const COL = 288;
const NODE_W = 252;

interface Layout {
  nodes: JourneyNode[];
  edges: Edge[];
}

function chainEdges(steps: StepView[], positions: Map<string, { x: number; y: number }>): Edge[] {
  const edges: Edge[] = [];
  for (let i = 0; i < steps.length - 1; i++) {
    const from = steps[i];
    const to = steps[i + 1];
    const a = positions.get(from.id);
    const b = positions.get(to.id);
    if (!a || !b) continue;
    let sourceHandle = "right";
    let targetHandle = "left";
    if (b.y > a.y + 10 && Math.abs(b.x - a.x) < 10) {
      sourceHandle = "bottom";
      targetHandle = "top";
    } else if (b.x < a.x - 10) {
      sourceHandle = "left-out";
      targetHandle = "right-in";
    }
    edges.push({
      id: `${from.id}-${to.id}`,
      source: from.id,
      target: to.id,
      sourceHandle,
      targetHandle,
      type: "smoothstep",
      animated: to.effective === "on",
      style: {
        stroke:
          to.effective === "on"
            ? "hsl(var(--primary) / 0.45)"
            : "hsl(var(--muted-foreground) / 0.3)",
        strokeWidth: 1.6,
      },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        width: 14,
        height: 14,
        color: to.effective === "on" ? "hsl(var(--primary) / 0.6)" : "hsl(var(--muted-foreground) / 0.4)",
      },
    });
  }
  return edges;
}

export function buildJourneyLayout(steps: StepView[], mode: "desktop" | "mobile"): Layout {
  const main = steps.filter((s) => s.trigger.on !== "tracking");
  const tracking = steps.filter((s) => s.trigger.on === "tracking");
  const nodes: JourneyNode[] = [];
  const positions = new Map<string, { x: number; y: number }>();

  if (mode === "desktop") {
    nodes.push({
      id: "lane-order",
      type: "lane",
      position: { x: 0, y: 0 },
      data: {
        label: "Jornada do pedido",
        hint: "Compra paga → preparação → envio → trânsito",
        icon: "order",
        count: main.length,
      },
      draggable: false,
      selectable: false,
      width: 860,
    });
    main.forEach((step, i) => {
      let x = 0;
      let y = 44;
      if (i < 4) {
        x = i * COL;
      } else if (i === 4) {
        x = 3 * COL;
        y = 274;
      } else if (i === 5) {
        x = 2 * COL;
        y = 274;
      } else {
        x = (i % 4) * COL;
        y = 274 + Math.floor((i - 4) / 4) * 240;
      }
      positions.set(step.id, { x, y });
      nodes.push({ id: step.id, type: "step", position: { x, y }, data: { step }, width: NODE_W });
    });
    nodes.push({
      id: "lane-tracking",
      type: "lane",
      position: { x: 0, y: 540 },
      data: {
        label: "Eventos de rastreio",
        hint: "Entradas independentes disparadas pela transportadora",
        icon: "tracking",
        count: tracking.length,
      },
      draggable: false,
      selectable: false,
      width: 860,
    });
    tracking.forEach((step, i) => {
      const x = (i % 3) * COL;
      const y = 590 + Math.floor(i / 3) * 240;
      positions.set(step.id, { x, y });
      nodes.push({ id: step.id, type: "step", position: { x, y }, data: { step }, width: NODE_W });
    });
  } else {
    let y = 0;
    nodes.push({
      id: "lane-order",
      type: "lane",
      position: { x: 0, y },
      data: {
        label: "Jornada do pedido",
        hint: "Compra paga → preparação → envio → trânsito",
        icon: "order",
        count: main.length,
      },
      draggable: false,
      selectable: false,
      width: 320,
    });
    y = 52;
    main.forEach((step) => {
      positions.set(step.id, { x: 0, y });
      nodes.push({ id: step.id, type: "step", position: { x: 0, y }, data: { step }, width: NODE_W });
      y += 252;
    });
    y += 20;
    nodes.push({
      id: "lane-tracking",
      type: "lane",
      position: { x: 0, y },
      data: {
        label: "Eventos de rastreio",
        hint: "Entradas independentes da transportadora",
        icon: "tracking",
        count: tracking.length,
      },
      draggable: false,
      selectable: false,
      width: 320,
    });
    y += 52;
    tracking.forEach((step) => {
      positions.set(step.id, { x: 0, y });
      nodes.push({ id: step.id, type: "step", position: { x: 0, y }, data: { step }, width: NODE_W });
      y += 252;
    });
  }

  return { nodes, edges: chainEdges(main, positions) };
}

export function JourneyGraph({
  steps,
  mode,
  onSelect,
  height = "desktop",
}: {
  steps: StepView[];
  mode: "desktop" | "mobile";
  onSelect: (stepId: string) => void;
  height?: "desktop" | "mobile";
}) {
  const { nodes, edges } = useMemo(() => buildJourneyLayout(steps, mode), [steps, mode]);

  return (
    <div
      className={
        height === "mobile"
          ? "relative h-[540px] w-full"
          : "relative h-[600px] w-full lg:h-[900px]"
      }
    >
      <ReactFlow
        key={mode}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView={mode === "desktop"}
        fitViewOptions={{ padding: 0.04, minZoom: 0.55, maxZoom: 1.05 }}
        defaultViewport={mode === "mobile" ? { x: 14, y: 4, zoom: 1 } : undefined}
        minZoom={0.4}
        maxZoom={1.5}
        nodesConnectable={false}
        nodesDraggable
        elementsSelectable
        panOnScroll
        selectionOnDrag={false}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, node) => {
          if (node.type === "step") onSelect(node.id);
        }}
      >
        <Background
          variant={BackgroundVariant.Dots}
          gap={22}
          size={1}
          color="hsl(var(--muted-foreground) / 0.24)"
        />
        <Controls showInteractive={false} position="bottom-right" />
      </ReactFlow>
      {mode === "mobile" && (
        <div className="pointer-events-none absolute bottom-3 left-3 rounded-full border bg-card/90 px-2.5 py-1 text-[10px] text-muted-foreground shadow-sm backdrop-blur">
          Arraste para explorar · role para ampliar
        </div>
      )}
    </div>
  );
}
