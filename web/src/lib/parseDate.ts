import { isValid, parse } from "date-fns";
import { dateLocale } from "@/i18n";
import { ISO, toISO } from "./dates";

// Kept apart from dates.ts: date-fns' parser is large and only the date field needs it.

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
