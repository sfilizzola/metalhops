import { render, screen, waitFor, within } from "@testing-library/react";
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

const empty: Profile = {
  id: "u1", username: "metal_fan", display_name: "Metal Fan", status: "active", moderation_reason: null, moderation_note: null, suspended_until: null,
  bio: null, gender: null, from_city: null, from_country: null, avatar_url: null, stats: { check_ins: 0, venues: 0 },
};
const full: Profile = {
  ...empty, bio: "Louder\nthan hell", gender: "non_binary", from_city: "Hamburg", from_country: "DE",
  avatar_url: "https://x.supabase.co/storage/v1/object/public/avatars/u1/old.webp", stats: { check_ins: 3, venues: 2 },
};

const storage = { upload: vi.fn(), getPublicUrl: vi.fn(), remove: vi.fn() };
const auth = { getSession: vi.fn(), signOut: vi.fn() };

beforeEach(() => {
  vi.resetAllMocks();
  auth.getSession.mockResolvedValue({ data: { session: { access_token: "tok" } } });
  storage.getPublicUrl.mockImplementation((p: string) => ({ data: { publicUrl: `https://x.supabase.co/storage/v1/object/public/avatars/${p}` } }));
  storage.upload.mockResolvedValue({ error: null });
  storage.remove.mockResolvedValue({ error: null });
  m.supabase = { auth, storage: { from: vi.fn(() => storage) } };
});

async function loaded(profile: Profile) {
  vi.mocked(getMe).mockResolvedValue({ kind: "ok", profile });
  render(<MeProvider userId="u1"><ProfilePage email="fan@example.com" /></MeProvider>);
  await waitFor(() => expect(screen.queryByText("Loading…")).toBeNull());
}

test("full profile shows photo, bio, From with country name, gender, counters and email", async () => {
  await loaded(full);
  const card = within(screen.getByRole("region", { name: "Profile summary" }));
  expect(document.querySelector("img.profile__photo")?.getAttribute("src")).toBe(full.avatar_url);
  expect(card.getByText("Metal Fan")).toBeTruthy();
  expect(card.getByText("@metal_fan")).toBeTruthy();
  expect(card.getByText(/Louder\s+than hell/)).toBeTruthy();
  expect(card.getByText("From Hamburg, Germany")).toBeTruthy();
  expect(card.getByText("Gender: Non-binary")).toBeTruthy();
  const stats = within(screen.getByRole("list", { name: "Stats" }));
  expect(stats.getByText("Check-ins").previousElementSibling?.textContent).toBe("3");
  expect(stats.getByText("Venues").previousElementSibling?.textContent).toBe("2");
  expect(screen.getByText("fan@example.com")).toBeTruthy();
});

test("empty profile shows initials, no empty labels, zero counters", async () => {
  await loaded(empty);
  const card = screen.getByRole("region", { name: "Profile summary" });
  expect(card.querySelector("img")).toBeNull();
  expect(within(card).getByText("MF")).toBeTruthy();
  expect(within(card).queryByText(/From/)).toBeNull();
  expect(card.querySelector(".profile__bio, .profile__meta")).toBeNull();
  const stats = within(screen.getByRole("list", { name: "Stats" }));
  expect(stats.getByText("Check-ins").previousElementSibling?.textContent).toBe("0");
  expect(stats.getByText("Venues").previousElementSibling?.textContent).toBe("0");
});

test("Following, Followers and Crews are Coming soon with no inputs or links; counters are not editable", async () => {
  await loaded(full);
  const soon = screen.getByRole("region", { name: "Coming soon" });
  expect(within(soon).getByText("Coming soon")).toBeTruthy();
  for (const l of ["Following", "Followers", "Crews"]) expect(within(soon).getByText(l)).toBeTruthy();
  expect(soon.querySelector("input, a, button, select, textarea")).toBeNull();
  const live = screen.getByRole("list", { name: "Stats" });
  expect(within(live).getAllByRole("listitem")).toHaveLength(2);
  expect(live.querySelector("input, a, button, select, textarea")).toBeNull();
});

test("the edit form sits under an Edit profile heading and selects carry a chevron icon", async () => {
  await loaded(full);
  expect(screen.getByRole("heading", { name: "Edit profile" })).toBeTruthy();
  expect(document.querySelectorAll(".field__control select").length).toBe(2);
  expect(document.querySelectorAll(".field__control .field__chevron").length).toBe(2);
});

test("editing every field saves only the changed fields", async () => {
  await loaded(empty);
  const user = userEvent.setup();
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile: { ...empty, bio: "Hi", from_city: "Oslo", from_country: "NO", gender: "woman", username: "new_name", display_name: "New Name" } });
  await user.clear(screen.getByLabelText("Username"));
  await user.type(screen.getByLabelText("Username"), "new_name");
  await user.type(screen.getByLabelText("Display name"), "!");
  await user.clear(screen.getByLabelText("Display name"));
  await user.type(screen.getByLabelText("Display name"), "New Name");
  await user.type(screen.getByLabelText("Bio"), "Hi");
  await user.type(screen.getByLabelText("From city"), "Oslo");
  await user.selectOptions(screen.getByLabelText("From country"), "NO");
  await user.selectOptions(screen.getByLabelText("Gender"), "woman");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), {
    username: "new_name", display_name: "New Name", bio: "Hi", from_city: "Oslo", from_country: "NO", gender: "woman",
  });
  expect(await screen.findByText("Profile saved")).toBeTruthy();
  expect((screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement).disabled).toBe(true);
});

test("clearing a field sends an empty string; Cancel resets every field", async () => {
  await loaded(full);
  const user = userEvent.setup();
  await user.clear(screen.getByLabelText("From city"));
  await user.selectOptions(screen.getByLabelText("Gender"), "man");
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile: { ...full, from_city: null, gender: "man" } });
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { from_city: "", gender: "man" });
  await user.type(screen.getByLabelText("Bio"), "zzz");
  await user.type(screen.getByLabelText("From city"), "Rome");
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect((screen.getByLabelText("Bio") as HTMLTextAreaElement).value).toBe(full.bio);
  expect((screen.getByLabelText("From city") as HTMLInputElement).value).toBe("");
  expect((screen.getByLabelText("Gender") as HTMLSelectElement).value).toBe("man");
});

test.each([
  ["bio of 281 code points", "Bio", "a".repeat(281), "Bio must be plain text of 280 characters or fewer"],
  ["city of 81 code points", "From city", "🤘".repeat(81), "City must be 1-80 characters on one line"],
])("%s shows an inline error without calling the API", async (_, label, value, msg) => {
  await loaded(empty);
  const user = userEvent.setup();
  await user.click(screen.getByLabelText(label));
  await user.paste(value);
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(screen.getByText(msg)).toBeTruthy();
  expect(saveProfile).not.toHaveBeenCalled();
});

test("bio of exactly 280 emoji is accepted and shows the counter", async () => {
  await loaded(empty);
  const user = userEvent.setup();
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile: empty });
  await user.click(screen.getByLabelText("Bio"));
  await user.paste("🤘".repeat(280));
  expect(screen.getByText("280/280")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(saveProfile).toHaveBeenCalledTimes(1);
});

// jsdom has no canvas or createImageBitmap.
function stubImaging(blobType = "image/webp") {
  const dims = { w: 0, h: 0 };
  vi.stubGlobal("createImageBitmap", vi.fn(async () => ({ width: 1024, height: 768, close: vi.fn() })));
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(function (this: HTMLCanvasElement) {
    dims.w = this.width; dims.h = this.height;
    return { drawImage: vi.fn() } as unknown as CanvasRenderingContext2D;
  });
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation((cb) => cb(new Blob(["x"], { type: blobType })));
  return dims;
}
const pickFile = (user: ReturnType<typeof userEvent.setup>) => user.upload(screen.getByLabelText("Photo file"), new File(["img"], "me.png", { type: "image/png" }));

test("avatar upload downscales, uploads under the user folder, saves avatar_url and deletes the old object", async () => {
  const dims = stubImaging();
  await loaded(full);
  const user = userEvent.setup();
  vi.mocked(saveProfile).mockImplementation(async (_b, _t, input) => ({ kind: "ok", profile: { ...full, avatar_url: input.avatar_url! } }));
  await pickFile(user);
  await waitFor(() => expect(saveProfile).toHaveBeenCalled());
  expect(dims).toEqual({ w: 512, h: 384 });
  const [path, , opts] = storage.upload.mock.calls[0];
  expect(path).toMatch(/^u1\/[0-9a-f-]{36}\.webp$/);
  expect(opts).toEqual({ contentType: "image/webp" });
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { avatar_url: `https://x.supabase.co/storage/v1/object/public/avatars/${path}` });
  await waitFor(() => expect(storage.remove).toHaveBeenCalledWith(["u1/old.webp"]));
  await waitFor(() => expect(document.querySelector("img.profile__photo")?.getAttribute("src")).toContain(path));
});

test("a failed upload shows an error and leaves the profile unchanged", async () => {
  stubImaging();
  storage.upload.mockResolvedValue({ error: new Error("too big") });
  await loaded(full);
  await pickFile(userEvent.setup());
  expect(await screen.findByText(/Couldn't upload that photo/)).toBeTruthy();
  expect(saveProfile).not.toHaveBeenCalled();
  expect(document.querySelector("img.profile__photo")?.getAttribute("src")).toBe(full.avatar_url);
});

test("an image the browser can encode as neither webp nor jpeg fails without uploading", async () => {
  stubImaging("image/png");
  await loaded(full);
  await pickFile(userEvent.setup());
  expect(await screen.findByText(/Couldn't upload that photo/)).toBeTruthy();
  expect(storage.upload).not.toHaveBeenCalled();
});

test("a browser that returns png for webp (Safari) uploads a jpeg instead", async () => {
  stubImaging();
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation((cb, type) => cb(new Blob(["x"], { type: type === "image/webp" ? "image/png" : type })));
  await loaded(full);
  await pickFile(userEvent.setup());
  await waitFor(() => expect(storage.upload).toHaveBeenCalled());
  const [path, , opts] = storage.upload.mock.calls[0];
  expect(path).toMatch(/^u1\/[0-9a-f-]{36}\.jpg$/);
  expect(opts).toEqual({ contentType: "image/jpeg" });
});

test("a rejected avatar_url save shows an error, keeps the profile and cleans up the new object", async () => {
  stubImaging();
  await loaded(full);
  vi.mocked(saveProfile).mockResolvedValue({ kind: "invalid", code: "validation_error", fields: { avatar_url: "bad" } });
  await pickFile(userEvent.setup());
  expect(await screen.findByText(/Couldn't save the photo/)).toBeTruthy();
  expect(document.querySelector("img.profile__photo")?.getAttribute("src")).toBe(full.avatar_url);
  expect(storage.remove).toHaveBeenCalledWith([storage.upload.mock.calls[0][0]]);
});

test("Remove clears avatar_url and falls back to initials", async () => {
  await loaded(full);
  vi.mocked(saveProfile).mockResolvedValue({ kind: "ok", profile: { ...full, avatar_url: null } });
  await userEvent.setup().click(screen.getByRole("button", { name: "Remove" }));
  expect(saveProfile).toHaveBeenCalledWith("http://api.test", expect.any(Function), { avatar_url: "" });
  await waitFor(() => expect(document.querySelector("img.profile__photo")).toBeNull());
  expect(screen.getByText("MF")).toBeTruthy();
  expect(storage.remove).toHaveBeenCalledWith(["u1/old.webp"]);
});
