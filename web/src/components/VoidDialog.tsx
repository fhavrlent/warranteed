import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { api, errorMessage, type Warranty } from "@/lib/api";
import { useInvalidate } from "@/lib/queries";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Field, Textarea } from "./ui/input";

const MAX_REASON = 500;

/** Asks why a warranty is void, then marks it so. */
export const VoidDialog = ({ w, onOpenChange }: { w: Warranty; onOpenChange: (open: boolean) => void }) => {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);

  const submit = async () => {
    setSaving(true);
    try {
      await api.setVoid(w.id, true, reason);
      invalidate();
      toast.success(t("detail.voided"));
      onOpenChange(false);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={onOpenChange}
      size="sm"
      title={t("detail.voidTitle")}
      description={t("detail.voidText")}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void submit()} loading={saving}>
            {t("detail.markVoid")}
          </Button>
        </>
      }
    >
      <Field label={t("detail.voidReason")} htmlFor="voidReason" optional>
        <Textarea
          id="voidReason"
          data-autofocus
          rows={3}
          maxLength={MAX_REASON}
          value={reason}
          placeholder={t("detail.voidReasonPlaceholder")}
          onChange={(e) => setReason(e.target.value)}
        />
      </Field>
    </Dialog>
  );
};
