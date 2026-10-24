import { QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect } from "react";
import { api, type Settings, type Warranty } from "./api";
import { formatDate } from "./dates";
import { formatMoney } from "./utils";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 30_000, retry: (n, err: any) => err?.status !== 401 && err?.status !== 404 && n < 2 },
  },
});

export const keys = {
  me: ["me"] as const,
  settings: ["settings"] as const,
  warranties: ["warranties"] as const,
  warranty: (id: number) => ["warranties", id] as const,
  tags: ["tags"] as const,
};

export function useMe() {
  return useQuery({ queryKey: keys.me, queryFn: api.me, staleTime: Infinity });
}

export function useSettingsQuery() {
  return useQuery({ queryKey: keys.settings, queryFn: api.settings, staleTime: Infinity });
}

const fallback: Pick<Settings, "dateFormat" | "currency" | "expiringSoonDays"> = {
  dateFormat: "yyyy-MM-dd",
  currency: "EUR",
  expiringSoonDays: 30,
};

/**
 * The single source of truth for user formatting preferences. Every date in
 * the UI goes through `fmtDate`, so changing the format applies everywhere.
 */
export function usePrefs() {
  const { data } = useSettingsQuery();
  const s = data ?? fallback;
  const fmtDate = useCallback((iso: string | null | undefined) => formatDate(iso, s.dateFormat), [s.dateFormat]);
  const fmtMoney = useCallback((n: number, cur?: string) => formatMoney(n, cur || s.currency), [s.currency]);
  return { settings: data, dateFormat: s.dateFormat, currency: s.currency, soonDays: s.expiringSoonDays, fmtDate, fmtMoney };
}

export function useWarranties() {
  return useQuery({ queryKey: keys.warranties, queryFn: api.warranties });
}

/** Polls while any Paperless upload is still being processed. */
export function useWarranty(id: number) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: keys.warranty(id),
    queryFn: () => api.warranty(id),
    initialData: () => qc.getQueryData<Warranty[]>(keys.warranties)?.find((w) => w.id === id),
    initialDataUpdatedAt: () => qc.getQueryState(keys.warranties)?.dataUpdatedAt,
    refetchInterval: (query) => (query.state.data?.attachments.some((a) => a.status === "processing") ? 2500 : false),
  });
  const processing = q.data?.attachments.some((a) => a.status === "processing");
  useEffect(() => {
    // When processing finishes, refresh the list so counts/thumbnails update.
    if (q.data && !processing) qc.invalidateQueries({ queryKey: keys.warranties, exact: true });
  }, [processing]); // eslint-disable-line react-hooks/exhaustive-deps
  return q;
}

export function useTags() {
  return useQuery({ queryKey: keys.tags, queryFn: api.tags });
}

/** Invalidates everything warranty-related after a change. */
export function useInvalidate() {
  const qc = useQueryClient();
  return useCallback(() => {
    qc.invalidateQueries({ queryKey: keys.warranties });
    qc.invalidateQueries({ queryKey: keys.tags });
  }, [qc]);
}

export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => qc.setQueryData(keys.settings, s),
  });
}
