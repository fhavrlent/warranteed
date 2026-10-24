import { cn } from "@/lib/utils";

export const LogoMark = ({ className }: { className?: string }) => (
  <svg viewBox="0 0 32 32" className={cn("size-7", className)} aria-hidden>
    <rect width="32" height="32" rx="8" className="fill-primary" />
    <path
      d="M16 6.5 9 9.2v6.1c0 4.5 3 8.4 7 9.7 4-1.3 7-5.2 7-9.7V9.2l-7-2.7Z"
      fill="none"
      className="stroke-primary-fg"
      strokeWidth="2"
      strokeLinejoin="round"
    />
    <path
      d="m12.8 15.8 2.3 2.3 4.3-4.6"
      fill="none"
      className="stroke-primary-fg"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

export const Logo = ({ className }: { className?: string }) => (
  <span className={cn("inline-flex items-center gap-2", className)}>
    <LogoMark />
    <span className="text-[15px] font-semibold tracking-tight">Warranteed</span>
  </span>
);
