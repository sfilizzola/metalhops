import { Link } from "react-router-dom";
import { LogOut } from "lucide-react";
import { Avatar } from "../profile/Avatar";
import { useMe } from "../profile/MeProvider";
import { supabase } from "../supabase";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";
import { Skeleton } from "../ui/Spinner";
import { NAV } from "./nav";
import { NavItem } from "./NavItem";

// A failed global sign-out (e.g. network) must still end the session on this device.
async function logOut() {
  const { error } = await supabase!.auth.signOut();
  if (error) await supabase!.auth.signOut({ scope: "local" });
}

function Identity({ onSelect }: { onSelect?: () => void }) {
  const { me } = useMe();
  const profile = me?.kind === "ok" ? me.profile : null;
  return (
    <Link to="/profile" className="identity shell-identity" onClick={onSelect}>
      {profile ? <Avatar profile={profile} /> : <span className="avatar" aria-hidden="true">?</span>}
      <span className="identity__text">
        {profile ? (
          <>
            <span className="identity__name">{profile.display_name}</span>
            <span className="identity__username">@{profile.username}</span>
          </>
        ) : me === null ? (
          <><span className="sr-only">Profile</span><Skeleton width="8rem" /></>
        ) : (
          <span className="identity__name">{me.kind === "no_profile" ? "No profile yet" : "Profile"}</span>
        )}
      </span>
    </Link>
  );
}

// Sidebar: derived from tokens because the design system has no spec for it. Shared by the desktop rail and the mobile drawer.
export function Sidebar({ onSelect }: { onSelect?: () => void }) {
  return (
    <div className="sidebar">
      <p className="t-title sidebar__wordmark">MetalHops</p>
      <nav aria-label="Main" className="sidebar__nav">
        {NAV.map((item) => <NavItem key={item.to} {...item} onSelect={onSelect} />)}
      </nav>
      <div className="sidebar__foot">
        <Identity onSelect={onSelect} />
        <Button variant="ghost" onClick={() => void logOut()}>
          <Icon icon={LogOut} />
          Log out
        </Button>
      </div>
    </div>
  );
}
