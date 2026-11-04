import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Fingerprint,
  KeyRound,
  Loader2,
  Mail,
  PartyPopper,
  Plus,
  ShieldCheck,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { useWorkspace } from "@/components/workspace";
import { usePasskeys } from "@/lib/queries";
import { api } from "@/lib/api";
import { passkeysSupported, registerPasskey } from "@/lib/passkey";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { formatDate, formatDateTime, initials } from "@/lib/format";
import type { Passkey } from "@/types";

function roleLabel(role: string) {
  return role === "owner" ? "Proprietário" : role === "member" ? "Membro" : role;
}

export function AccountPage() {
  const { me, flows } = useWorkspace();
  const [params, setParams] = useSearchParams();
  const welcome = params.get("welcome") === "1";
  const passkeysQuery = usePasskeys();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [deleting, setDeleting] = useState<Passkey | null>(null);
  const supported = passkeysSupported();

  const addMutation = useMutation({
    mutationFn: () => registerPasskey(name.trim() || "passkey"),
    onSuccess: (key) => {
      toast.success("Passkey adicionada", { description: key.name });
      setName("");
      void qc.invalidateQueries({ queryKey: ["passkeys"] });
    },
    onError: (e: Error) => {
      const msg = e.name === "NotAllowedError" ? "A operação foi cancelada no navegador." : e.message;
      toast.error("Não foi possível adicionar a passkey", { description: msg });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deletePasskey(id),
    onSuccess: () => {
      toast.success("Passkey removida");
      setDeleting(null);
      void qc.invalidateQueries({ queryKey: ["passkeys"] });
    },
    onError: (e: Error) => toast.error("Não foi possível remover", { description: e.message }),
  });

  return (
    <div className="space-y-5">
      <div>
        <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <UserRound className="h-3.5 w-3.5" />
          Sua identidade
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Conta</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Perfil, acessos e passkeys usadas para entrar no painel.
        </p>
      </div>

      {welcome && (
        <div className="flex items-start gap-3 rounded-xl border border-success/30 bg-success/5 px-4 py-3">
          <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-success/10 text-success">
            <PartyPopper className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium">Senha salva. Bem-vindo ao mailflow.</p>
            <p className="mt-0.5 text-xs text-muted-foreground">
              Adicione uma passkey abaixo para entrar sem digitar senha na próxima vez.
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Fechar aviso"
            onClick={() => {
              const next = new URLSearchParams(params);
              next.delete("welcome");
              setParams(next);
            }}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Perfil</CardTitle>
              <CardDescription>A identidade usada neste painel.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-5">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <div className="flex min-w-0 items-center gap-3">
                  <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-base font-semibold text-primary">
                    {initials(me.email)}
                  </span>
                  <div className="min-w-0">
                    <p className="break-all text-sm font-medium">{me.email}</p>
                    <p className="text-xs text-muted-foreground">Conta de acesso ao painel</p>
                  </div>
                </div>
                <Badge variant="secondary" className="w-fit shrink-0 sm:ml-auto">
                  <Mail className="h-3 w-3" />
                  verificado por convite
                </Badge>
              </div>
              <Separator />
              <div>
                <p className="mb-2 text-xs font-medium text-muted-foreground">Acesso às lojas</p>
                <div className="flex flex-wrap gap-2">
                  {me.tenants.length === 0 ? (
                    <span className="text-sm text-muted-foreground">Nenhum tenant vinculado.</span>
                  ) : (
                    me.tenants.map((t) => (
                      <span
                        key={t.id}
                        className="inline-flex items-center gap-2 rounded-lg border bg-muted/30 px-2.5 py-1.5 text-xs"
                      >
                        <span className="font-medium">{t.name || t.id}</span>
                        <Badge variant={t.role === "owner" ? "info" : "muted"}>{roleLabel(t.role)}</Badge>
                      </span>
                    ))
                  )}
                </div>
                <p className="mt-2 text-[11px] text-muted-foreground">
                  {flows.length === 1 ? "1 loja visível" : `${flows.length} lojas visíveis`} neste login.
                </p>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex-row items-start justify-between space-y-0">
              <div>
                <CardTitle className="text-base">Passkeys</CardTitle>
                <CardDescription>
                  Face ID, Touch ID, Windows Hello ou chave de segurança — vinculadas a este domínio.
                </CardDescription>
              </div>
              <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <Fingerprint className="h-4 w-4" />
              </span>
            </CardHeader>
            <CardContent className="space-y-4">
              {!supported && (
                <div className="rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-muted-foreground">
                  Este navegador não suporta WebAuthn. Use um navegador atualizado para cadastrar passkeys.
                </div>
              )}

              {passkeysQuery.isLoading ? (
                <div className="space-y-2">
                  <Skeleton className="h-14 w-full" />
                  <Skeleton className="h-14 w-full" />
                </div>
              ) : (passkeysQuery.data?.length ?? 0) === 0 ? (
                <div className="flex flex-col items-center justify-center rounded-xl border border-dashed px-6 py-8 text-center">
                  <KeyRound className="h-5 w-5 text-muted-foreground" />
                  <p className="mt-2 text-sm font-medium">Nenhuma passkey cadastrada</p>
                  <p className="mt-1 max-w-sm text-xs text-muted-foreground">
                    Adicione uma para entrar sem digitar senha. Você pode manter várias, uma por dispositivo.
                  </p>
                </div>
              ) : (
                <ul className="divide-y rounded-lg border">
                  {passkeysQuery.data?.map((key) => (
                    <li key={key.id} className="flex items-center gap-3 px-3 py-3">
                      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                        <KeyRound className="h-4 w-4" />
                      </span>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">{key.name}</p>
                        <p className="text-[11px] text-muted-foreground">
                          {key.last_used_at
                            ? `Último uso em ${formatDateTime(key.last_used_at)}`
                            : `Criada em ${formatDate(key.created_at)}`}
                        </p>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Remover ${key.name}`}
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setDeleting(key)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </li>
                  ))}
                </ul>
              )}

              <form
                className="flex flex-col gap-2 sm:flex-row"
                onSubmit={(e) => {
                  e.preventDefault();
                  addMutation.mutate();
                }}
              >
                <Label htmlFor="passkey-name" className="sr-only">
                  Nome do dispositivo
                </Label>
                <Input
                  id="passkey-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Nome do dispositivo (ex.: MacBook)"
                  maxLength={60}
                  disabled={!supported || addMutation.isPending}
                />
                <Button type="submit" disabled={!supported || addMutation.isPending}>
                  {addMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                  Adicionar passkey
                </Button>
              </form>
            </CardContent>
          </Card>
        </div>

        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-base">
                <ShieldCheck className="h-4 w-4 text-success" />
                Segurança da sessão
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-xs text-muted-foreground">
              <div className="flex items-start justify-between gap-3">
                <span>Cookie de sessão</span>
                <Badge variant="success">HttpOnly</Badge>
              </div>
              <div className="flex items-start justify-between gap-3">
                <span>SameSite</span>
                <Badge variant="secondary">Lax</Badge>
              </div>
              <div className="flex items-start justify-between gap-3">
                <span>Validade</span>
                <span className="font-medium text-foreground">30 dias</span>
              </div>
              <div className="flex items-start justify-between gap-3">
                <span>CSRF</span>
                <span className="font-medium text-foreground">Origin obrigatória</span>
              </div>
              <Separator />
              <p className="leading-relaxed">
                Sair encerra a sessão no servidor, não apenas no navegador. Passkeys nunca saem do seu dispositivo.
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Senha</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-xs leading-relaxed text-muted-foreground">
                Para redefinir a senha, peça um novo link de convite ao proprietário da conta. O convite é de uso
                único, expira em 48 horas e redefine a senha ao ser aceito.
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      <Dialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Remover esta passkey?</DialogTitle>
            <DialogDescription>
              A passkey <span className="font-medium text-foreground">{deleting?.name}</span> deixará de funcionar
              imediatamente. Se for a única forma de entrar, use sua senha.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              disabled={deleteMutation.isPending}
              onClick={() => deleting && deleteMutation.mutate(deleting.id)}
            >
              {deleteMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              Remover
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
