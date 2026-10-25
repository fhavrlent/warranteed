/** Lowercase and without accents, so "Káva" and "kava" compare equal. */
export const normalize = (s: string): string =>
  s
    .toLowerCase()
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "");

/** Optimal string alignment distance: insertions, deletions, substitutions and swaps of neighbours. */
const distance = (a: string, b: string): number => {
  // Three rolling rows: two back (for swaps), previous and current.
  let older: number[] = [];
  let prev = Array.from({ length: b.length + 1 }, (_, j) => j);
  for (let i = 1; i <= a.length; i++) {
    const cur = [i];
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      let d = Math.min((prev[j] ?? 0) + 1, (cur[j - 1] ?? 0) + 1, (prev[j - 1] ?? 0) + cost);
      if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) d = Math.min(d, (older[j - 2] ?? 0) + 1);
      cur.push(d);
    }
    older = prev;
    prev = cur;
  }
  return prev[b.length] ?? 0;
};

/** Typos allowed for a search word: none below 3 letters, then 1, and 2 from 6 letters. */
const allowedTypos = (term: string): number => (term.length < 3 ? 0 : term.length < 6 ? 1 : 2);

/**
 * Whether a search word approximately starts some word of the text. Comparing
 * against word prefixes lets "kav" find "kava" while still tolerating typos.
 */
const fuzzyHit = (term: string, words: string[]): boolean => {
  const max = allowedTypos(term);
  if (max === 0) return false;
  return words.some((word) => {
    // A whole word may be shorter than the term ("kav" vs "ka"), but a
    // word's beginning may not, or "ebo" would match anything starting "bo".
    // Words under 3 letters only match exactly ("kva" shouldn't find serial "KV-…").
    if (word.length < term.length) return word.length >= Math.max(3, term.length - max) && distance(term, word) <= max;
    for (let len = term.length; len <= Math.min(term.length + max, word.length); len++) {
      if (distance(term, word.slice(0, len)) <= max) return true;
    }
    return false;
  });
};

export interface Matcher {
  /** 2 = every word found as typed, 1 = found allowing typos, 0 = no match. */
  score: (text: string) => number;
}

/**
 * Builds a matcher for a query; every word of the query must match. The text
 * may hold several fields separated by "\u0000".
 */
export const matcher = (query: string): Matcher => {
  const terms = normalize(query).split(/\s+/).filter(Boolean);
  return {
    score: (text) => {
      if (terms.length === 0) return 2;
      const haystack = normalize(text);
      // Serial and model numbers: "ab12" should find "AB-1234". Each field
      // (separated by NUL) is compacted on its own so fields don't run together.
      const compact = haystack.split("\u0000").map((f) => f.replace(/[^\p{L}\p{N}]+/gu, ""));
      let exact = true;
      let words: string[] | null = null;
      for (const term of terms) {
        const bare = term.replace(/[^\p{L}\p{N}]+/gu, "");
        if (haystack.includes(term) || (bare && compact.some((f) => f.includes(bare)))) continue;
        words ??= haystack.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
        if (!fuzzyHit(term, words)) return 0;
        exact = false;
      }
      return exact ? 2 : 1;
    },
  };
};
