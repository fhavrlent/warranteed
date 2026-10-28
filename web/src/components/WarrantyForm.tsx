import { lazy, Suspense } from "react";
import type { FormProps } from "./WarrantyFormDialog";

// The form (and its calendar) is only downloaded when first opened.
const WarrantyFormDialog = lazy(() => import("./WarrantyFormDialog").then((m) => ({ default: m.WarrantyFormDialog })));

/** Add/edit dialog. The form is mounted only while open, so every opening starts from fresh state. */
export const WarrantyForm = ({ open, ...props }: FormProps & { open: boolean }) =>
  open ? (
    <Suspense fallback={null}>
      <WarrantyFormDialog {...props} />
    </Suspense>
  ) : null;
