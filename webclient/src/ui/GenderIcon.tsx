import { EyeOff } from "lucide-react";
import type { Gender } from "../api";
import { GENDER_LABEL } from "../profile/ProfileForm";
import { Icon } from "./Icon";

// Approved exception to "Lucide icons only": Lucide 0.469.0 has no gender symbols. Paths are drawn in Lucide's style
// (24 grid, stroke 2, round caps and joins, no fill), copied from design/mockups/profile/card-compact.html.
const PATHS = {
  woman: <><circle cx="12" cy="9" r="6" /><path d="M12 15v7" /><path d="M9 19h6" /></>,
  man: <><circle cx="10" cy="14" r="6" /><path d="M16 3h5v5" /><path d="m21 3-6.75 6.75" /></>,
  non_binary: <><circle cx="12" cy="15.5" r="5.5" /><path d="M12 10V5" /><path d="m9 2 6 6" /><path d="m15 2-6 6" /></>,
};

// The shape carries the meaning, never colour alone; the name and tooltip sit on the wrapper.
export function GenderIcon({ gender, className = "" }: { gender: Gender; className?: string }) {
  return (
    <span role="img" aria-label={`Gender: ${GENDER_LABEL[gender]}`} title={GENDER_LABEL[gender]} className={`gender-icon ${className}`.trim()}>
      {gender === "prefer_not_to_say" ? <Icon icon={EyeOff} size="sm" /> : (
        <svg className="icon icon--sm" viewBox="0 0 24 24" aria-hidden="true">{PATHS[gender]}</svg>
      )}
    </span>
  );
}
