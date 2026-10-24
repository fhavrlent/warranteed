import { addMonths, isAfter } from "date-fns";
import { Plus, Upload, X } from "lucide-react";
import { useRef, useState, type SubmitEvent } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { api, errorMessage, type Warranty, type WarrantyInput } from "@/lib/api";
import { durationLabel, fromISO, toISO, todayISO } from "@/lib/dates";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { storageFor, toPending, type PendingFile } from "@/lib/documents";
import { CURRENCIES, cn } from "@/lib/utils";
import { DateInput } from "./DateInput";
import { PendingFileList } from "./PendingFiles";
import { TagInput } from "./Tags";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Field, Input, Select, Textarea } from "./ui/input";
import { Segmented, Switch } from "./ui/misc";

type Mode = "duration" | "date" | "lifetime";
type Unit = "years" | "months";
interface Serial {
  id: string;
  value: string;
}

// Keys for editable list rows (crypto.randomUUID needs HTTPS, which a LAN
// install may not have).
let lastId = 0;
const newId = () => String(++lastId);
const serial = (value = ""): Serial => ({ id: newId(), value });

const PRESETS = [12, 24, 36, 60];

const initial = (w: Warranty | undefined, currency: string) => {
  const months = w?.warrantyMonths ?? 24;
  const mode: Mode = w?.isLifetime ? "lifetime" : w?.warrantyEndDate ? "date" : "duration";
  const unit: Unit = months % 12 === 0 ? "years" : "months";
  return {
    productName: w?.productName ?? "",
    brand: w?.brand ?? "",
    model: w?.model ?? "",
    vendor: w?.vendor ?? "",
    orderNumber: w?.orderNumber ?? "",
    purchaseDate: w?.purchaseDate ?? todayISO(),
    mode,
    amount: String(months % 12 === 0 ? months / 12 : months),
    unit,
    endDate: w?.warrantyEndDate ?? null,
    price: w?.price != null ? String(w.price) : "",
    currency: w?.currency || currency,
    productUrl: w?.productUrl ?? "",
    notes: w?.notes ?? "",
    serials: w?.serialNumbers.length ? w.serialNumbers.map((v) => serial(v)) : [serial()],
    tags: w?.tags.map((t) => t.name) ?? [],
  };
};

interface FormProps {
  onOpenChange: (o: boolean) => void;
  warranty?: Warranty;
}

/** Add/edit dialog. The form is mounted only while open, so every opening starts from fresh state. */
export const WarrantyForm = ({ open, ...props }: FormProps & { open: boolean }) =>
  open ? <WarrantyFormDialog {...props} /> : null;

const WarrantyFormDialog = ({ onOpenChange, warranty }: FormProps) => {
  const { currency, settings, fmtDate } = usePrefs();
  const paperless = settings?.paperlessEnabled ?? false;
  const [f, setF] = useState(() => initial(warranty, currency));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [files, setFiles] = useState<PendingFile[]>([]);
  // The serial row added last gets focus.
  const [newSerialId, setNewSerialId] = useState<string | null>(null);
  const [toPaperless, setToPaperless] = useState(() => paperless && (settings?.paperlessDefault ?? false));
  const fileRef = useRef<HTMLInputElement>(null);
  const invalidate = useInvalidate();
  const navigate = useNavigate();
  const editing = warranty !== undefined;

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

  const validate = () => {
    const e: Record<string, string> = {};
    if (!f.productName.trim()) e.productName = "Give the product a name";
    if (!f.purchaseDate) e.purchaseDate = "When did you buy it?";
    if (f.mode === "duration" && !months) e.amount = "Enter a length";
    if (f.mode === "date") {
      if (!f.endDate) e.endDate = "Pick the end date";
      else if (f.purchaseDate && isAfter(fromISO(f.purchaseDate), fromISO(f.endDate)))
        e.endDate = "Ends before the purchase date";
    }
    if (f.price && !Number.isFinite(Number(f.price.replace(",", ".")))) e.price = "Not a number";
    if (files.some((p) => p.kind === null)) e.files = "Choose a type for each file";
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const submit = async (ev: SubmitEvent<HTMLFormElement>) => {
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
      serialNumbers: f.serials.map((x) => x.value),
      tags: f.tags,
    };
    setSaving(true);
    try {
      const saved = editing ? await api.updateWarranty(warranty.id, input) : await api.createWarranty(input);
      let failed = 0;
      const typed = files.flatMap((p) => (p.kind ? [{ ...p, kind: p.kind }] : []));
      for (const p of typed) {
        try {
          await api.upload(saved.id, p.file, p.kind, storageFor(p, toPaperless));
        } catch (err) {
          failed++;
          toast.error(`${p.file.name}: ${errorMessage(err)}`);
        }
      }
      invalidate();
      onOpenChange(false);
      toast.success(editing ? "Changes saved" : `${saved.productName} added`, {
        description: failed ? `${failed} file(s) could not be uploaded` : undefined,
      });
      if (!editing) void navigate(`/w/${saved.id}`);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    setFiles((prev) => [...prev, ...toPending(list)]);
  };

  return (
    <Dialog
      open
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
            <DateInput
              id="purchaseDate"
              value={f.purchaseDate}
              onChange={(v) => set("purchaseDate", v ?? "")}
              invalid={!!errors.purchaseDate}
            />
          </Field>
          <Field label="Bought from" htmlFor="vendor" optional>
            <Input
              id="vendor"
              value={f.vendor}
              placeholder="Store or website"
              onChange={(e) => set("vendor", e.target.value)}
            />
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
              <Select
                aria-label="Currency"
                value={f.currency}
                onChange={(e) => set("currency", e.target.value)}
                className="w-24"
              >
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
                  <Select
                    aria-label="Unit"
                    value={f.unit}
                    onChange={(e) => set("unit", e.target.value as Unit)}
                    className="w-28"
                  >
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
                        months === m
                          ? "border-primary bg-primary-soft text-primary"
                          : "border-border bg-surface text-fg-muted hover:text-fg",
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
            {(errors.amount || errors.endDate) && (
              <p className="mt-1.5 text-xs text-bad">{errors.amount || errors.endDate}</p>
            )}
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
              {f.serials.map((sn, i) => (
                <div key={sn.id} className="flex gap-2">
                  <Input
                    aria-label={`Serial number ${i + 1}`}
                    autoFocus={sn.id === newSerialId}
                    value={sn.value}
                    className="font-mono text-[13px]"
                    onChange={(e) =>
                      set(
                        "serials",
                        f.serials.map((x) => (x.id === sn.id ? { ...x, value: e.target.value } : x)),
                      )
                    }
                  />
                  {f.serials.length > 1 && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Remove serial number"
                      onClick={() =>
                        set(
                          "serials",
                          f.serials.filter((x) => x.id !== sn.id),
                        )
                      }
                    >
                      <X />
                    </Button>
                  )}
                </div>
              ))}
              <button
                type="button"
                onClick={() => {
                  const added = serial();
                  set("serials", [...f.serials, added]);
                  setNewSerialId(added.id);
                }}
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
            <Input
              id="productUrl"
              type="url"
              value={f.productUrl}
              placeholder="https://"
              onChange={(e) => set("productUrl", e.target.value)}
            />
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
                Documents &amp; photos <span className="font-normal text-fg-subtle">optional</span>
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
              <div className="mt-2">
                <PendingFileList
                  files={files}
                  onChange={setFiles}
                  showMissing={errors.files !== undefined}
                  toPaperless={toPaperless}
                />
              </div>
            )}
            {errors.files && <p className="mt-1.5 text-xs text-bad">{errors.files}</p>}
          </section>
        )}
      </form>
    </Dialog>
  );
};
