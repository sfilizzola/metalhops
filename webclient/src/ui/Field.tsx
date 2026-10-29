import type { InputHTMLAttributes } from "react";
import { CircleAlert } from "lucide-react";
import { Icon } from "./Icon";

type Props = InputHTMLAttributes<HTMLInputElement> & { id: string; label: string; error?: string; help?: string };

export function Field({ id, label, error, help, disabled, className = "", ...input }: Props) {
  const helpId = `${id}-help`;
  const errorId = `${id}-error`;
  const describedBy = [error && errorId, help && helpId].filter(Boolean).join(" ") || undefined;
  return (
    <div className={`field${error ? " field--error" : ""}${disabled ? " field--disabled" : ""} ${className}`.trim()}>
      <label className="field__label" htmlFor={id}>{label}</label>
      <div className="field__control">
        <input {...input} id={id} disabled={disabled} aria-invalid={error ? true : undefined} aria-describedby={describedBy} />
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
