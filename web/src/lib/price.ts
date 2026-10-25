/**
 * Reads a price the way shops write it: "1 379", "1 379,50", "1.379,50",
 * "1,379.50", "1'379", optionally with a currency ("1 379 Kč", "€1,379").
 * When both "." and "," appear, the last one is the decimal separator; a
 * single separator followed by exactly three digits groups thousands.
 * Returns null for empty input and NaN when it isn't a price.
 */
export const parsePrice = (input: string): number | null => {
  // Drop spaces of every kind, apostrophes and anything that isn't part of a number.
  const s = input.replace(/[\s\u00a0\u202f\u2009'\u2019]/g, "").replace(/[^\d.,-]/g, "");
  if (s === "") return input.trim() === "" ? null : NaN;
  const lastDot = s.lastIndexOf(".");
  const lastComma = s.lastIndexOf(",");
  let normalized: string;
  if (lastDot >= 0 && lastComma >= 0) {
    const decimal = lastDot > lastComma ? "." : ",";
    const grouping = decimal === "." ? "," : ".";
    normalized = s.split(grouping).join("").replace(decimal, ".");
  } else if (lastDot >= 0 || lastComma >= 0) {
    const sep = lastDot >= 0 ? "." : ",";
    const parts = s.split(sep);
    const tail = parts[parts.length - 1] ?? "";
    // "1.379.000" or "1,379": grouping. "13,5" or "0.379": a decimal.
    const grouping = parts.length > 2 || (tail.length === 3 && parts[0] !== "0" && parts[0] !== "");
    normalized = grouping ? parts.join("") : parts.join(".");
  } else {
    normalized = s;
  }
  if (!/^\d+(\.\d+)?$/.test(normalized)) return NaN;
  return Number(normalized);
};

/** Shows a price grouped in the user's locale, e.g. 1379.5 -> "1 379,50" in Czech. */
export const formatPriceInput = (value: number): string =>
  new Intl.NumberFormat(undefined, {
    minimumFractionDigits: Number.isInteger(value) ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(value);
