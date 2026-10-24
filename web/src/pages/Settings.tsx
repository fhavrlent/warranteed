import { useQueryClient } from "@tanstack/react-query";
import { Bell, CheckCircle2, Database, Download, FileUp, Palette, Plug, Tag as TagIcon, Trash2 } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { TagChip } from "@/components/Tags";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/dialog";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { Card, Segmented, Switch } from "@/components/ui/misc";
import { api, errorMessage, type ImportResult, type Settings as SettingsT, type Tag } from "@/lib/api";
import { formatDate, todayISO } from "@/lib/dates";
import { keys, useInvalidate, useSaveSettings, useSettingsQuery, useTags } from "@/lib/queries";
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
];

export const Settings = () => {
  const { data: settings, isLoading } = useSettingsQuery();
  if (isLoading || !settings) return <div className="h-96 animate-pulse rounded-2xl bg-surface-3/60" />;
  return (
    <div className="animate-fade-in">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
      <p className="mt-1 mb-8 text-sm text-fg-muted">Changes save automatically.</p>
      <div className="flex flex-col gap-6">
        <Display s={settings} />
        <Notifications s={settings} />
        <Paperless s={settings} />
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
  const update = useAutoSave(s);
  const [soon, setSoon] = useState(String(s.expiringSoonDays));
  const sample = todayISO();
  return (
    <Section
      icon={<Palette />}
      title="Display"
      description="How dates and prices appear everywhere in the app, including date fields and reminders."
    >
      <Field label="Date format">
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
        <Field label="Default currency" hint="Used for new warranties and totals.">
          <Select value={s.currency} onChange={(e) => update({ currency: e.target.value })}>
            {[...new Set([s.currency, ...CURRENCIES])].map((c) => (
              <option key={c}>{c}</option>
            ))}
          </Select>
        </Field>
        <Field label="“Expiring soon” means within" hint="Days before expiry.">
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
            <span className="text-sm text-fg-muted">days</span>
          </div>
        </Field>
      </div>
      <Field label="Theme">
        <Segmented
          value={s.theme}
          onChange={(theme) => {
            applyTheme(theme);
            void update({ theme });
          }}
          options={[
            { value: "system", label: "System" },
            { value: "light", label: "Light" },
            { value: "dark", label: "Dark" },
          ]}
        />
      </Field>
    </Section>
  );
};

const Notifications = ({ s }: { s: SettingsT }) => {
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
      title="Reminders"
      description={
        <>
          Get a message before warranties expire. Each reminder is sent once. Uses{" "}
          <a
            className="text-primary hover:underline"
            href="https://shoutrrr.nickfedor.com/latest/services/overview/"
            target="_blank"
            rel="noreferrer"
          >
            Shoutrrr URLs
          </a>{" "}
          for ntfy, Gotify, Discord, Telegram, email, Pushover and more.
        </>
      }
    >
      {!s.isAdmin && (
        <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px] text-fg-muted">
          Discord, Telegram, Slack, Pushover, Pushbullet, IFTTT and Join work out of the box.{" "}
          {s.notifyAllowedHosts.length > 0
            ? `Self-hosted services (ntfy, Gotify, webhooks, email) can send to: ${s.notifyAllowedHosts.join(", ")}.`
            : "Self-hosted services (ntfy, Gotify, webhooks, email) need an admin to allow their host first."}
        </p>
      )}
      <Field
        label="Notification URLs"
        hint="One per line, e.g. ntfy://ntfy.sh/my-topic or smtp://user:pass@host:587/?to=me@example.com"
      >
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
        <Field label="Remind me" hint="Days before expiry, comma separated." className="w-56">
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
              toast.success("Test notification sent");
            } catch (e) {
              toast.error(errorMessage(e));
            } finally {
              setTesting(false);
            }
          }}
        >
          Send test
        </Button>
      </div>
      <p className="-mt-2 text-xs text-fg-subtle">Checked daily in the server's timezone ({s.timezone}).</p>
    </Section>
  );
};

const Paperless = ({ s }: { s: SettingsT }) => {
  const update = useAutoSave(s);
  const [url, setUrl] = useState(s.paperlessUrl);
  const [token, setToken] = useState("");
  const [testing, setTesting] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  const test = async () => {
    setTesting(true);
    setStatus(null);
    try {
      const info = await api.testPaperless(url, token || undefined);
      setStatus(
        `Connected to Paperless-ngx ${info.version || ""} (API v${info.apiVersion}) · ${info.documents} documents`,
      );
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setTesting(false);
    }
  };

  const saveConnection = async () => {
    const ok = await update({ paperlessUrl: url, ...(token ? { paperlessToken: token } : {}) });
    if (ok) {
      setToken("");
      toast.success("Paperless connection saved");
    }
  };

  return (
    <Section
      icon={<Plug />}
      title="Paperless-ngx"
      description="Store receipts and manuals in your Paperless-ngx archive instead of here, or link documents that are already there."
    >
      {!s.canSetPaperlessUrl && !s.paperlessServerUrl ? (
        <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px] text-fg-muted">
          Paperless isn&apos;t set up on this server. An admin can enable it with <code>PAPERLESS_URL</code>.
        </p>
      ) : (
        <>
          {s.paperlessServerToken && (
            <p className="rounded-lg bg-primary-soft/60 px-3 py-2 text-[13px] text-fg">
              As an admin you can use the server&apos;s Paperless connection. Fill these in only to use a different
              instance or your own token.
            </p>
          )}
          {!s.canSetPaperlessUrl && (
            <p className="rounded-lg bg-primary-soft/60 px-3 py-2 text-[13px] text-fg">
              Connect with your own Paperless account&apos;s token. You&apos;ll only see documents that account can see.
            </p>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            {s.canSetPaperlessUrl ? (
              <Field
                label="Paperless URL"
                hint={s.paperlessServerUrl ? `Leave empty to use ${s.paperlessServerUrl}` : undefined}
              >
                <Input
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder={s.paperlessServerUrl || "https://paperless.example.com"}
                />
              </Field>
            ) : (
              <Field label="Paperless">
                <p className="flex h-9 items-center truncate text-sm text-fg-muted">{s.paperlessServerUrl}</p>
              </Field>
            )}
            <Field
              label="API token"
              hint={
                s.hasPaperlessToken
                  ? "A token is saved. Enter a new one to replace it."
                  : "Profile → API Auth Token in Paperless."
              }
            >
              <Input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder={s.hasPaperlessToken ? "••••••••••••" : ""}
                autoComplete="off"
              />
            </Field>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" onClick={test} loading={testing}>
              Test connection
            </Button>
            <Button onClick={saveConnection} disabled={url === s.paperlessUrl && !token}>
              Save
            </Button>
            {(s.paperlessUrl || s.hasPaperlessToken) && (
              <Button
                variant="ghost"
                onClick={async () => {
                  if (await update({ paperlessUrl: "", paperlessToken: "" })) {
                    setUrl("");
                    setStatus(null);
                  }
                }}
              >
                Disconnect
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
          <span className="block text-sm font-medium">Upload documents to Paperless by default</span>
          <span className="block text-xs text-fg-subtle">Product photos always stay here.</span>
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
  const { data: tags = [] } = useTags();
  const invalidate = useInvalidate();
  const [deleting, setDeleting] = useState<Tag | null>(null);
  return (
    <Section
      icon={<TagIcon />}
      title="Tags"
      description="Rename, recolour or remove tags. Tags are created from the warranty form."
    >
      {tags.length === 0 ? (
        <p className="text-sm text-fg-subtle">No tags yet.</p>
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border">
          {tags.map((t) => (
            <TagRow key={t.id} t={t} onChanged={invalidate} onDelete={() => setDeleting(t)} />
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete tag “${deleting?.name}”?`}
        description={`It will be removed from ${deleting?.count ?? 0} warranties. The warranties themselves are kept.`}
        onConfirm={async () => {
          if (deleting) await api.deleteTag(deleting.id).catch((e: unknown) => toast.error(errorMessage(e)));
          invalidate();
          setDeleting(null);
        }}
      />
    </Section>
  );
};

const TagRow = ({ t, onChanged, onDelete }: { t: Tag; onChanged: () => void; onDelete: () => void }) => {
  const [nameDraft, setNameDraft] = useState<string | null>(null);
  const name = nameDraft ?? t.name;
  const save = async (n: string, color: string) => {
    try {
      await api.updateTag(t.id, n, color);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setNameDraft(null);
    }
  };
  return (
    <li className="flex flex-wrap items-center gap-3 px-3 py-2">
      <TagChip tag={{ name, color: t.color }} className="hidden sm:inline-flex" />
      <Input
        value={name}
        onChange={(e) => setNameDraft(e.target.value)}
        onBlur={() => {
          if (name.trim() && name !== t.name) void save(name, t.color);
          else setNameDraft(null);
        }}
        className="h-8 w-40"
        aria-label="Tag name"
      />
      <div className="flex flex-wrap gap-1">
        {TAG_COLORS.map((c) => (
          <button
            type="button"
            key={c}
            onClick={() => save(t.name, c)}
            aria-label={c}
            className={cn(
              "size-5 cursor-pointer rounded-full ring-offset-2 ring-offset-surface",
              tagDot[c],
              t.color === c && "ring-2 ring-fg",
            )}
          />
        ))}
      </div>
      <span className="ml-auto text-xs text-fg-subtle tabular-nums">{t.count} used</span>
      <Button variant="ghost" size="icon-sm" aria-label={`Delete ${t.name}`} onClick={onDelete}>
        <Trash2 />
      </Button>
    </li>
  );
};

const Data = () => {
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
      title="Import & export"
      description="Export everything to CSV, or import a CSV in the same format. Rows that already exist are skipped."
    >
      <div className="flex flex-wrap gap-2">
        <a href="/api/export.csv">
          <span className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3.5 text-sm font-medium shadow-card hover:bg-surface-2">
            <Download className="size-4" /> Export CSV
          </span>
        </a>
        <Button variant="outline" onClick={() => fileRef.current?.click()} loading={importing}>
          {!importing && <FileUp />} Import CSV
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
            Imported {result.imported} · skipped {result.skipped} existing
            {result.errors.length > 0 && <span className="text-bad"> · {result.errors.length} with problems</span>}
          </p>
          {result.errors.length > 0 && (
            <ul className="mt-2 max-h-48 overflow-y-auto text-[13px] text-fg-muted">
              {result.errors.map((e) => (
                <li key={e.row}>
                  Row {e.row}: {e.message}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Section>
  );
};
