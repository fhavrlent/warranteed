import i18n from "@/i18n";

export interface Tag {
  id: number;
  name: string;
  color: string;
  spaceId: number;
  count?: number;
}

export interface Attachment {
  id: number;
  warrantyId: number;
  kind: AttachmentKind;
  storage: "local" | "paperless";
  filename: string;
  mimeType: string;
  size: number;
  paperlessDocId: number | null;
  status: "ready" | "processing" | "failed";
  error: string;
  createdAt: string;
}

export type AttachmentKind = "purchase" | "terms" | "card" | "extended" | "claim" | "manual" | "photo" | "other";

export interface Warranty {
  id: number;
  productName: string;
  brand: string;
  model: string;
  vendor: string;
  orderNumber: string;
  purchaseDate: string;
  warrantyMonths: number | null;
  warrantyEndDate: string | null;
  isLifetime: boolean;
  expiresOn: string | null;
  price: number | null;
  currency: string;
  productUrl: string;
  notes: string;
  archivedAt: string | null;
  createdAt: string;
  updatedAt: string;
  serialNumbers: string[];
  tags: Tag[];
  attachments: Attachment[];
  spaceId: number;
  spaceKind: SpaceKind;
  addedBy: { id: number; name: string };
}

export type SpaceKind = "personal" | "household" | "custom";

export interface Space {
  id: number;
  name: string;
  kind: SpaceKind;
  members: number;
}

export interface WarrantyInput {
  productName: string;
  brand: string;
  model: string;
  vendor: string;
  orderNumber: string;
  purchaseDate: string;
  warrantyMonths: number | null;
  warrantyEndDate: string | null;
  isLifetime: boolean;
  price: number | null;
  currency: string;
  productUrl: string;
  notes: string;
  serialNumbers: string[];
  tags: string[];
  /** Who can see it; omitted means the household (new) or unchanged (edit). */
  spaceId?: number;
}

export interface Settings {
  dateFormat: string;
  currency: string;
  theme: "system" | "light" | "dark";
  /** Language code, or "" to follow the browser. */
  language: string;
  expiringSoonDays: number;
  notifyUrls: string;
  notifyDays: string;
  paperlessUrl: string;
  /** Address the browser uses for Paperless links (admins), empty = the Paperless URL. */
  paperlessPublicUrl: string;
  paperlessDefault: boolean;
  hasPaperlessToken: boolean;
  /** The server's Paperless instance (PAPERLESS_URL), empty if none. */
  paperlessServerUrl: string;
  /** An admin can use the server's own Paperless token. */
  paperlessServerToken: boolean;
  /** Only admins may point Paperless at another URL. */
  canSetPaperlessUrl: boolean;
  /** Hosts non-admins may use with self-hosted notification services. */
  notifyAllowedHosts: string[];
  isAdmin: boolean;
  paperlessEnabled: boolean;
  paperlessWebUrl: string;
  dateFormats: string[];
  /** Languages the server has translations for. */
  languages: string[];
  timezone: string;
}

export type SettingsInput = Omit<
  Settings,
  | "hasPaperlessToken"
  | "paperlessEnabled"
  | "paperlessWebUrl"
  | "paperlessServerUrl"
  | "paperlessServerToken"
  | "canSetPaperlessUrl"
  | "notifyAllowedHosts"
  | "isAdmin"
  | "dateFormats"
  | "languages"
  | "timezone"
> & { paperlessToken?: string };

export interface User {
  id: number;
  email: string;
  name: string;
  picture: string;
  isAdmin: boolean;
}
export interface AuthInfo {
  providerName: string;
  oidc: boolean;
  devLogin: boolean;
  autoRedirect: boolean;
}
export interface PaperlessDoc {
  id: number;
  title: string;
  created: string;
  originalFileName: string;
  mimeType: string;
}
export interface PaperlessInfo {
  version: string;
  apiVersion: number;
  documents: number;
}
export interface ImportResult {
  imported: number;
  skipped: number;
  errors: { row: number; message: string; code: string; params?: Record<string, unknown> }[];
}

/** Server messages come with a translation key; untranslated ones (e.g. from Paperless) as plain text. */
export const translateServer = (message: string, code?: string, params?: Record<string, unknown>): string =>
  code && i18n.exists(code) ? (i18n.t as (key: string, options?: object) => string)(code, params) : message;

/** A user-facing message for anything thrown. */
export const errorMessage = (e: unknown): string => {
  if (e instanceof ApiError) return translateServer(e.message, e.code, e.params);
  return e instanceof Error ? e.message : String(e);
};

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code?: string,
    public params?: Record<string, unknown>,
  ) {
    super(message);
  }
}

interface ErrorBody {
  error: string;
  code?: string;
  params?: Record<string, unknown>;
}

const isErrorBody = (data: unknown): data is ErrorBody =>
  typeof data === "object" && data !== null && "error" in data && typeof data.error === "string";

const request = async <T>(method: string, url: string, body?: unknown): Promise<T> => {
  const init: RequestInit = { method, headers: {}, credentials: "same-origin" };
  if (body instanceof FormData) {
    init.body = body;
  } else if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
  }
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch {
    throw new ApiError(0, i18n.t("errors.offline"));
  }
  if (res.status === 401 && !url.startsWith("/api/auth/")) {
    const ret = encodeURIComponent(location.pathname + location.search);
    if (!location.pathname.startsWith("/login")) location.assign(`/login?return=${ret}`);
    throw new ApiError(401, i18n.t("errors.signedOut"));
  }
  if (!res.ok) {
    let body: ErrorBody = { error: res.statusText || i18n.t("errors.requestFailed") };
    try {
      const data: unknown = await res.json();
      if (isErrorBody(data)) body = data;
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, body.error, body.code, body.params);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
};

export const api = {
  authInfo: () => request<AuthInfo>("GET", "/api/auth/info"),
  me: () => request<User>("GET", "/api/me"),
  logout: () => request<{ redirect: string }>("POST", "/auth/logout"),

  settings: () => request<Settings>("GET", "/api/settings"),
  saveSettings: (s: SettingsInput) => request<Settings>("PUT", "/api/settings", s),
  testNotification: (notifyUrls: string) =>
    request<{ ok: boolean }>("POST", "/api/settings/test-notification", { notifyUrls }),
  testPaperless: (url: string, token?: string) =>
    request<PaperlessInfo>("POST", "/api/settings/test-paperless", { url, token }),

  warranties: () => request<Warranty[]>("GET", "/api/warranties"),
  warranty: (id: number) => request<Warranty>("GET", `/api/warranties/${id}`),
  createWarranty: (w: WarrantyInput) => request<Warranty>("POST", "/api/warranties", w),
  updateWarranty: (id: number, w: WarrantyInput) => request<Warranty>("PUT", `/api/warranties/${id}`, w),
  deleteWarranty: (id: number) => request<undefined>("DELETE", `/api/warranties/${id}`),
  archive: (id: number, archived: boolean) =>
    request<Warranty>("POST", `/api/warranties/${id}/${archived ? "archive" : "unarchive"}`),

  upload: (warrantyId: number, file: File, kind: AttachmentKind, storage: "local" | "paperless") => {
    const fd = new FormData();
    fd.append("kind", kind);
    fd.append("storage", storage);
    fd.append("file", file);
    return request<Attachment>("POST", `/api/warranties/${warrantyId}/attachments`, fd);
  },
  linkPaperless: (warrantyId: number, documentIds: number[], kind: AttachmentKind) =>
    request<Attachment[]>("POST", `/api/warranties/${warrantyId}/attachments/paperless`, { documentIds, kind }),
  updateAttachment: (id: number, patch: { kind?: AttachmentKind; filename?: string }) =>
    request<Attachment>("PATCH", `/api/attachments/${id}`, patch),
  deleteAttachment: (id: number) => request<undefined>("DELETE", `/api/attachments/${id}`),
  retryAttachment: (id: number) => request<Attachment>("POST", `/api/attachments/${id}/retry`),
  searchPaperless: (q: string) => request<PaperlessDoc[]>("GET", `/api/paperless/search?q=${encodeURIComponent(q)}`),

  spaces: () => request<Space[]>("GET", "/api/spaces"),
  moveAllToSpace: (spaceId: number) => request<{ moved: number }>("POST", `/api/spaces/${spaceId}/move-all`),

  tags: () => request<Tag[]>("GET", "/api/tags"),
  updateTag: (id: number, name: string, color: string) =>
    request<undefined>("PATCH", `/api/tags/${id}`, { name, color }),
  deleteTag: (id: number) => request<undefined>("DELETE", `/api/tags/${id}`),

  importCsv: (file: File) => {
    const fd = new FormData();
    fd.append("file", file);
    return request<ImportResult>("POST", "/api/import", fd);
  },
};

// The version suffix keeps the browser cache honest: attachment IDs can be
// reused after deletion, and a Paperless upload changes once it is processed.
const version = (a: Attachment) => `v=${encodeURIComponent(`${a.createdAt}-${a.status}-${a.paperlessDocId ?? ""}`)}`;
export const fileUrl = (a: Attachment, download = false) =>
  `/api/attachments/${a.id}/file?${version(a)}${download ? "&download=1" : ""}`;
export const thumbUrl = (a: Attachment) => `/api/attachments/${a.id}/thumb?${version(a)}`;
