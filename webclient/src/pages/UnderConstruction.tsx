import { useEffect } from "react";
import { Link, useLocation } from "react-router-dom";
import { Construction } from "lucide-react";
import { Icon } from "../ui/Icon";

// Under-construction state: built on the design system's .list-state; the honest placeholder for unfinished sections.
export function UnderConstruction({ title }: { title: string }) {
  const { pathname } = useLocation();

  useEffect(() => {
    document.title = `${title} · MetalHops`;
  }, [title]);

  return (
    <section className="list-state">
      <Icon icon={Construction} />
      <h1 className="t-title">{title}</h1>
      <p>This part of MetalHops is under construction.</p>
      <div className="page-actions">
        {pathname !== "/" && <Link className="btn btn--secondary" to="/">Go to Home</Link>}
        <Link className="btn btn--secondary" to="/profile">Go to Profile</Link>
      </div>
    </section>
  );
}
