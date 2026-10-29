import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { getMe, saveProfile } from "../api";
import type { Profile } from "../api";
import { MeProvider } from "./MeProvider";
import { ProfileEditPage } from "./ProfileEditPage";
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

afterEach(() => vi.restoreAllMocks());

function Loc() {
  return <div data-testid="loc">{useLocation().pathname}</div>;
}
const loc = () => screen.getByTestId("loc").textContent;

const renderAt = (path: string) => render(
  <MemoryRouter initialEntries={[path]}>
    <MeProvider userId="u1">
      <Routes>
        <Route path="/profile" element={<ProfilePage email="fan@example.com" />} />
        <Route path="/profile/edit" element={<ProfileEditPage email="fan@example.com" />} />
      </Routes>
      <Loc />
    </MeProvider>
  </MemoryRouter>,
);
const setup = (me: Awaited<ReturnType<typeof getMe>>, path = "/profile/edit") => {
  vi.mocked(getMe).mockResolvedValue(me);
  return renderAt(path);
};
// Most tests here exercise the form, so they start on the edit screen.
const loaded = async (me: Awaited<ReturnType<typeof getMe>> = { kind: "ok", profile }, path = "/profile/edit") => {
  setup(me, path);
  await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());
};

test("user page shows the name as h1, email and status, with no form fields", async () => {
  await loaded(undefined, "/profile");
  expect(screen.getByRole("heading", { name: "Metal Fan", level: 1 })).toBeTruthy();
  expect(screen.getByText("fan@example.com")).toBeTruthy();
  expect(screen.getByText("Active")).toBeTruthy();
  expect(document.querySelector("input, select, textarea")).toBeNull();
  expect(screen.queryByRole("button", { name: /Save/ })).toBeNull();
  expect(screen.getByRole("link", { name: "Edit profile" }).getAttribute("href")).toBe("/profile/edit");
});

test("edit screen shows loaded values", async () => {
  await loaded();
  expect(screen.getByRole("heading", { name: "Edit profile", level: 1 })).toBeTruthy();
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe("metal_fan");
  expect((screen.getByLabelText("Display name") as HTMLInputElement).value).toBe("Metal Fan");
});

test.each(["/profile", "/profile/edit"])("%s shows loading then the slow message, and never an empty form", async (path) => {
  vi.useFakeTimers();
  vi.mocked(getMe).mockReturnValue(new Promise(() => {}));
  renderAt(path);
  expect(screen.getByRole("status").textContent).toBe("Loading…");
  expect(document.querySelector("form, input")).toBeNull();
  expect(document.querySelectorAll(".skeleton").length).toBeGreaterThan(0);
  try {
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(screen.getByRole("status").textContent).toBe("Waking up the server…");
  } finally {
    vi.useRealTimers();
  }
});

test("no_profile on /profile redirects to the create form on /profile/edit", async () => {
  await loaded({ kind: "no_profile" }, "/profile");
  await waitFor(() => expect(loc()).toBe("/profile/edit"));
  expect(screen.getByLabelText("Username")).toBeTruthy();
  expect(screen.getByLabelText("Display name")).toBeTruthy();
  expect(screen.queryByLabelText("Bio")).toBeNull();
  expect(screen.queryByLabelText("From city")).toBeNull();
  expect(screen.queryByRole("link", { name: "Back to profile" })).toBeNull();
});

test("create mode validates, then creates and lands on /profile with Profile saved", async () => {
  await loaded({ kind: "no_profile" }, "/profile");
  const user = userEvent.setup();
  expect(await screen.findByText("Signed in — no profile yet")).toBeTruthy();
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
  expect(loc()).toBe("/profile");
  expect(screen.getByText("@metal_fan")).toBeTruthy();
});

test("edit and save returns to /profile with saved values; unchanged disables Save", async () => {
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
  expect(loc()).toBe("/profile");
  expect(screen.getByRole("heading", { name: "Renamed", level: 1 })).toBeTruthy();
});

test("Profile saved shows once: the router state is cleared so a reload does not repeat it", async () => {
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  render(
    <MemoryRouter initialEntries={[{ pathname: "/profile", state: { saved: true } }]}>
      <MeProvider userId="u1">
        <Routes><Route path="/profile" element={<ProfilePage email="fan@example.com" />} /></Routes>
        <StateProbe />
      </MeProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("Profile saved")).toBeTruthy();
  await waitFor(() => expect(screen.getByTestId("state").textContent).toBe("null"));
  expect(screen.getByText("Profile saved")).toBeTruthy();
});

function StateProbe() {
  return <div data-testid="state">{JSON.stringify(useLocation().state)}</div>;
}

test("cancel without changes returns to /profile without asking or saving", async () => {
  await loaded();
  const confirm = vi.spyOn(window, "confirm");
  await userEvent.setup().click(screen.getByRole("button", { name: "Cancel" }));
  expect(loc()).toBe("/profile");
  expect(confirm).not.toHaveBeenCalled();
  expect(saveProfile).not.toHaveBeenCalled();
});

test.each([
  ["Cancel", "button"],
  ["Back to profile", "link"],
] as const)("%s with changes asks first; declining stays, accepting leaves without saving", async (name, role) => {
  await loaded();
  const user = userEvent.setup();
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  await user.type(screen.getByLabelText("Display name"), "x");
  await user.click(screen.getByRole(role, { name }));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(loc()).toBe("/profile/edit");
  expect((screen.getByLabelText("Display name") as HTMLInputElement).value).toBe("Metal Fanx");
  confirm.mockReturnValue(true);
  await user.click(screen.getByRole(role, { name }));
  expect(loc()).toBe("/profile");
  expect(saveProfile).not.toHaveBeenCalled();
});

test("beforeunload is registered only while the form is dirty", async () => {
  const add = vi.spyOn(window, "addEventListener");
  const remove = vi.spyOn(window, "removeEventListener");
  await loaded();
  const unloads = (spy: typeof add) => spy.mock.calls.filter(([t]) => t === "beforeunload").length;
  expect(unloads(add)).toBe(0);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Display name"), "x");
  expect(unloads(add)).toBe(1);
  await user.type(screen.getByLabelText("Display name"), "{Backspace}");
  expect(unloads(remove)).toBe(1);
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

test("suspended profile edits read-only; the user page shows moderation text as plain text", async () => {
  const suspended = { ...profile, status: "suspended" as const, moderation_reason: "<b>spam</b>", moderation_note: "<img src=x>", suspended_until: "2030-01-02T03:04:05Z" };
  await loaded({ kind: "ok", profile: suspended });
  expect((screen.getByLabelText("Username") as HTMLInputElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  expect(document.querySelector(".profile-edit b, .profile-edit img")).toBeNull();
  await userEvent.setup().click(screen.getByRole("link", { name: "Back to profile" }));
  expect(screen.getByText("Suspended", { selector: ".tag" })).toBeTruthy();
  expect(screen.getByText("<b>spam</b>")).toBeTruthy();
  expect(screen.getByText("<img src=x>")).toBeTruthy();
  expect(document.querySelector(".profile-view b, .profile-view img")).toBeNull();
});

test.each([
  ["rename_required", /Choose a new username/],
  ["suspended", /so it can't be changed right now/],
] as const)("%s shows a static notice, not a live alert, on both screens", async (status, text) => {
  await loaded({ kind: "ok", profile: { ...profile, status } });
  expect(screen.getByText(text)).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("status")).toBeNull();
  await userEvent.setup().click(screen.getByRole("link", { name: "Back to profile" }));
  expect(screen.getByText(text)).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});

test("load failure shows Retry that reloads", async () => {
  await loaded({ kind: "error" });
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByLabelText("Username")).toBeTruthy();
});
