// Checks every translation in src/locales against en.json, the source
// language: no missing, extra or empty texts, every plural form the language
// needs, and the same {{placeholders}} and <tags>. Also reports English keys
// that nothing uses. Run with `npm run i18n:check`.
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const web = join(dirname(fileURLToPath(import.meta.url)), "..");
const localesDir = join(web, "src", "locales");
const PLURAL = /_(zero|one|two|few|many|other)$/;

const flatten = (tree, prefix = "", out = {}) => {
  for (const [k, v] of Object.entries(tree)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "string") out[key] = v;
    else flatten(v, key, out);
  }
  return out;
};
const load = (lang) => flatten(JSON.parse(readFileSync(join(localesDir, `${lang}.json`), "utf8")));

// Placeholder names, ignoring formats: "{{count, number}}" -> "count".
const placeholders = (s) => new Set([...s.matchAll(/\{\{\s*(\w+)[^}]*\}\}/g)].map((m) => m[1]));
const tags = (s) =>
  [...s.matchAll(/<\/?(\w+)\s*\/?>/g)]
    .map((m) => m[0])
    .sort()
    .join(" ");
const sameSet = (a, b) => a.size === b.size && [...a].every((x) => b.has(x));

const en = load("en");
const groups = new Map(); // base key -> English texts by key
for (const [key, text] of Object.entries(en)) {
  const base = key.replace(PLURAL, "");
  if (!groups.has(base)) groups.set(base, { plural: PLURAL.test(key), texts: {} });
  groups.get(base).texts[key] = text;
}

const problems = [];
const langs = readdirSync(localesDir)
  .filter((f) => f.endsWith(".json") && f !== "en.json")
  .map((f) => f.slice(0, -5));

for (const lang of langs) {
  const tr = load(lang);
  const categories = new Intl.PluralRules(lang).resolvedOptions().pluralCategories;
  const expected = new Map(); // key -> English text it translates
  for (const [base, g] of groups) {
    if (!g.plural) expected.set(base, g.texts[base]);
    else
      for (const c of categories) expected.set(`${base}_${c}`, g.texts[`${base}_other`] ?? Object.values(g.texts)[0]);
  }
  for (const [key, source] of expected) {
    const text = tr[key];
    if (text === undefined) {
      problems.push(`${lang}: missing "${key}"`);
      continue;
    }
    if (!text.trim()) problems.push(`${lang}: "${key}" is empty`);
    const want = placeholders(source);
    const got = placeholders(text);
    // A plural form may leave out the number ("jeden den"), nothing else.
    const plural = PLURAL.test(key);
    const missing = [...want].filter((p) => !got.has(p) && !(plural && p === "count"));
    const extra = [...got].filter((p) => !want.has(p));
    if (missing.length || extra.length) {
      problems.push(
        `${lang}: "${key}" has placeholders {{${[...got].join("}}, {{")}}}, English has {{${[...want].join("}}, {{")}}}`,
      );
    }
    if (tags(text) !== tags(source)) problems.push(`${lang}: "${key}" must keep the tags ${tags(source) || "(none)"}`);
  }
  for (const key of Object.keys(tr)) if (!expected.has(key)) problems.push(`${lang}: "${key}" isn't in en.json`);
}

// Unused keys: every English key must appear in the web app or the server.
const read = (dir, exts) =>
  readdirSync(dir, { recursive: true, withFileTypes: true })
    .filter((e) => e.isFile() && exts.some((x) => e.name.endsWith(x)) && !e.name.endsWith("_test.go"))
    .map((e) => readFileSync(join(e.parentPath, e.name), "utf8"))
    .join("\n");
const sources = read(join(web, "src"), [".ts", ".tsx"]) + read(join(web, "..", "internal"), [".go"]);
// Keys built at runtime, e.g. t(`status.${s}`).
const DYNAMIC = ["status.", "kinds.", "signIn.", "settings.tags.colors.", "reminder.months"];
for (const base of groups.keys()) {
  if (DYNAMIC.some((p) => base.startsWith(p))) continue;
  const short = base.split(".").pop();
  const used =
    sources.includes(`"${base}"`) ||
    // Server validation errors are written as invalid("<key>", ...).
    (base.startsWith("server.") && sources.includes(`invalid("${short}"`)) ||
    (base.startsWith("server.") && sources.includes(`msg("${short}")`));
  if (!used) problems.push(`en: "${base}" isn't used anywhere`);
}

if (problems.length) {
  console.error(problems.join("\n"));
  console.error(`\n${problems.length} problem(s) in the translations.`);
  process.exit(1);
}
console.log(`Translations OK: en + ${langs.join(", ") || "no others"}.`);
