import type { ButtonHTMLAttributes } from "react";
import { Spinner } from "./Spinner";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "md" | "lg";
  loading?: boolean;
};

export function Button({ variant = "primary", size = "md", loading = false, disabled, className = "", children, type = "button", ...rest }: Props) {
  const cls = `btn btn--${variant}${size === "lg" ? " btn--lg" : ""} ${className}`.trim();
  return (
    <button {...rest} type={type} className={cls} disabled={disabled || loading} aria-busy={loading || undefined}>
      {loading && <Spinner />}
      {children}
    </button>
  );
}
