import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";
import App from "./App";
import { getMe, saveProfile } from "./api";

const m = vi.hoisted(() => ({ supabase: null as unknown, cb: null as null | ((e: string, s: unknown) => void) }));
vi.mock("./supabase", () => ({
  get supabase() { return m.supabase; },
  apiBaseUrl: "http://api.test",
}));
vi.mock("./api");

const TOKEN = "secret-jwt-token";
const session = { access_token: TOKEN, user: { id: "u1", email: "me@example.com" } };
const profile = { id: "u1", username: "metal_fan", display_name: "Metal Fan", status: "active" as const, moderation_reason: null, moderation_note: null, suspended_until: null };

const auth = {
  getSession: vi.fn(),
  onAuthStateChange: vi.fn(),
  signInWithPassword: vi.fn(),
  signUp: vi.fn(),
  signOut: vi.fn(),
};

const emit = (s: unknown) => act(() => m.cb!(s ? "INITIAL_SESSION" : "SIGNED_OUT", s));

beforeEach(() => {
  vi.resetAllMocks();
  m.supabase = { auth };
  auth.getSession.mockResolvedValue({ data: { session } });
  auth.onAuthStateChange.mockImplementation((cb) => {
    m.cb = cb;
    return { data: { subscription: { unsubscribe: vi.fn() } } };
  });
  auth.signOut.mockResolvedValue({ error: null });
});

async function signedIn(me: Awaited<ReturnType<typeof getMe>> = { kind: "ok", profile }) {
  vi.mocked(getMe).mockResolvedValue(me);
  render(<App />);
  emit(session);
}

async function fillAuth(email: string, password: string) {
  const u = userEvent.setup();
  await u.type(screen.getByLabelText("Email"), email);
  await u.type(screen.getByLabelText("Password"), password);
  return u;
}

test("shows_loading_until_initial_session", () => {
  render(<App />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading");
});

test("shows_login_when_no_session", async () => {
  render(<App />);
  emit(null);
  expect(await screen.findByLabelText("Email")).toBeInTheDocument();
  expect(screen.getByLabelText("Password")).toBeInTheDocument();
});

test("failed_login_shows_generic_error_and_reenables_button", async () => {
  auth.signInWithPassword.mockResolvedValue({ data: {}, error: { message: "User not found" } });
  render(<App />);
  emit(null);
  const u = await fillAuth("a@b.co", "whatever1");
  await u.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Invalid email or password");
  expect(screen.queryByText(/not found/i)).toBeNull();
  expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
});

test("login_button_disabled_while_pending", async () => {
  let done!: (v: unknown) => void;
  auth.signInWithPassword.mockReturnValue(new Promise((r) => { done = r; }));
  render(<App />);
  emit(null);
  const u = await fillAuth("a@b.co", "whatever1");
  await u.click(screen.getByRole("button", { name: "Sign in" }));
  expect(screen.getByRole("button", { name: "Sign in" })).toBeDisabled();
  done({ data: {}, error: { message: "x" } });
  await waitFor(() => expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled());
});

test("success_shows_profile", async () => {
  await signedIn();
  expect(await screen.findByText("Metal Fan")).toBeInTheDocument();
  expect(screen.getByText("@metal_fan")).toBeInTheDocument();
  expect(screen.getByText(/active/)).toBeInTheDocument();
  expect(screen.getByText(/me@example.com/)).toBeInTheDocument();
});

test("404_shows_no_profile_state", async () => {
  await signedIn({ kind: "no_profile" });
  expect(await screen.findByText(/no profile yet/)).toBeInTheDocument();
  expect(screen.getByLabelText("Username")).toBeInTheDocument();
});

test("401_signs_out_locally_and_shows_login", async () => {
  await signedIn({ kind: "unauthorized" });
  await waitFor(() => expect(auth.signOut).toHaveBeenCalledWith({ scope: "local" }));
  emit(null);
  expect(await screen.findByLabelText("Email")).toBeInTheDocument();
});

test("403_shows_unavailable_message", async () => {
  await signedIn({ kind: "forbidden" });
  expect(await screen.findByRole("alert")).toHaveTextContent("This account can't use the app");
});

test("logout_calls_signOut", async () => {
  await signedIn();
  await screen.findByText("Metal Fan");
  await userEvent.click(screen.getByRole("button", { name: "Log out" }));
  expect(auth.signOut).toHaveBeenCalledWith();
});

test("token_never_rendered", async () => {
  await signedIn();
  await screen.findByText("Metal Fan");
  expect(document.body.innerHTML).not.toContain(TOKEN);
});

test("signup_shows_check_email_when_no_session", async () => {
  auth.signUp.mockResolvedValue({ data: { session: null, user: {} }, error: null });
  render(<App />);
  emit(null);
  await userEvent.click(await screen.findByRole("button", { name: /Need an account/ }));
  const user = await fillAuth("a@b.co", "longenough1");
  await user.click(screen.getAllByRole("button", { name: "Sign up" })[0]);
  expect(await screen.findByRole("status")).toHaveTextContent("Check your email to confirm your account");
});

test("signup_signs_in_when_session_returned", async () => {
  auth.signUp.mockImplementation(async () => {
    emit(session);
    return { data: { session }, error: null };
  });
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  render(<App />);
  emit(null);
  await userEvent.click(await screen.findByRole("button", { name: /Need an account/ }));
  const user = await fillAuth("a@b.co", "longenough1");
  await user.click(screen.getAllByRole("button", { name: "Sign up" })[0]);
  expect(await screen.findByText("Metal Fan")).toBeInTheDocument();
  expect(screen.queryByText(/Check your email/)).toBeNull();
});

test("signup_generic_error", async () => {
  auth.signUp.mockResolvedValue({ data: {}, error: { message: "User already registered" } });
  render(<App />);
  emit(null);
  await userEvent.click(await screen.findByRole("button", { name: /Need an account/ }));
  const user = await fillAuth("a@b.co", "longenough1");
  await user.click(screen.getAllByRole("button", { name: "Sign up" })[0]);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not create the account. Check your details and try again.");
  expect(screen.queryByText(/already registered/i)).toBeNull();
});

async function profileFormSubmit(username: string, name: string) {
  await signedIn({ kind: "no_profile" });
  const u = userEvent.setup();
  await u.type(await screen.findByLabelText("Username"), username);
  await u.type(screen.getByLabelText("Display name"), name);
  await u.click(screen.getByRole("button", { name: "Create profile" }));
}

test("create_profile_success", async () => {
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile });
  await profileFormSubmit("metal_fan", "  Metal Fan ");
  expect(await screen.findByText("@metal_fan")).toBeInTheDocument();
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { username: "metal_fan", display_name: "Metal Fan" });
});

test("create_profile_client_validation_blocks_submit", async () => {
  await profileFormSubmit("a!", "   ");
  expect(await screen.findByText(/Username must be 3-30/)).toBeInTheDocument();
  expect(screen.getByText(/Display name must be 1-80/)).toBeInTheDocument();
  expect(saveProfile).not.toHaveBeenCalled();
});

test("create_profile_username_taken", async () => {
  vi.mocked(saveProfile).mockResolvedValue({ kind: "username_taken" });
  await profileFormSubmit("metal_fan", "Metal Fan");
  expect(await screen.findByText("That username is taken")).toBeInTheDocument();
});

test("create_profile_server_validation_fields", async () => {
  vi.mocked(saveProfile).mockResolvedValue({ kind: "invalid", code: "validation_error", fields: { display_name: "server says no" } });
  await profileFormSubmit("metal_fan", "Metal Fan");
  expect(await screen.findByText("server says no")).toBeInTheDocument();
});

test("missing_config_shows_message", () => {
  m.supabase = null;
  render(<App />);
  expect(screen.getByRole("alert")).toHaveTextContent(/VITE_SUPABASE_URL.*VITE_SUPABASE_PUBLISHABLE_KEY/);
});
