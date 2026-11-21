import { X } from "lucide-react";
import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { modalClosed, modalOpened, TopLayerContext } from "@/lib/topLayer";
import { cn } from "@/lib/utils";

// Where the browser can't light-dismiss with closedby="any", a click on the backdrop does it.
const nativeLightDismiss = typeof HTMLDialogElement !== "undefined" && "closedBy" in HTMLDialogElement.prototype;

/**
 * A native modal <dialog>: the browser makes the page inert, keeps focus inside, closes on
 * Esc or the back gesture and returns focus to whatever opened it. `onOpenChange(false)` asks
 * to close; the dialog stays open while the caller keeps `open` true (say, while saving).
 */
const Modal = ({
  open,
  onOpenChange,
  labelledBy,
  describedBy,
  className,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  labelledBy: string;
  describedBy?: string;
  className?: string;
  children: ReactNode;
}) => {
  const ref = useRef<HTMLDialogElement>(null);
  const [container, setContainer] = useState<HTMLDialogElement | undefined>();
  const openRef = useRef(open);
  const onOpenChangeRef = useRef(onOpenChange);
  useLayoutEffect(() => {
    openRef.current = open;
    onOpenChangeRef.current = onOpenChange;
  });

  useLayoutEffect(() => {
    const d = ref.current;
    if (!d || !open) return;
    if (!d.open) d.showModal();
    modalOpened(d);
    setContainer(d);
    // Focus the field marked for it rather than the first button (the close button).
    d.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    return () => {
      modalClosed(d);
      if (d.open) d.close();
    };
  }, [open]);

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    // Esc, the back gesture and light dismiss: ask, and let the caller decide.
    const onCancel = (e: Event) => {
      e.preventDefault();
      onOpenChangeRef.current(false);
    };
    // The browser closed it anyway (a repeated Esc can't be cancelled): reopen if the caller still wants it.
    const onClose = () => {
      if (!openRef.current) return;
      onOpenChangeRef.current(false);
      queueMicrotask(() => {
        if (openRef.current && d.isConnected && !d.open) d.showModal();
      });
    };
    d.addEventListener("cancel", onCancel);
    d.addEventListener("close", onClose);
    return () => {
      d.removeEventListener("cancel", onCancel);
      d.removeEventListener("close", onClose);
    };
  }, []);

  // Light dismiss fallback: the press must start and end on the backdrop (the dialog itself).
  const pressedBackdropRef = useRef(false);

  return createPortal(
    <dialog
      ref={ref}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      {...{ closedby: "any" }}
      onPointerDown={(e) => {
        pressedBackdropRef.current = e.target === e.currentTarget;
      }}
      onClick={(e) => {
        if (!nativeLightDismiss && pressedBackdropRef.current && e.target === e.currentTarget) onOpenChange(false);
        pressedBackdropRef.current = false;
      }}
      className={cn(
        "m-auto w-[calc(100vw-2rem)] overflow-hidden overscroll-contain rounded-2xl border border-border bg-surface p-0 text-fg shadow-pop outline-none",
        "open:animate-pop-in backdrop:animate-fade-in backdrop:bg-black/40",
        className,
      )}
    >
      <TopLayerContext value={container}>{open && children}</TopLayerContext>
    </dialog>,
    document.body,
  );
};

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
  const titleId = useId();
  const descId = useId();
  const widths = { sm: "max-w-md", md: "max-w-xl", lg: "max-w-2xl", xl: "max-w-4xl" };
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      labelledBy={titleId}
      describedBy={description ? descId : undefined}
      className={cn(
        "max-h-[calc(100dvh-2rem)] flex-col backdrop:backdrop-blur-[2px] open:flex",
        widths[size],
        className,
      )}
    >
      <div className="flex items-start justify-between gap-4 border-b border-border px-6 pt-5 pb-4">
        <div className="min-w-0">
          <h2 id={titleId} className="text-base font-semibold tracking-tight">
            {title}
          </h2>
          {description ? (
            <p id={descId} className="mt-1 text-sm text-fg-muted">
              {description}
            </p>
          ) : null}
        </div>
        <button
          type="button"
          onClick={() => onOpenChange(false)}
          className="-mt-1 -mr-2 cursor-pointer rounded-md p-1.5 text-fg-subtle hover:bg-surface-3 hover:text-fg"
          aria-label={t("common.close")}
        >
          <X className="size-4" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-5">{children}</div>
      {footer !== undefined && (
        <div className="flex items-center justify-end gap-2 rounded-b-2xl border-t border-border bg-surface-2 px-6 py-3.5">
          {footer}
        </div>
      )}
    </Modal>
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
  const titleId = useId();
  const descId = useId();
  return (
    <Modal open={open} onOpenChange={onOpenChange} labelledBy={titleId} describedBy={descId} className="max-w-sm p-6">
      <h2 id={titleId} className="text-base font-semibold">
        {title}
      </h2>
      <div id={descId} className="mt-2 text-sm text-fg-muted">
        {description}
      </div>
      <div className="mt-6 flex justify-end gap-2">
        {/* The safe choice takes focus, so Enter doesn't delete anything. */}
        <button
          type="button"
          data-autofocus
          onClick={() => onOpenChange(false)}
          className="h-9 cursor-pointer rounded-lg px-3.5 text-sm font-medium text-fg-muted hover:bg-surface-3"
        >
          {t("common.cancel")}
        </button>
        <button
          type="button"
          onClick={onConfirm}
          disabled={loading}
          className="h-9 cursor-pointer rounded-lg bg-bad px-3.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
        >
          {confirmLabel ?? t("common.delete")}
        </button>
      </div>
    </Modal>
  );
};
