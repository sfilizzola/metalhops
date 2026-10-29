import { Compass, House, MapPinCheck, Store, UserRound } from "lucide-react";
import type { LucideIcon } from "lucide-react";

// The single list of app sections, in display order (plan decision D3).
export const NAV: readonly { to: string; label: string; icon: LucideIcon }[] = [
  { to: "/", label: "Home", icon: House },
  { to: "/discover", label: "Discover", icon: Compass },
  { to: "/check-ins", label: "Check-ins", icon: MapPinCheck },
  { to: "/my-venues", label: "My venues", icon: Store },
  { to: "/profile", label: "Profile", icon: UserRound },
];
