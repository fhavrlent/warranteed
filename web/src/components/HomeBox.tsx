import { useQuery } from "@tanstack/react-query";
import { Boxes, ExternalLink, Link2, Loader2, Plus, Search, Unlink } from "lucide-react";
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

/** The warranty's HomeBox item, or ways to add or link one. Hidden when the server has no HomeBox. */
export const HomeBoxCard = ({ warranty }: { warranty: Warranty }) => {
  const { t } = useTranslation();
  const { settings } = usePrefs();
  const invalidate = useInvalidate();
  const [adding, setAdding] = useState(false);
  const [linking, setLinking] = useState(false);
  const [unlinking, setUnlinking] = useState(false);
  if (!settings?.homeboxServerUrl) return null;
  const link = warranty.homebox;

  const unlink = async () => {
    try {
      await api.unlinkHomebox(warranty.id);
      invalidate();
      toast.success(t("homebox.unlinked"));
    } catch (e) {
      toast.error(errorMessage(e));
    }
    setUnlinking(false);
  };

  return (
    <Card className="p-4">
      <h3 className="mb-3 flex items-center gap-2 text-xs font-medium text-fg-subtle">
        <Boxes className="size-4" /> HomeBox
      </h3>
      {link ? (
        <div className="flex flex-col gap-3">
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-[13px] text-fg-subtle">{t("homebox.assetId")}</span>
            <span className="font-mono text-sm">{link.assetId || "—"}</span>
          </div>
          <div className="flex gap-2">
            <a
              href={`${settings.homeboxServerUrl}/item/${encodeURIComponent(link.id)}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex h-8 flex-1 items-center justify-center gap-1.5 rounded-lg border border-border bg-surface px-3 text-[13px] font-medium shadow-card hover:bg-surface-2"
            >
              <ExternalLink className="size-3.5" /> {t("homebox.open")}
            </a>
            <Button variant="ghost" size="sm" onClick={() => setUnlinking(true)} aria-label={t("homebox.unlink")}>
              <Unlink />
            </Button>
          </div>
        </div>
      ) : settings.homeboxEnabled ? (
        <div className="flex flex-col gap-2">
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <Plus /> {t("homebox.add")}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setLinking(true)}>
            <Link2 /> {t("homebox.linkExisting")}
          </Button>
        </div>
      ) : (
        <p className="text-[13px] text-fg-muted">
          {t("homebox.connectHint")}{" "}
          <Link to="/settings#homebox" className="font-medium text-primary hover:underline">
            {t("nav.settings")}
          </Link>
        </p>
      )}
      {adding && <AddDialog warranty={warranty} onClose={() => setAdding(false)} />}
      {linking && <LinkDialog warranty={warranty} onClose={() => setLinking(false)} />}
      <ConfirmDialog
        open={unlinking}
        onOpenChange={setUnlinking}
        title={t("homebox.unlinkTitle")}
        description={t("homebox.unlinkText")}
        confirmLabel={t("homebox.unlink")}
        onConfirm={() => void unlink()}
      />
    </Card>
  );
};

/** Creates the item in HomeBox, optionally in a location. */
const AddDialog = ({ warranty, onClose }: { warranty: Warranty; onClose: () => void }) => {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const locations = useQuery({ queryKey: ["homebox-locations"], queryFn: api.homeboxLocations, retry: false });
  const [locationId, setLocationId] = useState("");
  const [saving, setSaving] = useState(false);

  const add = async () => {
    setSaving(true);
    try {
      await api.createHomeboxItem(warranty.id, locationId);
      toast.success(t("homebox.created"));
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
      title={t("homebox.addTitle")}
      description={t("homebox.addDescription")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={add} loading={saving}>
            {t("homebox.create")}
          </Button>
        </>
      }
    >
      <Field label={t("homebox.location")} optional error={locations.error ? errorMessage(locations.error) : null}>
        <Select value={locationId} onChange={(e) => setLocationId(e.target.value)} disabled={locations.isLoading}>
          <option value="">{t("homebox.noLocation")}</option>
          {(locations.data ?? []).map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </Select>
      </Field>
    </Dialog>
  );
};

/** Searches HomeBox for an existing item, suggesting a match by serial number first. */
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
    queryKey: ["homebox-search", query],
    queryFn: () => api.searchHomebox(query),
    retry: false,
  });
  const [picked, setPicked] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const link = async () => {
    if (!picked) return;
    setSaving(true);
    try {
      await api.linkHomeboxItem(warranty.id, picked);
      toast.success(t("homebox.linked"));
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
      title={t("homebox.linkTitle")}
      description={t("homebox.linkDescription")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={link} loading={saving} disabled={!picked}>
            {t("homebox.linkButton")}
          </Button>
        </>
      }
    >
      <div className="relative mb-3">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-subtle" />
        <Input
          data-autofocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t("homebox.search")}
          className="pl-9"
        />
        {search.isFetching && (
          <Loader2 className="absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-fg-subtle" />
        )}
      </div>
      {search.error ? (
        <p className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-bad">{errorMessage(search.error)}</p>
      ) : search.data?.length === 0 ? (
        <p className="py-8 text-center text-sm text-fg-muted">{t("homebox.noResults")}</p>
      ) : (
        <ul className="flex max-h-80 flex-col gap-1 overflow-y-auto">
          {(search.data ?? []).map((item) => (
            <li key={item.id}>
              <button
                type="button"
                onClick={() => setPicked(item.id)}
                className={cn(
                  "flex w-full cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2 text-left",
                  picked === item.id ? "border-primary bg-primary-soft/50" : "border-border hover:bg-surface-2",
                )}
              >
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{item.name}</span>
                  {item.location && <span className="block truncate text-xs text-fg-subtle">{item.location}</span>}
                </span>
                {item.assetId && <span className="shrink-0 font-mono text-xs text-fg-subtle">{item.assetId}</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
};
