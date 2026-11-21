import i18n from "@/i18n";
import type { Attachment, Warranty, WarrantyInput } from "./api";
import { daysUntil, fromISO, relativeDays } from "./dates";

export type Status = "draft" | "active" | "expiring" | "expired" | "lifetime" | "void" | "archived";

export const statusOf = (w: Warranty, expiringSoonDays: number): Status => {
  if (w.draft) return "draft";
  if (w.archivedAt) return "archived";
  if (w.voidedAt) return "void";
  if (w.isLifetime || !w.expiresOn) return "lifetime";
  const d = daysUntil(w.expiresOn);
  if (d < 0) return "expired";
  if (d <= expiringSoonDays) return "expiring";
  return "active";
};

export const statusLabel = (s: Status): string => i18n.t(`status.${s}`);

/** "Expires in 3 months" / "Expired 2 days ago" / "Never expires". */
export const expiryText = (w: Warranty): string => {
  const t = i18n.t;
  if (w.isLifetime || !w.expiresOn) return t("expiry.never");
  const d = daysUntil(w.expiresOn);
  if (d < 0) return t("expiry.past", { when: relativeDays(d) });
  if (d === 0) return t("expiry.today");
  return t("expiry.future", { when: relativeDays(d) });
};

/** Share of the coverage period already used, 0..1. */
export const coverageUsed = (w: Warranty): number | null => {
  if (w.isLifetime || !w.expiresOn) return null;
  const start = fromISO(w.purchaseDate).getTime();
  const end = fromISO(w.expiresOn).getTime();
  if (end <= start) return 1;
  return Math.min(1, Math.max(0, (Date.now() - start) / (end - start)));
};

export const photoOf = (w: Warranty): Attachment | undefined =>
  w.attachments.find((a) => a.kind === "photo" && a.status === "ready");

/** What the pieces cost together, or null without a price. */
export const totalPrice = (w: Warranty): number | null => (w.price == null ? null : w.price * w.quantity);

export const displayName = (w: Warranty): string => {
  if (w.brand && !w.productName.toLowerCase().includes(w.brand.toLowerCase())) return `${w.brand} ${w.productName}`;
  return w.productName;
};

/** Tailwind classes per tag colour (kept as literal strings so Tailwind sees them). */
export const tagClasses: Record<string, string> = {
  slate: "bg-slate-500/12 text-[light-dark(var(--color-slate-700),var(--color-slate-300))]",
  red: "bg-red-500/12 text-[light-dark(var(--color-red-700),var(--color-red-300))]",
  orange: "bg-orange-500/12 text-[light-dark(var(--color-orange-700),var(--color-orange-300))]",
  amber: "bg-amber-500/15 text-[light-dark(var(--color-amber-800),var(--color-amber-300))]",
  lime: "bg-lime-500/15 text-[light-dark(var(--color-lime-800),var(--color-lime-300))]",
  emerald: "bg-emerald-500/12 text-[light-dark(var(--color-emerald-700),var(--color-emerald-300))]",
  teal: "bg-teal-500/12 text-[light-dark(var(--color-teal-700),var(--color-teal-300))]",
  sky: "bg-sky-500/12 text-[light-dark(var(--color-sky-700),var(--color-sky-300))]",
  indigo: "bg-indigo-500/12 text-[light-dark(var(--color-indigo-700),var(--color-indigo-300))]",
  violet: "bg-violet-500/12 text-[light-dark(var(--color-violet-700),var(--color-violet-300))]",
  pink: "bg-pink-500/12 text-[light-dark(var(--color-pink-700),var(--color-pink-300))]",
  rose: "bg-rose-500/12 text-[light-dark(var(--color-rose-700),var(--color-rose-300))]",
};

export const tagDot: Record<string, string> = {
  slate: "bg-slate-500",
  red: "bg-red-500",
  orange: "bg-orange-500",
  amber: "bg-amber-500",
  lime: "bg-lime-500",
  emerald: "bg-emerald-500",
  teal: "bg-teal-500",
  sky: "bg-sky-500",
  indigo: "bg-indigo-500",
  violet: "bg-violet-500",
  pink: "bg-pink-500",
  rose: "bg-rose-500",
};

/** The editable fields of a warranty, e.g. to save it with one change. */
export const toInput = (w: Warranty): WarrantyInput => ({
  productName: w.productName,
  brand: w.brand,
  model: w.model,
  vendor: w.vendor,
  orderNumber: w.orderNumber,
  purchaseDate: w.purchaseDate,
  warrantyMonths: w.warrantyMonths,
  warrantyEndDate: w.warrantyEndDate,
  isLifetime: w.isLifetime,
  price: w.price,
  currency: w.currency,
  quantity: w.quantity,
  productUrl: w.productUrl,
  notes: w.notes,
  serialNumbers: w.serialNumbers,
  tags: w.tags.map((t) => t.name),
  spaceId: w.spaceId,
});
