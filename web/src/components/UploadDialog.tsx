import { Upload } from "lucide-react";
import { useRef, useState } from "react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { storageFor, toPending } from "@/lib/documents";
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
  onClose,
  onUploaded,
}: {
  warrantyId: number;
  initialFiles: File[];
  onClose: () => void;
  onUploaded: () => void;
}) => {
  const { settings } = usePrefs();
  const paperless = settings?.paperlessEnabled ?? false;
  const [files, setFiles] = useState(() => toPending(initialFiles));
  const [target, setTarget] = useState<"local" | "paperless">(() =>
    paperless && settings?.paperlessDefault ? "paperless" : "local",
  );
  const [attempted, setAttempted] = useState(false);
  const [uploading, setUploading] = useState(false);
  const moreRef = useRef<HTMLInputElement>(null);

  const untyped = files.filter((f) => f.kind === null).length;
  const hasPhotos = files.some((f) => f.kind === "photo");

  const upload = async () => {
    setAttempted(true);
    if (untyped > 0 || files.length === 0) return;
    setUploading(true);
    let failed = 0;
    const typed = files.flatMap((f) => (f.kind ? [{ ...f, kind: f.kind }] : []));
    for (const p of typed) {
      try {
        await api.upload(warrantyId, p.file, p.kind, storageFor(p, target === "paperless"));
      } catch (e) {
        failed++;
        toast.error(`${p.file.name}: ${errorMessage(e)}`);
      }
    }
    setUploading(false);
    onUploaded();
    if (failed < files.length) {
      const done = files.length - failed;
      toast.success(done === 1 ? "File added" : `${done} files added`);
    }
    onClose();
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !uploading && onClose()}
      title="Add documents"
      description="Tell us what each file is, so it's easy to find when you need to make a claim."
      footer={
        <>
          <Button variant="ghost" className="mr-auto" onClick={() => moreRef.current?.click()} disabled={uploading}>
            Add more files
          </Button>
          <Button variant="ghost" onClick={onClose} disabled={uploading}>
            Cancel
          </Button>
          <Button onClick={upload} loading={uploading} disabled={files.length === 0}>
            {!uploading && <Upload />} Upload {files.length > 1 && files.length}
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
          <p className="text-[13px] text-bad">
            Choose a type for {untyped === 1 ? "the highlighted file" : `the ${untyped} highlighted files`}.
          </p>
        )}
        {paperless && (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-surface-2 px-4 py-3">
            <div>
              <p className="text-[13px] font-medium">Where to store documents</p>
              {hasPhotos && <p className="text-xs text-fg-subtle">Product photos are always stored here.</p>}
            </div>
            <Segmented
              size="sm"
              value={target}
              onChange={setTarget}
              options={[
                { value: "local", label: "Here" },
                { value: "paperless", label: "Paperless" },
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
