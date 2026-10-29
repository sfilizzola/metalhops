// Up to two initials by code point (so emoji and accented letters stay whole); "?" when the name is blank.
export function initials(name: string): string {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => [...w][0]).join("").toUpperCase() || "?";
}
