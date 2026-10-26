import { useQuery } from "@tanstack/react-query";
import { KeyRound, Loader2, TriangleAlert } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { LogoMark } from "@/components/Logo";
import { api, translateServer } from "@/lib/api";
import { safeReturn } from "@/lib/utils";

export const Login = () => {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const error = params.get("error");
  const detail = params.get("detail") ?? "";
  const ret = safeReturn(params.get("return"));
  const { data: info, isLoading } = useQuery({ queryKey: ["auth-info"], queryFn: api.authInfo });

  // Already signed in? Skip the login screen.
  const me = useQuery({ queryKey: ["me-probe"], queryFn: api.me, retry: false });
  useEffect(() => {
    if (me.data) location.replace(ret);
  }, [me.data, ret]);

  const loginUrl = `/auth/login?return=${encodeURIComponent(ret)}`;
  useEffect(() => {
    if (info?.autoRedirect && !error && me.isError) location.assign(loginUrl);
  }, [info, error, me.isError, loginUrl]);

  return (
    <div className="flex min-h-dvh items-center justify-center bg-bg px-4">
      <div className="pointer-events-none fixed inset-0 bg-[radial-gradient(60rem_30rem_at_50%_-10%,var(--primary-soft),transparent)]" />
      <div className="relative w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          <LogoMark className="size-12" />
          <h1 className="mt-4 text-2xl font-semibold tracking-tight">Warranteed</h1>
          <p className="mt-1.5 text-sm text-fg-muted">{t("login.tagline")}</p>
        </div>
        <div className="rounded-2xl border border-border bg-surface p-6 shadow-pop">
          {error && (
            <div className="mb-4 flex gap-2 rounded-lg bg-bad-soft px-3 py-2.5 text-sm text-bad">
              <TriangleAlert className="mt-0.5 size-4 shrink-0" />
              <span>{translateServer(error, `signIn.${error}`, { detail })}</span>
            </div>
          )}
          {isLoading || me.isLoading ? (
            <div className="flex justify-center py-3">
              <Loader2 className="size-5 animate-spin text-fg-subtle" />
            </div>
          ) : (
            <div className="flex flex-col gap-2.5">
              {info?.oidc && (
                <a
                  href={loginUrl}
                  className="flex h-11 items-center justify-center gap-2 rounded-xl bg-primary text-[15px] font-medium text-primary-fg shadow-card transition-colors hover:bg-primary-hover"
                >
                  <KeyRound className="size-4" />
                  {t("login.continueWith", { provider: info.providerName })}
                </a>
              )}
              {info?.devLogin && (
                <a
                  href={`/auth/dev?return=${encodeURIComponent(ret)}`}
                  className="flex h-11 items-center justify-center rounded-xl border border-dashed border-border-strong text-sm font-medium text-fg-muted hover:bg-surface-2"
                >
                  {t("login.devLogin")}
                </a>
              )}
              {!info?.oidc && !info?.devLogin && (
                <p className="text-center text-sm text-fg-muted">{t("login.notConfigured")}</p>
              )}
            </div>
          )}
        </div>
        <p className="mt-6 text-center text-xs text-fg-subtle">{t("login.footer")}</p>
      </div>
    </div>
  );
};
