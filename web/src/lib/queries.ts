import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export function useMe() {
  return useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: 60_000 });
}

export function useFlows() {
  return useQuery({ queryKey: ["flows"], queryFn: api.flows, staleTime: 15_000 });
}

export function useOrders(store?: string) {
  return useQuery({
    queryKey: ["orders", store ?? "all"],
    queryFn: () => api.orders(store),
    staleTime: 10_000,
  });
}

export function usePasskeys() {
  return useQuery({ queryKey: ["passkeys"], queryFn: api.passkeys, staleTime: 10_000 });
}
