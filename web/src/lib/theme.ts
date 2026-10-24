export type Theme = "system" | "light" | "dark";

const KEY = "warranteed-theme";
const media = () => window.matchMedia("(prefers-color-scheme: dark)");

export const applyTheme = (theme: Theme) => {
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    /* storage unavailable */
  }
  const dark = theme === "dark" || (theme === "system" && media().matches);
  document.documentElement.classList.toggle("dark", dark);
};

/** Keeps "system" in sync when the OS theme changes. */
export const watchSystemTheme = (getTheme: () => Theme) => {
  const m = media();
  const onChange = () => getTheme() === "system" && applyTheme("system");
  m.addEventListener("change", onChange);
  return () => m.removeEventListener("change", onChange);
};
