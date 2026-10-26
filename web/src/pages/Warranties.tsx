import { ArrowDownUp, LayoutGrid, List, Lock, Paperclip, Plus, Search, ShieldCheck, Users, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router";
import { CoverageBar, StatusBadge } from "@/components/Status";
import { TagChip } from "@/components/Tags";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/input";
import { Card, Kbd, Segmented } from "@/components/ui/misc";
import { WarrantyForm } from "@/components/WarrantyForm";
import { errorMessage, thumbUrl, type Tag, type Warranty } from "@/lib/api";
import { usePrefs, useSharing, useTags, useWarranties } from "@/lib/queries";
import { matcher } from "@/lib/search";
import { cn } from "@/lib/utils";
import { displayName, photoOf, statusOf, tagDot, type Status, expiryText } from "@/lib/warranty";

type Filter = "current" | "expiring" | "expired" | "lifetime" | "archived" | "all";
const FILTERS: readonly Filter[] = ["current", "expiring", "expired", "lifetime", "archived", "all"];
type Who = "all" | "shared" | "mine";
const WHO: readonly Who[] = ["all", "shared", "mine"];
type Sort = "expiry" | "name" | "purchase" | "price" | "added";

// Placeholder cards while loading.
const SKELETONS = ["s1", "s2", "s3", "s4", "s5", "s6"];

const usePersisted = <T extends string>(key: string, def: T): [T, (v: T) => void] => {
  const [v, setV] = useState<T>(() => {
    try {
      return (localStorage.getItem(key) as T) || def;
    } catch {
      return def;
    }
  });
  return [
    v,
    (nv: T) => {
      setV(nv);
      try {
        localStorage.setItem(key, nv);
      } catch {
        /* ignore */
      }
    },
  ];
};

export const Warranties = () => {
  const { t } = useTranslation();
  const { data: list, isLoading, error } = useWarranties();
  const { data: tags = [] } = useTags();
  const { soonDays, fmtDate, fmtMoney, currency } = usePrefs();
  const [params, setParams] = useSearchParams();
  const [q, setQ] = useState(params.get("q") ?? "");
  const rawFilter = params.get("filter");
  const filter: Filter = FILTERS.find((f) => f === rawFilter) ?? "current";
  const tag = params.get("tag");
  const { sharing } = useSharing();
  const rawWho = params.get("who");
  const who: Who = sharing ? (WHO.find((x) => x === rawWho) ?? "all") : "all";
  // Tags with the same name in different spaces filter together.
  const tagChips = useMemo(() => {
    const byName = new Map<string, Tag>();
    for (const tg of tags) {
      const key = tg.name.toLowerCase();
      const seen = byName.get(key);
      byName.set(key, seen ? { ...seen, count: (seen.count ?? 0) + (tg.count ?? 0) } : tg);
    }
    return [...byName.values()];
  }, [tags]);
  const [sort, setSort] = usePersisted<Sort>("warranteed-sort", "expiry");
  const [view, setView] = usePersisted<"grid" | "list">("warranteed-view", "grid");
  const [adding, setAdding] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  const setParam = (k: string, v: string | null) => {
    const next = new URLSearchParams(params);
    if (v) next.set(k, v);
    else next.delete(k);
    setParams(next, { replace: true });
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = e.target instanceof Element && e.target.closest("input, textarea, select, [contenteditable]");
      if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "/") {
        e.preventDefault();
        searchRef.current?.focus();
      } else if (e.key === "n") {
        e.preventDefault();
        setAdding(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const withStatus = useMemo(() => (list ?? []).map((w) => ({ w, s: statusOf(w, soonDays) })), [list, soonDays]);

  const counts = useMemo(() => {
    const c: Record<Status, number> = { active: 0, expiring: 0, expired: 0, lifetime: 0, archived: 0 };
    for (const { s } of withStatus) c[s]++;
    return c;
  }, [withStatus]);

  const coveredValue = useMemo(
    () =>
      withStatus
        .filter(
          ({ w, s }) =>
            (s === "active" || s === "expiring" || s === "lifetime") &&
            w.price &&
            (!w.currency || w.currency === currency),
        )
        .reduce((sum, { w }) => sum + (w.price ?? 0), 0),
    [withStatus, currency],
  );

  const shown = useMemo(() => {
    const match = matcher(q);
    const scores = new Map<number, number>();
    const res = withStatus.filter(({ w, s }) => {
      switch (filter) {
        case "current":
          if (s === "archived" || s === "expired") return false;
          break;
        case "all":
          break;
        default:
          if (s !== filter) return false;
      }
      if (tag && !w.tags.some((tg) => tg.name.toLowerCase() === tag.toLowerCase())) return false;
      if (who === "mine" && w.spaceKind !== "personal") return false;
      if (who === "shared" && w.spaceKind === "personal") return false;
      const score = match.score(
        [
          w.productName,
          w.brand,
          w.model,
          w.vendor,
          w.orderNumber,
          w.notes,
          ...w.serialNumbers,
          ...w.tags.map((tg) => tg.name),
        ].join("\u0000"),
      );
      scores.set(w.id, score);
      return score > 0;
    });
    const byName = (a: Warranty, b: Warranty) => displayName(a).localeCompare(displayName(b));
    res.sort((A, B) => {
      const a = A.w,
        b = B.w;
      // When searching, exact matches come before ones found despite typos.
      const byScore = (scores.get(b.id) ?? 0) - (scores.get(a.id) ?? 0);
      if (byScore !== 0) return byScore;
      switch (sort) {
        case "name":
          return byName(a, b);
        case "purchase":
          return b.purchaseDate.localeCompare(a.purchaseDate) || byName(a, b);
        case "price":
          return (b.price ?? -1) - (a.price ?? -1) || byName(a, b);
        case "added":
          return b.createdAt.localeCompare(a.createdAt);
        default:
          if (!a.expiresOn !== !b.expiresOn) return a.expiresOn ? -1 : 1;
          return (a.expiresOn ?? "").localeCompare(b.expiresOn ?? "") || byName(a, b);
      }
    });
    return res;
  }, [withStatus, filter, tag, q, sort, who]);

  const total = list?.length ?? 0;

  return (
    <>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t("list.title")}</h1>
          <p className="mt-1 text-sm text-fg-muted">
            {total === 0
              ? t("list.empty")
              : t("list.summary", { covered: counts.active + counts.expiring + counts.lifetime, total })}
          </p>
        </div>
        <Button onClick={() => setAdding(true)} size="md">
          <Plus /> {t("list.add")}
          <Kbd>N</Kbd>
        </Button>
      </div>

      {total > 0 && (
        <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Stat
            label={t("list.statCovered")}
            value={counts.active + counts.lifetime + counts.expiring}
            onClick={() => setParam("filter", null)}
            active={filter === "current"}
            tone="ok"
          />
          <Stat
            label={t("list.statExpiring", { count: soonDays })}
            value={counts.expiring}
            onClick={() => setParam("filter", "expiring")}
            active={filter === "expiring"}
            tone="warn"
          />
          <Stat
            label={t("list.statExpired")}
            value={counts.expired}
            onClick={() => setParam("filter", "expired")}
            active={filter === "expired"}
            tone="bad"
          />
          <Stat label={t("list.statValue")} value={fmtMoney(coveredValue)} small />
        </div>
      )}

      {total > 0 && (
        <div className="mb-5 flex flex-col gap-3 lg:flex-row lg:items-center">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-subtle" />
            <input
              ref={searchRef}
              value={q}
              onChange={(e) => {
                setQ(e.target.value);
                setParam("q", e.target.value || null);
              }}
              onKeyDown={(e) => {
                if (e.key !== "Escape") return;
                setQ("");
                setParam("q", null);
                e.currentTarget.blur();
              }}
              placeholder={t("list.search")}
              className="h-10 w-full rounded-xl border border-border bg-surface pr-16 pl-9 text-sm shadow-card outline-none placeholder:text-fg-subtle focus:border-primary focus:ring-3 focus:ring-ring/40"
            />
            <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2">
              {q ? null : <Kbd>/</Kbd>}
            </span>
            {q && (
              <button
                type="button"
                onClick={() => {
                  setQ("");
                  setParam("q", null);
                }}
                className="absolute top-1/2 right-2 -translate-y-1/2 cursor-pointer rounded-md p-1 text-fg-subtle hover:text-fg"
                aria-label={t("list.clearSearch")}
              >
                <X className="size-4" />
              </button>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {sharing && (
              <Segmented
                value={who}
                onChange={(v) => setParam("who", v === "all" ? null : v)}
                options={[
                  { value: "all", label: t("list.everyone") },
                  {
                    value: "shared",
                    label: (
                      <>
                        <Users aria-hidden /> {t("common.household")}
                      </>
                    ),
                  },
                  {
                    value: "mine",
                    label: (
                      <>
                        <Lock aria-hidden /> {t("common.onlyMe")}
                      </>
                    ),
                  },
                ]}
              />
            )}
            <Segmented
              value={filter}
              onChange={(v) => setParam("filter", v === "current" ? null : v)}
              options={[
                { value: "current", label: t("list.filterCurrent") },
                { value: "expired", label: t("list.filterExpired"), count: counts.expired || undefined },
                { value: "archived", label: t("list.filterArchived"), count: counts.archived || undefined },
                { value: "all", label: t("list.filterAll") },
              ]}
            />
            <div className="relative">
              <ArrowDownUp className="pointer-events-none absolute top-1/2 left-2.5 z-10 size-3.5 -translate-y-1/2 text-fg-subtle" />
              <Select
                aria-label={t("list.sort")}
                value={sort}
                onChange={(e) => setSort(e.target.value as Sort)}
                className="h-9 w-auto rounded-lg pl-8 text-[13px]"
              >
                <option value="expiry">{t("list.sortExpiry")}</option>
                <option value="name">{t("list.sortName")}</option>
                <option value="purchase">{t("list.sortPurchase")}</option>
                <option value="price">{t("list.sortPrice")}</option>
                <option value="added">{t("list.sortAdded")}</option>
              </Select>
            </div>
            <Segmented
              value={view}
              onChange={setView}
              options={[
                { value: "grid", label: <LayoutGrid aria-label={t("list.gridView")} /> },
                { value: "list", label: <List aria-label={t("list.listView")} /> },
              ]}
            />
          </div>
        </div>
      )}

      {tagChips.length > 0 && total > 0 && (
        <div className="-mt-1 mb-5 flex flex-wrap gap-1.5">
          {tagChips.map((tg) => (
            <button
              type="button"
              key={tg.id}
              onClick={() => setParam("tag", tag === tg.name ? null : tg.name)}
              className={cn(
                "inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-full border px-2.5 text-xs font-medium transition-colors",
                tag === tg.name
                  ? "border-primary bg-primary-soft text-primary"
                  : "border-border bg-surface text-fg-muted hover:text-fg",
              )}
            >
              <span className={cn("size-2 rounded-full", tagDot[tg.color])} />
              {tg.name}
              <span className="text-fg-subtle tabular-nums">{tg.count}</span>
            </button>
          ))}
        </div>
      )}

      {isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {SKELETONS.map((key) => (
            <div key={key} className="h-44 animate-pulse rounded-2xl bg-surface-3/60" />
          ))}
        </div>
      ) : error ? (
        <Card className="p-8 text-center text-sm text-bad">{errorMessage(error)}</Card>
      ) : total === 0 ? (
        <EmptyState onAdd={() => setAdding(true)} />
      ) : shown.length === 0 ? (
        <Card className="flex flex-col items-center gap-2 p-12 text-center">
          <p className="font-medium">{t("list.noMatches")}</p>
          <p className="text-sm text-fg-muted">{t("list.noMatchesHint")}</p>
          <Button
            variant="outline"
            size="sm"
            className="mt-2"
            onClick={() => {
              setQ("");
              setParams({}, { replace: true });
            }}
          >
            {t("list.clearFilters")}
          </Button>
        </Card>
      ) : view === "grid" ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map(({ w, s }) => (
            <WarrantyCard key={w.id} w={w} s={s} fmtDate={fmtDate} />
          ))}
        </div>
      ) : (
        <Card className="overflow-hidden">
          <div className="hidden grid-cols-[minmax(0,2.2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.1fr)_auto] gap-4 border-b border-border bg-surface-2 px-4 py-2 text-xs font-medium text-fg-subtle md:grid">
            <span>{t("list.colProduct")}</span>
            <span>{t("list.colPurchased")}</span>
            <span>{t("list.colExpires")}</span>
            <span>{t("list.colTags")}</span>
            <span className="w-28 text-right">{t("list.colStatus")}</span>
          </div>
          {shown.map(({ w, s }) => (
            <Link
              key={w.id}
              to={`/w/${w.id}`}
              className="grid grid-cols-[1fr_auto] items-center gap-x-4 gap-y-1 border-b border-border px-4 py-3 last:border-0 hover:bg-surface-2 md:grid-cols-[minmax(0,2.2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.1fr)_auto]"
            >
              <div className="flex min-w-0 items-center gap-3">
                <ProductThumb w={w} className="size-9 rounded-lg" />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{displayName(w)}</p>
                  <p className="truncate text-xs text-fg-subtle">
                    {[w.model, w.vendor].filter(Boolean).join(" · ") || " "}
                  </p>
                </div>
              </div>
              <span className="hidden text-sm text-fg-muted tabular-nums md:block">{fmtDate(w.purchaseDate)}</span>
              <span className="hidden text-sm tabular-nums md:block">{w.isLifetime ? "—" : fmtDate(w.expiresOn)}</span>
              <span className="hidden min-w-0 gap-1 overflow-hidden md:flex">
                {w.tags.slice(0, 2).map((tg) => (
                  <TagChip key={tg.id} tag={tg} />
                ))}
                {w.tags.length > 2 && <span className="text-xs text-fg-subtle">+{w.tags.length - 2}</span>}
              </span>
              <span className="flex w-28 justify-end">
                <StatusBadge status={s} />
              </span>
            </Link>
          ))}
        </Card>
      )}

      <WarrantyForm open={adding} onOpenChange={setAdding} />
    </>
  );
};

const Stat = ({
  label,
  value,
  onClick,
  active,
  tone,
  small,
}: {
  label: string;
  value: number | string;
  onClick?: () => void;
  active?: boolean;
  tone?: "ok" | "warn" | "bad";
  small?: boolean;
}) => {
  const dot = { ok: "bg-ok", warn: "bg-warn", bad: "bg-bad" };
  const Comp = onClick ? "button" : "div";
  return (
    <Comp
      onClick={onClick}
      className={cn(
        "flex flex-col items-start rounded-2xl border bg-surface px-4 py-3.5 text-left shadow-card transition-colors",
        onClick && "cursor-pointer hover:border-border-strong",
        active ? "border-primary/60 ring-1 ring-primary/30" : "border-border",
      )}
    >
      <span className="flex items-center gap-1.5 text-xs font-medium text-fg-muted">
        {tone && <span className={cn("size-1.5 rounded-full", dot[tone])} />}
        {label}
      </span>
      <span className={cn("mt-1 font-semibold tracking-tight tabular-nums", small ? "text-xl" : "text-2xl")}>
        {value}
      </span>
    </Comp>
  );
};

const ProductThumb = ({ w, className }: { w: Warranty; className?: string }) => {
  const photo = photoOf(w);
  const [failed, setFailed] = useState(false);
  if (photo && !failed) {
    return (
      <img
        src={thumbUrl(photo)}
        alt=""
        loading="lazy"
        onError={() => setFailed(true)}
        className={cn("shrink-0 object-cover", className)}
      />
    );
  }
  const letter = displayName(w).trim()[0]?.toUpperCase() ?? "?";
  return (
    <span
      className={cn("flex shrink-0 items-center justify-center bg-surface-3 font-semibold text-fg-muted", className)}
    >
      {letter}
    </span>
  );
};

const WarrantyCard = ({ w, s, fmtDate }: { w: Warranty; s: Status; fmtDate: (d: string | null) => string }) => {
  const { t } = useTranslation();
  const docs = w.attachments.filter((a) => a.kind !== "photo").length;
  const { sharing } = useSharing();
  return (
    <Link
      to={`/w/${w.id}`}
      className={cn(
        "group flex min-w-0 flex-col rounded-2xl border border-border bg-surface p-4 shadow-card transition-all hover:-translate-y-px hover:border-border-strong hover:shadow-pop",
        s === "archived" && "opacity-70",
      )}
    >
      <div className="flex items-start gap-3">
        <ProductThumb w={w} className="size-11 rounded-xl text-base" />
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-1.5 truncate font-medium">
            <span className="truncate">{displayName(w)}</span>
            {sharing && w.spaceKind === "personal" && (
              <Lock className="size-3.5 shrink-0 text-fg-subtle" aria-label={t("list.private")} />
            )}
          </p>
          <p className="truncate text-[13px] text-fg-subtle">
            {[w.model, w.vendor].filter(Boolean).join(" · ") || " "}
          </p>
        </div>
        <StatusBadge status={s} />
      </div>
      <div className="mt-4">
        <div className="mb-1.5 flex items-baseline justify-between gap-2 text-[13px]">
          <span className={cn("font-medium", s === "expiring" && "text-warn", s === "expired" && "text-bad")}>
            {expiryText(w)}
          </span>
          <span className="text-fg-subtle tabular-nums">{w.isLifetime ? "" : fmtDate(w.expiresOn)}</span>
        </div>
        <CoverageBar w={w} status={s} />
      </div>
      <div className="mt-3.5 flex min-h-5.5 items-center gap-1.5">
        {w.tags.slice(0, 3).map((tg) => (
          <TagChip key={tg.id} tag={tg} />
        ))}
        {w.tags.length > 3 && <span className="text-xs text-fg-subtle">+{w.tags.length - 3}</span>}
        {docs > 0 && (
          <span className="ml-auto inline-flex items-center gap-1 text-xs text-fg-subtle">
            <Paperclip className="size-3.5" />
            {docs}
          </span>
        )}
      </div>
    </Link>
  );
};

const EmptyState = ({ onAdd }: { onAdd: () => void }) => {
  const { t } = useTranslation();
  return (
    <Card className="flex flex-col items-center px-6 py-16 text-center">
      <span className="flex size-14 items-center justify-center rounded-2xl bg-primary-soft text-primary">
        <ShieldCheck className="size-7" />
      </span>
      <h2 className="mt-5 text-lg font-semibold">{t("list.emptyTitle")}</h2>
      <p className="mt-1.5 max-w-sm text-sm text-fg-muted">{t("list.emptyText")}</p>
      <div className="mt-6 flex gap-2">
        <Button onClick={onAdd}>
          <Plus /> {t("list.add")}
        </Button>
        <Link
          to="/settings#data"
          className="inline-flex h-9 items-center rounded-lg border border-border bg-surface px-3.5 text-sm font-medium shadow-card hover:bg-surface-2"
        >
          {t("list.importCsv")}
        </Link>
      </div>
    </Card>
  );
};
