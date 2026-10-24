import {
  AlertTriangle,
  BookOpen,
  Camera,
  Download,
  ExternalLink,
  File as FileIcon,
  FileText,
  Loader2,
  MoreHorizontal,
  Paperclip,
  Receipt,
  RefreshCw,
  Search,
  Trash2,
  Upload,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, fileUrl, thumbUrl, type Attachment, type AttachmentKind, type PaperlessDoc, type Warranty } from "@/lib/api";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { cn, formatBytes } from "@/lib/utils";
import { Button } from "./ui/button";
import { ConfirmDialog, Dialog } from "./ui/dialog";
import { Input, Select } from "./ui/input";
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from "./ui/menu";
import { Segmented } from "./ui/misc";

const kindMeta: Record<AttachmentKind, { label: string; icon: typeof FileIcon }> = {
  receipt: { label: "Receipt", icon: Receipt },
  invoice: { label: "Invoice", icon: FileText },
  manual: { label: "Manual", icon: BookOpen },
  photo: { label: "Photo", icon: Camera },
  other: { label: "Other", icon: FileIcon },
};

function PaperlessMark({ className }: { className?: string }) {
  return (
    <span
      title="Stored in Paperless-ngx"
      className={cn("inline-flex h-4.5 items-center rounded bg-emerald-600/12 px-1 text-[10px] font-semibold tracking-wide text-emerald-700 uppercase dark:text-emerald-300", className)}
    >
      Paperless
    </span>
  );
}

function Thumb({ a }: { a: Attachment }) {
  const [failed, setFailed] = useState(false);
  const Icon = kindMeta[a.kind]?.icon ?? FileIcon;
  const hasThumb = a.status === "ready" && (a.storage === "paperless" || a.mimeType.startsWith("image/"));
  return (
    <div className="relative flex aspect-[4/3] items-center justify-center overflow-hidden rounded-t-xl bg-surface-2">
      {hasThumb && !failed ? (
        <img
          src={thumbUrl(a)}
          alt=""
          loading="lazy"
          onError={() => setFailed(true)}
          className={cn("size-full", a.kind === "photo" ? "object-cover" : "object-cover object-top")}
        />
      ) : a.status === "processing" ? (
        <div className="flex flex-col items-center gap-2 text-fg-subtle">
          <Loader2 className="size-5 animate-spin" />
          <span className="text-xs">Paperless is processing…</span>
        </div>
      ) : a.status === "failed" ? (
        <div className="flex flex-col items-center gap-2 px-3 text-center text-bad">
          <AlertTriangle className="size-5" />
          <span className="line-clamp-2 text-xs">{a.error || "Upload failed"}</span>
        </div>
      ) : (
        <Icon className="size-8 text-fg-subtle" strokeWidth={1.5} />
      )}
    </div>
  );
}

export function AttachmentsPanel({ warranty }: { warranty: Warranty }) {
  const { settings } = usePrefs();
  const invalidate = useInvalidate();
  const paperless = settings?.paperlessEnabled ?? false;
  const [target, setTarget] = useState<"local" | "paperless">("local");
  const [uploading, setUploading] = useState(0);
  const [drag, setDrag] = useState(false);
  const [picker, setPicker] = useState(false);
  const [preview, setPreview] = useState<Attachment | null>(null);
  const [toDelete, setToDelete] = useState<Attachment | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const photoRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setTarget(paperless && settings?.paperlessDefault ? "paperless" : "local");
  }, [paperless, settings?.paperlessDefault]);

  const upload = async (files: FileList | File[] | null, kind?: AttachmentKind) => {
    if (!files) return;
    const list = Array.from(files);
    setUploading((n) => n + list.length);
    for (const file of list) {
      const k: AttachmentKind = kind ?? (file.type.startsWith("image/") && target === "local" ? "photo" : "receipt");
      try {
        await api.upload(warranty.id, file, k, k === "photo" ? "local" : target);
      } catch (e: any) {
        toast.error(`${file.name}: ${e.message}`);
      } finally {
        setUploading((n) => n - 1);
      }
    }
    invalidate();
  };

  const docs = warranty.attachments;

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Paperclip className="size-4 text-fg-subtle" />
          Documents &amp; photos
          <span className="font-normal text-fg-subtle tabular-nums">{docs.length || ""}</span>
        </h2>
        <div className="flex items-center gap-2">
          {paperless && (
            <Segmented
              size="sm"
              value={target}
              onChange={setTarget}
              options={[
                { value: "local", label: "Store here" },
                { value: "paperless", label: "Paperless" },
              ]}
            />
          )}
          {paperless && (
            <Button size="sm" variant="outline" onClick={() => setPicker(true)}>
              <Search /> Link from Paperless
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => photoRef.current?.click()}>
            <Camera /> Photo
          </Button>
          <Button size="sm" onClick={() => fileRef.current?.click()} loading={uploading > 0}>
            {uploading === 0 && <Upload />} Upload
          </Button>
          <input ref={fileRef} type="file" multiple hidden onChange={(e) => { upload(e.target.files); e.target.value = ""; }} />
          <input ref={photoRef} type="file" accept="image/*" capture="environment" hidden onChange={(e) => { upload(e.target.files, "photo"); e.target.value = ""; }} />
        </div>
      </div>

      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDrag(true);
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDrag(false);
          upload(e.dataTransfer.files);
        }}
        className={cn("rounded-2xl transition-colors", drag && "bg-primary-soft/50 ring-2 ring-primary ring-offset-4 ring-offset-bg")}
      >
        {docs.length === 0 ? (
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            className="flex w-full cursor-pointer flex-col items-center gap-2 rounded-2xl border border-dashed border-border-strong px-6 py-10 text-center hover:border-primary hover:bg-primary-soft/30"
          >
            <Upload className="size-6 text-fg-subtle" />
            <p className="text-sm font-medium">Drop the receipt, invoice or manual here</p>
            <p className="text-xs text-fg-subtle">
              {paperless ? "Choose above whether files are stored here or in Paperless-ngx." : "PDFs and images up to the upload limit."}
            </p>
          </button>
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
            {docs.map((a) => (
              <AttachmentCard key={a.id} a={a} onOpen={() => setPreview(a)} onDelete={() => setToDelete(a)} onChanged={invalidate} webUrl={settings?.paperlessWebUrl} />
            ))}
          </div>
        )}
      </div>

      <PaperlessPicker open={picker} onOpenChange={setPicker} warranty={warranty} onLinked={invalidate} />
      <PreviewDialog a={preview} onClose={() => setPreview(null)} />
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={toDelete?.storage === "paperless" ? "Unlink document?" : "Delete file?"}
        confirmLabel={toDelete?.storage === "paperless" ? "Unlink" : "Delete"}
        description={
          toDelete?.storage === "paperless"
            ? "It will be removed from this warranty but stays in Paperless-ngx."
            : `“${toDelete?.filename}” will be permanently deleted.`
        }
        onConfirm={async () => {
          if (!toDelete) return;
          try {
            await api.deleteAttachment(toDelete.id);
            invalidate();
          } catch (e: any) {
            toast.error(e.message);
          }
          setToDelete(null);
        }}
      />
    </div>
  );
}

function AttachmentCard({
  a,
  onOpen,
  onDelete,
  onChanged,
  webUrl,
}: {
  a: Attachment;
  onOpen: () => void;
  onDelete: () => void;
  onChanged: () => void;
  webUrl?: string;
}) {
  const meta = kindMeta[a.kind] ?? kindMeta.other;
  const ready = a.status === "ready";
  return (
    <div className="group relative flex flex-col overflow-hidden rounded-xl border border-border bg-surface shadow-card transition-shadow hover:shadow-pop">
      <button type="button" onClick={ready ? onOpen : undefined} className={cn("text-left", ready ? "cursor-pointer" : "cursor-default")}>
        <Thumb a={a} />
      </button>
      <div className="flex items-start gap-2 border-t border-border px-3 py-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-medium" title={a.filename}>
            {a.filename}
          </p>
          <p className="mt-0.5 flex items-center gap-1.5 text-xs whitespace-nowrap text-fg-subtle">
            {meta.label}
            {a.size > 0 && <span className="truncate">· {formatBytes(a.size)}</span>}
            {a.storage === "paperless" && <PaperlessMark />}
          </p>
        </div>
        <Menu>
          <MenuTrigger asChild>
            <button className="-mr-1 cursor-pointer rounded-md p-1 text-fg-subtle hover:bg-surface-3 hover:text-fg" aria-label="File actions">
              <MoreHorizontal className="size-4" />
            </button>
          </MenuTrigger>
          <MenuContent>
            {ready && (
              <MenuItem icon={<Download />} onSelect={() => window.open(fileUrl(a, true), "_blank")}>
                Download
              </MenuItem>
            )}
            {a.storage === "paperless" && a.paperlessDocId && webUrl && (
              <MenuItem icon={<ExternalLink />} onSelect={() => window.open(`${webUrl}/documents/${a.paperlessDocId}/details`, "_blank", "noopener")}>
                Open in Paperless
              </MenuItem>
            )}
            {a.status !== "ready" && a.storage === "paperless" && (
              <MenuItem
                icon={<RefreshCw />}
                onSelect={async () => {
                  try {
                    await api.retryAttachment(a.id);
                    onChanged();
                  } catch (e: any) {
                    toast.error(e.message);
                  }
                }}
              >
                Check again
              </MenuItem>
            )}
            <MenuSeparator />
            <MenuLabel>Type</MenuLabel>
            {(Object.keys(kindMeta) as AttachmentKind[])
              .filter((k) => k !== "photo" || a.mimeType.startsWith("image/"))
              .map((k) => (
                <MenuItem
                  key={k}
                  icon={(() => {
                    const I = kindMeta[k].icon;
                    return <I />;
                  })()}
                  onSelect={async () => {
                    await api.setAttachmentKind(a.id, k).catch((e) => toast.error(e.message));
                    onChanged();
                  }}
                >
                  <span className={cn(k === a.kind && "font-semibold")}>{kindMeta[k].label}</span>
                </MenuItem>
              ))}
            <MenuSeparator />
            <MenuItem danger icon={<Trash2 />} onSelect={onDelete}>
              {a.storage === "paperless" ? "Unlink" : "Delete"}
            </MenuItem>
          </MenuContent>
        </Menu>
      </div>
    </div>
  );
}

function PreviewDialog({ a, onClose }: { a: Attachment | null; onClose: () => void }) {
  const isImage = a?.mimeType.startsWith("image/") && a.storage === "local";
  return (
    <Dialog
      open={!!a}
      onOpenChange={(o) => !o && onClose()}
      size="xl"
      title={a?.filename ?? ""}
      footer={
        a && (
          <>
            <Button variant="ghost" onClick={() => window.open(fileUrl(a), "_blank")}>
              <ExternalLink /> Open in new tab
            </Button>
            <Button variant="outline" onClick={() => window.open(fileUrl(a, true), "_blank")}>
              <Download /> Download
            </Button>
          </>
        )
      }
    >
      {a &&
        (isImage ? (
          <img src={fileUrl(a)} alt={a.filename} className="mx-auto max-h-[70vh] rounded-lg object-contain" />
        ) : (
          <iframe src={fileUrl(a)} title={a.filename} className="h-[70vh] w-full rounded-lg border border-border bg-white" />
        ))}
    </Dialog>
  );
}

function PaperlessPicker({
  open,
  onOpenChange,
  warranty,
  onLinked,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  warranty: Warranty;
  onLinked: () => void;
}) {
  const { fmtDate } = usePrefs();
  const [q, setQ] = useState("");
  const [results, setResults] = useState<PaperlessDoc[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [picked, setPicked] = useState<number[]>([]);
  const [kind, setKind] = useState<AttachmentKind>("receipt");
  const [saving, setSaving] = useState(false);
  const linked = new Set(warranty.attachments.map((a) => a.paperlessDocId).filter(Boolean));

  useEffect(() => {
    if (!open) return;
    setPicked([]);
    setQ((prev) => prev || warranty.productName);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!open) return;
    const t = setTimeout(async () => {
      setLoading(true);
      setError(null);
      try {
        setResults(await api.searchPaperless(q));
      } catch (e: any) {
        setError(e.message);
        setResults([]);
      } finally {
        setLoading(false);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [q, open]);

  const link = async () => {
    setSaving(true);
    try {
      await api.linkPaperless(warranty.id, picked, kind);
      onLinked();
      toast.success(picked.length === 1 ? "Document linked" : `${picked.length} documents linked`);
      onOpenChange(false);
    } catch (e: any) {
      toast.error(e.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      title="Link documents from Paperless"
      description="Search your Paperless-ngx archive and attach existing documents to this warranty."
      footer={
        <>
          <Select aria-label="Document type" value={kind} onChange={(e) => setKind(e.target.value as AttachmentKind)} className="mr-auto w-36">
            <option value="receipt">As receipt</option>
            <option value="invoice">As invoice</option>
            <option value="manual">As manual</option>
            <option value="other">As other</option>
          </Select>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={!picked.length} loading={saving} onClick={link}>
            Link {picked.length > 0 && picked.length}
          </Button>
        </>
      }
    >
      <div className="relative mb-4">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-subtle" />
        <Input data-autofocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search by text, title or document ID" className="pl-9" />
        {loading && <Loader2 className="absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-fg-subtle" />}
      </div>
      {error ? (
        <p className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-bad">{error}</p>
      ) : results && results.length === 0 && !loading ? (
        <p className="py-10 text-center text-sm text-fg-muted">No documents found.</p>
      ) : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {(results ?? []).map((d) => {
            const on = picked.includes(d.id);
            const already = linked.has(d.id);
            return (
              <button
                key={d.id}
                type="button"
                disabled={already}
                onClick={() => setPicked(on ? picked.filter((x) => x !== d.id) : [...picked, d.id])}
                className={cn(
                  "flex cursor-pointer flex-col overflow-hidden rounded-xl border bg-surface text-left transition-all disabled:cursor-not-allowed disabled:opacity-50",
                  on ? "border-primary ring-2 ring-primary/40" : "border-border hover:border-border-strong",
                )}
              >
                <div className="aspect-[3/4] overflow-hidden bg-surface-2">
                  <img src={`/api/paperless/documents/${d.id}/thumb`} alt="" loading="lazy" className="size-full object-cover object-top" />
                </div>
                <div className="border-t border-border px-2.5 py-2">
                  <p className="line-clamp-2 text-[13px] leading-snug font-medium">{d.title || d.originalFileName}</p>
                  <p className="mt-0.5 text-xs text-fg-subtle">
                    #{d.id} · {fmtDate(d.created)}
                    {already && " · linked"}
                  </p>
                </div>
              </button>
            );
          })}
        </div>
      )}
    </Dialog>
  );
}
