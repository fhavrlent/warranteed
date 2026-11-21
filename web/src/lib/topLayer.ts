import { createContext, use } from "react";

/**
 * While a modal <dialog> is open, everything outside it is inert. Toasts and popovers
 * therefore have to live inside the topmost open dialog to be seen, clicked and read out.
 */
const open: HTMLDialogElement[] = [];
let host: HTMLElement | null = null;

/** Where the toasts are rendered; follows the topmost open dialog. */
export const toastHost = () => {
  if (!host) {
    host = document.createElement("div");
    document.body.append(host);
  }
  return host;
};

const placeToasts = () => {
  const h = toastHost();
  const target = open.at(-1) ?? document.body;
  if (h.parentNode === target) return;
  // moveBefore keeps the toasts' animations and focus where it's supported.
  if ("moveBefore" in target && h.isConnected) target.moveBefore(h, null);
  else target.append(h);
};

export const modalOpened = (d: HTMLDialogElement) => {
  open.push(d);
  placeToasts();
};

export const modalClosed = (d: HTMLDialogElement) => {
  const i = open.indexOf(d);
  if (i >= 0) open.splice(i, 1);
  placeToasts();
};

/** The dialog a popover inside it must portal into; undefined outside dialogs (the body). */
export const TopLayerContext = createContext<HTMLElement | undefined>(undefined);
export const usePortalContainer = () => use(TopLayerContext);
