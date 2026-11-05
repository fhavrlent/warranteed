import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { api, errorMessage, type Warranty } from "@/lib/api";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Field, Input } from "./ui/input";
import { Switch } from "./ui/misc";

/**
 * Moves some pieces of a warranty to a new one, e.g. when one is sold or
 * lent, so each can be archived, voided or claimed on by itself.
 */
export const SplitDialog = ({ w, onClose }: { w: Warranty; onClose: () => void }) => {
  const { t } = useTranslation();
  const { settings } = usePrefs();
  const invalidate = useInvalidate();
  const navigate = useNavigate();
  const max = w.quantity - 1;
  const [pieces, setPieces] = useState("1");
  const [serials, setSerials] = useState<string[]>([]);
  const canSplitItem = w.homebox !== null && (settings?.homeboxEnabled ?? false);
  const [homebox, setHomebox] = useState(canSplitItem);
  const canSplitThing = w.wherehouse !== null && (settings?.wherehouseConnected ?? false);
  const [wherehouse, setWherehouse] = useState(canSplitThing);
  const [saving, setSaving] = useState(false);
  const n = Number(pieces);
  const valid = Number.isInteger(n) && n >= 1 && n <= max;

  const submit = async () => {
    if (!valid) return;
    setSaving(true);
    try {
      const res = await api.splitWarranty(w.id, {
        pieces: n,
        serialNumbers: serials,
        homebox: homebox && canSplitItem,
        wherehouse: wherehouse && canSplitThing,
      });
      invalidate();
      toast.success(t("split.done", { count: n }));
      if (res.homeboxError) toast.error(t("split.homeboxFailed", { error: res.homeboxError }));
      if (res.wherehouseError) toast.error(t("split.wherehouseFailed", { error: res.wherehouseError }));
      onClose();
      void navigate(`/w/${res.warranty.id}`);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      size="sm"
      title={t("split.title")}
      description={t("split.description", { count: w.quantity })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void submit()} loading={saving} disabled={!valid}>
            {t("split.confirm")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <Field
          label={t("split.pieces")}
          htmlFor="splitPieces"
          error={pieces !== "" && !valid ? t("split.piecesRange", { max }) : null}
          hint={valid ? t("split.left", { count: w.quantity - n }) : undefined}
        >
          <Input
            id="splitPieces"
            data-autofocus
            type="number"
            inputMode="numeric"
            min={1}
            max={max}
            step={1}
            value={pieces}
            className="w-24 tabular-nums"
            onChange={(e) => setPieces(e.target.value)}
          />
        </Field>
        {w.serialNumbers.length > 0 && (
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1.5 text-[13px] font-medium">{t("split.serials")}</legend>
            {w.serialNumbers.map((sn) => (
              <label key={sn} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="size-4 accent-primary"
                  checked={serials.includes(sn)}
                  onChange={(e) => setSerials((p) => (e.target.checked ? [...p, sn] : p.filter((x) => x !== sn)))}
                />
                <span className="font-mono text-[13px]">{sn}</span>
              </label>
            ))}
          </fieldset>
        )}
        {canSplitItem && (
          <label htmlFor="splitHomeBox" className="flex items-start justify-between gap-4">
            <span>
              <span className="block text-[13px] font-medium">{t("split.homebox")}</span>
              <span className="block text-xs text-fg-subtle">{t("split.homeboxHint")}</span>
            </span>
            <Switch id="splitHomeBox" checked={homebox} onCheckedChange={setHomebox} />
          </label>
        )}
        {canSplitThing && (
          <label htmlFor="splitWherehouse" className="flex items-start justify-between gap-4">
            <span>
              <span className="block text-[13px] font-medium">{t("split.wherehouse")}</span>
              <span className="block text-xs text-fg-subtle">{t("split.wherehouseHint")}</span>
            </span>
            <Switch id="splitWherehouse" checked={wherehouse} onCheckedChange={setWherehouse} />
          </label>
        )}
        <p className="text-xs text-fg-subtle">{t("split.copies")}</p>
      </div>
    </Dialog>
  );
};
