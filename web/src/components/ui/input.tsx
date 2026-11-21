import { useId, useMemo, useState, type ComponentProps, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { FieldContext, useFieldControl } from "@/lib/field";
import { cn } from "@/lib/utils";

// Touch screens get 16px text, or iOS zooms the page in when a field takes focus.
export const fieldBase =
  "w-full rounded-lg border border-border bg-surface px-3 text-sm pointer-coarse:text-base text-fg shadow-card placeholder:text-fg-subtle " +
  "transition-[border-color,box-shadow] outline-none focus:border-primary focus:ring-3 focus:ring-ring/40 " +
  "disabled:opacity-60 aria-invalid:border-bad aria-invalid:focus:ring-bad/25";

export const Input = ({ className, ...props }: ComponentProps<"input">) => (
  <input className={cn(fieldBase, "h-9", className)} {...props} {...useFieldControl(props)} />
);

export const Textarea = ({ className, ...props }: ComponentProps<"textarea">) => (
  <textarea
    className={cn(fieldBase, "min-h-20 py-2 leading-relaxed", className)}
    {...props}
    {...useFieldControl(props)}
  />
);

export const Field = ({
  label,
  hint,
  error,
  children,
  className,
  htmlFor,
  optional,
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: string | null;
  children: ReactNode;
  className?: string;
  htmlFor?: string;
  optional?: boolean;
}) => {
  const { t } = useTranslation();
  const autoId = useId();
  const id = htmlFor ?? autoId;
  // The label points at a control only once one has taken the field's id.
  const [claims, setClaims] = useState(0);
  const [claim] = useState(() => () => {
    setClaims((n) => n + 1);
    return () => setClaims((n) => n - 1);
  });
  const desc = error || hint ? `${id}-desc` : undefined;
  const invalid = !!error;
  const info = useMemo(
    () => ({ id, labelId: `${id}-label`, descId: desc, invalid, claim }),
    [id, desc, invalid, claim],
  );
  return (
    <FieldContext value={info}>
      <div className={cn("flex flex-col gap-1.5", className)}>
        <label
          id={`${id}-label`}
          htmlFor={htmlFor ?? (claims > 0 ? id : undefined)}
          className="text-[13px] font-medium text-fg"
        >
          {label}
          {optional && <span className="ml-1 font-normal text-fg-subtle">{t("common.optional")}</span>}
        </label>
        {children}
        {error ? (
          <p id={desc} className="text-xs text-bad">
            {error}
          </p>
        ) : hint ? (
          <p id={desc} className="text-xs text-fg-subtle">
            {hint}
          </p>
        ) : null}
      </div>
    </FieldContext>
  );
};

export const Select = ({ className, children, ...props }: ComponentProps<"select">) => (
  <select
    className={cn(
      fieldBase,
      "h-9 cursor-pointer appearance-none bg-[length:16px] bg-[right_0.6rem_center] bg-no-repeat pr-8",
      "bg-[url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E\")]",
      className,
    )}
    {...props}
    {...useFieldControl(props)}
  >
    {children}
  </select>
);
