import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import type { DayPickerLocale } from "react-day-picker";
import { enUS } from "date-fns/locale/en-US";

interface Language {
  /** The language's own name. */
  name: string;
  /** Loads its texts and its date-fns locale (month and day names), only when needed. */
  load: () => Promise<{ translation: object; dates: DayPickerLocale }>;
}

/**
 * The available languages. To add one, create locales/<code>.json (see
 * TRANSLATING.md) and add an entry here with its name and date-fns locale.
 */
export const LANGUAGES: Record<string, Language> = {
  en: {
    name: "English",
    load: async () => ({ translation: (await import("./locales/en.json")).default, dates: enUS }),
  },
  cs: {
    name: "Čeština",
    load: async () => {
      const [texts, dates] = await Promise.all([import("./locales/cs.json"), import("react-day-picker/locale/cs")]);
      return { translation: texts.default, dates: dates.cs };
    },
  },
};

/** The first of the browser's preferred languages that the app has. */
export const browserLanguage = (): string => {
  for (const tag of navigator.languages) {
    const code = tag.toLowerCase().split("-")[0] ?? "";
    if (code in LANGUAGES) return code;
  }
  return "en";
};

// The language last used, so the page doesn't flash English while settings load.
const STORAGE_KEY = "warranteed-language";
const remembered = (): string | null => {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v && v in LANGUAGES ? v : null;
  } catch {
    return null;
  }
};

const dateLocales: Record<string, DayPickerLocale> = { en: enUS };

const loadLanguage = async (code: string): Promise<void> => {
  const language = LANGUAGES[code];
  if (!language || i18n.hasResourceBundle(code, "translation")) return;
  const { translation, dates } = await language.load();
  i18n.addResourceBundle(code, "translation", translation);
  dateLocales[code] = dates;
};

/** Switches to the language chosen in settings ("" follows the browser). */
export const applyLanguage = async (setting: string): Promise<void> => {
  const lng = setting && setting in LANGUAGES ? setting : browserLanguage();
  try {
    localStorage.setItem(STORAGE_KEY, lng);
  } catch {
    /* ignore */
  }
  await loadLanguage(lng);
  if (i18n.language !== lng) await i18n.changeLanguage(lng);
};

/** The date-fns locale of the current language, for month and day names. */
export const dateLocale = (): DayPickerLocale => dateLocales[i18n.language] ?? enUS;

i18n.on("languageChanged", (lng) => {
  document.documentElement.lang = lng;
});

/** Loads the starting language; the app renders once it's ready. */
export const initI18n = async (): Promise<void> => {
  const lng = remembered() ?? browserLanguage();
  await i18n.use(initReactI18next).init({
    lng,
    fallbackLng: "en",
    resources: {},
    interpolation: { escapeValue: false }, // React escapes already
    returnNull: false,
  });
  await loadLanguage(lng);
  // English fills in any text a translation lacks; it isn't needed to start.
  void loadLanguage("en");
};

export default i18n;
