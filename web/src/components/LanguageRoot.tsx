import { Fragment, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

/** Re-renders everything when the language changes, including text from helper functions. */
export const LanguageRoot = ({ children }: { children: ReactNode }) => {
  const { i18n } = useTranslation();
  return <Fragment key={i18n.language}>{children}</Fragment>;
};
