# Translating Warranteed

All text, including reminder messages and errors from the server, lives in one JSON file per language in [`web/src/locales`](web/src/locales). English (`en.json`) is the source; every other language translates its keys.

## Adding a language

1. Copy `web/src/locales/en.json` to `web/src/locales/<code>.json`, where `<code>` is the two-letter [ISO 639-1 code](https://en.wikipedia.org/wiki/List_of_ISO_639_language_codes) (`de`, `fr`, `pl`, …).
2. Translate the values. Keep the keys as they are.
3. Register the language in [`web/src/i18n.ts`](web/src/i18n.ts) by adding an entry to `LANGUAGES`. It loads your file and the [date-fns locale](https://github.com/date-fns/date-fns/tree/main/src/locale) for month and day names; both are downloaded only by people using the language:

   ```ts
   de: {
     name: "Deutsch",
     load: async () => {
       const [texts, dates] = await Promise.all([import("./locales/de.json"), import("react-day-picker/locale/de")]);
       return { translation: texts.default, dates: dates.de };
     },
   },
   ```

   `name` is the language's own name, as people who speak it would look for it.
4. Check your work (needs Node.js):

   ```sh
   cd web
   npm ci
   npm run i18n:check
   ```

   It lists missing, extra or empty texts, missing plural forms, and placeholders or tags that don't match English.
5. Open a pull request.

The server picks the new file up automatically. Users get it when their browser asks for that language, or by choosing it under **Settings → Display → Language**.

## What to keep

- **Placeholders** in double braces are filled in by the app: `{{name}}`, `{{date}}`, `{{count}}`. Keep them, but move them wherever the sentence needs them. `{{count, number}}` formats the number for the language.
- **Tags** like `<b>…</b>`, `<link>…</link>` and `<code>…</code>` mark bold text, links and code. Keep each pair around the matching words.
- **Plurals** use suffixes on the key: `_one`, `_few`, `_many`, `_other` (and `_zero`, `_two` in some languages). English has only `_one` and `_other`. Your language needs exactly the forms its [CLDR plural rules](https://www.unicode.org/cldr/charts/latest/supplemental/language_plural_rules.html) define; `npm run i18n:check` tells you which ones are missing. For example, Czech uses `_one` (1), `_few` (2–4), `_many` (decimals like 2.5) and `_other` (0, 5 and more).
- **Month names** in `reminder.monthsLong` and `reminder.monthsShort` are comma-separated lists of all twelve months, used for dates in reminders. Write them the way they appear inside a date (in Czech, "31. prosince", not "prosinec").

## Fixing a translation

Edit the text in the language's file and open a pull request. `npm run i18n:check` should still pass.

## For developers

- Use `t("section.key")` from `useTranslation()` in components, or `i18n.t` in plain functions; keys are type-checked against `en.json`.
- Add new texts to `en.json` and every other language. A missing translation falls back to English, but the check (part of `npm run check` and CI) fails until it's added.
- Server messages go through `internal/i18n`: errors are sent with a translation key (`server.*`) and shown in the user's language by the web app. Reminders are written on the server in the user's language.
