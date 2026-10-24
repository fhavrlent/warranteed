import { Archive, CircleAlert, CircleCheck, CircleX, Infinity as InfinityIcon } from "lucide-react";
import type { Warranty } from "@/lib/api";
import { daysUntil, relativeDays } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { coverageUsed, statusLabel, type Status } from "@/lib/warranty";

const styles: Record<Status, string> = {
  active: "bg-ok-soft text-ok",
  expiring: "bg-warn-soft text-warn",
  expired: "bg-bad-soft text-bad",
  lifetime: "bg-life-soft text-life",
  archived: "bg-surface-3 text-fg-muted",
};

const icons = {
  active: CircleCheck,
  expiring: CircleAlert,
  expired: CircleX,
  lifetime: InfinityIcon,
  archived: Archive,
};

export function StatusBadge({ status, className }: { status: Status; className?: string }) {
  const Icon = icons[status];
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center gap-1 rounded-full px-2 text-xs font-semibold whitespace-nowrap",
        styles[status],
        className,
      )}
    >
      <Icon className="size-3.5" strokeWidth={2.25} />
      {statusLabel[status]}
    </span>
  );
}

/** "Expires in 3 months" / "Expired 2 days ago" / "Never expires". */
export function expiryText(w: Warranty): string {
  if (w.isLifetime || !w.expiresOn) return "Never expires";
  const d = daysUntil(w.expiresOn);
  if (d < 0) return `Expired ${relativeDays(d)}`;
  if (d === 0) return "Expires today";
  return `Expires ${relativeDays(d)}`;
}

const barColor: Record<Status, string> = {
  active: "bg-ok",
  expiring: "bg-warn",
  expired: "bg-bad",
  lifetime: "bg-life",
  archived: "bg-fg-subtle",
};

export function CoverageBar({ w, status, className }: { w: Warranty; status: Status; className?: string }) {
  const used = coverageUsed(w);
  return (
    <div className={cn("h-1.5 overflow-hidden rounded-full bg-surface-3", className)}>
      <div
        className={cn("h-full rounded-full transition-[width]", barColor[status])}
        style={{ width: `${used === null ? 100 : Math.max(3, Math.round(used * 100))}%`, opacity: used === null ? 0.35 : 1 }}
      />
    </div>
  );
}
