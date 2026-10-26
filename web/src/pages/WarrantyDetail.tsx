import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  Copy,
  ExternalLink,
  Lock,
  MoreHorizontal,
  Pencil,
  Trash2,
  Users,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { AttachmentsPanel } from "@/components/Attachments";
import { CoverageBar, StatusBadge } from "@/components/Status";
import { TagChip } from "@/components/Tags";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/dialog";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Card } from "@/components/ui/misc";
import { WarrantyForm } from "@/components/WarrantyForm";
import { api, errorMessage, thumbUrl } from "@/lib/api";
import { durationLabel } from "@/lib/dates";
import { useInvalidate, useMe, usePrefs, useSharing, useWarranty } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { displayName, photoOf, statusOf, expiryText, toInput } from "@/lib/warranty";

export const WarrantyDetail = () => {
  const { t } = useTranslation();
  const id = Number(useParams().id);
  const { data: w, isLoading, error } = useWarranty(id);
  const { soonDays, fmtDate, fmtMoney } = usePrefs();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const invalidate = useInvalidate();
  const navigate = useNavigate();
  const { sharing, household, personal } = useSharing();
  const { data: me } = useMe();

  if (isLoading) {
    return <div className="h-64 animate-pulse rounded-2xl bg-surface-3/60" />;
  }
  if (error || !w) {
    return (
      <Card className="flex flex-col items-center gap-3 p-12 text-center">
        <p className="font-medium">{t("detail.notFound")}</p>
        <Link to="/" className="text-sm font-medium text-primary hover:underline">
          {t("detail.back")}
        </Link>
      </Card>
    );
  }

  const s = statusOf(w, soonDays);
  const photo = photoOf(w);

  const archive = async () => {
    try {
      await api.archive(w.id, !w.archivedAt);
      invalidate();
      toast.success(w.archivedAt ? t("detail.restored") : t("detail.archived"));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const remove = async () => {
    try {
      await api.deleteWarranty(w.id);
      invalidate();
      toast.success(t("detail.deleted", { name: w.productName }));
      void navigate("/", { replace: true });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  // In a shared space only the person who added it can delete it or make it private.
  const shared = w.spaceKind !== "personal";
  const mine = w.addedBy.id === me?.id;
  const canManage = !shared || mine;

  const moveTo = async (spaceId: number, message: string) => {
    try {
      await api.updateWarranty(w.id, { ...toInput(w), spaceId });
      invalidate();
      toast.success(message);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const coverage = w.isLifetime
    ? t("detail.lifetime")
    : w.warrantyMonths
      ? durationLabel(w.warrantyMonths)
      : t("detail.until", { date: fmtDate(w.warrantyEndDate) });

  return (
    <div className="animate-fade-in">
      <Link to="/" className="mb-5 inline-flex items-center gap-1.5 text-sm text-fg-muted hover:text-fg">
        <ArrowLeft className="size-4" /> {t("detail.backShort")}
      </Link>

      <div className="flex flex-col gap-6 lg:flex-row">
        <div className="flex min-w-0 flex-1 flex-col gap-6">
          <Card className="p-5 sm:p-6">
            <div className="flex items-start gap-4">
              {photo ? (
                <img src={thumbUrl(photo)} alt="" className="size-16 shrink-0 rounded-2xl object-cover sm:size-20" />
              ) : (
                <span className="flex size-16 shrink-0 items-center justify-center rounded-2xl bg-surface-3 text-2xl font-semibold text-fg-muted sm:size-20">
                  {displayName(w)[0]?.toUpperCase()}
                </span>
              )}
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge status={s} />
                  {w.tags.map((t) => (
                    <Link key={t.id} to={`/?tag=${encodeURIComponent(t.name)}&filter=all`}>
                      <TagChip tag={t} />
                    </Link>
                  ))}
                </div>
                <h1 className="mt-2 text-xl font-semibold tracking-tight sm:text-2xl">{displayName(w)}</h1>
                {(w.model || w.vendor) && (
                  <p className="mt-0.5 text-sm text-fg-muted">
                    {[w.model, w.vendor && t("detail.from", { vendor: w.vendor })].filter(Boolean).join(" · ")}
                  </p>
                )}
                {sharing && (
                  <p className="mt-1.5 flex items-center gap-1.5 text-xs text-fg-subtle">
                    {shared ? <Users className="size-3.5" /> : <Lock className="size-3.5" />}
                    {shared ? t("detail.shared") : t("detail.private")}
                    {shared && !mine && ` · ${t("detail.addedBy", { name: w.addedBy.name })}`}
                  </p>
                )}
              </div>
              <div className="flex shrink-0 gap-1.5">
                <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                  <Pencil /> <span className="hidden sm:inline">{t("detail.edit")}</span>
                </Button>
                <Menu>
                  <MenuTrigger asChild>
                    <Button variant="outline" size="icon" className="size-8" aria-label={t("detail.moreActions")}>
                      <MoreHorizontal />
                    </Button>
                  </MenuTrigger>
                  <MenuContent>
                    <MenuItem icon={w.archivedAt ? <ArchiveRestore /> : <Archive />} onSelect={archive}>
                      {w.archivedAt ? t("detail.unarchive") : t("detail.archive")}
                    </MenuItem>
                    {sharing && !shared && household && (
                      <MenuItem icon={<Users />} onSelect={() => void moveTo(household.id, t("detail.shared"))}>
                        {t("detail.share")}
                      </MenuItem>
                    )}
                    {sharing && shared && canManage && personal && (
                      <MenuItem icon={<Lock />} onSelect={() => void moveTo(personal.id, t("detail.madePrivate"))}>
                        {t("detail.makePrivate")}
                      </MenuItem>
                    )}
                    <MenuSeparator />
                    {canManage ? (
                      <MenuItem danger icon={<Trash2 />} onSelect={() => setDeleting(true)}>
                        {t("common.delete")}
                      </MenuItem>
                    ) : (
                      <p className="max-w-56 px-2.5 py-1.5 text-xs text-fg-subtle">
                        {t("detail.onlyOwnerDeletes", { name: w.addedBy.name })}
                      </p>
                    )}
                  </MenuContent>
                </Menu>
              </div>
            </div>

            <div className="mt-6 rounded-xl bg-surface-2 p-4">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <span
                  className={cn(
                    "text-[15px] font-semibold",
                    s === "expiring" && "text-warn",
                    s === "expired" && "text-bad",
                  )}
                >
                  {expiryText(w)}
                </span>
                <span className="text-sm text-fg-muted tabular-nums">
                  {fmtDate(w.purchaseDate)} → {w.isLifetime ? "∞" : fmtDate(w.expiresOn)}
                </span>
              </div>
              <CoverageBar w={w} status={s} className="mt-3 h-2" />
            </div>
          </Card>

          <Card className="p-5 sm:p-6">
            <AttachmentsPanel warranty={w} />
          </Card>
        </div>

        <aside className="flex w-full flex-col gap-6 lg:w-80">
          <Card className="divide-y divide-border">
            <Detail label={t("detail.purchased")}>{fmtDate(w.purchaseDate)}</Detail>
            <Detail label={t("detail.coverage")}>{coverage}</Detail>
            <Detail label={t("detail.expires")}>{w.isLifetime ? t("detail.never") : fmtDate(w.expiresOn)}</Detail>
            {w.price != null && <Detail label={t("detail.price")}>{fmtMoney(w.price, w.currency)}</Detail>}
            {w.vendor && <Detail label={t("detail.boughtFrom")}>{w.vendor}</Detail>}
            {w.orderNumber && (
              <Detail label={t("detail.orderNo")} copy={w.orderNumber}>
                <span className="font-mono text-[13px]">{w.orderNumber}</span>
              </Detail>
            )}
            {w.serialNumbers.map((sn, i) => (
              <Detail
                key={sn}
                label={w.serialNumbers.length > 1 ? t("detail.serialN", { n: i + 1 }) : t("detail.serial")}
                copy={sn}
              >
                <span className="font-mono text-[13px] break-all">{sn}</span>
              </Detail>
            ))}
            {w.productUrl && (
              <Detail label={t("detail.link")}>
                <a
                  href={w.productUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-primary hover:underline"
                >
                  {hostOf(w.productUrl)}
                  <ExternalLink className="size-3.5" />
                </a>
              </Detail>
            )}
          </Card>

          {w.notes && (
            <Card className="p-4">
              <h3 className="mb-2 text-xs font-medium text-fg-subtle">{t("detail.notes")}</h3>
              <p className="text-sm leading-relaxed whitespace-pre-wrap">{w.notes}</p>
            </Card>
          )}

          <p className="px-1 text-xs text-fg-subtle">
            {t("detail.added", { date: fmtDate(w.createdAt.slice(0, 10)) })}
            {w.updatedAt !== w.createdAt && ` · ${t("detail.updated", { date: fmtDate(w.updatedAt.slice(0, 10)) })}`}
          </p>
        </aside>
      </div>

      <WarrantyForm open={editing} onOpenChange={setEditing} warranty={w} />
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={t("detail.deleteTitle", { name: w.productName })}
        description={t("detail.deleteText")}
        onConfirm={remove}
      />
    </div>
  );
};

const hostOf = (url: string): string => {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
};

const Detail = ({ label, children, copy }: { label: string; children: ReactNode; copy?: string }) => {
  const { t } = useTranslation();
  return (
    <div className="group flex items-start justify-between gap-4 px-4 py-3">
      <span className="shrink-0 text-[13px] text-fg-subtle">{label}</span>
      <span className="flex min-w-0 items-center gap-1 text-right text-sm">
        {children}
        {copy && (
          <button
            type="button"
            onClick={() => {
              // The Clipboard API only exists on HTTPS (or localhost) pages.
              if (!window.isSecureContext) {
                toast.error(t("common.copyNeedsHttps"));
                return;
              }
              navigator.clipboard.writeText(copy).then(
                () => toast.success(t("common.copied")),
                () => toast.error(t("common.copyFailed")),
              );
            }}
            className="cursor-pointer rounded p-0.5 text-fg-subtle opacity-0 group-hover:opacity-100 hover:text-fg focus:opacity-100"
            aria-label={t("common.copy", { label })}
          >
            <Copy className="size-3.5" />
          </button>
        )}
      </span>
    </div>
  );
};
