import { useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { saveProfile } from "../api";
import type { Profile } from "../api";
import { apiBaseUrl, supabase } from "../supabase";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { useMe } from "./MeProvider";

const BUCKET = "avatars";
const MAX_PX = 512;

async function downscale(file: File): Promise<Blob> {
  const img = await createImageBitmap(file);
  const k = Math.min(1, MAX_PX / Math.max(img.width, img.height));
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(img.width * k));
  canvas.height = Math.max(1, Math.round(img.height * k));
  canvas.getContext("2d")!.drawImage(img, 0, 0, canvas.width, canvas.height);
  img.close?.();
  // Safari cannot encode webp and silently returns png, so fall back to jpeg; anything else is refused rather than stored mislabelled.
  for (const type of ["image/webp", "image/jpeg"]) {
    const blob = await new Promise<Blob | null>((ok) => canvas.toBlob(ok, type, 0.85));
    if (blob?.type === type) return blob;
  }
  throw new Error("encode");
}

// Object path inside the bucket, from a public URL.
const objectPath = (url: string | null) => (url?.includes(`/${BUCKET}/`) ? url.split(`/${BUCKET}/`)[1] : null);

export function AvatarUpload({ profile }: { profile: Profile }) {
  const { getToken, setProfile, reload } = useMe();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);

  const removeObject = (path: string | null) => path && void supabase?.storage.from(BUCKET).remove([path]).catch(() => {});

  // Saves avatar_url through the normal profile path; the profile only changes when that succeeds.
  async function save(avatar_url: string): Promise<boolean> {
    const res = await saveProfile(apiBaseUrl, getToken, { avatar_url });
    if (res.kind === "ok") { setProfile(res.profile); return true; }
    if (res.kind === "unauthorized") void supabase?.auth.signOut({ scope: "local" });
    setError(res.kind === "suspended" ? "This profile is suspended" : "Couldn't save the photo. Please try again.");
    // Refetch so the page re-renders read-only, as ProfileForm does.
    if (res.kind === "suspended") reload();
    return false;
  }

  async function pick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file || !supabase) return;
    setError("");
    setBusy(true);
    try {
      const blob = await downscale(file);
      const path = `${profile.id}/${crypto.randomUUID()}.${blob.type === "image/webp" ? "webp" : "jpg"}`;
      const up = await supabase.storage.from(BUCKET).upload(path, blob, { contentType: blob.type });
      if (up.error) throw up.error;
      if (await save(supabase.storage.from(BUCKET).getPublicUrl(path).data.publicUrl)) removeObject(objectPath(profile.avatar_url));
      else removeObject(path);
    } catch {
      setError("Couldn't upload that photo. Try a different image.");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setError("");
    setBusy(true);
    if (await save("")) removeObject(objectPath(profile.avatar_url));
    setBusy(false);
  }

  if (profile.status === "suspended") return null;
  return (
    <div className="profile__avatar-actions">
      <input ref={input} type="file" accept="image/*" hidden aria-label="Photo file" onChange={pick} />
      <Button variant="secondary" loading={busy} onClick={() => input.current?.click()}>{profile.avatar_url ? "Change photo" : "Add photo"}</Button>
      {profile.avatar_url && <Button variant="ghost" disabled={busy} onClick={remove}>Remove</Button>}
      {error && <Alert kind="error">{error}</Alert>}
    </div>
  );
}
