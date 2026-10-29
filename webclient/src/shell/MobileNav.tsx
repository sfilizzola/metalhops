import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { useLocation } from "react-router-dom";
import { Menu, X } from "lucide-react";
import { Icon } from "../ui/Icon";
import { Sidebar } from "./Sidebar";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

// Top bar and drawer: derived from tokens because the design system has no spec for them. Hidden at 960px and wider (shell.css).
export function MobileNav() {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLButtonElement>(null);
  const drawerRef = useRef<HTMLDivElement>(null);
  const wasOpen = useRef(false);
  const { pathname } = useLocation();

  useEffect(() => setOpen(false), [pathname]);

  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = prev; };
  }, [open]);

  // Same breakpoint as shell.css: the drawer has no place on the desktop layout.
  useEffect(() => {
    const mq = window.matchMedia?.("(min-width: 960px)");
    if (!mq) return;
    const onChange = (e: { matches: boolean }) => { if (e.matches) setOpen(false); };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    if (open) drawerRef.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    else if (wasOpen.current) menuRef.current?.focus();
    wasOpen.current = open;
  }, [open]);

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") return setOpen(false);
    if (e.key !== "Tab") return;
    const items = [...drawerRef.current!.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }

  return (
    <div className="mobile-nav">
      <header className="topbar">
        <button
          ref={menuRef}
          type="button"
          className="btn btn--ghost topbar__menu"
          aria-label={open ? "Close menu" : "Open menu"}
          aria-expanded={open}
          aria-controls="mobile-drawer"
          onClick={() => setOpen(!open)}
        >
          <Icon icon={Menu} size="lg" />
        </button>
        <span className="t-title topbar__wordmark">MetalHops</span>
      </header>
      {open && (
        <>
          <div className="drawer-scrim" data-testid="drawer-scrim" onClick={() => setOpen(false)} />
          <div ref={drawerRef} id="mobile-drawer" className="drawer" role="dialog" aria-modal="true" aria-label="Menu" onKeyDown={onKeyDown}>
            <button type="button" className="btn btn--ghost drawer__close" aria-label="Close menu" onClick={() => setOpen(false)}>
              <Icon icon={X} size="lg" />
            </button>
            <Sidebar onSelect={() => setOpen(false)} />
          </div>
        </>
      )}
    </div>
  );
}
