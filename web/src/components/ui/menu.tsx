import * as M from "@radix-ui/react-dropdown-menu";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export const Menu = M.Root;
export const MenuTrigger = M.Trigger;

export const MenuContent = ({
  children,
  align = "end",
  className,
}: {
  children: ReactNode;
  align?: "start" | "end";
  className?: string;
}) => (
  <M.Portal>
    <M.Content
      align={align}
      sideOffset={6}
      className={cn(
        "animate-slide-up z-50 min-w-44 rounded-xl border border-border bg-surface p-1 shadow-pop",
        className,
      )}
    >
      {children}
    </M.Content>
  </M.Portal>
);

export const MenuItem = ({
  children,
  onSelect,
  danger,
  icon,
}: {
  children: ReactNode;
  onSelect?: () => void;
  danger?: boolean;
  icon?: ReactNode;
}) => (
  <M.Item
    onSelect={onSelect}
    className={cn(
      "flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-sm outline-none select-none [&_svg]:size-4",
      danger ? "text-bad data-highlighted:bg-bad-soft" : "text-fg data-highlighted:bg-surface-3",
    )}
  >
    {icon !== undefined && <span className={danger ? "text-bad" : "text-fg-muted"}>{icon}</span>}
    {children}
  </M.Item>
);

export const MenuLabel = ({ children }: { children: ReactNode }) => (
  <M.Label className="px-2.5 pt-1.5 pb-1 text-xs text-fg-subtle">{children}</M.Label>
);

export const MenuSeparator = () => <M.Separator className="my-1 h-px bg-border" />;
