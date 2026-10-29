import { CircleAlert } from "lucide-react";
import type { MeResult, Profile } from "../api";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";

// Loading skeleton or load error for both profile screens; renders nothing once a Profile (or no_profile) is known.
export function MeLoadState({ me, slow, reload }: { me: MeResult | null; slow: boolean; reload: () => void }) {
  if (!me) {
    return (
      <>
        <p role="status">{slow ? "Waking up the server…" : "Loading…"}</p>
        <div className="skeleton profile__skeleton" aria-hidden="true" />
        <div className="skeleton profile__skeleton" aria-hidden="true" />
      </>
    );
  }
  if (me.kind === "ok" || me.kind === "no_profile") return null;
  return (
    <div className="list-state list-state--error">
      <Icon icon={CircleAlert} size="lg" />
      <p role="alert">
        {me.kind === "forbidden" ? "This account can't use the app." : `Something went wrong. Please try again.${"requestId" in me && me.requestId ? ` (request ${me.requestId})` : ""}`}
      </p>
      {me.kind !== "forbidden" && <Button variant="secondary" onClick={reload}>Retry</Button>}
    </div>
  );
}

// Static status shown on load, so not live regions.
export function StatusAlerts({ profile }: { profile: Profile }) {
  if (profile.status === "rename_required") return <Alert kind="error" live={false}>Your username needs to change. Choose a new username to continue.</Alert>;
  if (profile.status === "suspended") return <Alert kind="error" live={false}>This profile is suspended, so it can't be changed right now.</Alert>;
  return null;
}
