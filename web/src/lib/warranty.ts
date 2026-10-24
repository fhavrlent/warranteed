import type { Attachment, Warranty } from "./api";
import { daysUntil, fromISO, relativeDays } from "./dates";

export type Status = "active" | "expiring" | "expired" | "lifetime" | "archived";

export const statusOf = (w: Warranty, expiringSoonDays: number): Status => {
  if (w.archivedAt) return "archived";
  if (w.isLifetime || !w.expiresOn) return "lifetime";
  const d = daysUntil(w.expiresOn);
  if (d < 0) return "expired";
  if (d <= expiringSoonDays) return "expiring";
  return "active";
};

export const statusLabel: Record<Status, string> = {
  active: "Active",
  expiring: "Expiring soon",
  expired: "Expired",
  lifetime: "Lifetime",
  archived: "Archived",
};

/** "Expires in 3 months" / "Expired 2 days ago" / "Never expires". */
export const expiryText = (w: Warranty): string => {
  if (w.isLifetime || !w.expiresOn) return "Never expires";
  const d = daysUntil(w.expiresOn);
  if (d < 0) return `Expired ${relativeDays(d)}`;
  if (d === 0) return "Expires today";
  return `Expires ${relativeDays(d)}`;
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

export const displayName = (w: Warranty): string => {
  if (w.brand && !w.productName.toLowerCase().includes(w.brand.toLowerCase())) return `${w.brand} ${w.productName}`;
  return w.productName;
};

/** Tailwind classes per tag colour (kept as literal strings so Tailwind sees them). */
export const tagClasses: Record<string, string> = {
  slate: "bg-slate-500/12 text-slate-700 dark:text-slate-300",
  red: "bg-red-500/12 text-red-700 dark:text-red-300",
  orange: "bg-orange-500/12 text-orange-700 dark:text-orange-300",
  amber: "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  lime: "bg-lime-500/15 text-lime-800 dark:text-lime-300",
  emerald: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
  teal: "bg-teal-500/12 text-teal-700 dark:text-teal-300",
  sky: "bg-sky-500/12 text-sky-700 dark:text-sky-300",
  indigo: "bg-indigo-500/12 text-indigo-700 dark:text-indigo-300",
  violet: "bg-violet-500/12 text-violet-700 dark:text-violet-300",
  pink: "bg-pink-500/12 text-pink-700 dark:text-pink-300",
  rose: "bg-rose-500/12 text-rose-700 dark:text-rose-300",
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
