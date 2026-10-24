import { differenceInCalendarDays, format, isValid, parse, parseISO } from "date-fns";

export const ISO = "yyyy-MM-dd";

/** Parses a YYYY-MM-DD string as a local calendar date. */
export const fromISO = (iso: string): Date => parseISO(iso);

export const toISO = (d: Date): string => format(d, ISO);

export const todayISO = (): string => toISO(new Date());

export const formatDate = (iso: string | null | undefined, fmt: string): string => {
  if (!iso) return "";
  const d = fromISO(iso);
  return isValid(d) ? format(d, fmt) : iso;
};

/**
 * Parses what a user typed. The preferred format comes first, then common
 * alternatives, so typing "2024-05-03" still works for a dd.MM.yyyy user.
 * Two-digit years are accepted.
 */
export const parseUserDate = (text: string, fmt: string): string | null => {
  const s = text.trim();
  if (!s) return null;
  const usFirst = fmt.startsWith("MM/") || fmt.startsWith("MMM") || fmt.startsWith("MMMM");
  const candidates = [
    fmt,
    ISO,
    ...(usFirst ? ["M/d/yyyy", "M/d/yy"] : ["d.M.yyyy", "d. M. yyyy", "d.M.yy", "d/M/yyyy", "d/M/yy"]),
    "d MMM yyyy",
    "MMM d, yyyy",
    "d MMMM yyyy",
    "MMMM d, yyyy",
    "yyyyMMdd",
  ];
  const ref = new Date();
  for (const c of candidates) {
    const d = parse(s, c, ref);
    if (isValid(d) && d.getFullYear() >= 1900 && d.getFullYear() <= 2200) return toISO(d);
  }
  return null;
};

export const daysUntil = (iso: string): number => differenceInCalendarDays(fromISO(iso), new Date());

/** Week starts on Sunday only for US-style formats. */
export const weekStartsOn = (fmt: string): 0 | 1 => (fmt.startsWith("MM/") || fmt.startsWith("MMM") ? 0 : 1);

export const relativeDays = (days: number): string => {
  const abs = Math.abs(days);
  if (days === 0) return "today";
  if (days === 1) return "tomorrow";
  if (days === -1) return "yesterday";
  const unit = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;
  let text: string;
  if (abs < 60) text = unit(abs, "day");
  else if (abs < 730) text = unit(Math.round(abs / 30.44), "month");
  else text = unit(Math.round((abs / 365.25) * 10) / 10, "year");
  return days > 0 ? `in ${text}` : `${text} ago`;
};

export const durationLabel = (months: number): string => {
  if (months % 12 === 0) {
    const y = months / 12;
    return `${y} year${y === 1 ? "" : "s"}`;
  }
  if (months > 12) {
    const y = Math.floor(months / 12);
    const m = months % 12;
    return `${y} yr ${m} mo`;
  }
  return `${months} month${months === 1 ? "" : "s"}`;
};
