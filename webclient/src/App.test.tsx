import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import App from "./App";
import { getMe } from "./api";

const m = vi.hoisted(() => ({ supabase: null as unknown, api: "", cb: null as null | ((e: string, s: unknown) => void) }));
vi.mock("./supabase", () => ({
  get supabase() { return m.supabase; },
  get apiBaseUrl() { return m.api; },
}));
vi.mock("./api");

const TOKEN = "secret-jwt-token";
const session = { access_token: TOKEN, user: { id: "u1", email: "me@example.com" } };
const profile = { id: "u1", username: "metal_fan", display_name: "Metal Fan", status: "active" as const, moderation_reason: null, moderation_note: null, suspended_until: null, bio: null, gender: null, from_city: null, from_country: null, avatar_url: null, stats: { check_ins: 0, venues: 0 } };

const auth = {
  getSession: vi.fn(),
  onAuthStateChange: vi.fn(),
  signInWithPassword: vi.fn(),
  signUp: vi.fn(),
  signOut: vi.fn(),
};

const PROTECTED = ["/", "/discover", "/check-ins", "/my-venues", "/profile"];
const SECTIONS = [
  { path: "/", label: "Home", title: "Home" },
  { path: "/discover", label: "Discover", title: "Discover" },
  { path: "/check-ins", label: "Check-ins", title: "Check-ins" },
  { path: "/my-venues", label: "My venues", title: "My venues" },
  { path: "/profile", label: "Profile", title: "Profile" },
];

const emit = (s: unknown) => act(() => m.cb!(s ? "SIGNED_IN" : "SIGNED_OUT", s));

function Loc() {
  return <div data-testid="loc">{useLocation().pathname}</div>;
}
const loc = () => screen.getByTestId("loc").textContent;

function renderAt(path: string) {
  return render(<MemoryRouter initialEntries={[path]}><App /><Loc /></MemoryRouter>);
}

beforeEach(() => {
  vi.resetAllMocks();
  m.supabase = { auth };
  m.api = "http://api.test";
  auth.getSession.mockResolvedValue({ data: { session } });
  auth.onAuthStateChange.mockImplementation((cb) => {
    m.cb = cb;
    return { data: { subscription: { unsubscribe: vi.fn() } } };
  });
  auth.signOut.mockResolvedValue({ error: null });
});
afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

function signedInAt(path: string, me: Awaited<ReturnType<typeof getMe>> = { kind: "ok", profile }) {
  vi.mocked(getMe).mockResolvedValue(me);
  renderAt(path);
  emit(session);
}

function signedOutAt(path: string) {
  renderAt(path);
  emit(null);
}

async function fillAuth(email: string, password: string) {
  const u = userEvent.setup();
  if (email) await u.type(screen.getByLabelText("Email"), email);
  if (password) await u.type(screen.getByLabelText("Password"), password);
  return u;
}

// jsdom applies shell.css but not its 960px media query, so the desktop sidebar is display:none here; query it with hidden: true.
const sidebarEl = () => document.querySelector<HTMLElement>("aside.shell__sidebar")!;
const sidebarNav = () => within(within(sidebarEl()).getByRole("navigation", { name: "Main", hidden: true }));

// ---------- Auth ----------

test("shows_loading_until_initial_session_without_login_flash", () => {
  renderAt("/");
  expect(screen.getByRole("status")).toHaveTextContent("Loading");
  expect(screen.queryByLabelText("Email")).toBeNull();
  expect(auth.onAuthStateChange).toHaveBeenCalledTimes(1);
});

test.each(PROTECTED)("unauthed_visit_to_%s_redirects_to_login", (path) => {
  signedOutAt(path);
  expect(loc()).toBe("/login");
  expect(screen.getByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
  expect(screen.getByLabelText("Email")).toBeInTheDocument();
  expect(screen.getByLabelText("Password")).toBeInTheDocument();
});

test.each(["/login", "/signup"])("authed_visit_to_%s_redirects_home", (path) => {
  signedInAt(path);
  expect(loc()).toBe("/");
  expect(screen.getByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
});

test("login_success_signs_in_and_goes_home", async () => {
  auth.signInWithPassword.mockImplementation(async () => {
    emit(session);
    return { data: { session }, error: null };
  });
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  signedOutAt("/login");
  const u = await fillAuth(" a@b.co ", "whatever1");
  await u.click(screen.getByRole("button", { name: "Sign in" }));
  expect(auth.signInWithPassword).toHaveBeenCalledWith({ email: "a@b.co", password: "whatever1" });
  expect(await screen.findByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
  expect(loc()).toBe("/");
});

test("failed_login_shows_generic_error_and_reenables_button", async () => {
  auth.signInWithPassword.mockResolvedValue({ data: {}, error: { message: "User not found" } });
  signedOutAt("/login");
  const u = await fillAuth("a@b.co", "whatever1");
  await u.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Invalid email or password");
  expect(screen.queryByText(/not found/i)).toBeNull();
  expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
});

test.each([["/login", "Sign in", "Invalid email or password"], ["/signup", "Sign up", "Could not create the account"]])("%s_thrown_error_shows_generic_message_and_reenables_button", async (path, name, message) => {
  auth.signInWithPassword.mockRejectedValue(new Error("fetch failed"));
  auth.signUp.mockRejectedValue(new Error("fetch failed"));
  signedOutAt(path);
  const u = await fillAuth("a@b.co", "longenough1");
  await u.click(screen.getByRole("button", { name }));
  expect(await screen.findByRole("alert")).toHaveTextContent(message);
  expect(screen.queryByText(/fetch failed/)).toBeNull();
  expect(screen.getByRole("button", { name })).toBeEnabled();
});

test("login_with_empty_email_shows_generic_error_without_calling_supabase", async () => {
  signedOutAt("/login");
  await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
  expect(screen.getByRole("alert")).toHaveTextContent("Invalid email or password");
  expect(auth.signInWithPassword).not.toHaveBeenCalled();
});

test("login_button_disabled_while_pending", async () => {
  let done!: (v: unknown) => void;
  auth.signInWithPassword.mockReturnValue(new Promise((r) => { done = r; }));
  signedOutAt("/login");
  const u = await fillAuth("a@b.co", "whatever1");
  await u.click(screen.getByRole("button", { name: "Sign in" }));
  expect(screen.getByRole("button", { name: "Sign in" })).toBeDisabled();
  done({ data: {}, error: { message: "x" } });
  await waitFor(() => expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled());
});

test("login_and_signup_link_to_each_other", async () => {
  signedOutAt("/login");
  await userEvent.click(screen.getByRole("link", { name: /Need an account/ }));
  expect(loc()).toBe("/signup");
  expect(screen.getByRole("heading", { level: 1, name: "Sign up" })).toBeInTheDocument();
  await userEvent.click(screen.getByRole("link", { name: /Have an account/ }));
  expect(loc()).toBe("/login");
});

test("signup_shows_check_email_when_no_session", async () => {
  auth.signUp.mockResolvedValue({ data: { session: null, user: {} }, error: null });
  signedOutAt("/signup");
  const u = await fillAuth("a@b.co", "longenough1");
  await u.click(screen.getByRole("button", { name: "Sign up" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Check your email to confirm your account");
  expect(loc()).toBe("/signup");
});

test("signup_signs_in_when_session_returned", async () => {
  auth.signUp.mockImplementation(async () => {
    emit(session);
    return { data: { session }, error: null };
  });
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  signedOutAt("/signup");
  const u = await fillAuth("a@b.co", "longenough1");
  await u.click(screen.getByRole("button", { name: "Sign up" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
  expect(screen.queryByText(/Check your email/)).toBeNull();
});

test("signup_generic_error", async () => {
  auth.signUp.mockResolvedValue({ data: {}, error: { message: "User already registered" } });
  signedOutAt("/signup");
  const u = await fillAuth("a@b.co", "longenough1");
  await u.click(screen.getByRole("button", { name: "Sign up" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not create the account. Check your details and try again.");
  expect(screen.queryByText(/already registered/i)).toBeNull();
});

test("signup_client_validation_blocks_short_password", async () => {
  signedOutAt("/signup");
  const u = await fillAuth("a@b.co", "short");
  await u.click(screen.getByRole("button", { name: "Sign up" }));
  expect(screen.getByRole("alert")).toHaveTextContent("Enter an email and a password of at least 8 characters.");
  expect(auth.signUp).not.toHaveBeenCalled();
});

test("logout_calls_signOut_and_returns_to_login", async () => {
  signedInAt("/discover");
  fireEvent.click(within(sidebarEl()).getByRole("button", { name: "Log out", hidden: true }));
  expect(auth.signOut).toHaveBeenCalledWith();
  emit(null);
  expect(loc()).toBe("/login");
  expect(screen.getByLabelText("Email")).toBeInTheDocument();
  expect(auth.signOut).toHaveBeenCalledTimes(1);
});

test("logout_falls_back_to_local_signout_when_global_fails", async () => {
  auth.signOut.mockResolvedValueOnce({ error: { message: "network" } });
  signedInAt("/discover");
  fireEvent.click(within(sidebarEl()).getByRole("button", { name: "Log out", hidden: true }));
  await waitFor(() => expect(auth.signOut).toHaveBeenLastCalledWith({ scope: "local" }));
  expect(auth.signOut).toHaveBeenCalledTimes(2);
});

test("missing_config_shows_message", () => {
  m.supabase = null;
  renderAt("/");
  expect(screen.getByRole("alert")).toHaveTextContent(/VITE_SUPABASE_URL.*VITE_SUPABASE_PUBLISHABLE_KEY.*VITE_API_BASE_URL/);
});

test("missing_api_base_url_shows_message_and_sends_nothing", () => {
  m.api = "";
  renderAt("/");
  expect(screen.getByRole("alert")).toHaveTextContent(/VITE_API_BASE_URL/);
  expect(auth.getSession).not.toHaveBeenCalled();
  expect(getMe).not.toHaveBeenCalled();
});

test("non_https_api_base_url_outside_dev_shows_message", () => {
  vi.stubEnv("DEV", false);
  renderAt("/");
  expect(screen.getByRole("alert")).toHaveTextContent(/VITE_API_BASE_URL/);
  expect(getMe).not.toHaveBeenCalled();
});

test("https_api_base_url_outside_dev_is_accepted", () => {
  vi.stubEnv("DEV", false);
  m.api = "https://api.test";
  renderAt("/");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(auth.onAuthStateChange).toHaveBeenCalled();
});

// ---------- Profile through the shell ----------

test("profile_route_shows_profile_and_email", async () => {
  signedInAt("/profile");
  expect(await screen.findByText("me@example.com")).toBeInTheDocument();
  expect(within(screen.getByRole("main")).getByText("@metal_fan")).toBeInTheDocument();
});

test("404_shows_no_profile_state", async () => {
  signedInAt("/profile", { kind: "no_profile" });
  expect(await screen.findByText(/no profile yet/)).toBeInTheDocument();
  expect(screen.getByLabelText("Username")).toBeInTheDocument();
});

test("401_signs_out_locally_and_shows_login", async () => {
  signedInAt("/profile", { kind: "unauthorized" });
  await waitFor(() => expect(auth.signOut).toHaveBeenCalledWith({ scope: "local" }));
  emit(null);
  expect(await screen.findByLabelText("Email")).toBeInTheDocument();
});

test("403_shows_unavailable_message", async () => {
  signedInAt("/profile", { kind: "forbidden" });
  expect(await screen.findByRole("alert")).toHaveTextContent("This account can't use the app");
});

test("token_never_rendered", async () => {
  signedInAt("/profile");
  await screen.findByText("me@example.com");
  expect(document.body.innerHTML).not.toContain(TOKEN);
});

// ---------- Shell and navigation ----------

test("sidebar_has_five_nav_links_in_order_with_hrefs", () => {
  signedInAt("/");
  const links = sidebarNav().getAllByRole("link", { hidden: true });
  expect(links.map((a) => [a.textContent, a.getAttribute("href")])).toEqual(SECTIONS.map((s) => [s.label, s.path]));
  expect(within(sidebarEl()).getByText("MetalHops")).toBeInTheDocument();
  expect(screen.getAllByRole("main")).toHaveLength(1);
});

test.each(SECTIONS)("route $path marks its nav link active and renders its h1", ({ path, label, title }) => {
  signedInAt(path);
  const nav = sidebarNav();
  expect(nav.getByRole("link", { name: label, hidden: true })).toHaveAttribute("aria-current", "page");
  for (const other of SECTIONS.filter((s) => s.path !== path)) {
    expect(nav.getByRole("link", { name: other.label, hidden: true })).not.toHaveAttribute("aria-current");
  }
  expect(screen.getByRole("heading", { level: 1, name: title })).toBeInTheDocument();
});

test.each(SECTIONS.slice(0, 4))("route $path shows under construction", ({ path, title }) => {
  signedInAt(path);
  const main = within(screen.getByRole("main"));
  expect(main.getByText("This part of MetalHops is under construction.")).toBeInTheDocument();
  expect(main.getByRole("link", { name: "Go to Profile" })).toHaveAttribute("href", "/profile");
  if (path === "/") expect(main.queryByRole("link", { name: "Go to Home" })).toBeNull();
  else expect(main.getByRole("link", { name: "Go to Home" })).toHaveAttribute("href", "/");
  expect(document.title).toBe(`${title} · MetalHops`);
});

test("unknown_path_redirects_home", () => {
  signedInAt("/no/such/page");
  expect(loc()).toBe("/");
  expect(screen.getByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
});

test("identity_shows_initials_name_and_username", async () => {
  signedInAt("/");
  const identity = await within(sidebarEl()).findByRole("link", { name: /Metal Fan/, hidden: true });
  expect(identity).toHaveAttribute("href", "/profile");
  expect(identity).toHaveTextContent("MF");
  expect(identity).toHaveTextContent("@metal_fan");
});

test("identity_without_profile_links_to_profile", async () => {
  signedInAt("/", { kind: "no_profile" });
  const identity = await within(sidebarEl()).findByRole("link", { name: /No profile yet/, hidden: true });
  expect(identity).toHaveAttribute("href", "/profile");
  expect(identity).toHaveTextContent("?");
});

// ---------- Mobile drawer ----------

async function openDrawer() {
  const u = userEvent.setup();
  const button = screen.getByRole("button", { name: "Open menu" });
  expect(button).toHaveAttribute("aria-expanded", "false");
  expect(button).toHaveAttribute("aria-controls", "mobile-drawer");
  await u.click(button);
  const dialog = screen.getByRole("dialog", { name: "Menu" });
  return { u, button, dialog };
}

test("drawer_opens_as_modal_dialog_and_moves_focus_in", async () => {
  signedInAt("/");
  const { button, dialog } = await openDrawer();
  expect(dialog).toHaveAttribute("aria-modal", "true");
  expect(dialog).toHaveAttribute("id", "mobile-drawer");
  expect(button).toHaveAttribute("aria-expanded", "true");
  expect(button).toHaveAccessibleName("Close menu");
  expect(dialog).toContainElement(document.activeElement as HTMLElement);
  expect(within(dialog).getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(expect.arrayContaining(PROTECTED));
});

test("drawer_closes_on_escape_and_returns_focus", async () => {
  signedInAt("/");
  const { u, button } = await openDrawer();
  await u.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(button).toHaveAttribute("aria-expanded", "false");
  expect(button).toHaveFocus();
});

test("drawer_closes_on_scrim_click", async () => {
  signedInAt("/");
  const { u, button } = await openDrawer();
  await u.click(screen.getByTestId("drawer-scrim"));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(button).toHaveFocus();
});

test("drawer_closes_on_nav_selection_and_navigates", async () => {
  signedInAt("/");
  const { u, button } = await openDrawer();
  await u.click(within(screen.getByRole("dialog")).getByRole("link", { name: "Discover" }));
  expect(loc()).toBe("/discover");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(button).toHaveFocus();
});

test("drawer_closes_on_route_change", async () => {
  signedInAt("/discover");
  await openDrawer();
  fireEvent.click(within(screen.getByRole("main")).getByRole("link", { name: "Go to Home" }));
  expect(loc()).toBe("/");
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("drawer_locks_body_scroll_while_open", async () => {
  signedInAt("/");
  const { u } = await openDrawer();
  expect(document.body.style.overflow).toBe("hidden");
  await u.keyboard("{Escape}");
  expect(document.body.style.overflow).toBe("");
});

test("drawer_closes_when_viewport_reaches_desktop", async () => {
  let onChange!: (e: { matches: boolean }) => void;
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: (_: string, cb: typeof onChange) => { onChange = cb; }, removeEventListener: vi.fn() }));
  signedInAt("/");
  await openDrawer();
  act(() => onChange({ matches: true }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.body.style.overflow).toBe("");
});

test("drawer_traps_focus_with_tab_and_shift_tab", async () => {
  signedInAt("/");
  const { u, dialog } = await openDrawer();
  const items = [...dialog.querySelectorAll<HTMLElement>("a[href], button")];
  const first = items[0];
  const last = items[items.length - 1];
  expect(first).toHaveFocus();
  await u.tab({ shift: true });
  expect(last).toHaveFocus();
  await u.tab();
  expect(first).toHaveFocus();
});
