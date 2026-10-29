import { NavLink } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import { Icon } from "../ui/Icon";

// Nav item: derived from tokens because the design system has no spec for it. NavLink sets aria-current="page".
export function NavItem({ to, label, icon, onSelect }: { to: string; label: string; icon: LucideIcon; onSelect?: () => void }) {
  return (
    <NavLink to={to} end={to === "/"} className="nav-item" onClick={onSelect}>
      <Icon icon={icon} />
      <span>{label}</span>
    </NavLink>
  );
}
