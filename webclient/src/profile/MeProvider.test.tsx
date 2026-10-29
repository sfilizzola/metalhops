import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { getMe } from "../api";
import { MeProvider, useMe } from "./MeProvider";

const m = vi.hoisted(() => ({ supabase: null as unknown }));
vi.mock("../supabase", () => ({
  get supabase() { return m.supabase; },
  apiBaseUrl: "http://api.test",
}));
vi.mock("../api");

const profile = { id: "u1", username: "metal_fan", display_name: "Metal Fan", status: "active" as const, moderation_reason: null, moderation_note: null, suspended_until: null, bio: null, gender: null, from_city: null, from_country: null, avatar_url: null, stats: { check_ins: 0, venues: 0 } };
const auth = { getSession: vi.fn(), signOut: vi.fn() };

function Probe() {
  const { me, slow, reload, setProfile, getToken } = useMe();
  return (
    <>
      <p data-testid="state">{me ? me.kind : "loading"}{slow ? " slow" : ""}</p>
      {me?.kind === "ok" && <p>{me.profile.display_name}</p>}
      <button onClick={reload}>reload</button>
      <button onClick={() => setProfile({ ...profile, display_name: "Renamed" })}>set</button>
      <button onClick={async () => document.body.setAttribute("data-token", String(await getToken()))}>token</button>
    </>
  );
}

const ui = (userId = "u1") => <MeProvider userId={userId}><Probe /></MeProvider>;

beforeEach(() => {
  vi.resetAllMocks();
  m.supabase = { auth };
  auth.getSession.mockResolvedValue({ data: { session: { access_token: "tok" } } });
  auth.signOut.mockResolvedValue({ error: null });
});
afterEach(() => { vi.useRealTimers(); });

test("ok exposes the profile", async () => {
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  render(ui());
  expect(await screen.findByText("Metal Fan")).toBeInTheDocument();
  expect(getMe).toHaveBeenCalledWith("http://api.test", expect.any(Function), expect.any(AbortSignal));
});

test("no_profile is exposed", async () => {
  vi.mocked(getMe).mockResolvedValue({ kind: "no_profile" });
  render(ui());
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("no_profile"));
});

test("unauthorized signs out locally and stores nothing", async () => {
  vi.mocked(getMe).mockResolvedValue({ kind: "unauthorized" });
  render(ui());
  await waitFor(() => expect(auth.signOut).toHaveBeenCalledWith({ scope: "local" }));
  expect(screen.getByTestId("state")).toHaveTextContent("loading");
});

test("a rejected load becomes an error instead of loading forever", async () => {
  vi.mocked(getMe).mockRejectedValue(new Error("getSession failed"));
  render(ui());
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("error"));
});

test("slow flag appears after 5 seconds", async () => {
  vi.useFakeTimers();
  vi.mocked(getMe).mockReturnValue(new Promise(() => {}));
  render(ui());
  expect(screen.getByTestId("state")).toHaveTextContent(/^loading$/);
  await act(() => vi.advanceTimersByTimeAsync(5000));
  expect(screen.getByTestId("state")).toHaveTextContent("loading slow");
});

test("reload refetches", async () => {
  vi.mocked(getMe).mockResolvedValue({ kind: "error" });
  render(ui());
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("error"));
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  await userEvent.click(screen.getByText("reload"));
  expect(await screen.findByText("Metal Fan")).toBeInTheDocument();
  expect(getMe).toHaveBeenCalledTimes(2);
});

test("setProfile replaces state, user change refetches, getToken reads fresh", async () => {
  vi.mocked(getMe).mockResolvedValue({ kind: "no_profile" });
  const { rerender } = render(ui());
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("no_profile"));
  await userEvent.click(screen.getByText("set"));
  expect(screen.getByText("Renamed")).toBeInTheDocument();
  await userEvent.click(screen.getByText("token"));
  await waitFor(() => expect(document.body).toHaveAttribute("data-token", "tok"));
  rerender(ui("u2"));
  await waitFor(() => expect(getMe).toHaveBeenCalledTimes(2));
});
