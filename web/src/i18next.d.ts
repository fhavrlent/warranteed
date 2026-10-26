import "i18next";
import type en from "./locales/en.json";

// English is the source language: keys are checked against it.
declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    resources: { translation: typeof en };
  }
}
