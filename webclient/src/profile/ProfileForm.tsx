import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { saveProfile } from "../api";
import type { ApiFailure, Gender, Profile, ProfileInput } from "../api";
import { apiBaseUrl, supabase } from "../supabase";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Field } from "../ui/Field";
import { Avatar } from "./Avatar";
import { AvatarUpload } from "./AvatarUpload";
import { COUNTRIES } from "./countries";
import { useMe } from "./MeProvider";

const USERNAME_RE = /^[A-Za-z0-9_]{3,30}$/;
// Field ids in form order (first error gets focus) and the client wording used for both local and server (400) errors.
const FIELD_IDS = {
  username: "profile-username",
  display_name: "profile-display-name",
  bio: "profile-bio",
  from_city: "profile-from-city",
  from_country: "profile-from-country",
  gender: "profile-gender",
} as const;
export const GENDER_LABEL: Record<Gender, string> = { woman: "Woman", man: "Man", non_binary: "Non-binary", prefer_not_to_say: "Prefer not to say" };
const BIO_MAX = 280;
const FIELD_MESSAGES: Record<string, string> = {
  username: "Username must be 3-30 letters, numbers or underscores",
  display_name: "Display name must be 1-80 characters",
  bio: `Bio must be plain text of ${BIO_MAX} characters or fewer`,
  from_city: "City must be 1-80 characters on one line",
  from_country: "Choose a country from the list",
  gender: "Choose one of the listed options",
};

function failureMessage(f: ApiFailure): string {
  if (f.kind === "forbidden") return "This account can't use the app.";
  const id = f.kind === "rate_limited" || f.kind === "error" ? f.requestId : undefined;
  return `Something went wrong. Please try again.${id ? ` (request ${id})` : ""}`;
}

type Props = {
  profile: Profile | null;
  suspended: boolean;
  onSaved: () => void;
  onCancel: () => void;
  onDirtyChange?: (dirty: boolean) => void;
};

// profile === null means create mode (no profile yet). The edit screen layout (mockup edit-2): Photo and Identity, then About and Location and details.
export function ProfileForm({ profile, suspended, onSaved, onCancel, onDirtyChange }: Props) {
  const { getToken, setProfile, reload } = useMe();
  const [username, setUsername] = useState(profile?.username ?? "");
  const [displayName, setDisplayName] = useState(profile?.display_name ?? "");
  const [bio, setBio] = useState(profile?.bio ?? "");
  const [city, setCity] = useState(profile?.from_city ?? "");
  const [country, setCountry] = useState(profile?.from_country ?? "");
  const [gender, setGender] = useState<string>(profile?.gender ?? "");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [general, setGeneral] = useState("");
  const [retryable, setRetryable] = useState(false);
  const [pending, setPending] = useState(false);

  // Fields that differ from the loaded profile, in API wording ("" clears a nullable field).
  function changes(): ProfileInput {
    if (!profile) return { username, display_name: displayName.trim() };
    const next: ProfileInput = {
      username, display_name: displayName.trim(), bio: bio.trim(), from_city: city.trim(), from_country: country, gender,
    };
    const before: ProfileInput = {
      username: profile.username, display_name: profile.display_name, bio: profile.bio ?? "",
      from_city: profile.from_city ?? "", from_country: profile.from_country ?? "", gender: profile.gender ?? "",
    };
    return Object.fromEntries(Object.entries(next).filter(([k, v]) => v !== before[k as keyof ProfileInput]));
  }
  const dirty = !!profile && Object.keys(changes()).length > 0;
  useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange]);

  // Runs for client and server errors alike; every setErrors call passes a new object.
  useEffect(() => {
    const first = (Object.keys(FIELD_IDS) as (keyof typeof FIELD_IDS)[]).find((k) => errors[k]);
    if (first) document.getElementById(FIELD_IDS[first])?.focus();
  }, [errors]);

  function edit(set: (v: string) => void) {
    return (e: { target: { value: string } }) => set(e.target.value);
  }

  async function submit(e?: FormEvent) {
    e?.preventDefault();
    setGeneral("");
    setRetryable(false);
    const name = displayName.trim();
    const next: Record<string, string> = {};
    // Count code points like the backend (RuneCountInString), not UTF-16 units.
    const nameLength = [...name].length;
    if (!USERNAME_RE.test(username)) next.username = FIELD_MESSAGES.username;
    if (nameLength < 1 || nameLength > 80) next.display_name = FIELD_MESSAGES.display_name;
    if ([...bio.trim()].length > BIO_MAX) next.bio = FIELD_MESSAGES.bio;
    if ([...city.trim()].length > 80) next.from_city = FIELD_MESSAGES.from_city;
    setErrors(next);
    if (Object.keys(next).length) return;

    setPending(true);
    const res = await saveProfile(apiBaseUrl, getToken, changes());
    setPending(false);
    switch (res.kind) {
      case "ok":
        setProfile(res.profile);
        return onSaved();
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

  const identity = (
    <section className="profile-card profile-section" aria-labelledby="profile-identity-title">
      <h2 id="profile-identity-title" className="t-heading">Identity</h2>
      <Field id="profile-username" label="Username" value={username} onChange={edit(setUsername)} error={errors.username} disabled={suspended} autoComplete="username" autoCapitalize="none" spellCheck={false} />
      <Field id="profile-display-name" label="Display name" value={displayName} onChange={edit(setDisplayName)} error={errors.display_name} disabled={suspended} autoComplete="nickname" />
    </section>
  );

  return (
    <form className="profile-form" onSubmit={submit} noValidate>
      {profile ? (
        <div className="profile-edit__cols">
          <div className="profile-edit__col">
            <section className="profile-card profile-section" aria-labelledby="profile-photo-title">
              <h2 id="profile-photo-title" className="t-heading">Photo</h2>
              <div className="profile-photo">
                <Avatar profile={profile} className="profile-avatar--lg" />
                <AvatarUpload profile={profile} />
              </div>
            </section>
            {identity}
          </div>
          <div className="profile-edit__col">
            <section className="profile-card profile-section" aria-labelledby="profile-about-title">
              <h2 id="profile-about-title" className="t-heading">About</h2>
              <Field as="textarea" rows={4} id="profile-bio" label="Bio" value={bio} onChange={edit(setBio)} error={errors.bio} disabled={suspended} help={`${[...bio].length}/${BIO_MAX}`} className="num" />
            </section>
            <section className="profile-card profile-section" aria-labelledby="profile-location-title">
              <h2 id="profile-location-title" className="t-heading">Location &amp; details</h2>
              <Field id="profile-from-city" label="From city" value={city} onChange={edit(setCity)} error={errors.from_city} disabled={suspended} autoComplete="address-level2" />
              <Field as="select" id="profile-from-country" label="From country" value={country} onChange={edit(setCountry)} error={errors.from_country} disabled={suspended} autoComplete="country">
                <option value="">Not set</option>
                {COUNTRIES.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}
              </Field>
              <Field as="select" id="profile-gender" label="Gender" value={gender} onChange={edit(setGender)} error={errors.gender} disabled={suspended}>
                <option value="">Not set</option>
                {(Object.keys(GENDER_LABEL) as Gender[]).map((g) => <option key={g} value={g}>{GENDER_LABEL[g]}</option>)}
              </Field>
            </section>
          </div>
        </div>
      ) : identity}
      {general && <Alert kind="error">{general}</Alert>}
      {!suspended && (
        <div className="profile-actions">
          <Button type="submit" loading={pending} disabled={!!profile && !dirty}>{profile ? "Save changes" : "Create profile"}</Button>
          {retryable && <Button variant="secondary" onClick={() => void submit()} disabled={pending}>Retry</Button>}
          {profile && <Button variant="secondary" onClick={onCancel} disabled={pending}>Cancel</Button>}
        </div>
      )}
    </form>
  );
}
