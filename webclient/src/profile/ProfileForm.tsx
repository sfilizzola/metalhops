import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { saveProfile } from "../api";
import type { ApiFailure, Profile } from "../api";
import { apiBaseUrl, supabase } from "../supabase";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Field } from "../ui/Field";
import { useMe } from "./MeProvider";

const USERNAME_RE = /^[A-Za-z0-9_]{3,30}$/;
// Field ids in form order (first error gets focus) and the client wording used for both local and server (400) errors.
const FIELD_IDS = { username: "profile-username", display_name: "profile-display-name" } as const;
const FIELD_MESSAGES: Record<string, string> = {
  username: "Username must be 3-30 letters, numbers or underscores",
  display_name: "Display name must be 1-80 characters",
};

function failureMessage(f: ApiFailure): string {
  if (f.kind === "forbidden") return "This account can't use the app.";
  const id = f.kind === "rate_limited" || f.kind === "error" ? f.requestId : undefined;
  return `Something went wrong. Please try again.${id ? ` (request ${id})` : ""}`;
}

// profile === null means create mode (no profile yet).
export function ProfileForm({ profile, suspended }: { profile: Profile | null; suspended: boolean }) {
  const { getToken, setProfile, reload } = useMe();
  const [username, setUsername] = useState(profile?.username ?? "");
  const [displayName, setDisplayName] = useState(profile?.display_name ?? "");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [general, setGeneral] = useState("");
  const [retryable, setRetryable] = useState(false);
  const [saved, setSaved] = useState(false);
  const [pending, setPending] = useState(false);

  const dirty = !!profile && (username !== profile.username || displayName.trim() !== profile.display_name);

  // Runs for client and server errors alike; every setErrors call passes a new object.
  useEffect(() => {
    const first = (Object.keys(FIELD_IDS) as (keyof typeof FIELD_IDS)[]).find((k) => errors[k]);
    if (first) document.getElementById(FIELD_IDS[first])?.focus();
  }, [errors]);

  function edit(set: (v: string) => void) {
    return (e: { target: { value: string } }) => { set(e.target.value); setSaved(false); };
  }

  function cancel() {
    setUsername(profile?.username ?? "");
    setDisplayName(profile?.display_name ?? "");
    setErrors({});
    setGeneral("");
    setRetryable(false);
    setSaved(false);
  }

  async function submit(e?: FormEvent) {
    e?.preventDefault();
    setGeneral("");
    setRetryable(false);
    setSaved(false);
    const name = displayName.trim();
    const next: Record<string, string> = {};
    // Count code points like the backend (RuneCountInString), not UTF-16 units.
    const nameLength = [...name].length;
    if (!USERNAME_RE.test(username)) next.username = FIELD_MESSAGES.username;
    if (nameLength < 1 || nameLength > 80) next.display_name = FIELD_MESSAGES.display_name;
    setErrors(next);
    if (Object.keys(next).length) return;

    setPending(true);
    const res = await saveProfile(apiBaseUrl, getToken, { username, display_name: name });
    setPending(false);
    switch (res.kind) {
      case "ok":
        setUsername(res.profile.username);
        setDisplayName(res.profile.display_name);
        setProfile(res.profile);
        return setSaved(true);
      case "invalid": {
        // Never show backend field text: known keys get the client wording, anything else the general message.
        const keys = Object.keys(res.fields ?? {});
        const known = keys.filter((k) => Object.hasOwn(FIELD_MESSAGES, k));
        if (known.length) setErrors(Object.fromEntries(known.map((k) => [k, FIELD_MESSAGES[k]])));
        if (!known.length || known.length < keys.length) {
          setGeneral(res.code === "username_reserved" && !keys.length ? "That username is reserved" : "Check the details and try again");
        }
        return;
      }
      case "username_taken": return setErrors({ username: "That username is taken" });
      case "suspended":
        setGeneral("This profile is suspended");
        // Refetch so the page re-renders read-only with the new status.
        return reload();
      case "unauthorized": return void supabase?.auth.signOut({ scope: "local" });
      default:
        setGeneral(failureMessage(res));
        setRetryable(res.kind !== "forbidden");
    }
  }

  return (
    <form className="profile__form" onSubmit={submit} noValidate>
      <Field id="profile-username" label="Username" value={username} onChange={edit(setUsername)} error={errors.username} disabled={suspended} autoComplete="username" autoCapitalize="none" spellCheck={false} />
      <Field id="profile-display-name" label="Display name" value={displayName} onChange={edit(setDisplayName)} error={errors.display_name} disabled={suspended} autoComplete="nickname" />
      {general && <Alert kind="error">{general}</Alert>}
      {saved && <Alert kind="success">Profile saved</Alert>}
      {!suspended && (
        <div className="profile__actions">
          <Button type="submit" loading={pending} disabled={!!profile && !dirty}>{profile ? "Save changes" : "Create profile"}</Button>
          {retryable && <Button variant="secondary" onClick={() => void submit()} disabled={pending}>Retry</Button>}
          {dirty && <Button variant="ghost" onClick={cancel} disabled={pending}>Cancel</Button>}
        </div>
      )}
    </form>
  );
}
