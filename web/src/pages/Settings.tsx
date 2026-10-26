import { useQueryClient } from "@tanstack/react-query";
import {
  Bell,
  CheckCircle2,
  Database,
  Download,
  FileUp,
  Palette,
  Plug,
  Tag as TagIcon,
  Trash2,
  Users,
} from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { toast } from "sonner";
import { TagChip } from "@/components/Tags";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/dialog";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { Card, Segmented, Switch } from "@/components/ui/misc";
import { browserLanguage, LANGUAGES } from "@/i18n";
import { api, errorMessage, translateServer, type ImportResult, type Settings as SettingsT, type Tag } from "@/lib/api";
import { formatDate, todayISO } from "@/lib/dates";
import {
  keys,
  useInvalidate,
  useSaveSettings,
  useSettingsQuery,
  useSharing,
  useTags,
  useWarranties,
} from "@/lib/queries";
import { applyTheme } from "@/lib/theme";
import { CURRENCIES, cn } from "@/lib/utils";
import { tagDot } from "@/lib/warranty";

const TAG_COLORS = [
  "slate",
  "red",
  "orange",
  "amber",
  "lime",
  "emerald",
  "teal",
  "sky",
  "indigo",
  "violet",
  "pink",
  "rose",
] as const;

export const Settings = () => {
  const { t } = useTranslation();
  const { data: settings, isLoading } = useSettingsQuery();
  if (isLoading || !settings) return <div className="h-96 animate-pulse rounded-2xl bg-surface-3/60" />;
  return (
    <div className="animate-fade-in">
      <h1 className="text-2xl font-semibold tracking-tight">{t("settings.title")}</h1>
      <p className="mt-1 mb-8 text-sm text-fg-muted">{t("settings.autosave")}</p>
      <div className="flex flex-col gap-6">
        <Display s={settings} />
        <Notifications s={settings} />
        <Paperless s={settings} />
        <Household />
        <Tags />
        <Data />
      </div>
    </div>
  );
};

const Section = ({
  icon,
  title,
  description,
  children,
  id,
}: {
  icon: ReactNode;
  title: string;
  description: ReactNode;
  children: ReactNode;
  id?: string;
}) => (
  <Card className="grid gap-6 p-5 sm:p-6 md:grid-cols-[16rem_1fr]">
    <div id={id} className="scroll-mt-20">
      <h2 className="flex items-center gap-2 text-sm font-semibold [&_svg]:size-4 [&_svg]:text-fg-subtle">
        {icon}
        {title}
      </h2>
      <p className="mt-1.5 text-[13px] leading-relaxed text-fg-muted">{description}</p>
    </div>
    <div className="flex min-w-0 flex-col gap-5">{children}</div>
  </Card>
);

/** Saves a partial change immediately and reports errors. */
const useAutoSave = (s: SettingsT) => {
  const save = useSaveSettings();
  return async (patch: Partial<SettingsT> & { paperlessToken?: string }) => {
    try {
      await save.mutateAsync({ ...s, ...patch });
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      return false;
    }
  };
};

const Display = ({ s }: { s: SettingsT }) => {
  const { t } = useTranslation();
  const update = useAutoSave(s);
  const [soon, setSoon] = useState(String(s.expiringSoonDays));
  const sample = todayISO();
  return (
    <Section icon={<Palette />} title={t("settings.display.title")} description={t("settings.display.description")}>
      <Field label={t("settings.display.language")} hint={t("settings.display.languageHint")}>
        <Select value={s.language} onChange={(e) => update({ language: e.target.value })} className="sm:w-72">
          <option value="">
            {t("settings.display.languageAuto", { language: LANGUAGES[browserLanguage()]?.name ?? "English" })}
          </option>
          {s.languages
            .filter((code) => code in LANGUAGES)
            .map((code) => (
              <option key={code} value={code}>
                {LANGUAGES[code]?.name ?? code}
              </option>
            ))}
        </Select>
      </Field>
      <Field label={t("settings.display.dateFormat")}>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {s.dateFormats.map((f) => (
            <button
              type="button"
              key={f}
              onClick={() => update({ dateFormat: f })}
              className={cn(
                "flex cursor-pointer flex-col items-start rounded-xl border px-3 py-2 text-left transition-colors",
                s.dateFormat === f
                  ? "border-primary bg-primary-soft/60 ring-1 ring-primary/40"
                  : "border-border hover:border-border-strong",
              )}
            >
              <span className="text-sm font-medium tabular-nums">{formatDate(sample, f)}</span>
              <span className="font-mono text-[11px] text-fg-subtle">{f}</span>
            </button>
          ))}
        </div>
      </Field>
      <div className="grid gap-5 sm:grid-cols-2">
        <Field label={t("settings.display.currency")} hint={t("settings.display.currencyHint")}>
          <Select value={s.currency} onChange={(e) => update({ currency: e.target.value })}>
            {[...new Set([s.currency, ...CURRENCIES])].map((c) => (
              <option key={c}>{c}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("settings.display.soon")} hint={t("settings.display.soonHint")}>
          <div className="flex items-center gap-2">
            <Input
              inputMode="numeric"
              value={soon}
              onChange={(e) => setSoon(e.target.value.replace(/\D/g, ""))}
              onBlur={() => {
                const n = parseInt(soon, 10);
                if (n && n !== s.expiringSoonDays) {
                  void update({ expiringSoonDays: n }).then((ok) => {
                    if (!ok) setSoon(String(s.expiringSoonDays));
                  });
                } else {
                  setSoon(String(s.expiringSoonDays));
                }
              }}
              className="w-24 tabular-nums"
            />
            <span className="text-sm text-fg-muted">{t("settings.display.days")}</span>
          </div>
        </Field>
      </div>
      <Field label={t("settings.display.theme")}>
        <Segmented
          value={s.theme}
          onChange={(theme) => {
            applyTheme(theme);
            void update({ theme });
          }}
          options={[
            { value: "system", label: t("theme.system") },
            { value: "light", label: t("theme.light") },
            { value: "dark", label: t("theme.dark") },
          ]}
        />
      </Field>
    </Section>
  );
};

const Notifications = ({ s }: { s: SettingsT }) => {
  const { t } = useTranslation();
  const update = useAutoSave(s);
  // Unsaved edits; null means "show the saved value".
  const [urlsDraft, setUrlsDraft] = useState<string | null>(null);
  const [daysDraft, setDaysDraft] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  const urls = urlsDraft ?? s.notifyUrls;
  const days = daysDraft ?? s.notifyDays;

  // Saves a draft; on failure it is kept so the user can correct it.
  const saveDraft = (draft: string | null, saved: string, patch: Partial<SettingsT>, clear: () => void) => {
    if (draft === null || draft === saved) {
      clear();
      return;
    }
    void update(patch).then((ok) => {
      if (ok) clear();
    });
  };

  return (
    <Section
      icon={<Bell />}
      title={t("settings.reminders.title")}
      description={
        <Trans
          i18nKey="settings.reminders.description"
          components={{
            link: (
              <a
                className="text-primary hover:underline"
                href="https://shoutrrr.nickfedor.com/latest/services/overview/"
                target="_blank"
                rel="noreferrer"
              />
            ),
          }}
        />
      }
    >
      {!s.isAdmin && (
        <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px] text-fg-muted">
          {t("settings.reminders.builtIn")}{" "}
          {s.notifyAllowedHosts.length > 0
            ? t("settings.reminders.allowedHosts", { hosts: s.notifyAllowedHosts.join(", ") })
            : t("settings.reminders.needsAdmin")}
        </p>
      )}
      <Field label={t("settings.reminders.urls")} hint={t("settings.reminders.urlsHint")}>
        <Textarea
          value={urls}
          onChange={(e) => setUrlsDraft(e.target.value)}
          onBlur={() => saveDraft(urlsDraft, s.notifyUrls, { notifyUrls: urls }, () => setUrlsDraft(null))}
          placeholder="ntfy://ntfy.sh/my-warranties"
          className="font-mono text-[13px]"
          spellCheck={false}
        />
      </Field>
      <div className="flex flex-wrap items-end gap-3">
        <Field label={t("settings.reminders.days")} hint={t("settings.reminders.daysHint")} className="w-56">
          <Input
            value={days}
            onChange={(e) => setDaysDraft(e.target.value)}
            onBlur={() => saveDraft(daysDraft, s.notifyDays, { notifyDays: days }, () => setDaysDraft(null))}
          />
        </Field>
        <Button
          variant="outline"
          className="mb-5"
          loading={testing}
          disabled={!urls.trim()}
          onClick={async () => {
            setTesting(true);
            try {
              await api.testNotification(urls);
              toast.success(t("settings.reminders.testSent"));
            } catch (e) {
              toast.error(errorMessage(e));
            } finally {
              setTesting(false);
            }
          }}
        >
          {t("settings.reminders.test")}
        </Button>
      </div>
      <p className="-mt-2 text-xs text-fg-subtle">{t("settings.reminders.schedule", { timezone: s.timezone })}</p>
    </Section>
  );
};

const Paperless = ({ s }: { s: SettingsT }) => {
  const { t } = useTranslation();
  const update = useAutoSave(s);
  const [url, setUrl] = useState(s.paperlessUrl);
  const [publicUrl, setPublicUrl] = useState(s.paperlessPublicUrl);
  const [token, setToken] = useState("");
  const [testing, setTesting] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  const test = async () => {
    setTesting(true);
    setStatus(null);
    try {
      const info = await api.testPaperless(url, token || undefined);
      setStatus(
        t("settings.paperless.connected", { version: info.version, api: info.apiVersion, count: info.documents }),
      );
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setTesting(false);
    }
  };

  const saveConnection = async () => {
    const ok = await update({
      paperlessUrl: url,
      paperlessPublicUrl: publicUrl,
      ...(token ? { paperlessToken: token } : {}),
    });
    if (ok) {
      setToken("");
      toast.success(t("settings.paperless.saved"));
    }
  };

  return (
    <Section icon={<Plug />} title="Paperless-ngx" description={t("settings.paperless.description")}>
      {!s.canSetPaperlessUrl && !s.paperlessServerUrl ? (
        <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px] text-fg-muted">
          <Trans i18nKey="settings.paperless.notSetUp" components={{ code: <code /> }} />
        </p>
      ) : (
        <>
          {s.paperlessServerToken && (
            <p className="rounded-lg bg-primary-soft/60 px-3 py-2 text-[13px] text-fg">
              {t("settings.paperless.adminServer")}
            </p>
          )}
          {!s.canSetPaperlessUrl && (
            <p className="rounded-lg bg-primary-soft/60 px-3 py-2 text-[13px] text-fg">
              {t("settings.paperless.ownToken")}
            </p>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            {s.canSetPaperlessUrl ? (
              <Field
                label={t("settings.paperless.url")}
                hint={s.paperlessServerUrl ? t("settings.paperless.urlHint", { url: s.paperlessServerUrl }) : undefined}
              >
                <Input
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder={s.paperlessServerUrl || "https://paperless.example.com"}
                />
              </Field>
            ) : (
              <Field label={t("settings.paperless.server")}>
                <p className="flex h-9 items-center truncate text-sm text-fg-muted">{s.paperlessServerUrl}</p>
              </Field>
            )}
            <Field
              label={t("settings.paperless.token")}
              hint={s.hasPaperlessToken ? t("settings.paperless.tokenSaved") : t("settings.paperless.tokenHint")}
            >
              <Input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder={s.hasPaperlessToken ? "••••••••••••" : ""}
                autoComplete="off"
              />
            </Field>
            {s.canSetPaperlessUrl && (
              <Field
                label={t("settings.paperless.linkAddress")}
                optional
                hint={t("settings.paperless.linkAddressHint")}
                className="sm:col-span-2"
              >
                <Input
                  value={publicUrl}
                  onChange={(e) => setPublicUrl(e.target.value)}
                  placeholder={url || s.paperlessServerUrl || "https://paperless.example.com"}
                />
              </Field>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" onClick={test} loading={testing}>
              {t("settings.paperless.test")}
            </Button>
            <Button
              onClick={saveConnection}
              disabled={url === s.paperlessUrl && publicUrl === s.paperlessPublicUrl && !token}
            >
              {t("common.save")}
            </Button>
            {(s.paperlessUrl || s.hasPaperlessToken) && (
              <Button
                variant="ghost"
                onClick={async () => {
                  if (await update({ paperlessUrl: "", paperlessPublicUrl: "", paperlessToken: "" })) {
                    setUrl("");
                    setPublicUrl("");
                    setStatus(null);
                  }
                }}
              >
                {t("settings.paperless.disconnect")}
              </Button>
            )}
          </div>
          {status && (
            <p className="flex items-center gap-2 text-[13px] text-ok">
              <CheckCircle2 className="size-4" />
              {status}
            </p>
          )}
        </>
      )}
      <label
        className={cn(
          "flex items-center justify-between gap-4 rounded-xl border border-border px-4 py-3",
          !s.paperlessEnabled && "opacity-50",
        )}
      >
        <span>
          <span className="block text-sm font-medium">{t("settings.paperless.default")}</span>
          <span className="block text-xs text-fg-subtle">{t("settings.paperless.defaultHint")}</span>
        </span>
        <Switch
          checked={s.paperlessDefault}
          disabled={!s.paperlessEnabled}
          onCheckedChange={(v) => update({ paperlessDefault: v })}
        />
      </label>
    </Section>
  );
};

const Tags = () => {
  const { t } = useTranslation();
  const { data: tags = [] } = useTags();
  const invalidate = useInvalidate();
  const [deleting, setDeleting] = useState<Tag | null>(null);
  return (
    <Section icon={<TagIcon />} title={t("settings.tags.title")} description={t("settings.tags.description")}>
      {tags.length === 0 ? (
        <p className="text-sm text-fg-subtle">{t("settings.tags.none")}</p>
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border">
          {tags.map((tag) => (
            <TagRow key={tag.id} tag={tag} onChanged={invalidate} onDelete={() => setDeleting(tag)} />
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t("settings.tags.deleteTitle", { name: deleting?.name ?? "" })}
        description={t("settings.tags.deleteText", { count: deleting?.count ?? 0 })}
        onConfirm={async () => {
          if (deleting) await api.deleteTag(deleting.id).catch((e: unknown) => toast.error(errorMessage(e)));
          invalidate();
          setDeleting(null);
        }}
      />
    </Section>
  );
};

const TagRow = ({ tag, onChanged, onDelete }: { tag: Tag; onChanged: () => void; onDelete: () => void }) => {
  const { t } = useTranslation();
  const [nameDraft, setNameDraft] = useState<string | null>(null);
  const name = nameDraft ?? tag.name;
  const save = async (n: string, color: string) => {
    try {
      await api.updateTag(tag.id, n, color);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setNameDraft(null);
    }
  };
  return (
    <li className="flex flex-wrap items-center gap-3 px-3 py-2">
      <TagChip tag={{ name, color: tag.color }} className="hidden sm:inline-flex" />
      <Input
        value={name}
        onChange={(e) => setNameDraft(e.target.value)}
        onBlur={() => {
          if (name.trim() && name !== tag.name) void save(name, tag.color);
          else setNameDraft(null);
        }}
        className="h-8 w-40"
        aria-label={t("settings.tags.name")}
      />
      <div className="flex flex-wrap gap-1">
        {TAG_COLORS.map((c) => (
          <button
            type="button"
            key={c}
            onClick={() => save(tag.name, c)}
            aria-label={t(`settings.tags.colors.${c}`)}
            className={cn(
              "size-5 cursor-pointer rounded-full ring-offset-2 ring-offset-surface",
              tagDot[c],
              tag.color === c && "ring-2 ring-fg",
            )}
          />
        ))}
      </div>
      <span className="ml-auto text-xs text-fg-subtle tabular-nums">
        {t("settings.tags.used", { count: tag.count ?? 0 })}
      </span>
      <Button variant="ghost" size="icon-sm" aria-label={t("common.remove", { name: tag.name })} onClick={onDelete}>
        <Trash2 />
      </Button>
    </li>
  );
};

/** Moves private warranties into the household in one go. Hidden until someone else joins. */
const Household = () => {
  const { t } = useTranslation();
  const { sharing, household } = useSharing();
  const { data: list = [] } = useWarranties();
  const invalidate = useInvalidate();
  const [confirming, setConfirming] = useState(false);
  const [moving, setMoving] = useState(false);
  const privateCount = list.filter((w) => w.spaceKind === "personal").length;
  if (!sharing || !household) return null;

  const moveAll = async () => {
    setMoving(true);
    try {
      const { moved } = await api.moveAllToSpace(household.id);
      invalidate();
      toast.success(t("settings.household.shared", { count: moved }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setMoving(false);
      setConfirming(false);
    }
  };

  return (
    <Section
      icon={<Users />}
      title={t("common.household")}
      description={t("settings.household.description", { count: household.members })}
    >
      {privateCount === 0 ? (
        <p className="text-sm text-fg-muted">{t("settings.household.allShared")}</p>
      ) : (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border px-4 py-3">
          <span className="text-sm">{t("settings.household.private", { count: privateCount })}</span>
          <Button variant="outline" onClick={() => setConfirming(true)}>
            {t("settings.household.shareAll")}
          </Button>
        </div>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t("settings.household.confirmTitle")}
        description={t("settings.household.confirmText", { count: privateCount })}
        confirmLabel={t("settings.household.confirm")}
        loading={moving}
        onConfirm={() => void moveAll()}
      />
    </Section>
  );
};

const Data = () => {
  const { t } = useTranslation();
  const fileRef = useRef<HTMLInputElement>(null);
  const [importing, setImporting] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);
  const qc = useQueryClient();

  const onImport = async (file: File | undefined) => {
    if (!file) return;
    setImporting(true);
    setResult(null);
    try {
      const r = await api.importCsv(file);
      setResult(r);
      void qc.invalidateQueries({ queryKey: keys.warranties });
      void qc.invalidateQueries({ queryKey: keys.tags });
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setImporting(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  return (
    <Section
      id="data"
      icon={<Database />}
      title={t("settings.data.title")}
      description={t("settings.data.description")}
    >
      <div className="flex flex-wrap gap-2">
        <a href="/api/export.csv">
          <span className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3.5 text-sm font-medium shadow-card hover:bg-surface-2">
            <Download className="size-4" /> {t("settings.data.export")}
          </span>
        </a>
        <Button variant="outline" onClick={() => fileRef.current?.click()} loading={importing}>
          {!importing && <FileUp />} {t("settings.data.import")}
        </Button>
        <input
          ref={fileRef}
          type="file"
          accept=".csv,text/csv"
          hidden
          onChange={(e) => onImport(e.target.files?.[0])}
        />
      </div>
      {result && (
        <div className="rounded-xl border border-border bg-surface-2 p-4 text-sm">
          <p className="font-medium">
            {t("settings.data.result", { imported: result.imported, skipped: result.skipped })}
            {result.errors.length > 0 && (
              <span className="text-bad"> · {t("settings.data.problems", { count: result.errors.length })}</span>
            )}
          </p>
          {result.errors.length > 0 && (
            <ul className="mt-2 max-h-48 overflow-y-auto text-[13px] text-fg-muted">
              {result.errors.map((e) => (
                <li key={e.row}>
                  {t("settings.data.row", { row: e.row, message: translateServer(e.message, e.code, e.params) })}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Section>
  );
};
