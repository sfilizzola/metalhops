import type { Profile } from "../api";
import { initials } from "./initials";
import "./profile.css";

// Photo or initials in the design system's .avatar; aria-hidden because the name is always next to it.
export function Avatar({ profile, className = "" }: { profile: Pick<Profile, "avatar_url" | "display_name">; className?: string }) {
  return (
    <span className={`avatar ${className}`.trim()} aria-hidden="true">
      {profile.avatar_url ? <img className="profile__photo" src={profile.avatar_url} alt="" /> : initials(profile.display_name)}
    </span>
  );
}
