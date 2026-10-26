import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import i18n from "@/i18n";

export const cn = (...inputs: ClassValue[]) => twMerge(clsx(inputs));

export const formatBytes = (n: number): string => {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
};

export const formatMoney = (amount: number, currency: string): string => {
  try {
    return new Intl.NumberFormat(i18n.language, { style: "currency", currency, maximumFractionDigits: 2 }).format(
      amount,
    );
  } catch {
    return `${amount.toFixed(2)} ${currency}`;
  }
};

/** Common currencies first; any ISO code can still be typed. */
export const CURRENCIES = ["EUR", "USD", "GBP", "CZK", "PLN", "CHF", "SEK", "NOK", "DKK", "HUF", "CAD", "AUD", "JPY"];

/**
 * Only same-origin paths are allowed as post-login destinations. Mirrors the
 * server's check: browsers strip tabs/newlines and treat "\" as "/", so those
 * could turn "/x" into "//evil.example".
 */
export const safeReturn = (value: string | null): string => {
  if (!value?.startsWith("/") || value.startsWith("//")) return "/";
  for (const ch of value) {
    const code = ch.charCodeAt(0);
    if (code < 0x20 || code === 0x7f || ch === "\\") return "/";
  }
  try {
    const url = new URL(value, location.origin);
    return url.origin === location.origin ? value : "/";
  } catch {
    return "/";
  }
};
