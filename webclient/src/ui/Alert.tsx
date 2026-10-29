import type { ReactNode } from "react";
import { CircleAlert, CircleCheck } from "lucide-react";
import { Icon } from "./Icon";
import "./ui.css";

// live={false} renders a static notice (no live region) for state shown on load, not caused by a user action.
export function Alert({ kind, live = true, children }: { kind: "error" | "success"; live?: boolean; children: ReactNode }) {
  const error = kind === "error";
  return (
    <div className={`form-alert${error ? "" : " form-alert--success"}`} role={live ? (error ? "alert" : "status") : undefined}>
      <Icon icon={error ? CircleAlert : CircleCheck} />
      <div>{children}</div>
    </div>
  );
}
