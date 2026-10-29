import { useCallback, useEffect, useState } from "react";
import type { MouseEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { Icon } from "../ui/Icon";
import { ProfileForm } from "./ProfileForm";
import { MeLoadState, StatusAlerts } from "./ProfileStatus";
import { useMe } from "./MeProvider";
import "./profile.css";

const DISCARD = "Discard your unsaved changes?";

// Edit screen (mockup edit-2, two columns). Also the create flow when the User has no Profile yet.
export function ProfileEditPage({ email }: { email: string }) {
  const { me, slow, reload } = useMe();
  const navigate = useNavigate();
  const [dirty, setDirty] = useState(false);
  const profile = me?.kind === "ok" ? me.profile : null;
  const create = me?.kind === "no_profile";

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  const onSaved = useCallback(() => navigate("/profile", { state: { saved: true } }), [navigate]);
  const leave = () => { if (!dirty || window.confirm(DISCARD)) navigate("/profile"); };
  const back = (e: MouseEvent) => { if (dirty && !window.confirm(DISCARD)) e.preventDefault(); };

  return (
    <div className="profile-edit">
      {/* No Back in create mode: /profile would send the User straight back here. */}
      {!create && <Link className="btn btn--ghost profile-edit__back" to="/profile" onClick={back}><Icon icon={ArrowLeft} />Back to profile</Link>}
      <h1 className="t-title">{create ? "Create profile" : "Edit profile"}</h1>
      <MeLoadState me={me} slow={slow} reload={reload} />
      {create && (
        <>
          <p className="t-heading">Signed in — no profile yet</p>
          <dl className="profile-facts">
            <div><dt className="t-label">Email</dt><dd>{email}</dd></div>
          </dl>
        </>
      )}
      {profile && <StatusAlerts profile={profile} />}
      {(create || profile) && (
        <ProfileForm profile={profile} suspended={profile?.status === "suspended"} onSaved={onSaved} onCancel={leave} onDirtyChange={setDirty} />
      )}
    </div>
  );
}
