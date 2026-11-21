import { createPortal } from "react-dom";
import { Toaster } from "sonner";
import { useColorScheme } from "@/lib/theme";
import { toastHost } from "@/lib/topLayer";

/** Toasts in the page's scheme, kept inside whichever dialog is open so they stay usable. */
export const Toasts = () =>
  createPortal(
    <Toaster
      position="bottom-right"
      richColors
      closeButton
      theme={useColorScheme()}
      toastOptions={{ className: "font-sans" }}
    />,
    toastHost(),
  );
