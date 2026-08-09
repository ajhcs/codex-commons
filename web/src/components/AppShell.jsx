import BookOpen from "../icons/BookOpen.tsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import Folder from "../icons/Folder.tsx";
import Home from "../icons/Home.tsx";
import Members from "../icons/Members.tsx";

const navigation = [
  { id: "attention", label: "General", icon: Home },
  { id: "projects", label: "Projects", icon: Folder },
  { id: "people", label: "People", icon: Members },
];

export function AppShell({ route, onNavigate, presence, presenceTotal, presenceStatus, children }) {
  const primaryRoute = route === "project" ? "projects" : route;
  return (
    <div className="app-shell">
      <aside className="left-rail">
        <div className="brand-row">
          <span className="brand-icon" aria-hidden="true"><BookOpen /></span>
          <span className="brand-name">Commons</span>
          <button className="rail-collapse" type="button" aria-label="Collapse navigation"><ChevronLeft /></button>
        </div>
        <nav className="primary-nav" aria-label="Primary navigation">
          {navigation.map(({ id, label, icon: Icon }) => (
            <button key={id} type="button" className={primaryRoute === id ? "is-current" : ""} aria-current={primaryRoute === id ? "page" : undefined} onClick={() => onNavigate(id)}>
              <Icon aria-hidden="true" /><span>{label}</span>
            </button>
          ))}
        </nav>
        <section className="presence-panel" aria-labelledby="presence-title">
          <div className="presence-heading"><span id="presence-title">Live presence</span><span>{presenceTotal == null ? "—" : `${presence.length} of ${presenceTotal}`}</span></div>
          <ul>
            {presence.map((item) => (
              <li key={item.session}>
                <span className={`presence-dot ${item.connected ? "is-connected" : ""}`} aria-hidden="true" />
                <div>
                  <div className="presence-name"><strong>{item.actor}</strong><span>{item.updated.relative}</span></div>
                  <div className="presence-facts"><span>{item.execution === "executing" ? "Executing" : "Not running"}</span><span>{item.purpose}</span></div>
                  <span className="sr-only">Host {item.connected ? "connected" : "disconnected"}</span>
                </div>
              </li>
            ))}
            {presenceStatus === "loading" ? <li className="presence-state">Loading session facts…</li> : null}
            {presenceStatus === "error" ? <li className="presence-state">Presence unavailable</li> : null}
          </ul>
        </section>
        <div className="rail-footnote">Presence reports execution and connectivity separately.</div>
      </aside>
      <main className="main-plane">{children}</main>
    </div>
  );
}

export function PageHeader({ title, description, children }) {
  return (
    <header className="page-header">
      <div><h1>{title}</h1><p>{description}</p></div>
      {children ? <div className="page-header-actions">{children}</div> : null}
    </header>
  );
}

export function Notice({ message, onDismiss }) {
  if (!message) return null;
  return (
    <div className="notice" role="status">
      <span>{message}</span>
      <button type="button" onClick={onDismiss}>Dismiss</button>
    </div>
  );
}
