import {
  AlertTriangle,
  Camera,
  Download,
  ExternalLink,
  Loader2,
  MoreHorizontal,
  Paperclip,
  Pencil,
  RefreshCw,
  Search,
  Trash2,
  Upload,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type SubmitEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  api,
  errorMessage,
  fileUrl,
  thumbUrl,
  type Attachment,
  type AttachmentKind,
  type PaperlessDoc,
  type Warranty,
} from "@/lib/api";
import { DOCUMENT_KINDS, kindIcon, kindLabel, kindsFor } from "@/lib/documents";
import { useInvalidate, usePrefs } from "@/lib/queries";
import { cn, formatBytes } from "@/lib/utils";
import { Button } from "./ui/button";
import { ConfirmDialog, Dialog } from "./ui/dialog";
import { Input, Select } from "./ui/input";
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from "./ui/menu";
import { UploadDialog } from "./UploadDialog";

const PaperlessMark = ({ className }: { className?: string }) => {
  const { t } = useTranslation();
  return (
    <span
      title={t("attachments.inPaperless")}
      className={cn(
        "inline-flex h-4.5 items-center rounded bg-emerald-600 px-1 text-[10px] font-semibold tracking-wide text-white uppercase",
        className,
      )}
    >
      Paperless
    </span>
  );
};

const Thumb = ({ a }: { a: Attachment }) => {
  const { t } = useTranslation();
  const [failed, setFailed] = useState(false);
  const Icon = kindIcon[a.kind];
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
          <span className="text-xs">{t("attachments.processing")}</span>
        </div>
      ) : a.status === "failed" ? (
        <div className="flex flex-col items-center gap-2 px-3 text-center text-bad">
          <AlertTriangle className="size-5" />
          <span className="line-clamp-2 text-xs">{a.error || t("attachments.failed")}</span>
        </div>
      ) : (
        <Icon className="size-8 text-fg-subtle" strokeWidth={1.5} />
      )}
    </div>
  );
};

export const AttachmentsPanel = ({ warranty }: { warranty: Warranty }) => {
  const { t } = useTranslation();
  const { settings } = usePrefs();
  const invalidate = useInvalidate();
  const paperless = settings?.paperlessEnabled ?? false;
  const [toUpload, setToUpload] = useState<File[] | null>(null);
  const [uploadingPhotos, setUploadingPhotos] = useState(false);
  const [drag, setDrag] = useState(false);
  const [picker, setPicker] = useState(false);
  const [preview, setPreview] = useState<Attachment | null>(null);
  const [toDelete, setToDelete] = useState<Attachment | null>(null);
  const [renaming, setRenaming] = useState<Attachment | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const photoRef = useRef<HTMLInputElement>(null);

  const pick = (files: FileList | null) => {
    if (files?.length) setToUpload(Array.from(files));
  };

  const uploadPhotos = async (files: FileList | null) => {
    if (!files) return;
    setUploadingPhotos(true);
    for (const file of Array.from(files)) {
      try {
        await api.upload(warranty.id, file, "photo", "local");
      } catch (e) {
        toast.error(`${file.name}: ${errorMessage(e)}`);
      }
    }
    setUploadingPhotos(false);
    invalidate();
  };

  const docs = warranty.attachments;
  // One section per type, in the order types are listed.
  const groups = DOCUMENT_KINDS.map((kind) => ({ kind, items: docs.filter((a) => a.kind === kind) })).filter(
    (g) => g.items.length > 0,
  );

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Paperclip className="size-4 text-fg-subtle" />
          {t("attachments.title")}
          <span className="font-normal text-fg-subtle tabular-nums">{docs.length || ""}</span>
        </h2>
        <div className="flex items-center gap-2">
          {paperless && (
            <Button size="sm" variant="outline" onClick={() => setPicker(true)}>
              <Search /> {t("attachments.linkFromPaperless")}
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => photoRef.current?.click()} loading={uploadingPhotos}>
            {!uploadingPhotos && <Camera />} {t("attachments.photo")}
          </Button>
          <Button size="sm" onClick={() => fileRef.current?.click()}>
            <Upload /> {t("attachments.addDocuments")}
          </Button>
          <input
            ref={fileRef}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              pick(e.target.files);
              e.target.value = "";
            }}
          />
          <input
            ref={photoRef}
            type="file"
            accept="image/*"
            capture="environment"
            hidden
            onChange={(e) => {
              void uploadPhotos(e.target.files);
              e.target.value = "";
            }}
          />
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
          pick(e.dataTransfer.files);
        }}
        className={cn(
          "rounded-2xl transition-colors",
          drag && "bg-primary-soft/50 ring-2 ring-primary ring-offset-4 ring-offset-bg",
        )}
      >
        {docs.length === 0 ? (
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            className="flex w-full cursor-pointer flex-col items-center gap-2 rounded-2xl border border-dashed border-border-strong px-6 py-10 text-center hover:border-primary hover:bg-primary-soft/30"
          >
            <Upload className="size-6 text-fg-subtle" />
            <p className="text-sm font-medium">{t("attachments.dropHere")}</p>
            <p className="text-xs text-fg-subtle">
              {paperless ? t("attachments.dropHintPaperless") : t("attachments.dropHint")}
            </p>
          </button>
        ) : (
          <div className="flex flex-col gap-6">
            {groups.map(({ kind, items }) => {
              const label = kindLabel(kind);
              const Icon = kindIcon[kind];
              return (
                <section key={kind} aria-label={label}>
                  <h3 className="mb-2 flex items-center gap-2 text-[13px] font-medium">
                    <Icon className="size-4 text-fg-subtle" />
                    {label}
                    <span className="font-normal text-fg-subtle tabular-nums">{items.length}</span>
                  </h3>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                    {items.map((a) => (
                      <AttachmentCard
                        key={a.id}
                        a={a}
                        onOpen={() => setPreview(a)}
                        onRename={() => setRenaming(a)}
                        onDelete={() => setToDelete(a)}
                        onChanged={invalidate}
                        webUrl={settings?.paperlessWebUrl}
                      />
                    ))}
                  </div>
                </section>
              );
            })}
          </div>
        )}
      </div>

      {picker && <PaperlessPicker onOpenChange={setPicker} warranty={warranty} onLinked={invalidate} />}
      {toUpload && (
        <UploadDialog
          warrantyId={warranty.id}
          initialFiles={toUpload}
          onClose={() => setToUpload(null)}
          onUploaded={invalidate}
        />
      )}
      <PreviewDialog a={preview} onClose={() => setPreview(null)} />
      {renaming && <RenameDialog a={renaming} onClose={() => setRenaming(null)} onRenamed={invalidate} />}
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={toDelete?.storage === "paperless" ? t("attachments.unlinkTitle") : t("attachments.deleteTitle")}
        confirmLabel={toDelete?.storage === "paperless" ? t("attachments.unlink") : t("common.delete")}
        description={
          toDelete?.storage === "paperless"
            ? t("attachments.unlinkText")
            : t("attachments.deleteText", { name: toDelete?.filename ?? "" })
        }
        onConfirm={async () => {
          if (!toDelete) return;
          try {
            await api.deleteAttachment(toDelete.id);
            invalidate();
          } catch (e) {
            toast.error(errorMessage(e));
          }
          setToDelete(null);
        }}
      />
    </div>
  );
};

const AttachmentCard = ({
  a,
  onOpen,
  onRename,
  onDelete,
  onChanged,
  webUrl,
}: {
  a: Attachment;
  onOpen: () => void;
  onRename: () => void;
  onDelete: () => void;
  onChanged: () => void;
  webUrl?: string;
}) => {
  const { t } = useTranslation();
  const { fmtDate } = usePrefs();
  const ready = a.status === "ready";
  return (
    <div className="group relative flex flex-col overflow-hidden rounded-xl border border-border bg-surface shadow-card transition-shadow hover:shadow-pop">
      <button
        type="button"
        onClick={ready ? onOpen : undefined}
        className={cn("text-left", ready ? "cursor-pointer" : "cursor-default")}
      >
        <Thumb a={a} />
      </button>
      {a.storage === "paperless" && (
        <PaperlessMark className="pointer-events-none absolute top-2 right-2 shadow-card" />
      )}
      <div className="flex items-start gap-2 border-t border-border px-3 py-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-medium" title={a.filename}>
            {a.filename}
          </p>
          <p className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs whitespace-nowrap text-fg-subtle">
            <span className="truncate">
              {t("attachments.added", { date: fmtDate(a.createdAt.slice(0, 10)) })}
              {a.size > 0 && ` · ${formatBytes(a.size)}`}
            </span>
          </p>
        </div>
        <Menu>
          <MenuTrigger asChild>
            <button
              type="button"
              className="-mr-1 cursor-pointer rounded-md p-1 text-fg-subtle hover:bg-surface-3 hover:text-fg"
              aria-label={t("attachments.actions")}
            >
              <MoreHorizontal className="size-4" />
            </button>
          </MenuTrigger>
          <MenuContent>
            {ready && (
              <MenuItem icon={<Download />} onSelect={() => window.open(fileUrl(a, true), "_blank")}>
                {t("attachments.download")}
              </MenuItem>
            )}
            {a.storage === "paperless" && a.paperlessDocId !== null && webUrl !== undefined && webUrl !== "" && (
              <MenuItem
                icon={<ExternalLink />}
                onSelect={() => window.open(`${webUrl}/documents/${a.paperlessDocId}/details`, "_blank", "noopener")}
              >
                {t("attachments.openInPaperless")}
              </MenuItem>
            )}
            {a.status !== "ready" && a.storage === "paperless" && (
              <MenuItem
                icon={<RefreshCw />}
                onSelect={async () => {
                  try {
                    await api.retryAttachment(a.id);
                    onChanged();
                  } catch (e) {
                    toast.error(errorMessage(e));
                  }
                }}
              >
                {t("attachments.checkAgain")}
              </MenuItem>
            )}
            <MenuItem icon={<Pencil />} onSelect={onRename}>
              {t("attachments.rename")}
            </MenuItem>
            <MenuSeparator />
            <MenuLabel>{t("attachments.type")}</MenuLabel>
            {kindsFor(a.mimeType).map((k) => (
              <MenuItem
                key={k}
                icon={(() => {
                  const I = kindIcon[k];
                  return <I />;
                })()}
                onSelect={async () => {
                  await api.updateAttachment(a.id, { kind: k }).catch((e: unknown) => toast.error(errorMessage(e)));
                  onChanged();
                }}
              >
                <span className={cn(k === a.kind && "font-semibold")}>{kindLabel(k)}</span>
              </MenuItem>
            ))}
            <MenuSeparator />
            <MenuItem danger icon={<Trash2 />} onSelect={onDelete}>
              {a.storage === "paperless" ? t("attachments.unlink") : t("common.delete")}
            </MenuItem>
          </MenuContent>
        </Menu>
      </div>
    </div>
  );
};

/** Renames the file as shown here; the extension is kept and Paperless is untouched. */
const RenameDialog = ({ a, onClose, onRenamed }: { a: Attachment; onClose: () => void; onRenamed: () => void }) => {
  const { t } = useTranslation();
  const dot = a.filename.lastIndexOf(".");
  const ext = dot > 0 ? a.filename.slice(dot) : "";
  const [name, setName] = useState(ext ? a.filename.slice(0, dot) : a.filename);
  const [saving, setSaving] = useState(false);

  const save = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    try {
      await api.updateAttachment(a.id, { filename: name.trim() + ext });
      onRenamed();
      onClose();
    } catch (err) {
      toast.error(errorMessage(err));
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      size="sm"
      title={t("attachments.renameTitle")}
      description={a.storage === "paperless" ? t("attachments.renamePaperless") : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" form="rename-form" loading={saving} disabled={!name.trim()}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form id="rename-form" onSubmit={save} className="flex items-center gap-2">
        <Input
          data-autofocus
          aria-label={t("attachments.fileName")}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        {ext && <span className="shrink-0 text-sm text-fg-subtle">{ext}</span>}
      </form>
    </Dialog>
  );
};

const PreviewDialog = ({ a, onClose }: { a: Attachment | null; onClose: () => void }) => {
  const { t } = useTranslation();
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
              <ExternalLink /> {t("attachments.openInTab")}
            </Button>
            <Button variant="outline" onClick={() => window.open(fileUrl(a, true), "_blank")}>
              <Download /> {t("attachments.download")}
            </Button>
          </>
        )
      }
    >
      {a &&
        (isImage ? (
          <img src={fileUrl(a)} alt={a.filename} className="mx-auto max-h-[70vh] rounded-lg object-contain" />
        ) : (
          // Chrome's built-in PDF viewer refuses to render inside sandboxed frames.
          // eslint-disable-next-line @eslint-react/dom-no-missing-iframe-sandbox
          <iframe
            src={fileUrl(a)}
            title={a.filename}
            className="h-[70vh] w-full rounded-lg border border-border bg-white"
          />
        ))}
    </Dialog>
  );
};

const useDebounced = <T,>(value: T, ms: number): T => {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
};

/** Mounted only while open, so each opening starts with a fresh search. */
const PaperlessPicker = ({
  onOpenChange,
  warranty,
  onLinked,
}: {
  onOpenChange: (o: boolean) => void;
  warranty: Warranty;
  onLinked: () => void;
}) => {
  const { t } = useTranslation();
  const { fmtDate } = usePrefs();
  const [q, setQ] = useState(warranty.productName);
  const query = useDebounced(q, 300);
  const search = useQuery({
    queryKey: ["paperless-search", query],
    queryFn: () => api.searchPaperless(query),
    retry: false,
  });
  const results: PaperlessDoc[] | undefined = search.data;
  const loading = search.isFetching;
  const error = search.error ? errorMessage(search.error) : null;
  const [picked, setPicked] = useState<number[]>([]);
  const [kind, setKind] = useState<AttachmentKind>("purchase");
  const [saving, setSaving] = useState(false);
  const linked = new Set(warranty.attachments.map((a) => a.paperlessDocId).filter((id) => id !== null));

  const link = async () => {
    setSaving(true);
    try {
      await api.linkPaperless(warranty.id, picked, kind);
      onLinked();
      toast.success(t("picker.linked", { count: picked.length }));
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
      size="xl"
      title={t("picker.title")}
      description={t("picker.description")}
      footer={
        <>
          <Select
            aria-label={t("picker.documentType")}
            value={kind}
            onChange={(e) => setKind(e.target.value as AttachmentKind)}
            className="mr-auto w-48"
          >
            {DOCUMENT_KINDS.filter((k) => k !== "photo").map((k) => (
              <option key={k} value={k}>
                {t("picker.as", { kind: kindLabel(k) })}
              </option>
            ))}
          </Select>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button disabled={!picked.length} loading={saving} onClick={link}>
            {picked.length > 0 ? t("picker.linkN", { count: picked.length }) : t("picker.link")}
          </Button>
        </>
      }
    >
      <div className="relative mb-4">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-subtle" />
        <Input
          data-autofocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t("picker.search")}
          className="pl-9"
        />
        {loading && (
          <Loader2 className="absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-fg-subtle" />
        )}
      </div>
      {error ? (
        <p className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-bad">{error}</p>
      ) : results?.length === 0 && !loading ? (
        <p className="py-10 text-center text-sm text-fg-muted">{t("picker.noResults")}</p>
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
                  <img
                    src={`/api/paperless/documents/${d.id}/thumb`}
                    alt=""
                    loading="lazy"
                    className="size-full object-cover object-top"
                  />
                </div>
                <div className="border-t border-border px-2.5 py-2">
                  <p className="line-clamp-2 text-[13px] leading-snug font-medium">{d.title || d.originalFileName}</p>
                  <p className="mt-0.5 text-xs text-fg-subtle">
                    #{d.id} · {fmtDate(d.created)}
                    {already && ` · ${t("picker.alreadyLinked")}`}
                  </p>
                </div>
              </button>
            );
          })}
        </div>
      )}
    </Dialog>
  );
};
