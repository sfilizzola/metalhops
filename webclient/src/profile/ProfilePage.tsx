import { CircleAlert } from "lucide-react";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";
import { initials } from "./initials";
import { ProfileForm } from "./ProfileForm";
import { useMe } from "./MeProvider";
import "./profile.css";

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
          <div className="identity identity--full">
            <span className="avatar" aria-hidden="true">{initials(profile.display_name)}</span>
            <div className="identity__text">
              <span className="identity__name">{profile.display_name}</span>
              <span className="identity__username">@{profile.username}</span>
            </div>
          </div>
          {/* Static status shown on load, so not live regions. */}
          {profile.status === "rename_required" && <Alert kind="error" live={false}>Your username needs to change. Choose a new username to continue.</Alert>}
          {profile.status === "suspended" && <Alert kind="error" live={false}>This profile is suspended, so it can't be changed right now.</Alert>}
        </>
      )}
      {/* One slot for both modes so "Profile saved" survives create -> edit. */}
      {(me?.kind === "no_profile" || profile) && <ProfileForm profile={profile} suspended={profile?.status === "suspended"} />}
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
