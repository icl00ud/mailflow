import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useFlows, useMe } from "@/lib/queries";
import type { FlowView, Me } from "@/types";

interface WorkspaceValue {
  me: Me;
  flows: FlowView[];
  flow: FlowView;
  selectedStore: string;
  setSelectedStore: (id: string) => void;
  isLoading: boolean;
}

const WorkspaceContext = createContext<WorkspaceValue | null>(null);
const STORE_KEY = "mf-store";

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const meQuery = useMe();
  const flowsQuery = useFlows();
  const [selected, setSelected] = useState(() => localStorage.getItem(STORE_KEY) ?? "");

  const flows = flowsQuery.data ?? [];
  const selectedStore = useMemo(() => {
    if (selected && flows.some((f) => f.store.id === selected)) return selected;
    return flows[0]?.store.id ?? "";
  }, [selected, flows]);

  useEffect(() => {
    if (selectedStore) localStorage.setItem(STORE_KEY, selectedStore);
  }, [selectedStore]);

  const setSelectedStore = useCallback((id: string) => setSelected(id), []);

  const flow = flows.find((f) => f.store.id === selectedStore);

  if (meQuery.isLoading || flowsQuery.isLoading) {
    return <WorkspaceSkeleton />;
  }
  if (meQuery.error || flowsQuery.error) {
    return (
      <div className="flex min-h-screen items-center justify-center p-8">
        <div className="max-w-md space-y-2 text-center">
          <h1 className="text-lg font-semibold">Não foi possível carregar o painel</h1>
          <p className="text-sm text-muted-foreground">
            {meQuery.error?.message || flowsQuery.error?.message || "Erro inesperado."}
          </p>
        </div>
      </div>
    );
  }
  if (!meQuery.data || !flow) {
    return (
      <div className="flex min-h-screen items-center justify-center p-8">
        <div className="max-w-md space-y-2 text-center">
          <h1 className="text-lg font-semibold">Nenhuma loja vinculada</h1>
          <p className="text-sm text-muted-foreground">
            Sua conta ainda não tem acesso a uma loja com fluxo configurado. Peça um convite ao dono da conta.
          </p>
        </div>
      </div>
    );
  }

  return (
    <WorkspaceContext.Provider
      value={{
        me: meQuery.data,
        flows,
        flow,
        selectedStore: flow.store.id,
        setSelectedStore,
        isLoading: false,
      }}
    >
      {children}
    </WorkspaceContext.Provider>
  );
}

export function useWorkspace() {
  const ctx = useContext(WorkspaceContext);
  if (!ctx) throw new Error("useWorkspace must be used inside WorkspaceProvider");
  return ctx;
}

function WorkspaceSkeleton() {
  return (
    <div className="flex min-h-screen bg-background">
      <div className="hidden w-64 border-r p-4 md:block">
        <div className="h-8 w-32 animate-pulse rounded-md bg-muted" />
        <div className="mt-10 space-y-3">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-9 animate-pulse rounded-md bg-muted" />
          ))}
        </div>
      </div>
      <div className="flex-1 p-6">
        <div className="h-8 w-48 animate-pulse rounded-md bg-muted" />
        <div className="mt-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-28 animate-pulse rounded-xl bg-muted" />
          ))}
        </div>
      </div>
    </div>
  );
}
