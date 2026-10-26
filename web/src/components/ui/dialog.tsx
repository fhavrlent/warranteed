import * as D from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

export const Dialog = ({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  className,
  size = "md",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
  size?: "sm" | "md" | "lg" | "xl";
}) => {
  const { t } = useTranslation();
  const widths = { sm: "max-w-md", md: "max-w-xl", lg: "max-w-2xl", xl: "max-w-4xl" };
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="animate-fade-in fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px]" />
        <D.Content
          className={cn(
            "animate-pop-in fixed top-1/2 left-1/2 z-50 flex max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 flex-col",
            "rounded-2xl border border-border bg-surface shadow-pop outline-none",
            widths[size],
            className,
          )}
          onOpenAutoFocus={(e) => {
            // Focus the first field rather than the close button.
            const first = (e.currentTarget as HTMLElement).querySelector<HTMLElement>("[data-autofocus]");
            if (first) {
              e.preventDefault();
              first.focus();
            }
          }}
        >
          <div className="flex items-start justify-between gap-4 border-b border-border px-6 pt-5 pb-4">
            <div className="min-w-0">
              <D.Title className="text-base font-semibold tracking-tight">{title}</D.Title>
              {description ? (
                <D.Description className="mt-1 text-sm text-fg-muted">{description}</D.Description>
              ) : (
                <D.Description className="sr-only">
                  {typeof title === "string" ? title : t("common.dialog")}
                </D.Description>
              )}
            </div>
            <D.Close
              className="-mt-1 -mr-2 rounded-md p-1.5 text-fg-subtle hover:bg-surface-3 hover:text-fg"
              aria-label={t("common.close")}
            >
              <X className="size-4" />
            </D.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">{children}</div>
          {footer !== undefined && (
            <div className="flex items-center justify-end gap-2 rounded-b-2xl border-t border-border bg-surface-2 px-6 py-3.5">
              {footer}
            </div>
          )}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
};

export const ConfirmDialog = ({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  loading,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  onConfirm: () => void;
  loading?: boolean;
}) => {
  const { t } = useTranslation();
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="animate-fade-in fixed inset-0 z-40 bg-black/40" />
        <D.Content className="animate-pop-in fixed top-1/2 left-1/2 z-50 w-[calc(100vw-2rem)] max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-surface p-6 shadow-pop">
          <D.Title className="text-base font-semibold">{title}</D.Title>
          <D.Description className="mt-2 text-sm text-fg-muted">{description}</D.Description>
          <div className="mt-6 flex justify-end gap-2">
            <D.Close asChild>
              <button
                type="button"
                className="h-9 cursor-pointer rounded-lg px-3.5 text-sm font-medium text-fg-muted hover:bg-surface-3"
              >
                {t("common.cancel")}
              </button>
            </D.Close>
            <button
              type="button"
              onClick={onConfirm}
              disabled={loading}
              className="h-9 cursor-pointer rounded-lg bg-bad px-3.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            >
              {confirmLabel ?? t("common.delete")}
            </button>
          </div>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
};
