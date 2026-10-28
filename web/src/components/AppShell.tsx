import { LogOut, Monitor, Moon, Settings as SettingsIcon, ShieldCheck, Sun } from "lucide-react";
import { Suspense, useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link, NavLink, useLocation, useNavigate } from "react-router";
import { applyLanguage } from "@/i18n";
import { api } from "@/lib/api";
import { useMe, useSaveSettings, useSettingsQuery } from "@/lib/queries";
import { applyTheme, watchSystemTheme, type Theme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { Logo } from "./Logo";
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from "./ui/menu";

const Avatar = ({ name, picture }: { name: string; picture?: string }) => {
  const initials = name
    .split(/\s+/)
    .map((p) => p[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
  const [failed, setFailed] = useState<string | null>(null);
  return picture && failed !== picture ? (
    <img
      src={picture}
      alt=""
      className="size-8 rounded-full object-cover"
      onError={() => {
        setFailed(picture);
      }}
    />
  ) : (
    <span className="flex size-8 items-center justify-center rounded-full bg-primary-soft text-xs font-semibold text-primary">
      {initials || "?"}
    </span>
  );
};

export const AppShell = ({ children }: { children: ReactNode }) => {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const { data: settings } = useSettingsQuery();
  const save = useSaveSettings();
  const navigate = useNavigate();
  const { pathname } = useLocation();

  const language = settings?.language;
  useEffect(() => {
    if (language !== undefined) void applyLanguage(language);
  }, [language]);

  const theme = settings?.theme;
  useEffect(() => {
    if (theme) applyTheme(theme);
    return watchSystemTheme(() => theme ?? "system");
  }, [theme]);

  const setTheme = (theme: Theme) => {
    applyTheme(theme);
    if (settings) save.mutate({ ...settings, theme });
  };

  const logout = async () => {
    try {
      const { redirect } = await api.logout();
      location.assign(redirect);
    } catch {
      location.assign("/login");
    }
  };

  const nav = (to: string, label: string) => (
    <NavLink
      to={to}
      end={to === "/"}
      className={({ isActive }) =>
        cn(
          "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
          isActive || (to === "/" && pathname.startsWith("/w/"))
            ? "bg-surface-3 text-fg"
            : "text-fg-muted hover:text-fg",
        )
      }
    >
      {label}
    </NavLink>
  );

  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-30 border-b border-border bg-bg/80 backdrop-blur-lg">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4 sm:px-6">
          <Link to="/" className="flex rounded-lg">
            <Logo />
          </Link>
          <nav className="hidden items-center gap-1 sm:flex">
            {nav("/", t("nav.warranties"))}
            {nav("/settings", t("nav.settings"))}
          </nav>
          <div className="ml-auto">
            <Menu>
              <MenuTrigger asChild>
                <button
                  type="button"
                  className="cursor-pointer rounded-full ring-offset-2 ring-offset-bg hover:ring-2 hover:ring-border-strong"
                  aria-label={t("account.menu")}
                >
                  <Avatar name={me?.name || me?.email || ""} picture={me?.picture} />
                </button>
              </MenuTrigger>
              <MenuContent className="w-60">
                <div className="px-2.5 py-2">
                  <p className="truncate text-sm font-medium">{me?.name || t("account.signedIn")}</p>
                  <p className="truncate text-xs text-fg-subtle">{me?.email}</p>
                </div>
                <MenuSeparator />
                <MenuItem icon={<SettingsIcon />} onSelect={() => navigate("/settings")}>
                  {t("nav.settings")}
                </MenuItem>
                <MenuLabel>{t("account.theme")}</MenuLabel>
                <div className="flex gap-1 px-1.5 pb-1.5">
                  {(
                    [
                      ["light", Sun, t("theme.light")],
                      ["dark", Moon, t("theme.dark")],
                      ["system", Monitor, t("theme.auto")],
                    ] as const
                  ).map(([value, Icon, label]) => (
                    <button
                      type="button"
                      key={value}
                      onClick={() => setTheme(value)}
                      className={cn(
                        "flex flex-1 cursor-pointer flex-col items-center gap-1 rounded-lg py-1.5 text-[11px]",
                        settings?.theme === value ? "bg-primary-soft text-primary" : "text-fg-muted hover:bg-surface-3",
                      )}
                    >
                      <Icon className="size-4" />
                      {label}
                    </button>
                  ))}
                </div>
                {me?.isAdmin && (
                  <div className="flex items-center gap-1.5 px-2.5 pb-1.5 text-xs text-fg-subtle">
                    <ShieldCheck className="size-3.5" /> {t("account.administrator")}
                  </div>
                )}
                <MenuSeparator />
                <MenuItem icon={<LogOut />} onSelect={logout}>
                  {t("account.signOut")}
                </MenuItem>
              </MenuContent>
            </Menu>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 pt-6 pb-16 sm:px-6 sm:pt-8">
        {/* Keeps the header in place while a page downloads. */}
        <Suspense fallback={null}>{children}</Suspense>
      </main>
    </div>
  );
};
