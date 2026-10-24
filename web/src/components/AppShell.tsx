import { LogOut, Monitor, Moon, Settings as SettingsIcon, ShieldCheck, Sun } from "lucide-react";
import { useEffect, type ReactNode } from "react";
import { Link, NavLink, useLocation, useNavigate } from "react-router";
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
  return picture ? (
    <img src={picture} alt="" className="size-8 rounded-full object-cover" referrerPolicy="no-referrer" />
  ) : (
    <span className="flex size-8 items-center justify-center rounded-full bg-primary-soft text-xs font-semibold text-primary">
      {initials || "?"}
    </span>
  );
};

export const AppShell = ({ children }: { children: ReactNode }) => {
  const { data: me } = useMe();
  const { data: settings } = useSettingsQuery();
  const save = useSaveSettings();
  const navigate = useNavigate();
  const { pathname } = useLocation();

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
          <Link to="/" className="rounded-lg">
            <Logo />
          </Link>
          <nav className="hidden items-center gap-1 sm:flex">
            {nav("/", "Warranties")}
            {nav("/settings", "Settings")}
          </nav>
          <div className="ml-auto">
            <Menu>
              <MenuTrigger asChild>
                <button
                  type="button"
                  className="cursor-pointer rounded-full ring-offset-2 ring-offset-bg hover:ring-2 hover:ring-border-strong"
                  aria-label="Account"
                >
                  <Avatar name={me?.name || me?.email || ""} picture={me?.picture} />
                </button>
              </MenuTrigger>
              <MenuContent className="w-60">
                <div className="px-2.5 py-2">
                  <p className="truncate text-sm font-medium">{me?.name || "Signed in"}</p>
                  <p className="truncate text-xs text-fg-subtle">{me?.email}</p>
                </div>
                <MenuSeparator />
                <MenuItem icon={<SettingsIcon />} onSelect={() => navigate("/settings")}>
                  Settings
                </MenuItem>
                <MenuLabel>Theme</MenuLabel>
                <div className="flex gap-1 px-1.5 pb-1.5">
                  {(
                    [
                      ["light", Sun, "Light"],
                      ["dark", Moon, "Dark"],
                      ["system", Monitor, "Auto"],
                    ] as const
                  ).map(([t, Icon, label]) => (
                    <button
                      type="button"
                      key={t}
                      onClick={() => setTheme(t)}
                      className={cn(
                        "flex flex-1 cursor-pointer flex-col items-center gap-1 rounded-lg py-1.5 text-[11px]",
                        settings?.theme === t ? "bg-primary-soft text-primary" : "text-fg-muted hover:bg-surface-3",
                      )}
                    >
                      <Icon className="size-4" />
                      {label}
                    </button>
                  ))}
                </div>
                {me?.isAdmin && (
                  <div className="flex items-center gap-1.5 px-2.5 pb-1.5 text-xs text-fg-subtle">
                    <ShieldCheck className="size-3.5" /> Administrator
                  </div>
                )}
                <MenuSeparator />
                <MenuItem icon={<LogOut />} onSelect={logout}>
                  Sign out
                </MenuItem>
              </MenuContent>
            </Menu>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 pt-6 pb-16 sm:px-6 sm:pt-8">{children}</main>
    </div>
  );
};
