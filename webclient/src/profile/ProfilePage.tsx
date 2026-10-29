import { useEffect, useState } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import type { ReactNode } from "react";
import { CalendarClock, ChevronDown, Flag, House, Lock, MapPinCheck, Mail, MessageSquareText, Pencil, Store } from "lucide-react";
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

function AccRow({ icon, label, children }: { icon: Parameters<typeof Icon>[0]["icon"]; label: string; children: ReactNode }) {
  return <div className="profile-acc__row"><Icon icon={icon} /><dt className="t-caption">{label}</dt><dd>{children}</dd></div>;
}

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
      <details className="profile-card profile-acc" open={profile.status !== "active"}>
        <summary className="profile-acc__sum">
          <Icon icon={Lock} />
          <span className="profile-acc__t"><span className="t-label">Account</span><span className="t-caption">Only you</span></span>
          <span className="tag">{STATUS_LABEL[profile.status]}</span>
          <Icon icon={ChevronDown} className="profile-acc__chev" />
        </summary>
        <dl className="profile-acc__rows">
          <AccRow icon={Mail} label="Email">{email}</AccRow>
          {profile.status !== "active" && profile.moderation_reason && <AccRow icon={Flag} label="Reason">{profile.moderation_reason}</AccRow>}
          {profile.status !== "active" && profile.moderation_note && <AccRow icon={MessageSquareText} label="Moderator note"><p>{profile.moderation_note}</p></AccRow>}
          {profile.status !== "active" && profile.suspended_until && (
            <AccRow icon={CalendarClock} label="Suspended until"><time dateTime={profile.suspended_until}>{new Date(profile.suspended_until).toLocaleString()}</time></AccRow>
          )}
        </dl>
      </details>
    </div>
  );
}
