import type { ComponentProps } from "react";
import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

type Variant = "primary" | "secondary" | "ghost" | "danger" | "outline";
type Size = "sm" | "md" | "lg" | "icon" | "icon-sm";

const variants: Record<Variant, string> = {
  primary: "bg-primary text-primary-fg hover:bg-primary-hover shadow-card",
  secondary: "bg-surface-3 text-fg hover:bg-border",
  outline: "border border-border bg-surface text-fg hover:bg-surface-2 shadow-card",
  ghost: "text-fg-muted hover:bg-surface-3 hover:text-fg",
  danger: "bg-bad text-white hover:opacity-90 shadow-card",
};

const sizes: Record<Size, string> = {
  sm: "h-8 px-3 text-[13px] gap-1.5 rounded-lg",
  md: "h-9 px-3.5 text-sm gap-2 rounded-lg",
  lg: "h-11 px-5 text-[15px] gap-2 rounded-xl",
  icon: "size-9 rounded-lg",
  "icon-sm": "size-7 rounded-md",
};

export type ButtonProps = ComponentProps<"button"> & {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
};

export const Button = ({
  className,
  variant = "primary",
  size = "md",
  loading = false,
  disabled = false,
  children,
  type = "button",
  ...props
}: ButtonProps) => (
  <button
    type={type}
    disabled={disabled || loading}
    className={cn(
      "inline-flex shrink-0 cursor-pointer items-center justify-center font-medium whitespace-nowrap transition-colors select-none",
      "disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
      variants[variant],
      sizes[size],
      className,
    )}
    {...props}
  >
    {loading && <Loader2 className="animate-spin" />}
    {children}
  </button>
);
