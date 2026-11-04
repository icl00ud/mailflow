import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, PackagePlus } from "lucide-react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { api } from "@/lib/api";

type FormValues = {
  email: string;
  name: string;
  customer: string;
  item: string;
  total: string;
};

export function CreateOrderDialog({ storeId }: { storeId: string }) {
  const [open, setOpen] = useState(false);
  const qc = useQueryClient();
  const form = useForm<FormValues>({
    defaultValues: {
      email: "cliente@exemplo.com",
      name: "",
      customer: "Alex Morgan",
      item: "Azure Chronograph Leather",
      total: "$104.99",
    },
  });

  const mutation = useMutation({
    mutationFn: (values: FormValues) =>
      api.createOrder({
        store: storeId,
        email: values.email.trim(),
        name: values.name.trim() || undefined,
        customer: values.customer.trim() || undefined,
        item: values.item.trim() || undefined,
        total: values.total.trim() || undefined,
      }),
    onSuccess: (order) => {
      toast.success("Pedido de teste criado", {
        description: `${order.name} · ${order.email} entrou na jornada.`,
      });
      void qc.invalidateQueries({ queryKey: ["orders"] });
      void qc.invalidateQueries({ queryKey: ["flows"] });
      form.reset();
      setOpen(false);
    },
    onError: (e: Error) => toast.error("Não foi possível criar o pedido", { description: e.message }),
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <PackagePlus className="h-4 w-4" />
          Criar pedido
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Pedido de teste</DialogTitle>
          <DialogDescription>
            Cria um pedido manual na loja <span className="font-medium text-foreground">{storeId}</span> e agenda os
            passos disparados por “pedido pago”. Nenhum e-mail real é enviado pela criação.
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))} className="space-y-4">
            <FormField
              control={form.control}
              name="email"
              rules={{
                required: "Informe o e-mail do cliente.",
                pattern: { value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/, message: "E-mail inválido." },
              }}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>E-mail do cliente</FormLabel>
                  <FormControl>
                    <Input placeholder="cliente@exemplo.com" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Número do pedido</FormLabel>
                    <FormControl>
                      <Input placeholder="#T1042 (automático)" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="customer"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Cliente</FormLabel>
                    <FormControl>
                      <Input placeholder="Alex Morgan" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="item"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Produto</FormLabel>
                    <FormControl>
                      <Input placeholder="Azure Chronograph" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="total"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Total</FormLabel>
                    <FormControl>
                      <Input placeholder="$104.99" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <FormDescription>
              O pedido recebe o horário atual como pagamento e entra no topo da lista de pedidos.
            </FormDescription>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                Cancelar
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <PackagePlus className="h-4 w-4" />}
                Criar pedido
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
