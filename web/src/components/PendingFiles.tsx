import { X } from "lucide-react";
import type { AttachmentKind } from "@/lib/api";
import { kindMeta, kindsFor, type PendingFile } from "@/lib/documents";
import { cn, formatBytes } from "@/lib/utils";
import { Button } from "./ui/button";
import { Select } from "./ui/input";

/** Files waiting to be uploaded, each with a type picker. */
export const PendingFileList = ({
  files,
  onChange,
  showMissing = false,
}: {
  files: PendingFile[];
  onChange: (files: PendingFile[]) => void;
  /** Highlight files that still need a type. */
  showMissing?: boolean;
}) => (
  <ul className="flex flex-col gap-1.5">
    {files.map((p) => {
      const Icon = p.kind ? kindMeta[p.kind].icon : kindMeta.other.icon;
      const missing = showMissing && p.kind === null;
      return (
        <li
          key={p.id}
          className="flex items-center gap-2 rounded-lg border border-border bg-surface px-2.5 py-1.5 text-sm"
        >
          <Icon className="size-4 shrink-0 text-fg-subtle" />
          <span className="min-w-0 flex-1 truncate" title={p.file.name}>
            {p.file.name}
          </span>
          <span className="hidden text-xs text-fg-subtle sm:inline">{formatBytes(p.file.size)}</span>
          <Select
            aria-label={`Type of ${p.file.name}`}
            aria-invalid={missing || undefined}
            value={p.kind ?? ""}
            onChange={(e) =>
              onChange(files.map((x) => (x.id === p.id ? { ...x, kind: e.target.value as AttachmentKind } : x)))
            }
            className={cn("h-8 w-44 text-[13px]", p.kind === null && "text-fg-subtle")}
          >
            <option value="" disabled>
              Choose type…
            </option>
            {kindsFor(p.file.type).map((k) => (
              <option key={k} value={k}>
                {kindMeta[k].label}
              </option>
            ))}
          </Select>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Remove ${p.file.name}`}
            onClick={() => onChange(files.filter((x) => x.id !== p.id))}
          >
            <X />
          </Button>
        </li>
      );
    })}
  </ul>
);
