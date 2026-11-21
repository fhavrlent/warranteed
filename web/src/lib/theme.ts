import { useSyncExternalStore } from "react";

export type Theme = "system" | "light" | "dark";

const KEY = "warranteed-theme";

const setMeta = (theme: Theme) => {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]');
  if (meta) meta.content = theme === "system" ? "light dark" : theme;
};

/** Pins light or dark through <meta name="color-scheme">; "system" leaves it to the OS. */
export const applyTheme = (theme: Theme) => {
  setMeta(theme);
  try {
    if (theme === "system") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, theme);
  } catch {
    /* storage unavailable; the theme still applies to this page */
  }
};

/** Follows a theme picked in another tab. */
export const watchOtherTabs = () => {
  const onStorage = (e: StorageEvent) => {
    if (e.key === KEY || e.key === null) setMeta((e.newValue as Theme | null) ?? "system");
  };
  window.addEventListener("storage", onStorage);
  return () => window.removeEventListener("storage", onStorage);
};

const dark = () => window.matchMedia("(prefers-color-scheme: dark)");
const subscribe = (cb: () => void) => {
  const m = dark();
  m.addEventListener("change", cb);
  return () => m.removeEventListener("change", cb);
};

/** The scheme the OS asks for right now. */
export const useSystemScheme = (): "light" | "dark" =>
  useSyncExternalStore(subscribe, () => (dark().matches ? "dark" : "light"));

const schemeMeta = () => document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]');
const subscribeScheme = (cb: () => void) => {
  const meta = schemeMeta();
  const observer = new MutationObserver(cb);
  if (meta) observer.observe(meta, { attributes: true, attributeFilter: ["content"] });
  const stop = subscribe(cb);
  return () => {
    observer.disconnect();
    stop();
  };
};

/** The scheme the page is shown in: the pinned one, or the system's. */
export const useColorScheme = (): "light" | "dark" =>
  useSyncExternalStore(subscribeScheme, () => {
    const pinned = schemeMeta()?.content;
    if (pinned === "light" || pinned === "dark") return pinned;
    return dark().matches ? "dark" : "light";
  });
