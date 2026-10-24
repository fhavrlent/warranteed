import { addMonths, isAfter } from "date-fns";
import { FileText, Plus, Upload, X } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { api, type AttachmentKind, type Warranty, type WarrantyInput } from "@/lib/api";
import { durationLabel, fromISO, toISO, todayISO } from "@/lib/dates";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { CURRENCIES, cn, formatBytes } from "@/lib/utils";
import { DateInput } from "./DateInput";
import { TagInput } from "./Tags";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Field, Input, Select, Textarea } from "./ui/input";
import { Segmented, Switch } from "./ui/misc";

type Mode = "duration" | "date" | "lifetime";
type Unit = "years" | "months";
type PendingFile = { file: File; kind: AttachmentKind };

const PRESETS = [12, 24, 36, 60];

function initial(w: Warranty | undefined, currency: string) {
  const months = w?.warrantyMonths ?? 24;
  return {
    productName: w?.productName ?? "",
    brand: w?.brand ?? "",
    model: w?.model ?? "",
    vendor: w?.vendor ?? "",
    orderNumber: w?.orderNumber ?? "",
    purchaseDate: w?.purchaseDate ?? todayISO(),
    mode: (w?.isLifetime ? "lifetime" : w?.warrantyEndDate ? "date" : "duration") as Mode,
    amount: String(months % 12 === 0 ? months / 12 : months),
    unit: (months % 12 === 0 ? "years" : "months") as Unit,
    endDate: w?.warrantyEndDate ?? null,
    price: w?.price != null ? String(w.price) : "",
    currency: w?.currency || currency,
    productUrl: w?.productUrl ?? "",
    notes: w?.notes ?? "",
    serials: w?.serialNumbers.length ? [...w.serialNumbers] : [""],
    tags: w?.tags.map((t) => t.name) ?? [],
  };
}

function guessKind(f: File): AttachmentKind {
  const n = f.name.toLowerCase();
  if (f.type.startsWith("image/") && !/(receipt|invoice|bill|uctenka|faktura)/.test(n)) return "photo";
  if (/(invoice|faktura|rechnung)/.test(n)) return "invoice";
  if (/(manual|guide|navod|handbuch)/.test(n)) return "manual";
  return "receipt";
}

export function WarrantyForm({
  open,
  onOpenChange,
  warranty,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  warranty?: Warranty;
}) {
  const { currency, settings } = usePrefs();
  const [f, setF] = useState(() => initial(warranty, currency));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [files, setFiles] = useState<PendingFile[]>([]);
  const [toPaperless, setToPaperless] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const invalidate = useInvalidate();
  const navigate = useNavigate();
  const editing = !!warranty;
  const paperless = settings?.paperlessEnabled ?? false;

  useEffect(() => {
    if (open) {
      setF(initial(warranty, currency));
      setErrors({});
      setFiles([]);
      setToPaperless(paperless && (settings?.paperlessDefault ?? false));
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((p) => ({ ...p, [k]: v }));

  const months = (() => {
    const n = parseInt(f.amount, 10);
    if (!Number.isFinite(n) || n <= 0) return null;
    return f.unit === "years" ? n * 12 : n;
  })();

  const computedExpiry =
    f.mode === "lifetime"
      ? null
      : f.mode === "date"
        ? f.endDate
        : months && f.purchaseDate
          ? toISO(addMonths(fromISO(f.purchaseDate), months))
          : null;
  const { fmtDate } = usePrefs();

  const validate = () => {
    const e: Record<string, string> = {};
    if (!f.productName.trim()) e.productName = "Give the product a name";
    if (!f.purchaseDate) e.purchaseDate = "When did you buy it?";
    if (f.mode === "duration" && !months) e.amount = "Enter a length";
    if (f.mode === "date") {
      if (!f.endDate) e.endDate = "Pick the end date";
      else if (f.purchaseDate && isAfter(fromISO(f.purchaseDate), fromISO(f.endDate))) e.endDate = "Ends before the purchase date";
    }
    if (f.price && !Number.isFinite(Number(f.price.replace(",", ".")))) e.price = "Not a number";
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const submit = async (ev: FormEvent) => {
    ev.preventDefault();
    if (!validate()) return;
    const input: WarrantyInput = {
      productName: f.productName,
      brand: f.brand,
      model: f.model,
      vendor: f.vendor,
      orderNumber: f.orderNumber,
      purchaseDate: f.purchaseDate,
      warrantyMonths: f.mode === "duration" ? months : null,
      warrantyEndDate: f.mode === "date" ? f.endDate : null,
      isLifetime: f.mode === "lifetime",
      price: f.price ? Number(f.price.replace(",", ".")) : null,
      currency: f.currency,
      productUrl: f.productUrl,
      notes: f.notes,
      serialNumbers: f.serials,
      tags: f.tags,
    };
    setSaving(true);
    try {
      const saved = editing ? await api.updateWarranty(warranty.id, input) : await api.createWarranty(input);
      let failed = 0;
      for (const p of files) {
        try {
          await api.upload(saved.id, p.file, p.kind, toPaperless && p.kind !== "photo" ? "paperless" : "local");
        } catch (err: any) {
          failed++;
          toast.error(`${p.file.name}: ${err.message}`);
        }
      }
      invalidate();
      onOpenChange(false);
      toast.success(editing ? "Changes saved" : `${saved.productName} added`, {
        description: failed ? `${failed} file(s) could not be uploaded` : undefined,
      });
      if (!editing) navigate(`/w/${saved.id}`);
    } catch (err: any) {
      toast.error(err.message);
    } finally {
      setSaving(false);
    }
  };

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    setFiles((prev) => [...prev, ...Array.from(list).map((file) => ({ file, kind: guessKind(file) }))]);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? "Edit warranty" : "Add a warranty"}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="warranty-form" loading={saving}>
            {editing ? "Save changes" : "Add warranty"}
          </Button>
        </>
      }
    >
      <form id="warranty-form" onSubmit={submit} className="flex flex-col gap-6" noValidate>
        <section className="grid gap-4 sm:grid-cols-2">
          <Field label="Product" htmlFor="productName" error={errors.productName} className="sm:col-span-2">
            <Input
              id="productName"
              data-autofocus
              value={f.productName}
              placeholder="e.g. Dishwasher, MacBook Air, Drill"
              aria-invalid={!!errors.productName || undefined}
              onChange={(e) => set("productName", e.target.value)}
            />
          </Field>
          <Field label="Brand" htmlFor="brand" optional>
            <Input id="brand" value={f.brand} placeholder="Bosch" onChange={(e) => set("brand", e.target.value)} />
          </Field>
          <Field label="Model" htmlFor="model" optional>
            <Input id="model" value={f.model} placeholder="SMS4HVI33E" onChange={(e) => set("model", e.target.value)} />
          </Field>
        </section>

        <section className="grid gap-4 sm:grid-cols-2">
          <Field label="Purchase date" htmlFor="purchaseDate" error={errors.purchaseDate}>
            <DateInput id="purchaseDate" value={f.purchaseDate} onChange={(v) => set("purchaseDate", v ?? "")} invalid={!!errors.purchaseDate} />
          </Field>
          <Field label="Bought from" htmlFor="vendor" optional>
            <Input id="vendor" value={f.vendor} placeholder="Store or website" onChange={(e) => set("vendor", e.target.value)} />
          </Field>
          <Field label="Price" htmlFor="price" error={errors.price} optional>
            <div className="flex gap-2">
              <Input
                id="price"
                inputMode="decimal"
                value={f.price}
                placeholder="0.00"
                className="tabular-nums"
                onChange={(e) => set("price", e.target.value)}
              />
              <Select aria-label="Currency" value={f.currency} onChange={(e) => set("currency", e.target.value)} className="w-24">
                {[...new Set([f.currency, ...CURRENCIES])].map((c) => (
                  <option key={c}>{c}</option>
                ))}
              </Select>
            </div>
          </Field>
          <Field label="Order number" htmlFor="orderNumber" optional>
            <Input id="orderNumber" value={f.orderNumber} onChange={(e) => set("orderNumber", e.target.value)} />
          </Field>
        </section>

        <section className="rounded-xl border border-border bg-surface-2 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h3 className="text-[13px] font-medium">Warranty</h3>
            <Segmented
              size="sm"
              value={f.mode}
              onChange={(m) => set("mode", m)}
              options={[
                { value: "duration", label: "Length" },
                { value: "date", label: "End date" },
                { value: "lifetime", label: "Lifetime" },
              ]}
            />
          </div>
          <div className="mt-3">
            {f.mode === "duration" && (
              <div className="flex flex-wrap items-start gap-2">
                <div className="flex gap-2">
                  <Input
                    aria-label="Warranty length"
                    inputMode="numeric"
                    value={f.amount}
                    aria-invalid={!!errors.amount || undefined}
                    onChange={(e) => set("amount", e.target.value.replace(/\D/g, ""))}
                    className="w-20 tabular-nums"
                  />
                  <Select aria-label="Unit" value={f.unit} onChange={(e) => set("unit", e.target.value as Unit)} className="w-28">
                    <option value="years">years</option>
                    <option value="months">months</option>
                  </Select>
                </div>
                <div className="flex flex-wrap gap-1">
                  {PRESETS.map((m) => (
                    <button
                      key={m}
                      type="button"
                      onClick={() => setF((p) => ({ ...p, amount: String(m / 12), unit: "years" }))}
                      className={cn(
                        "h-9 cursor-pointer rounded-lg border px-3 text-[13px] font-medium transition-colors",
                        months === m ? "border-primary bg-primary-soft text-primary" : "border-border bg-surface text-fg-muted hover:text-fg",
                      )}
                    >
                      {durationLabel(m)}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {f.mode === "date" && (
              <div className="max-w-56">
                <DateInput value={f.endDate} onChange={(v) => set("endDate", v)} invalid={!!errors.endDate} />
              </div>
            )}
            {f.mode === "lifetime" && <p className="text-sm text-fg-muted">This warranty never expires.</p>}
            {(errors.amount || errors.endDate) && <p className="mt-1.5 text-xs text-bad">{errors.amount || errors.endDate}</p>}
            {computedExpiry && !errors.endDate && (
              <p className="mt-2.5 text-[13px] text-fg-muted">
                Covered until <span className="font-medium text-fg">{fmtDate(computedExpiry)}</span>
              </p>
            )}
          </div>
        </section>

        <section className="grid gap-4 sm:grid-cols-2">
          <Field label="Serial numbers" optional className="sm:col-span-2">
            <div className="flex flex-col gap-2">
              {f.serials.map((s, i) => (
                <div key={i} className="flex gap-2">
                  <Input
                    aria-label={`Serial number ${i + 1}`}
                    value={s}
                    className="font-mono text-[13px]"
                    onChange={(e) => set("serials", f.serials.map((x, j) => (j === i ? e.target.value : x)))}
                  />
                  {f.serials.length > 1 && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Remove serial number"
                      onClick={() => set("serials", f.serials.filter((_, j) => j !== i))}
                    >
                      <X />
                    </Button>
                  )}
                </div>
              ))}
              <button
                type="button"
                onClick={() => set("serials", [...f.serials, ""])}
                className="inline-flex w-fit cursor-pointer items-center gap-1 text-[13px] font-medium text-primary hover:underline"
              >
                <Plus className="size-3.5" /> Add another
              </button>
            </div>
          </Field>
          <Field label="Tags" htmlFor="tags" optional className="sm:col-span-2">
            <TagInput id="tags" value={f.tags} onChange={(v) => set("tags", v)} />
          </Field>
          <Field label="Product link" htmlFor="productUrl" optional className="sm:col-span-2">
            <Input id="productUrl" type="url" value={f.productUrl} placeholder="https://" onChange={(e) => set("productUrl", e.target.value)} />
          </Field>
          <Field label="Notes" htmlFor="notes" optional className="sm:col-span-2">
            <Textarea
              id="notes"
              value={f.notes}
              placeholder="Support phone, claim instructions, extended warranty details…"
              onChange={(e) => set("notes", e.target.value)}
            />
          </Field>
        </section>

        {!editing && (
          <section>
            <div className="mb-1.5 flex items-center justify-between">
              <span className="text-[13px] font-medium">
                Receipts, photos &amp; manuals <span className="font-normal text-fg-subtle">optional</span>
              </span>
              {paperless && (
                <label className="flex cursor-pointer items-center gap-2 text-[13px] text-fg-muted">
                  <Switch checked={toPaperless} onCheckedChange={setToPaperless} />
                  Store documents in Paperless
                </label>
              )}
            </div>
            <div
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => {
                e.preventDefault();
                addFiles(e.dataTransfer.files);
              }}
              onClick={() => fileRef.current?.click()}
              className="flex cursor-pointer flex-col items-center justify-center gap-1 rounded-xl border border-dashed border-border-strong bg-surface-2 px-4 py-5 text-center transition-colors hover:border-primary hover:bg-primary-soft/40"
            >
              <Upload className="size-5 text-fg-subtle" />
              <p className="text-sm text-fg-muted">
                Drop files here or <span className="font-medium text-primary">browse</span>
              </p>
              <input ref={fileRef} type="file" multiple hidden onChange={(e) => addFiles(e.target.files)} />
            </div>
            {files.length > 0 && (
              <ul className="mt-2 flex flex-col gap-1.5">
                {files.map((p, i) => (
                  <li key={i} className="flex items-center gap-2 rounded-lg border border-border bg-surface px-2.5 py-1.5 text-sm">
                    <FileText className="size-4 shrink-0 text-fg-subtle" />
                    <span className="min-w-0 flex-1 truncate">{p.file.name}</span>
                    <span className="text-xs text-fg-subtle">{formatBytes(p.file.size)}</span>
                    <Select
                      aria-label="Document type"
                      value={p.kind}
                      onChange={(e) => setFiles(files.map((x, j) => (j === i ? { ...x, kind: e.target.value as AttachmentKind } : x)))}
                      className="h-7 w-28 text-xs"
                    >
                      <option value="receipt">Receipt</option>
                      <option value="invoice">Invoice</option>
                      <option value="manual">Manual</option>
                      {p.file.type.startsWith("image/") && <option value="photo">Photo</option>}
                      <option value="other">Other</option>
                    </Select>
                    <Button variant="ghost" size="icon-sm" aria-label="Remove file" onClick={() => setFiles(files.filter((_, j) => j !== i))}>
                      <X />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        )}
      </form>
    </Dialog>
  );
}
