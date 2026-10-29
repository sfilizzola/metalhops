import type { InputHTMLAttributes, ReactNode } from "react";
import { ChevronDown, CircleAlert } from "lucide-react";
import { Icon } from "./Icon";

// `as` swaps the control; textarea and select take the same label, help and error pattern.
type Props = InputHTMLAttributes<HTMLInputElement> & {
  id: string; label: string; error?: string; help?: string;
  as?: "input" | "textarea" | "select"; rows?: number; children?: ReactNode;
};

export function Field({ id, label, error, help, disabled, className = "", as: Control = "input", ...input }: Props) {
  const helpId = `${id}-help`;
  const errorId = `${id}-error`;
  const describedBy = [error && errorId, help && helpId].filter(Boolean).join(" ") || undefined;
  return (
    <div className={`field${error ? " field--error" : ""}${disabled ? " field--disabled" : ""} ${className}`.trim()}>
      <label className="field__label" htmlFor={id}>{label}</label>
      <div className="field__control">
        <Control {...(input as object)} id={id} disabled={disabled} aria-invalid={error ? true : undefined} aria-describedby={describedBy} />
        {Control === "select" && <Icon icon={ChevronDown} className="field__chevron" />}
      </div>
      {help && <div className="field__help" id={helpId}>{help}</div>}
      {error && (
        <div className="field__error" id={errorId} role="alert">
          <Icon icon={CircleAlert} size="sm" />
          <span>{error}</span>
        </div>
      )}
    </div>
  );
}
