import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";
import { getMe, saveProfile } from "../api";
import type { Profile } from "../api";
import { MeProvider } from "./MeProvider";
import { ProfilePage } from "./ProfilePage";

const m = vi.hoisted(() => ({ supabase: null as unknown }));
vi.mock("../supabase", () => ({
  get supabase() { return m.supabase; },
  apiBaseUrl: "http://api.test",
}));
vi.mock("../api");

const profile: Profile = { id: "u1", username: "metal_fan", display_name: "Metal Fan", status: "active", moderation_reason: null, moderation_note: null, suspended_until: null, bio: null, gender: null, from_city: null, from_country: null, avatar_url: null, stats: { check_ins: 0, venues: 0 } };
const auth = { getSession: vi.fn(), signOut: vi.fn() };

beforeEach(() => {
  vi.resetAllMocks();
  auth.getSession.mockResolvedValue({ data: { session: { access_token: "tok" } } });
  m.supabase = { auth };
});

const setup = (me: Awaited<ReturnType<typeof getMe>>) => {
  vi.mocked(getMe).mockResolvedValue(me);
  return render(<MeProvider userId="u1"><ProfilePage email="fan@example.com" /></MeProvider>);
};
const loaded = async (me: Awaited<ReturnType<typeof getMe>> = { kind: "ok", profile }) => {
  setup(me);
  await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());
};

test("shows loaded values, email and status", async () => {
  await loaded();
  expect(screen.getByRole("heading", { name: "Profile", level: 1 })).toBeTruthy();
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe("metal_fan");
  expect((screen.getByLabelText("Display name") as HTMLInputElement).value).toBe("Metal Fan");
  expect(screen.getByText("fan@example.com")).toBeTruthy();
  expect(screen.getByText("Active")).toBeTruthy();
});

test("shows loading then the slow message", async () => {
  vi.useFakeTimers();
  vi.mocked(getMe).mockReturnValue(new Promise(() => {}));
  render(<MeProvider userId="u1"><ProfilePage email="a@b.c" /></MeProvider>);
  expect(screen.getByRole("status").textContent).toBe("Loading…");
  try {
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(screen.getByRole("status").textContent).toBe("Waking up the server…");
  } finally {
    vi.useRealTimers();
  }
});

test("create mode validates, then creates", async () => {
  await loaded({ kind: "no_profile" });
  const user = userEvent.setup();
  expect(screen.getByText("Signed in — no profile yet")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Create profile" }));
  expect(screen.getByText("Username must be 3-30 letters, numbers or underscores")).toBeTruthy();
  expect(screen.getByText("Display name must be 1-80 characters")).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByLabelText("Username"));
  expect(saveProfile).not.toHaveBeenCalled();
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile });
  await user.type(screen.getByLabelText("Username"), "metal_fan");
  await user.type(screen.getByLabelText("Display name"), "  Metal Fan ");
  await user.click(screen.getByRole("button", { name: "Create profile" }));
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { username: "metal_fan", display_name: "Metal Fan" });
  expect(await screen.findByText("Profile saved")).toBeTruthy();
  expect(screen.getByText("@metal_fan")).toBeTruthy();
});

test("edit and save shows saved values; unchanged disables Save", async () => {
  await loaded();
  const user = userEvent.setup();
  const save = screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement;
  expect(save.disabled).toBe(true);
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile: { ...profile, display_name: "Renamed" } });
  await user.clear(screen.getByLabelText("Display name"));
  await user.type(screen.getByLabelText("Display name"), "Renamed");
  expect(save.disabled).toBe(false);
  await user.click(save);
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { display_name: "Renamed" });
  expect(await screen.findByText("Profile saved")).toBeTruthy();
  expect(screen.getByText("Renamed", { selector: ".identity__name" })).toBeTruthy();
  expect(save.disabled).toBe(true);
});

test("cancel discards edits", async () => {
  await loaded();
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Display name"), "x");
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect((screen.getByLabelText("Display name") as HTMLInputElement).value).toBe("Metal Fan");
  expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
});

async function submitWith(res: Awaited<ReturnType<typeof saveProfile>>) {
  await loaded();
  const user = userEvent.setup();
  vi.mocked(saveProfile).mockResolvedValue(res);
  await user.type(screen.getByLabelText("Display name"), "x");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
}

test("username taken moves focus to Username", async () => {
  await submitWith({ kind: "username_taken" });
  expect(await screen.findByText("That username is taken")).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByLabelText("Username"));
});

test("400 with fields uses client wording, never server text, and focuses the field", async () => {
  await submitWith({ kind: "invalid", code: "validation_error", fields: { display_name: "Server says no" } });
  expect(await screen.findByText("Display name must be 1-80 characters")).toBeTruthy();
  expect(screen.queryByText("Server says no")).toBeNull();
  expect(screen.queryByText("Check the details and try again")).toBeNull();
  expect(document.activeElement).toBe(screen.getByLabelText("Display name"));
});

test("400 with only unknown fields falls back to the general message", async () => {
  await submitWith({ kind: "invalid", code: "validation_error", fields: { avatar_url: "<b>raw</b>" } });
  expect(await screen.findByText("Check the details and try again")).toBeTruthy();
  expect(screen.queryByText(/raw/)).toBeNull();
});

test("400 with known and unknown fields shows both the field error and the general message", async () => {
  await submitWith({ kind: "invalid", code: "validation_error", fields: { username: "nope", toString: "x" } });
  expect(await screen.findByText("Username must be 3-30 letters, numbers or underscores")).toBeTruthy();
  expect(screen.getByText("Check the details and try again")).toBeTruthy();
  expect(screen.queryByText("nope")).toBeNull();
});

test.each([
  ["80 characters pass", "a".repeat(80), true],
  ["81 characters fail", "a".repeat(81), false],
  ["80 emoji pass (counted as characters, not UTF-16 units)", "🤘".repeat(80), true],
])("display name length: %s", async (_, name, ok) => {
  await loaded();
  const user = userEvent.setup();
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile: { ...profile, display_name: name } });
  await user.clear(screen.getByLabelText("Display name"));
  await user.click(screen.getByLabelText("Display name"));
  await user.paste(name);
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  if (ok) {
    expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { display_name: name });
  } else {
    expect(screen.getByText("Display name must be 1-80 characters")).toBeTruthy();
    expect(saveProfile).not.toHaveBeenCalled();
  }
});

test("suspended on save reloads the profile and renders read-only", async () => {
  await loaded();
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile: { ...profile, status: "suspended" } });
  const user = userEvent.setup();
  vi.mocked(saveProfile).mockResolvedValue({ kind: "suspended" });
  await user.type(screen.getByLabelText("Display name"), "x");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(getMe).toHaveBeenCalledTimes(2));
  await waitFor(() => expect((screen.getByLabelText("Username") as HTMLInputElement).disabled).toBe(true));
  expect(screen.getByText(/so it can't be changed right now/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
});

test("create mode shows the account email read-only", async () => {
  await loaded({ kind: "no_profile" });
  expect(screen.getByText("fan@example.com")).toBeTruthy();
  expect(screen.queryByDisplayValue("fan@example.com")).toBeNull();
});

test("400 reserved", async () => {
  await submitWith({ kind: "invalid", code: "username_reserved" });
  expect(await screen.findByText("That username is reserved")).toBeTruthy();
});

test("save failure shows request id and Retry", async () => {
  await submitWith({ kind: "error", requestId: "req-1" });
  expect(await screen.findByText(/Something went wrong.*req-1/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
});

test("unauthorized on save signs out locally", async () => {
  await submitWith({ kind: "unauthorized" });
  await waitFor(() => expect(auth.signOut).toHaveBeenCalledWith({ scope: "local" }));
});

test("suspended profile is read-only and shows moderation text as plain text", async () => {
  await loaded({ kind: "ok", profile: { ...profile, status: "suspended", moderation_reason: "<b>spam</b>", moderation_note: "<img src=x>", suspended_until: "2030-01-02T03:04:05Z" } });
  expect((screen.getByLabelText("Username") as HTMLInputElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  expect(screen.getByText("Suspended", { selector: ".tag" })).toBeTruthy();
  expect(screen.getByText("<b>spam</b>")).toBeTruthy();
  expect(screen.getByText("<img src=x>")).toBeTruthy();
  expect(document.querySelector(".profile b, .profile img")).toBeNull();
});

test.each([
  ["rename_required", /Choose a new username/],
  ["suspended", /so it can't be changed right now/],
] as const)("%s shows a static notice, not a live alert", async (status, text) => {
  await loaded({ kind: "ok", profile: { ...profile, status } });
  expect(screen.getByText(text)).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("status")).toBeNull();
});

test("load failure shows Retry that reloads", async () => {
  await loaded({ kind: "error" });
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByLabelText("Username")).toBeTruthy();
});
