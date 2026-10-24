import * as S from "@radix-ui/react-switch";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function Switch({
  checked,
  onCheckedChange,
  id,
  disabled,
}: {
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
  id?: string;
  disabled?: boolean;
}) {
  return (
    <S.Root
      id={id}
      checked={checked}
      disabled={disabled}
      onCheckedChange={onCheckedChange}
      className="relative h-5 w-9 shrink-0 cursor-pointer rounded-full bg-border-strong transition-colors data-[state=checked]:bg-primary disabled:opacity-50"
    >
      <S.Thumb className="block size-4 translate-x-0.5 rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-[18px]" />
    </S.Root>
  );
}

/** A compact segmented control. */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  className,
  size = "md",
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode; count?: number }[];
  className?: string;
  size?: "sm" | "md";
}) {
  return (
    <div role="radiogroup" className={cn("inline-flex w-fit rounded-lg bg-surface-3 p-0.5", className)}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "inline-flex cursor-pointer items-center gap-1.5 rounded-md font-medium whitespace-nowrap transition-all [&_svg]:size-4",
            size === "sm" ? "h-7 px-2.5 text-xs" : "h-8 px-3 text-[13px]",
            value === o.value ? "bg-surface text-fg shadow-card" : "text-fg-muted hover:text-fg",
          )}
        >
          {o.label}
          {o.count !== undefined && (
            <span className={cn("tabular-nums", value === o.value ? "text-fg-muted" : "text-fg-subtle")}>{o.count}</span>
          )}
        </button>
      ))}
    </div>
  );
}

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("rounded-2xl border border-border bg-surface shadow-card", className)}>{children}</div>;
}

export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="hidden h-5 [@media(hover:hover)]:inline-flex min-w-5 items-center justify-center rounded border border-border bg-surface-2 px-1 font-sans text-[11px] text-fg-subtle">
      {children}
    </kbd>
  );
}
