export type GetToken = () => Promise<string | null>;
export type Profile = {
  id: string;
  username: string;
  display_name: string;
  status: "active" | "rename_required" | "suspended";
  moderation_reason: string | null;
  moderation_note: string | null;
  suspended_until: string | null;
};
export type ApiFailure =
  | { kind: "unauthorized" }
  | { kind: "forbidden" }
  | { kind: "rate_limited"; retryAfter?: number; requestId?: string }
  | { kind: "error"; status?: number; code?: string; requestId?: string };
export type MeResult = { kind: "ok"; profile: Profile } | { kind: "no_profile" } | ApiFailure;
export type SaveProfileResult =
  | { kind: "ok"; profile: Profile }
  | { kind: "invalid"; code: "validation_error" | "username_reserved"; fields?: Record<string, string> }
  | { kind: "username_taken" }
  | { kind: "suspended"; suspendedUntil?: string }
  | ApiFailure;

const TIMEOUT_MS = 60_000;

type Raw = { status: number; body: any; retryAfter?: number } | { network: true } | { unauthorized: true };

async function attempt(
  baseUrl: string,
  getToken: GetToken,
  path: string,
  init: { method: string; body?: string },
  signal?: AbortSignal,
): Promise<Raw> {
  const token = await getToken();
  if (!token) return { unauthorized: true };
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), TIMEOUT_MS);
  const onAbort = () => ctrl.abort();
  signal?.addEventListener("abort", onAbort);
  if (signal?.aborted) ctrl.abort();
  try {
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (init.body !== undefined) headers["Content-Type"] = "application/json";
    const res = await fetch(`${baseUrl.replace(/\/+$/, "")}${path}`, { ...init, headers, signal: ctrl.signal });
    const body = await res.json().catch(() => undefined);
    const ra = Number(res.headers.get("Retry-After"));
    return { status: res.status, body, retryAfter: Number.isFinite(ra) && ra > 0 ? ra : undefined };
  } catch {
    return { network: true };
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", onAbort);
  }
}

function failure(r: Exclude<Raw, { unauthorized: true }>): ApiFailure {
  if ("network" in r) return { kind: "error" };
  if (r.status === 401) return { kind: "unauthorized" };
  const code: string | undefined = r.body?.error?.code;
  const requestId: string | undefined = r.body?.error?.request_id;
  if (r.status === 403 && code === "forbidden") return { kind: "forbidden" };
  if (r.status === 429) return { kind: "rate_limited", retryAfter: r.retryAfter, requestId };
  return { kind: "error", status: r.status, code, requestId };
}

export async function getMe(baseUrl: string, getToken: GetToken, signal?: AbortSignal): Promise<MeResult> {
  let r = await attempt(baseUrl, getToken, "/v1/me", { method: "GET" }, signal);
  if ("network" in r && !signal?.aborted) r = await attempt(baseUrl, getToken, "/v1/me", { method: "GET" }, signal);
  if ("unauthorized" in r) return { kind: "unauthorized" };
  if (!("network" in r)) {
    if (r.status === 200 && r.body?.data) return { kind: "ok", profile: r.body.data };
    if (r.status === 404 && r.body?.error?.code === "profile_not_found") return { kind: "no_profile" };
  }
  return failure(r);
}

export async function saveProfile(
  baseUrl: string,
  getToken: GetToken,
  input: { username: string; display_name: string },
  signal?: AbortSignal,
): Promise<SaveProfileResult> {
  const r = await attempt(
    baseUrl,
    getToken,
    "/v1/me",
    { method: "PATCH", body: JSON.stringify({ username: input.username, display_name: input.display_name }) },
    signal,
  );
  if ("unauthorized" in r) return { kind: "unauthorized" };
  if (!("network" in r)) {
    const e = r.body?.error;
    if (r.status === 200 && r.body?.data) return { kind: "ok", profile: r.body.data };
    if (r.status === 400) {
      return { kind: "invalid", code: e?.code === "username_reserved" ? "username_reserved" : "validation_error", fields: e?.fields };
    }
    if (r.status === 409) return { kind: "username_taken" };
    if (r.status === 403 && e?.code === "profile_suspended") {
      return { kind: "suspended", suspendedUntil: e.details?.suspended_until };
    }
  }
  return failure(r);
}
