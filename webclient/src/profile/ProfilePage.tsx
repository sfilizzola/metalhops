import { CircleAlert } from "lucide-react";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";
import { AvatarUpload } from "./AvatarUpload";
import { countryName } from "./countries";
import { initials } from "./initials";
import { GENDER_LABEL, ProfileForm } from "./ProfileForm";
import { useMe } from "./MeProvider";
import "./profile.css";

const SOON = ["Following", "Followers", "Crews"];
const STATUS_LABEL = { active: "Active", rename_required: "Rename required", suspended: "Suspended" } as const;

export function ProfilePage({ email }: { email: string }) {
  const { me, slow, reload } = useMe();
  const profile = me?.kind === "ok" ? me.profile : null;

  return (
    <div className="profile">
      <h1 className="t-title">Profile</h1>
      {!me && (
        <>
          <p role="status">{slow ? "Waking up the server…" : "Loading…"}</p>
          <div className="skeleton profile__skeleton" aria-hidden="true" />
          <div className="skeleton profile__skeleton" aria-hidden="true" />
        </>
      )}
      {me && me.kind !== "ok" && me.kind !== "no_profile" && (
        <div className="list-state list-state--error">
          <Icon icon={CircleAlert} size="lg" />
          <p role="alert">
            {me.kind === "forbidden" ? "This account can't use the app." : `Something went wrong. Please try again.${"requestId" in me && me.requestId ? ` (request ${me.requestId})` : ""}`}
          </p>
          {me.kind !== "forbidden" && <Button variant="secondary" onClick={reload}>Retry</Button>}
        </div>
      )}
      {me?.kind === "no_profile" && <p className="t-heading">Signed in — no profile yet</p>}
      {profile && (
        <>
          <section className="profile__card" aria-label="Profile summary">
            <div className="identity identity--full">
              <span className="avatar" aria-hidden="true">
                {profile.avatar_url ? <img className="profile__photo" src={profile.avatar_url} alt="" /> : initials(profile.display_name)}
              </span>
              <div className="identity__text">
                <span className="identity__name">{profile.display_name}</span>
                <span className="identity__username">@{profile.username}</span>
              </div>
            </div>
            {profile.bio && <p className="profile__bio">{profile.bio}</p>}
            {(profile.from_city || profile.from_country) && (
              <p className="profile__meta">From {[profile.from_city, profile.from_country && countryName(profile.from_country)].filter(Boolean).join(", ")}</p>
            )}
            {profile.gender && <p className="profile__meta">Gender: {GENDER_LABEL[profile.gender]}</p>}
            <AvatarUpload profile={profile} />
          </section>
          <ul className="profile__stats" aria-label="Stats">
            <li><span className="t-heading">{profile.stats.check_ins}</span><span className="t-label">Check-ins</span></li>
            <li><span className="t-heading">{profile.stats.venues}</span><span className="t-label">Venues</span></li>
          </ul>
          <section className="profile__soon" aria-label="Coming soon">
            <span className="t-caption">Coming soon</span>
            <ul>
              {SOON.map((label) => <li key={label} className="t-label">{label}</li>)}
            </ul>
          </section>
          {/* Static status shown on load, so not live regions. */}
          {profile.status === "rename_required" && <Alert kind="error" live={false}>Your username needs to change. Choose a new username to continue.</Alert>}
          {profile.status === "suspended" && <Alert kind="error" live={false}>This profile is suspended, so it can't be changed right now.</Alert>}
        </>
      )}
      {/* One slot for both modes so "Profile saved" survives create -> edit. */}
      {(me?.kind === "no_profile" || profile) && (
        <div className="profile__edit">
          {profile && <h2 className="t-heading">Edit profile</h2>}
          <ProfileForm profile={profile} suspended={profile?.status === "suspended"} />
        </div>
      )}
      {(me?.kind === "no_profile" || profile) && (
        <dl className="profile__facts">
          <div><dt className="t-label">Email</dt><dd>{email}</dd></div>
          {profile && (
            <>
              <div><dt className="t-label">Account status</dt><dd><span className="tag">{STATUS_LABEL[profile.status]}</span></dd></div>
              {profile.status !== "active" && profile.moderation_reason && <div><dt className="t-label">Reason</dt><dd>{profile.moderation_reason}</dd></div>}
              {profile.status !== "active" && profile.moderation_note && <div><dt className="t-label">Note</dt><dd>{profile.moderation_note}</dd></div>}
              {profile.status !== "active" && profile.suspended_until && (
                <div><dt className="t-label">Suspended until</dt><dd>{new Date(profile.suspended_until).toLocaleString()}</dd></div>
              )}
            </>
          )}
        </dl>
      )}
    </div>
  );
}
