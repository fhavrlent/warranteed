import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import type { DayPickerLocale } from "react-day-picker";
import { cs as csDates } from "react-day-picker/locale/cs";
import { enUS } from "react-day-picker/locale/en-US";
import cs from "./locales/cs.json";
import en from "./locales/en.json";

/**
 * The available languages. To add one, create locales/<code>.json (see
 * TRANSLATING.md) and add a line here with its name and date-fns locale.
 */
export const LANGUAGES: Record<string, { name: string; translation: object; dates: DayPickerLocale }> = {
  en: { name: "English", translation: en, dates: enUS },
  cs: { name: "Čeština", translation: cs, dates: csDates },
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

/** Switches to the language chosen in settings ("" follows the browser). */
export const applyLanguage = async (setting: string): Promise<void> => {
  const lng = setting && setting in LANGUAGES ? setting : browserLanguage();
  try {
    localStorage.setItem(STORAGE_KEY, lng);
  } catch {
    /* ignore */
  }
  if (i18n.language !== lng) await i18n.changeLanguage(lng);
};

/** The date-fns locale of the current language, for month and day names. */
export const dateLocale = (): DayPickerLocale => (LANGUAGES[i18n.language] ?? LANGUAGES.en)?.dates ?? enUS;

i18n.on("languageChanged", (lng) => {
  document.documentElement.lang = lng;
});

void i18n.use(initReactI18next).init({
  resources: Object.fromEntries(Object.entries(LANGUAGES).map(([code, l]) => [code, { translation: l.translation }])),
  lng: remembered() ?? browserLanguage(),
  fallbackLng: "en",
  interpolation: { escapeValue: false }, // React escapes already
  returnNull: false,
});

export default i18n;
