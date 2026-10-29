import type { LucideIcon } from "lucide-react";

const SIZES = { sm: 16, md: 20, lg: 24 } as const;

export function Icon({ icon: Glyph, size = "md", className = "" }: { icon: LucideIcon; size?: keyof typeof SIZES; className?: string }) {
  return <Glyph className={`icon icon--${size} ${className}`.trim()} size={SIZES[size]} strokeWidth={2} aria-hidden="true" />;
}
