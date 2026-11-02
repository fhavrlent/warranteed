import {
  CircleAlert,
  FileText,
  Link2,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
  Upload,
  Wrench,
} from "lucide-react";
import { useRef, useState, type SubmitEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  api,
  CLAIM_STATUSES,
  errorMessage,
  fileUrl,
  type Claim,
  type ClaimInput,
  type ClaimStatus,
  type Warranty,
} from "@/lib/api";
import { todayISO } from "@/lib/dates";
import { paperlessAccepts } from "@/lib/documents";
import { formatPriceInput, parsePrice } from "@/lib/price";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { PaperlessPicker } from "./Attachments";
import { DateInput } from "./DateInput";
import { Button } from "./ui/button";
import { ConfirmDialog, Dialog } from "./ui/dialog";
import { Field, Input, Select, Textarea } from "./ui/input";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "./ui/menu";

const statusStyles: Record<ClaimStatus, string> = {
  open: "bg-warn-soft text-warn",
  repaired: "bg-ok-soft text-ok",
  replaced: "bg-ok-soft text-ok",
  refunded: "bg-ok-soft text-ok",
  rejected: "bg-bad-soft text-bad",
};

export const ClaimBadge = ({ status, className }: { status: ClaimStatus; className?: string }) => {
  const { t } = useTranslation();
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center rounded-full px-2 text-xs font-semibold whitespace-nowrap",
        statusStyles[status],
        className,
      )}
    >
      {t(`claims.status.${status}`)}
    </span>
  );
};

/** A warranty's repairs and claims, newest first. */
export const ClaimsPanel = ({ warranty }: { warranty: Warranty }) => {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const [editing, setEditing] = useState<Claim | "new" | null>(null);
  const [deleting, setDeleting] = useState<Claim | null>(null);

  const remove = async () => {
    if (!deleting) return;
    try {
      await api.deleteClaim(deleting.id);
      invalidate();
      toast.success(t("claims.deleted"));
    } catch (e) {
      toast.error(errorMessage(e));
    }
    setDeleting(null);
  };

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Wrench className="size-4 text-fg-subtle" />
          {t("claims.title")}
          <span className="font-normal text-fg-subtle tabular-nums">{warranty.claims.length || ""}</span>
        </h2>
        <Button size="sm" variant="outline" onClick={() => setEditing("new")}>
          <Plus /> {t("claims.add")}
        </Button>
      </div>
      {warranty.claims.length === 0 ? (
        <p className="text-[13px] text-fg-muted">{t("claims.empty")}</p>
      ) : (
        <div className="divide-y divide-border">
          {warranty.claims.map((c) => (
            <ClaimRow
              key={c.id}
              claim={c}
              warranty={warranty}
              onEdit={() => setEditing(c)}
              onDelete={() => setDeleting(c)}
            />
          ))}
        </div>
      )}
      {editing && (
        <ClaimDialog warranty={warranty} claim={editing === "new" ? null : editing} onClose={() => setEditing(null)} />
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t("claims.deleteTitle", { title: deleting?.title ?? "" })}
        description={warranty.homebox ? t("claims.deleteTextHomeBox") : t("claims.deleteText")}
        onConfirm={() => void remove()}
      />
    </div>
  );
};

const ClaimRow = ({
  claim: c,
  warranty,
  onEdit,
  onDelete,
}: {
  claim: Claim;
  warranty: Warranty;
  onEdit: () => void;
  onDelete: () => void;
}) => {
  const { t } = useTranslation();
  const { settings, fmtDate, fmtMoney } = usePrefs();
  const invalidate = useInvalidate();
  const fileRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [picker, setPicker] = useState(false);
  const docs = warranty.attachments.filter((a) => a.claimId === c.id);
  const paperless = settings?.paperlessEnabled ?? false;

  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setUploading(true);
    for (const file of Array.from(files)) {
      // Where the user keeps documents by default, if Paperless takes the file.
      const storage = paperless && settings?.paperlessDefault && paperlessAccepts(file) ? "paperless" : "local";
      try {
        await api.upload(warranty.id, file, "claim", storage, c.id);
      } catch (e) {
        toast.error(`${file.name}: ${errorMessage(e)}`);
      }
    }
    setUploading(false);
    invalidate();
  };

  const facts = [
    t("claims.reportedOn", { date: fmtDate(c.reportedOn) }),
    c.resolvedOn && t("claims.resolvedOn", { date: fmtDate(c.resolvedOn) }),
    c.caseNumber && t("claims.caseNumber", { number: c.caseNumber }),
    c.cost != null && t("claims.costOf", { cost: fmtMoney(c.cost, warranty.currency) }),
  ].filter(Boolean);

  return (
    <article className="py-4 first:pt-0 last:pb-0">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-medium [overflow-wrap:anywhere]">{c.title}</h3>
            <ClaimBadge status={c.status} />
          </div>
          <p className="mt-1 text-[13px] text-fg-muted">{facts.join(" · ")}</p>
        </div>
        <Menu>
          <MenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              loading={uploading}
              aria-label={t("claims.actions", { title: c.title })}
            >
              {!uploading && <MoreHorizontal />}
            </Button>
          </MenuTrigger>
          <MenuContent>
            <MenuItem icon={<Pencil />} onSelect={onEdit}>
              {t("claims.edit")}
            </MenuItem>
            <MenuItem icon={<Upload />} onSelect={() => fileRef.current?.click()}>
              {t("claims.addDocuments")}
            </MenuItem>
            {paperless && (
              <MenuItem icon={<Link2 />} onSelect={() => setPicker(true)}>
                {t("attachments.linkFromPaperless")}
              </MenuItem>
            )}
            <MenuSeparator />
            <MenuItem icon={<Trash2 />} danger onSelect={onDelete}>
              {t("claims.delete")}
            </MenuItem>
          </MenuContent>
        </Menu>
      </div>
      {c.notes && <p className="mt-2 text-sm [overflow-wrap:anywhere] whitespace-pre-line">{c.notes}</p>}
      {docs.length > 0 && (
        <ul className="mt-2 flex flex-wrap gap-2">
          {docs.map((a) => (
            <li key={a.id}>
              {/* Only a ready document can be opened; Paperless may still be processing it. */}
              {a.status === "ready" ? (
                <a
                  href={fileUrl(a)}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex max-w-64 items-center gap-1.5 rounded-lg border border-border px-2 py-1 text-[13px] hover:bg-surface-2"
                >
                  <FileText className="size-3.5 shrink-0 text-fg-subtle" />
                  <span className="truncate">{a.filename}</span>
                </a>
              ) : (
                <span
                  title={a.status === "failed" ? a.error : undefined}
                  className={cn(
                    "inline-flex max-w-64 items-center gap-1.5 rounded-lg border border-dashed px-2 py-1 text-[13px]",
                    a.status === "failed" ? "border-bad/40 text-bad" : "border-border text-fg-muted",
                  )}
                >
                  {a.status === "failed" ? (
                    <CircleAlert className="size-3.5 shrink-0" />
                  ) : (
                    <Loader2 className="size-3.5 shrink-0 animate-spin" />
                  )}
                  <span className="truncate">{a.filename}</span>
                  <span className="shrink-0 text-xs">
                    {a.status === "failed" ? t("attachments.failed") : t("attachments.processing")}
                  </span>
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
      <input
        ref={fileRef}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          void upload(e.target.files);
          e.target.value = "";
        }}
      />
      {picker && <PaperlessPicker onOpenChange={setPicker} warranty={warranty} onLinked={invalidate} claimId={c.id} />}
    </article>
  );
};

const ClaimDialog = ({
  warranty,
  claim,
  onClose,
}: {
  warranty: Warranty;
  claim: Claim | null;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const { currency } = usePrefs();
  const [f, setF] = useState(() => ({
    title: claim?.title ?? "",
    status: claim?.status ?? "open",
    reportedOn: claim?.reportedOn ?? todayISO(),
    resolvedOn: claim?.resolvedOn ?? (null as string | null),
    caseNumber: claim?.caseNumber ?? "",
    cost: claim?.cost != null ? formatPriceInput(claim.cost) : "",
    notes: claim?.notes ?? "",
  }));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((p) => ({ ...p, [k]: v }));

  const submit = async (ev: SubmitEvent<HTMLFormElement>) => {
    ev.preventDefault();
    const e: Record<string, string> = {};
    if (!f.title.trim()) e.title = t("claims.errTitle");
    if (!f.reportedOn) e.reportedOn = t("claims.errReported");
    const cost = parsePrice(f.cost);
    if (Number.isNaN(cost)) e.cost = t("form.errPrice", { example: formatPriceInput(1379.5) });
    if (f.status !== "open" && f.resolvedOn && f.resolvedOn < f.reportedOn) e.resolvedOn = t("claims.errResolved");
    setErrors(e);
    if (Object.keys(e).length) return;

    const input: ClaimInput = {
      title: f.title,
      status: f.status,
      reportedOn: f.reportedOn,
      resolvedOn: f.status === "open" ? null : f.resolvedOn,
      caseNumber: f.caseNumber,
      cost,
      notes: f.notes,
    };
    setSaving(true);
    try {
      if (claim) await api.updateClaim(claim.id, input);
      else await api.createClaim(warranty.id, input);
      invalidate();
      toast.success(claim ? t("claims.saved") : t("claims.added"));
      onClose();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={claim ? t("claims.editTitle") : t("claims.addTitle")}
      description={warranty.homebox ? t("claims.homeboxHint") : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" form="claim-form" loading={saving}>
            {claim ? t("claims.save") : t("claims.add")}
          </Button>
        </>
      }
    >
      <form id="claim-form" onSubmit={(e) => void submit(e)} className="grid gap-4 sm:grid-cols-2" noValidate>
        <Field label={t("claims.whatsWrong")} htmlFor="claimTitle" error={errors.title} className="sm:col-span-2">
          <Input
            id="claimTitle"
            data-autofocus
            maxLength={200}
            value={f.title}
            placeholder={t("claims.whatsWrongPlaceholder")}
            aria-invalid={!!errors.title || undefined}
            onChange={(e) => set("title", e.target.value)}
          />
        </Field>
        <Field label={t("claims.reported")} htmlFor="claimReported" error={errors.reportedOn}>
          <DateInput
            id="claimReported"
            value={f.reportedOn}
            onChange={(v) => set("reportedOn", v ?? "")}
            invalid={!!errors.reportedOn}
          />
        </Field>
        <Field label={t("claims.statusLabel")} htmlFor="claimStatus">
          <Select id="claimStatus" value={f.status} onChange={(e) => set("status", e.target.value as ClaimStatus)}>
            {CLAIM_STATUSES.map((s) => (
              <option key={s} value={s}>
                {t(`claims.status.${s}`)}
              </option>
            ))}
          </Select>
        </Field>
        {f.status !== "open" && (
          <Field label={t("claims.resolved")} htmlFor="claimResolved" error={errors.resolvedOn} optional>
            <DateInput
              id="claimResolved"
              value={f.resolvedOn}
              onChange={(v) => set("resolvedOn", v)}
              invalid={!!errors.resolvedOn}
            />
          </Field>
        )}
        <Field label={t("claims.case")} htmlFor="claimCase" optional>
          <Input id="claimCase" value={f.caseNumber} onChange={(e) => set("caseNumber", e.target.value)} />
        </Field>
        <Field
          label={t("claims.cost")}
          htmlFor="claimCost"
          error={errors.cost}
          hint={t("claims.costHint", { currency: warranty.currency || currency })}
          optional
        >
          <Input
            id="claimCost"
            inputMode="decimal"
            value={f.cost}
            className="tabular-nums"
            aria-invalid={!!errors.cost || undefined}
            onChange={(e) => set("cost", e.target.value)}
          />
        </Field>
        <Field label={t("claims.notes")} htmlFor="claimNotes" className="sm:col-span-2" optional>
          <Textarea
            id="claimNotes"
            rows={3}
            maxLength={5000}
            value={f.notes}
            placeholder={t("claims.notesPlaceholder")}
            onChange={(e) => set("notes", e.target.value)}
          />
        </Field>
      </form>
    </Dialog>
  );
};

/** Shows in the list that a warranty has a claim still open; compact is the icon alone. */
export const OpenClaimMark = ({ w, compact }: { w: Warranty; compact?: boolean }) => {
  const { t } = useTranslation();
  if (!w.claims.some((c) => c.status === "open")) return null;
  if (compact) {
    return (
      <span title={t("claims.openMark")} className="inline-flex">
        <Wrench className="size-4 shrink-0 text-warn" aria-label={t("claims.openMark")} role="img" />
      </span>
    );
  }
  return (
    <span className="inline-flex h-6 items-center gap-1 rounded-full bg-warn-soft px-2 text-xs font-semibold whitespace-nowrap text-warn">
      <Wrench className="size-3.5" strokeWidth={2.25} />
      {t("claims.openMark")}
    </span>
  );
};
