import { LoaderCircle } from "lucide-react";
import { Icon } from "./Icon";
import "./ui.css";

export function Spinner({ className = "" }: { className?: string }) {
  return <Icon icon={LoaderCircle} size="sm" className={`spinner ${className}`.trim()} />;
}

export function Skeleton({ width = "100%", height = "1rem" }: { width?: string; height?: string }) {
  return <div className="skeleton" aria-hidden="true" style={{ width, height }} />;
}
