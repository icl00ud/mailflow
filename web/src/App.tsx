import { Link, Route, Routes } from "react-router-dom";
import { AppShell } from "@/components/app-shell";
import { WorkspaceProvider } from "@/components/workspace";
import { DashboardPage } from "@/pages/dashboard";
import { FlowPage } from "@/pages/flow";
import { OrdersPage } from "@/pages/orders";
import { AccountPage } from "@/pages/account";
import { Button } from "@/components/ui/button";

function NotFound() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 p-8 text-center">
      <p className="font-mono text-xs text-muted-foreground">404</p>
      <h1 className="text-2xl font-semibold tracking-tight">Página não encontrada</h1>
      <p className="max-w-sm text-sm text-muted-foreground">
        O endereço não existe neste painel. Volte para a visão geral para continuar.
      </p>
      <Button asChild>
        <Link to="/">Voltar para a visão geral</Link>
      </Button>
    </div>
  );
}

export function App() {
  return (
    <Routes>
      <Route
        element={
          <WorkspaceProvider>
            <AppShell />
          </WorkspaceProvider>
        }
      >
        <Route index element={<DashboardPage />} />
        <Route path="flow" element={<FlowPage />} />
        <Route path="orders" element={<OrdersPage />} />
        <Route path="account" element={<AccountPage />} />
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}
