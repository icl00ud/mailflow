import { useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import {
  ChevronDown,
  LayoutDashboard,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Package,
  Store,
  Sun,
  UserRound,
  Workflow,
} from "lucide-react";
import { Logo } from "@/components/logo";
import { useTheme } from "@/components/theme-provider";
import { useWorkspace } from "@/components/workspace";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { initials } from "@/lib/format";

const nav = [
  { to: "/", label: "Visão geral", icon: LayoutDashboard, end: true },
  { to: "/flow", label: "Fluxo", icon: Workflow },
  { to: "/orders", label: "Pedidos", icon: Package },
  { to: "/account", label: "Conta", icon: UserRound },
];

export function logout() {
  const form = document.createElement("form");
  form.method = "post";
  form.action = "/logout";
  document.body.appendChild(form);
  form.submit();
}

function StoreSwitcher() {
  const { flows, flow, setSelectedStore } = useWorkspace();
  if (flows.length <= 1) {
    return (
      <div className="mx-3 rounded-lg border bg-background/60 px-3 py-2.5">
        <div className="flex items-center gap-2">
          <Store className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="truncate text-sm font-medium">{flow.store.name}</span>
        </div>
        <p className="mt-0.5 truncate pl-5 text-[11px] text-muted-foreground">{flow.store.id}</p>
      </div>
    );
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="mx-3 flex w-[calc(100%-1.5rem)] items-center gap-2.5 rounded-lg border bg-background/60 px-3 py-2.5 text-left transition-colors hover:bg-accent">
          <Store className="h-4 w-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">{flow.store.name}</span>
            <span className="block truncate text-[11px] text-muted-foreground">{flow.store.id}</span>
          </span>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuLabel>Lojas</DropdownMenuLabel>
        {flows.map((f) => (
          <DropdownMenuItem key={f.store.id} onSelect={() => setSelectedStore(f.store.id)}>
            <Store className="h-4 w-4" />
            <span className="truncate">{f.store.name}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SidebarBody({ onNavigate }: { onNavigate?: () => void }) {
  const { flow } = useWorkspace();
  return (
    <div className="flex h-full flex-col bg-card">
      <div className="flex h-16 items-center px-5">
        <Logo />
      </div>
      <StoreSwitcher />
      <nav className="mt-5 flex-1 px-3">
        <p className="mb-2 px-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/80">
          Operação
        </p>
        <ul className="space-y-1">
          {nav.map((item) => (
            <li key={item.to}>
              <NavLink
                to={item.to}
                end={item.end}
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn(
                    "group flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium transition-colors",
                    isActive
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground",
                  )
                }
              >
                {({ isActive }) => (
                  <>
                    <item.icon className={cn("h-4 w-4", isActive ? "text-primary" : "text-muted-foreground")} />
                    {item.label}
                  </>
                )}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
      <div className="border-t p-3">
        <div className="rounded-lg bg-muted/55 px-3 py-2.5">
          <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
            <span className="h-1.5 w-1.5 rounded-full bg-success" />
            Remetente ativo
          </div>
          <p className="mt-1 truncate text-xs text-foreground/80" title={flow.store.from}>
            {flow.store.from}
          </p>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            {flow.store.timezone} · {flow.store.send_window.start}h–{flow.store.send_window.end}h
          </p>
        </div>
      </div>
    </div>
  );
}

function pageTitle(pathname: string) {
  if (pathname.startsWith("/flow")) return "Fluxo";
  if (pathname.startsWith("/orders")) return "Pedidos";
  if (pathname.startsWith("/account")) return "Conta";
  return "Visão geral";
}

export function AppShell() {
  const { me, flow } = useWorkspace();
  const { theme, setTheme, resolvedTheme, toggle } = useTheme();
  const [mobileOpen, setMobileOpen] = useState(false);
  const location = useLocation();

  return (
    <div className="min-h-screen bg-background">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 border-r md:block">
        <SidebarBody />
      </aside>

      <div className="md:pl-64">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-2 border-b bg-background/85 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/70 sm:px-6">
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" className="md:hidden" aria-label="Abrir menu">
                <Menu className="h-5 w-5" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-[280px] p-0">
              <SheetTitle className="sr-only">Navegação</SheetTitle>
              <SidebarBody onNavigate={() => setMobileOpen(false)} />
            </SheetContent>
          </Sheet>

          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium">{pageTitle(location.pathname)}</span>
            <span className="hidden text-muted-foreground/50 sm:inline">/</span>
            <span className="hidden truncate text-sm text-muted-foreground sm:inline">{flow.store.name}</span>
          </div>

          <div className="ml-auto flex items-center gap-1">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label="Tema" title="Tema">
                  {resolvedTheme === "dark" ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel>Tema</DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setTheme("light")}>
                  <Sun className="h-4 w-4" /> Claro
                  {theme === "light" && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setTheme("dark")}>
                  <Moon className="h-4 w-4" /> Escuro
                  {theme === "dark" && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setTheme("system")}>
                  <Monitor className="h-4 w-4" /> Sistema
                  {theme === "system" && <span className="ml-auto text-xs text-muted-foreground">✓</span>}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  className="ml-1 flex items-center gap-2 rounded-full border bg-card p-0.5 pr-2 transition-colors hover:bg-accent"
                  aria-label="Menu da conta"
                >
                  <span className="flex h-7 w-7 items-center justify-center rounded-full bg-primary/12 text-[11px] font-semibold text-primary">
                    {initials(me.email)}
                  </span>
                  <span className="hidden max-w-[150px] truncate text-xs font-medium sm:block">{me.email}</span>
                  <ChevronDown className="hidden h-3 w-3 text-muted-foreground sm:block" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-60">
                <DropdownMenuLabel className="font-normal">
                  <p className="text-xs text-muted-foreground">Conectado como</p>
                  <p className="truncate text-sm font-medium">{me.email}</p>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <NavLink to="/account">
                    <UserRound className="h-4 w-4" />
                    Conta
                  </NavLink>
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => toggle()}>
                  {resolvedTheme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
                  {resolvedTheme === "dark" ? "Modo claro" : "Modo escuro"}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={logout} className="text-destructive focus:text-destructive">
                  <LogOut className="h-4 w-4" />
                  Sair
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        <div className="h-[calc(100vh-3.5rem)] overflow-y-auto">
          <main className="mx-auto w-full max-w-[1440px] p-4 sm:p-6 lg:p-8">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  );
}
