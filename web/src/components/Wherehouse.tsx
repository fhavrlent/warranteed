import { useQuery } from "@tanstack/react-query";
import { ExternalLink, Link2, Loader2, MapPin, Plus, Search, Unlink } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { toast } from "sonner";
import { api, errorMessage, type Warranty } from "@/lib/api";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { ConfirmDialog, Dialog } from "./ui/dialog";
import { Field, Input, Select } from "./ui/input";
import { Card } from "./ui/misc";

/** The warranty's thing in Wherehouse, or ways to add or link one. Hidden when the server has no Wherehouse. */
export const WherehouseCard = ({ warranty }: { warranty: Warranty }) => {
  const { t } = useTranslation();
  const { settings } = usePrefs();
  const invalidate = useInvalidate();
  const [adding, setAdding] = useState(false);
  const [linking, setLinking] = useState(false);
  const [unlinking, setUnlinking] = useState(false);
  if (!settings?.wherehouseServerUrl) return null;
  const link = warranty.wherehouse;

  const unlink = async () => {
    try {
      await api.unlinkWherehouse(warranty.id);
      invalidate();
      toast.success(t("wherehouse.unlinked"));
    } catch (e) {
      toast.error(errorMessage(e));
    }
    setUnlinking(false);
  };

  return (
    <Card className="p-4">
      <h3 className="mb-3 flex items-center gap-2 text-xs font-medium text-fg-subtle">
        <MapPin className="size-4" /> Wherehouse
      </h3>
      {link ? (
        <div className="flex flex-col gap-3">
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-[13px] text-fg-subtle">{t("wherehouse.where")}</span>
            <span className="min-w-0 truncate text-right text-sm">{link.path || t("wherehouse.nowhere")}</span>
          </div>
          {link.label && (
            <div className="flex items-baseline justify-between gap-3">
              <span className="text-[13px] text-fg-subtle">{t("wherehouse.label")}</span>
              <span className="font-mono text-sm">{link.label}</span>
            </div>
          )}
          <div className="flex gap-2">
            <a
              href={`${settings.wherehouseServerUrl}/i/${link.thing}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex h-8 flex-1 items-center justify-center gap-1.5 rounded-lg border border-border bg-surface px-3 text-[13px] font-medium shadow-card hover:bg-surface-2"
            >
              <ExternalLink className="size-3.5" /> {t("wherehouse.open")}
            </a>
            <Button variant="ghost" size="sm" onClick={() => setUnlinking(true)} aria-label={t("wherehouse.unlink")}>
              <Unlink />
            </Button>
          </div>
        </div>
      ) : settings.wherehouseConnected ? (
        <div className="flex flex-col gap-2">
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <Plus /> {t("wherehouse.add")}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setLinking(true)}>
            <Link2 /> {t("wherehouse.linkExisting")}
          </Button>
        </div>
      ) : (
        <p className="text-[13px] text-fg-muted">
          {t("wherehouse.connectHint")}{" "}
          <Link to="/settings#wherehouse" className="font-medium text-primary hover:underline">
            {t("nav.settings")}
          </Link>
        </p>
      )}
      {adding && <AddDialog warranty={warranty} onClose={() => setAdding(false)} />}
      {linking && <LinkDialog warranty={warranty} onClose={() => setLinking(false)} />}
      <ConfirmDialog
        open={unlinking}
        onOpenChange={setUnlinking}
        title={t("wherehouse.unlinkTitle")}
        description={t("wherehouse.unlinkText")}
        confirmLabel={t("wherehouse.unlink")}
        onConfirm={() => void unlink()}
      />
    </Card>
  );
};

/** Adds the product to Wherehouse, in a place or not put anywhere yet. */
const AddDialog = ({ warranty, onClose }: { warranty: Warranty; onClose: () => void }) => {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const places = useQuery({ queryKey: ["wherehouse-places"], queryFn: api.wherehousePlaces, retry: false });
  const [place, setPlace] = useState("");
  const [saving, setSaving] = useState(false);
  const add = async () => {
    setSaving(true);
    try {
      await api.addToWherehouse(warranty.id, place ? Number(place) : null);
      toast.success(t("wherehouse.created"));
      onClose();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      invalidate();
      setSaving(false);
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !saving && onClose()}
      size="sm"
      title={t("wherehouse.addTitle")}
      description={t("wherehouse.addDescription")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={add} loading={saving}>
            {t("wherehouse.create")}
          </Button>
        </>
      }
    >
      <Field label={t("wherehouse.place")} optional error={places.error ? errorMessage(places.error) : null}>
        <Select value={place} onChange={(e) => setPlace(e.target.value)} disabled={places.isLoading}>
          <option value="">{t("wherehouse.noPlace")}</option>
          {(places.data ?? []).map((p) => (
            <option key={p.id} value={p.id}>
              {p.path ? `${p.path} › ${p.name}` : p.name}
            </option>
          ))}
        </Select>
      </Field>
    </Dialog>
  );
};

/** Searches Wherehouse for the thing, starting with its serial number or name. */
const LinkDialog = ({ warranty, onClose }: { warranty: Warranty; onClose: () => void }) => {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const [q, setQ] = useState(warranty.serialNumbers[0] ?? warranty.productName);
  const [query, setQuery] = useState(q);
  useEffect(() => {
    const timer = setTimeout(() => setQuery(q), 300);
    return () => clearTimeout(timer);
  }, [q]);
  const search = useQuery({
    queryKey: ["wherehouse-search", query],
    queryFn: () => api.searchWherehouse(query),
    retry: false,
  });
  const [picked, setPicked] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const pickedShown = picked !== null && (search.data ?? []).some((x) => x.id === picked);
  const link = async () => {
    if (picked === null || !pickedShown) return;
    setSaving(true);
    try {
      await api.linkWherehouse(warranty.id, picked);
      toast.success(t("wherehouse.linked"));
      onClose();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      invalidate();
      setSaving(false);
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !saving && onClose()}
      title={t("wherehouse.linkTitle")}
      description={t("wherehouse.linkDescription")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={link} loading={saving} disabled={!pickedShown}>
            {t("wherehouse.linkButton")}
          </Button>
        </>
      }
    >
      <div className="relative mb-3">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-subtle" />
        <Input
          data-autofocus
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setPicked(null);
          }}
          placeholder={t("wherehouse.search")}
          className="pl-9"
        />
        {search.isFetching && (
          <Loader2 className="absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-fg-subtle" />
        )}
      </div>
      {search.error ? (
        <p className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-bad">{errorMessage(search.error)}</p>
      ) : search.data?.length === 0 ? (
        <p className="py-8 text-center text-sm text-fg-muted">{t("wherehouse.noResults")}</p>
      ) : (
        <ul className="flex max-h-80 flex-col gap-1 overflow-y-auto">
          {(search.data ?? []).map((x) => (
            <li key={x.id}>
              <button
                type="button"
                onClick={() => setPicked(x.id)}
                className={cn(
                  "flex w-full cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2 text-left",
                  picked === x.id ? "border-primary bg-primary-soft/50" : "border-border hover:bg-surface-2",
                )}
              >
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{x.name}</span>
                  {x.path && <span className="block truncate text-xs text-fg-subtle">{x.path}</span>}
                </span>
                {x.label && <span className="shrink-0 font-mono text-xs text-fg-subtle">{x.label}</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
};
