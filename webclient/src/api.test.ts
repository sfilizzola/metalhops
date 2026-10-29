import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getMe, saveProfile } from "./api";

const profile = {
  id: "1", username: "metal_fan", display_name: "Metal Fan", status: "active" as const,
  moderation_reason: null, moderation_note: null, suspended_until: null,
};
const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers });
const err = (status: number, code: string, extra: object = {}) =>
  json(status, { error: { code, message: "m", request_id: "rid", ...extra } });
const tok = async () => "tkn";
const B = "https://api.test";
const fetchMock = vi.fn();

beforeEach(() => { fetchMock.mockReset(); vi.stubGlobal("fetch", fetchMock); });
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

describe("getMe", () => {
  it("sends_single_bearer_header", async () => {
    fetchMock.mockResolvedValue(json(200, { data: profile }));
    await getMe(B, tok);
    expect(fetchMock.mock.calls[0][1].headers).toEqual({ Authorization: "Bearer tkn" });
  });
  it("no_token_returns_unauthorized_without_fetch", async () => {
    expect(await getMe(B, async () => null)).toEqual({ kind: "unauthorized" });
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("maps_200_ok", async () => {
    fetchMock.mockResolvedValue(json(200, { data: profile }));
    expect(await getMe(B, tok)).toEqual({ kind: "ok", profile });
  });
  it("maps_404_no_profile", async () => {
    fetchMock.mockResolvedValue(err(404, "profile_not_found"));
    expect(await getMe(B, tok)).toEqual({ kind: "no_profile" });
  });
  it("maps_401", async () => {
    fetchMock.mockResolvedValue(err(401, "unauthorized"));
    expect(await getMe(B, tok)).toEqual({ kind: "unauthorized" });
  });
  it("maps_403", async () => {
    fetchMock.mockResolvedValue(err(403, "forbidden"));
    expect(await getMe(B, tok)).toEqual({ kind: "forbidden" });
  });
  it("maps_429", async () => {
    fetchMock.mockResolvedValue(json(429, { error: { code: "rate_limited", message: "m", request_id: "rid" } }, { "Retry-After": "7" }));
    expect(await getMe(B, tok)).toEqual({ kind: "rate_limited", retryAfter: 7, requestId: "rid" });
  });
  it("maps_500_with_request_id", async () => {
    fetchMock.mockResolvedValue(err(500, "internal"));
    expect(await getMe(B, tok)).toEqual({ kind: "error", status: 500, code: "internal", requestId: "rid" });
  });
  it("network_error_returns_error_after_one_retry", async () => {
    fetchMock.mockRejectedValue(new TypeError("fail"));
    expect(await getMe(B, tok)).toEqual({ kind: "error" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
  it("timeout_aborts", async () => {
    vi.useFakeTimers();
    fetchMock.mockImplementation((_u: string, init: RequestInit) =>
      new Promise((_, reject) => init.signal!.addEventListener("abort", () => reject(new DOMException("a", "AbortError")))));
    const p = getMe(B, tok);
    await vi.advanceTimersByTimeAsync(60_000);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(await p).toEqual({ kind: "error" });
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
  });
  it("uses_only_configured_base_url", async () => {
    fetchMock.mockResolvedValue(json(200, { data: profile }));
    await getMe(`${B}/`, tok);
    expect(fetchMock.mock.calls[0][0]).toBe(`${B}/v1/me`);
  });
});

describe("saveProfile", () => {
  const input = { username: "metal_fan", display_name: "Metal Fan" };
  it("save_sends_patch_json_with_bearer", async () => {
    fetchMock.mockResolvedValue(json(200, { data: profile }));
    await saveProfile(B, tok, input);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${B}/v1/me`);
    expect(init.method).toBe("PATCH");
    expect(init.headers).toEqual({ Authorization: "Bearer tkn", "Content-Type": "application/json" });
    expect(JSON.parse(init.body)).toEqual(input);
  });
  it("save_maps_200", async () => {
    fetchMock.mockResolvedValue(json(200, { data: profile }));
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "ok", profile });
  });
  it("save_maps_400_validation_with_fields", async () => {
    fetchMock.mockResolvedValue(err(400, "validation_error", { fields: { username: "too short" } }));
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "invalid", code: "validation_error", fields: { username: "too short" } });
  });
  it("save_maps_400_username_reserved", async () => {
    fetchMock.mockResolvedValue(err(400, "username_reserved"));
    expect(await saveProfile(B, tok, input)).toMatchObject({ kind: "invalid", code: "username_reserved" });
  });
  it("save_maps_409_username_taken", async () => {
    fetchMock.mockResolvedValue(err(409, "username_taken"));
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "username_taken" });
  });
  it("save_maps_403_suspended", async () => {
    fetchMock.mockResolvedValue(err(403, "profile_suspended", { details: { suspended_until: "2026-10-16T12:00:00Z" } }));
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "suspended", suspendedUntil: "2026-10-16T12:00:00Z" });
  });
  it("save_maps_401_and_403_forbidden", async () => {
    fetchMock.mockResolvedValueOnce(err(401, "unauthorized")).mockResolvedValueOnce(err(403, "forbidden"));
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "unauthorized" });
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "forbidden" });
  });
  it("save_network_error_does_not_retry", async () => {
    fetchMock.mockRejectedValue(new TypeError("fail"));
    expect(await saveProfile(B, tok, input)).toEqual({ kind: "error" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
