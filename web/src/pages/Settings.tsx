import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bell,
  Boxes,
  CalendarDays,
  CheckCircle2,
  Copy,
  Database,
  Download,
  FileUp,
  MapPin,
  Palette,
  Plug,
  Tag as TagIcon,
  Trash2,
  Users,
} from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { toast } from "sonner";
import { TagChip } from "@/components/Tags";
import { Button } from "@/components/ui/button";
import { ConfirmDialog, Dialog } from "@/components/ui/dialog";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { Card, Segmented, Switch } from "@/components/ui/misc";
import { browserLanguage, LANGUAGES } from "@/i18n";
import {
  api,
  errorMessage,
  translateServer,
  type DraftSource,
  type ImportResult,
  type Settings as SettingsT,
  type Tag,
} from "@/lib/api";
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
        <Calendar />
        <Paperless s={settings} />
        <HomeBox s={settings} />
        {settings.wherehouseServerUrl && <Wherehouse s={settings} />}
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
  return async (patch: Partial<SettingsT> & { paperlessToken?: string; homeboxToken?: string }) => {
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

  const [syncing, setSyncing] = useState(false);

  // Turning links on adds them to documents already attached to warranties.
  const setBacklinks = async (on: boolean) => {
    if (!(await update({ paperlessBacklinks: on })) || !on) return;
    setSyncing(true);
    try {
      const res = await api.syncPaperlessLinks();
      if (res.failed > 0) {
        const why = res.error ? translateServer(res.error.error, res.error.code, res.error.params) : "";
        toast.error(t("settings.paperless.backlinksFailed", { count: res.documents, failed: res.failed }), {
          description: why,
        });
      } else {
        toast.success(t("settings.paperless.backlinksDone", { count: res.documents }));
      }
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSyncing(false);
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
      <label
        className={cn(
          "flex items-center justify-between gap-4 rounded-xl border border-border px-4 py-3",
          !s.paperlessEnabled && "opacity-50",
        )}
      >
        <span>
          <span className="block text-sm font-medium">{t("settings.paperless.backlinks")}</span>
          <span className="block text-xs text-fg-subtle">
            {s.homeboxServerUrl
              ? t("settings.paperless.backlinksHintHomebox", {
                  warranty: s.paperlessWarrantyField,
                  homebox: s.paperlessHomeboxField,
                })
              : t("settings.paperless.backlinksHint", { warranty: s.paperlessWarrantyField })}
          </span>
        </span>
        <Switch
          checked={s.paperlessBacklinks}
          disabled={!s.paperlessEnabled || syncing}
          onCheckedChange={(v) => void setBacklinks(v)}
        />
      </label>
      {(s.webhookEnabled || s.isAdmin) && (
        <div className={cn("rounded-xl border border-border px-4 py-3", !s.paperlessEnabled && "opacity-50")}>
          <Field
            label={t("settings.paperless.drafts")}
            hint={
              !s.webhookEnabled ? (
                <Trans i18nKey="settings.paperless.draftsNeedWebhook" components={{ code: <code /> }} />
              ) : s.draftSource === "claude" ? (
                t("settings.paperless.draftsClaudeHint")
              ) : (
                t("settings.paperless.draftsHint")
              )
            }
          >
            <Select
              value={s.draftSource}
              onChange={(e) => update({ draftSource: e.target.value as DraftSource })}
              disabled={!s.paperlessEnabled || !s.webhookEnabled}
              className="sm:w-80"
            >
              {s.draftSources.map((src) => (
                <option key={src} value={src}>
                  {t(`settings.paperless.draftSources.${src}`)}
                </option>
              ))}
            </Select>
          </Field>
          {s.draftSource === "claude" && (
            <Field
              className="mt-3"
              label={t("settings.paperless.draftItems")}
              hint={
                s.draftItems === "all"
                  ? t("settings.paperless.draftItemsAllHint")
                  : t("settings.paperless.draftItemsWorthyHint")
              }
            >
              <Select
                value={s.draftItems}
                onChange={(e) => update({ draftItems: e.target.value as SettingsT["draftItems"] })}
                disabled={!s.paperlessEnabled || !s.webhookEnabled}
                className="sm:w-80"
              >
                <option value="worthy">{t("settings.paperless.draftItemsWorthy")}</option>
                <option value="all">{t("settings.paperless.draftItemsAll")}</option>
              </Select>
            </Field>
          )}
          {s.draftSource === "claude" && (
            <Field
              className="mt-3"
              label={t("settings.paperless.warrantyStart")}
              hint={t("settings.paperless.warrantyStartHint")}
            >
              <Select
                value={s.warrantyStart}
                onChange={(e) => update({ warrantyStart: e.target.value as SettingsT["warrantyStart"] })}
                disabled={!s.paperlessEnabled || !s.webhookEnabled}
                className="sm:w-80"
              >
                <option value="received">{t("settings.paperless.warrantyStartReceived")}</option>
                <option value="sold">{t("settings.paperless.warrantyStartSold")}</option>
              </Select>
            </Field>
          )}
          {s.isAdmin && s.webhookEnabled && (
            <p className="mt-3 text-xs leading-relaxed text-fg-subtle">
              <Trans
                i18nKey="settings.paperless.webhookSetup"
                // {{doc_id}} is Paperless's placeholder, shown as is.
                values={{ url: `${location.origin}/hooks/paperless`, doc_id: "{{doc_id}}" }}
                components={{ code: <code className="break-all" /> }}
              />
            </p>
          )}
        </div>
      )}
    </Section>
  );
};

const HomeBox = ({ s }: { s: SettingsT }) => {
  const { t } = useTranslation();
  const update = useAutoSave(s);
  const [token, setToken] = useState("");
  const [testing, setTesting] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  const test = async () => {
    setTesting(true);
    setStatus(null);
    try {
      const user = await api.testHomebox(token || undefined);
      setStatus(t("settings.homebox.connected", { name: user.name || user.email }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setTesting(false);
    }
  };

  const save = async () => {
    if (await update({ homeboxToken: token })) {
      setToken("");
      toast.success(t("settings.homebox.saved"));
    }
  };

  return (
    <Section id="homebox" icon={<Boxes />} title="HomeBox" description={t("settings.homebox.description")}>
      {!s.homeboxServerUrl ? (
        <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px] text-fg-muted">
          <Trans i18nKey="settings.homebox.notSetUp" components={{ code: <code /> }} />
        </p>
      ) : (
        <>
          <Field
            label={t("settings.homebox.key")}
            hint={s.hasHomeboxToken ? t("settings.homebox.keySaved") : t("settings.homebox.keyHint")}
          >
            <Input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={s.hasHomeboxToken ? "••••••••••••" : ""}
              autoComplete="off"
            />
          </Field>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" onClick={test} loading={testing} disabled={!token && !s.hasHomeboxToken}>
              {t("settings.paperless.test")}
            </Button>
            <Button onClick={save} disabled={!token}>
              {t("common.save")}
            </Button>
            {s.hasHomeboxToken && (
              <Button
                variant="ghost"
                onClick={async () => {
                  if (await update({ homeboxToken: "" })) setStatus(null);
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
          <label
            className={cn(
              "flex items-center justify-between gap-4 rounded-xl border border-border px-4 py-3",
              !s.homeboxEnabled && "opacity-50",
            )}
          >
            <span>
              <span className="block text-sm font-medium">{t("settings.homebox.photosDefault")}</span>
              <span className="block text-xs text-fg-subtle">{t("settings.homebox.photosDefaultHint")}</span>
            </span>
            <Switch
              checked={s.homeboxPhotosDefault}
              disabled={!s.homeboxEnabled}
              onCheckedChange={(v) => update({ homeboxPhotosDefault: v })}
            />
          </label>
        </>
      )}
    </Section>
  );
};

const Wherehouse = ({ s }: { s: SettingsT }) => {
  const { t } = useTranslation();
  const qc = useQueryClient();
  // Coming back from connecting: say how it went, once.
  const [result] = useState(() => new URLSearchParams(location.search).get("wherehouse"));
  useEffect(() => {
    if (!result) return;
    if (result === "connected") toast.success(t("settings.wherehouse.connectedToast"));
    else toast.error(t("settings.wherehouse.failed"));
    history.replaceState(null, "", location.pathname + location.hash);
  }, [result, t]);
  const connect = async () => {
    try {
      location.assign((await api.connectWherehouse()).url);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  // Disconnecting is asked first, with the choice to unlink everything too.
  const [asking, setAsking] = useState(false);
  const [unlinkAll, setUnlinkAll] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const disconnect = async () => {
    setDisconnecting(true);
    try {
      await api.disconnectWherehouse(unlinkAll);
      setAsking(false);
      await qc.invalidateQueries();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setDisconnecting(false);
    }
  };
  return (
    <Section id="wherehouse" icon={<MapPin />} title="Wherehouse" description={t("settings.wherehouse.description")}>
      {s.wherehouseConnected ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="flex items-center gap-2 text-[13px] text-ok">
            <CheckCircle2 className="size-4" />
            {t("settings.wherehouse.connected", { household: s.wherehouseHousehold })}
          </p>
          <Button
            variant="ghost"
            onClick={() => {
              setUnlinkAll(false);
              setAsking(true);
            }}
          >
            {t("settings.paperless.disconnect")}
          </Button>
          <Dialog
            open={asking}
            onOpenChange={setAsking}
            size="sm"
            title={t("settings.wherehouse.disconnectTitle")}
            description={t("settings.wherehouse.disconnectText")}
            footer={
              <>
                <Button variant="ghost" onClick={() => setAsking(false)}>
                  {t("common.cancel")}
                </Button>
                <Button variant="danger" onClick={disconnect} loading={disconnecting}>
                  {t("settings.paperless.disconnect")}
                </Button>
              </>
            }
          >
            <label htmlFor="wherehouseUnlinkAll" className="flex items-start justify-between gap-4">
              <span>
                <span className="block text-[13px] font-medium">{t("settings.wherehouse.unlinkAll")}</span>
                <span className="block text-xs text-fg-subtle">{t("settings.wherehouse.unlinkAllHint")}</span>
              </span>
              <Switch id="wherehouseUnlinkAll" checked={unlinkAll} onCheckedChange={setUnlinkAll} />
            </label>
          </Dialog>
        </div>
      ) : (
        <Button onClick={connect} className="w-fit">
          {t("settings.wherehouse.connect")}
        </Button>
      )}
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

const Calendar = () => {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data, isPending, isError, refetch } = useQuery({ queryKey: keys.calendar, queryFn: api.calendar });
  const [busy, setBusy] = useState(false);
  const [renewing, setRenewing] = useState(false);
  const url = data?.url ?? null;

  const change = async (call: () => Promise<{ url: string | null }>, message: string) => {
    setBusy(true);
    try {
      qc.setQueryData(keys.calendar, await call());
      toast.success(message);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
      setRenewing(false);
    }
  };

  const copy = () => {
    // The Clipboard API only exists on HTTPS (or localhost) pages.
    if (!url || !window.isSecureContext) {
      toast.error(t("common.copyNeedsHttps"));
      return;
    }
    navigator.clipboard.writeText(url).then(
      () => toast.success(t("common.copied")),
      () => toast.error(t("common.copyFailed")),
    );
  };

  return (
    <Section
      icon={<CalendarDays />}
      title={t("settings.calendar.title")}
      description={t("settings.calendar.description")}
    >
      {/* Only offer a new link once we know there isn't one. */}
      {isPending ? (
        <div className="h-9 w-48 animate-pulse rounded-lg bg-surface-3/60" />
      ) : isError ? (
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-sm text-bad">{t("settings.calendar.loadFailed")}</p>
          <Button variant="outline" size="sm" onClick={() => void refetch()}>
            {t("common.retry")}
          </Button>
        </div>
      ) : !url ? (
        <div>
          <Button
            variant="outline"
            loading={busy}
            onClick={() => void change(api.newCalendar, t("settings.calendar.created"))}
          >
            {t("settings.calendar.create")}
          </Button>
        </div>
      ) : (
        <>
          <Field label={t("settings.calendar.link")} htmlFor="calendarUrl" hint={t("settings.calendar.linkHint")}>
            <div className="flex gap-2">
              <Input
                id="calendarUrl"
                readOnly
                value={url}
                className="font-mono text-[13px]"
                onFocus={(e) => e.currentTarget.select()}
              />
              <Button
                variant="outline"
                size="icon"
                aria-label={t("settings.calendar.copy")}
                title={t("settings.calendar.copy")}
                onClick={copy}
              >
                <Copy />
              </Button>
            </div>
          </Field>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => window.location.assign(url.replace(/^https?:/, "webcal:"))}>
              {t("settings.calendar.subscribe")}
            </Button>
            <Button variant="ghost" disabled={busy} onClick={() => setRenewing(true)}>
              {t("settings.calendar.renew")}
            </Button>
            <Button
              variant="ghost"
              disabled={busy}
              onClick={() => void change(api.deleteCalendar, t("settings.calendar.removed"))}
            >
              {t("settings.calendar.remove")}
            </Button>
          </div>
        </>
      )}
      <ConfirmDialog
        open={renewing}
        onOpenChange={setRenewing}
        title={t("settings.calendar.renewTitle")}
        description={t("settings.calendar.renewText")}
        confirmLabel={t("settings.calendar.renewConfirm")}
        loading={busy}
        onConfirm={() => void change(api.renewCalendar, t("settings.calendar.renewed"))}
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
