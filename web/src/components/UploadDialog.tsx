import { Upload } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { storageFor, toPending } from "@/lib/documents";
import type { AttachmentKind } from "@/lib/api";
import { usePrefs } from "@/lib/queries";
import { PendingFileList } from "./PendingFiles";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Segmented } from "./ui/misc";

/**
 * Asks what each file is (and where to store it) before uploading.
 * Mounted only while open, with the files that were picked or dropped.
 */
export const UploadDialog = ({
  warrantyId,
  initialFiles,
  initialKind,
  homebox,
  onClose,
  onUploaded,
}: {
  warrantyId: number;
  initialFiles: File[];
  /** Gives every file this type, e.g. when they were picked as photos. */
  initialKind?: AttachmentKind;
  /** Photos can be stored on the warranty's HomeBox item. */
  homebox: boolean;
  onClose: () => void;
  onUploaded: () => void;
}) => {
  const { t } = useTranslation();
  const { settings } = usePrefs();
  const paperless = settings?.paperlessEnabled ?? false;
  const [files, setFiles] = useState(() =>
    toPending(initialFiles).map((p) => (initialKind ? { ...p, kind: initialKind } : p)),
  );
  const [target, setTarget] = useState<"local" | "paperless">(() =>
    paperless && settings?.paperlessDefault ? "paperless" : "local",
  );
  const [photoTarget, setPhotoTarget] = useState<"local" | "homebox">(() =>
    homebox && settings?.homeboxPhotosDefault ? "homebox" : "local",
  );
  const [attempted, setAttempted] = useState(false);
  const [uploading, setUploading] = useState(false);
  const moreRef = useRef<HTMLInputElement>(null);

  const untyped = files.filter((f) => f.kind === null).length;
  const hasPhotos = files.some((f) => f.kind === "photo");
  const hasDocuments = files.some((f) => f.kind !== "photo");

  const upload = async () => {
    setAttempted(true);
    if (untyped > 0 || files.length === 0) return;
    setUploading(true);
    let failed = 0;
    const typed = files.flatMap((f) => (f.kind ? [{ ...f, kind: f.kind }] : []));
    for (const p of typed) {
      try {
        await api.upload(warrantyId, p.file, p.kind, storageFor(p, target === "paperless", photoTarget === "homebox"));
      } catch (e) {
        failed++;
        toast.error(`${p.file.name}: ${errorMessage(e)}`);
      }
    }
    setUploading(false);
    onUploaded();
    if (failed < files.length) {
      toast.success(t("upload.added", { count: files.length - failed }));
    }
    onClose();
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !uploading && onClose()}
      title={initialKind === "photo" ? t("upload.titlePhotos") : t("upload.title")}
      description={initialKind === "photo" ? undefined : t("upload.description")}
      footer={
        <>
          <Button variant="ghost" className="mr-auto" onClick={() => moreRef.current?.click()} disabled={uploading}>
            {t("upload.addMore")}
          </Button>
          <Button variant="ghost" onClick={onClose} disabled={uploading}>
            {t("common.cancel")}
          </Button>
          <Button onClick={upload} loading={uploading} disabled={files.length === 0}>
            {!uploading && <Upload />}{" "}
            {files.length > 1 ? t("upload.uploadN", { count: files.length }) : t("upload.upload")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <PendingFileList
          files={files}
          onChange={setFiles}
          showMissing={attempted}
          toPaperless={paperless && target === "paperless"}
        />
        {attempted && untyped > 0 && (
          <p className="text-[13px] text-bad">{t("upload.chooseTypes", { count: untyped })}</p>
        )}
        {paperless && hasDocuments && (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-surface-2 px-4 py-3">
            <div>
              <p className="text-[13px] font-medium">{t("upload.where")}</p>
              {hasPhotos && !homebox && <p className="text-xs text-fg-subtle">{t("upload.photosHere")}</p>}
            </div>
            <Segmented
              size="sm"
              value={target}
              onChange={setTarget}
              options={[
                { value: "local", label: t("upload.here") },
                { value: "paperless", label: t("upload.paperless") },
              ]}
            />
          </div>
        )}
        {homebox && hasPhotos && (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-surface-2 px-4 py-3">
            <div>
              <p className="text-[13px] font-medium">{t("upload.wherePhotos")}</p>
              <p className="text-xs text-fg-subtle">{t("upload.wherePhotosHint")}</p>
            </div>
            <Segmented
              size="sm"
              value={photoTarget}
              onChange={setPhotoTarget}
              options={[
                { value: "local", label: t("upload.here") },
                { value: "homebox", label: "HomeBox" },
              ]}
            />
          </div>
        )}
      </div>
      <input
        ref={moreRef}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files) setFiles([...files, ...toPending(e.target.files)]);
          e.target.value = "";
        }}
      />
    </Dialog>
  );
};
