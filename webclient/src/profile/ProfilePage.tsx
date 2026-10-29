import { useEffect, useState } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { House, MapPinCheck, Pencil, Store } from "lucide-react";
import { Alert } from "../ui/Alert";
import { GenderIcon } from "../ui/GenderIcon";
import { Icon } from "../ui/Icon";
import { Avatar } from "./Avatar";
import { countryName } from "./countries";
import { MeLoadState, StatusAlerts } from "./ProfileStatus";
import { useMe } from "./MeProvider";
import "./profile.css";

const SOON = ["Following", "Followers", "Crews"];
const STATUS_LABEL = { active: "Active", rename_required: "Rename required", suspended: "Suspended" } as const;

// Read-only user page (mockup variant B, Backline stage card). Editing lives on /profile/edit.
export function ProfilePage({ email }: { email: string }) {
  const { me, slow, reload } = useMe();
  const location = useLocation();
  const navigate = useNavigate();
  // Router state survives a reload, so read the flag once and clear it from history.
  const [saved] = useState(() => !!(location.state as { saved?: boolean } | null)?.saved);
  useEffect(() => {
    if (saved) navigate(location.pathname, { replace: true, state: null });
  }, [saved, navigate, location.pathname]);

  if (me?.kind === "no_profile") return <Navigate to="/profile/edit" replace />;
  const profile = me?.kind === "ok" ? me.profile : null;

  if (!profile) {
    return (
      <div className="profile-view">
        <h1 className="sr-only">Profile</h1>
        <MeLoadState me={me} slow={slow} reload={reload} />
      </div>
    );
  }

  const country = profile.from_country ? countryName(profile.from_country) : null;
  return (
    <div className="profile-view">
      {saved && <Alert kind="success">Profile saved</Alert>}
      <StatusAlerts profile={profile} />
      <section className="profile-card profile-stage" aria-label="Profile summary">
        <div className="profile-stage__top">
          <Avatar profile={profile} className="profile-avatar--xl profile-stage__avatar" />
          <div className="profile-stage__id">
            <h1 className="t-title profile-name">{profile.display_name}</h1>
            <p className="profile-username">
              @{profile.username}
              {(country || profile.from_city || profile.gender) && (
                <span className="profile-byline">
                  {(country || profile.from_city) && (
                    <span className="profile-byline__loc">
                      <Icon icon={House} size="sm" className="profile-byline__house" />
                      <span className="sr-only">From </span>
                      {profile.from_city && <span className="profile-byline__city">{profile.from_city}</span>}
                      {profile.from_city && country && <span className="profile-byline__sub"> – </span>}
                      {country && <span className={profile.from_city ? "profile-byline__sub" : undefined}>{country}</span>}
                    </span>
                  )}
                  {profile.gender && <GenderIcon gender={profile.gender} className="profile-byline__g" />}
                </span>
              )}
            </p>
          </div>
          {profile.bio && <p className="profile-stage__bio">{profile.bio}</p>}
          <div className="profile-stage__act">
            <Link className="btn btn--primary" to="/profile/edit"><Icon icon={Pencil} />Edit profile</Link>
          </div>
        </div>
        <ul className="profile-strip" aria-label="Stats">
          <li className="profile-stat"><span className="profile-stat__n">{profile.stats.check_ins}</span><span className="profile-stat__l t-label"><Icon icon={MapPinCheck} size="sm" />Check-ins</span></li>
          <li className="profile-stat"><span className="profile-stat__n">{profile.stats.venues}</span><span className="profile-stat__l t-label"><Icon icon={Store} size="sm" />Venues</span></li>
        </ul>
      </section>
      <section className="profile-soon" aria-label="Coming soon">
        <span className="t-caption">Coming soon</span>
        <ul>
          {SOON.map((label) => <li key={label} className="t-label">{label}</li>)}
        </ul>
      </section>
      <section className="profile-account" aria-labelledby="profile-account-title">
        <h2 id="profile-account-title" className="t-label profile-section-title">Account (only you see this)</h2>
        <dl className="profile-facts">
          <div><dt className="t-label">Email</dt><dd>{email}</dd></div>
          <div><dt className="t-label">Account status</dt><dd><span className="tag">{STATUS_LABEL[profile.status]}</span></dd></div>
          {profile.status !== "active" && profile.moderation_reason && <div><dt className="t-label">Reason</dt><dd>{profile.moderation_reason}</dd></div>}
          {profile.status !== "active" && profile.moderation_note && <div><dt className="t-label">Note</dt><dd>{profile.moderation_note}</dd></div>}
          {profile.status !== "active" && profile.suspended_until && (
            <div><dt className="t-label">Suspended until</dt><dd>{new Date(profile.suspended_until).toLocaleString()}</dd></div>
          )}
        </dl>
      </section>
    </div>
  );
}
