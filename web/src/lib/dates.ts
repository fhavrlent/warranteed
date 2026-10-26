import { differenceInCalendarDays, format, isValid, parse, parseISO } from "date-fns";
import i18n, { dateLocale } from "@/i18n";

export const ISO = "yyyy-MM-dd";

/** Parses a YYYY-MM-DD string as a local calendar date. */
export const fromISO = (iso: string): Date => parseISO(iso);

export const toISO = (d: Date): string => format(d, ISO);

export const todayISO = (): string => toISO(new Date());

export const formatDate = (iso: string | null | undefined, fmt: string): string => {
  if (!iso) return "";
  const d = fromISO(iso);
  return isValid(d) ? format(d, fmt, { locale: dateLocale() }) : iso;
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
  // Month names are accepted in the current language and in English.
  const locales = [dateLocale(), undefined];
  for (const c of candidates) {
    for (const locale of locales) {
      const d = parse(s, c, ref, locale ? { locale } : undefined);
      if (isValid(d) && d.getFullYear() >= 1900 && d.getFullYear() <= 2200) return toISO(d);
    }
  }
  return null;
};

export const daysUntil = (iso: string): number => differenceInCalendarDays(fromISO(iso), new Date());

/** Week starts on Sunday only for US-style formats. */
export const weekStartsOn = (fmt: string): 0 | 1 => (fmt.startsWith("MM/") || fmt.startsWith("MMM") ? 0 : 1);

/** "in 3 months", "2 days ago", "tomorrow". */
export const relativeDays = (days: number): string => {
  const t = i18n.t;
  const abs = Math.abs(days);
  if (days === 0) return t("time.today");
  if (days === 1) return t("time.tomorrow");
  if (days === -1) return t("time.yesterday");
  const future = days > 0;
  if (abs < 60) return future ? t("time.inDays", { count: abs }) : t("time.daysAgo", { count: abs });
  if (abs < 730) {
    const count = Math.round(abs / 30.44);
    return future ? t("time.inMonths", { count }) : t("time.monthsAgo", { count });
  }
  const count = Math.round((abs / 365.25) * 10) / 10;
  return future ? t("time.inYears", { count }) : t("time.yearsAgo", { count });
};

export const durationLabel = (months: number): string => {
  const t = i18n.t;
  if (months % 12 === 0) return t("duration.years", { count: months / 12 });
  if (months > 12) return t("duration.yearsMonths", { years: Math.floor(months / 12), months: months % 12 });
  return t("duration.months", { count: months });
};
