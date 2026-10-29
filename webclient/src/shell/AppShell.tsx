import { Outlet } from "react-router-dom";
import { MobileNav } from "./MobileNav";
import { Sidebar } from "./Sidebar";
import "./shell.css";

// App shell: derived from tokens because the design system has no spec for page layout.
export function AppShell() {
  return (
    <div className="shell">
      <aside className="shell__sidebar"><Sidebar /></aside>
      <MobileNav />
      <main className="shell__main"><Outlet /></main>
    </div>
  );
}
