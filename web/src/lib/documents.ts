import {
  BookOpen,
  Camera,
  File as FileIcon,
  IdCard,
  type LucideIcon,
  Receipt,
  ScrollText,
  ShieldPlus,
  Wrench,
} from "lucide-react";
import type { AttachmentKind } from "./api";

/** Document types in display order. */
export const DOCUMENT_KINDS: readonly AttachmentKind[] = [
  "purchase",
  "terms",
  "card",
  "extended",
  "claim",
  "manual",
  "photo",
  "other",
];

export const kindMeta: Record<AttachmentKind, { label: string; icon: LucideIcon }> = {
  purchase: { label: "Proof of purchase", icon: Receipt },
  terms: { label: "Warranty terms", icon: ScrollText },
  card: { label: "Warranty card", icon: IdCard },
  extended: { label: "Extended warranty", icon: ShieldPlus },
  claim: { label: "Repair & claims", icon: Wrench },
  manual: { label: "Manual", icon: BookOpen },
  photo: { label: "Product photo", icon: Camera },
  other: { label: "Other", icon: FileIcon },
};

/** Types a file can be given; product photos must be images. */
export const kindsFor = (mimeType: string): AttachmentKind[] =>
  DOCUMENT_KINDS.filter((k) => k !== "photo" || mimeType.startsWith("image/"));

// File-name hints (English, Czech, Slovak, German), checked in order.
const HINTS: [AttachmentKind, RegExp][] = [
  ["terms", /terms|condition|podmien|podmink|bedingung|agb/],
  ["card", /warranty.?card|certificate|zarucn.?list|garancn|garantieschein/],
  ["extended", /extended|prodl[ou]{1,2}zen|predlzen|verlanger|insurance|pojist|poisten|protect|care.?pack/],
  ["claim", /claim|repair|reklamac|oprav|servis|service|rma/],
  ["manual", /manual|guide|instruction|navod|handbuch|anleitung/],
  ["purchase", /receipt|invoice|bill|order|faktur|uctenk|doklad|objednav|rechnung|quittung|beleg/],
];

/** Guesses a file's type from its name; null when there is no good guess. */
export const guessKind = (file: File): AttachmentKind | null => {
  const name = file.name
    .toLowerCase()
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "");
  for (const [kind, re] of HINTS) if (re.test(name)) return kind;
  return null;
};

export interface PendingFile {
  id: string;
  file: File;
  kind: AttachmentKind | null;
}

let lastId = 0;
export const toPending = (files: Iterable<File>): PendingFile[] =>
  Array.from(files, (file) => ({ id: `f${++lastId}`, file, kind: guessKind(file) }));

// File types Paperless-ngx can consume (office formats and e-mails need its
// optional Tika integration). Checked by extension: browsers often report
// no MIME type for office files.
const PAPERLESS_EXTENSIONS = new Set([
  "pdf",
  "png",
  "jpg",
  "jpeg",
  "tif",
  "tiff",
  "gif",
  "webp",
  "txt",
  "doc",
  "docx",
  "xls",
  "xlsx",
  "ppt",
  "pptx",
  "odt",
  "ods",
  "odp",
  "eml",
]);

export const extensionOf = (name: string): string => {
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
};

export const paperlessAccepts = (file: File): boolean => PAPERLESS_EXTENSIONS.has(extensionOf(file.name));

/** Where a file ends up: photos and types Paperless can't take always stay here. */
export const storageFor = (p: PendingFile, wantPaperless: boolean): "local" | "paperless" =>
  wantPaperless && p.kind !== "photo" && paperlessAccepts(p.file) ? "paperless" : "local";
