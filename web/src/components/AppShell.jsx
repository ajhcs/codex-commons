import { useState } from "react";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { createIdempotencyKey, LoginDialog, SessionControl } from "./AuthControls.jsx";
import { SettingsDialog } from "./SettingsDialog.jsx";

import BookOpen from "../icons/BookOpen.tsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import FileDocument from "../icons/FileDocument.tsx";
import Folder from "../icons/Folder.tsx";

const navigation = [
  { id: "posts", label: "Posts", icon: FileDocument },
  { id: "projects", label: "Projects", icon: Folder },
];

export function AppShell({ route, onNavigate, railContent = null, children }) {
  const auth = useAuthSession();
  const [collapsed, setCollapsed] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [accountMessage, setAccountMessage] = useState("");
  const primaryRoute = route === "project" ? "projects" : route === "post" ? "posts" : route;
  async function signOut() {
    if (!auth.session?.authenticated) return;
    try {
      const session = await commonsAdapter.logout(auth.session.csrfToken, createIdempotencyKey());
      auth.accept(session);
      setAccountMessage("Signed out");
    } catch (error) {
      setAccountMessage(error.message);
      if (error.status === 401 || error.code === "csrf_failed") auth.expire();
    }
  }

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    setAccountMessage("");
  }

  return (
    <>
    <div className={`app-shell${collapsed ? " app-shell--collapsed" : ""}`}>
      <aside className="left-rail">
        <div className="brand-row">
          <span className="brand-icon" aria-hidden="true"><BookOpen /></span>
          <span className="brand-name">Codex Commons</span>
          <button
            className="rail-collapse"
            type="button"
            aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
            aria-expanded={!collapsed}
            onClick={() => setCollapsed((value) => !value)}
          >
            <ChevronLeft />
          </button>
        </div>
        <nav className="primary-nav" aria-label="Primary navigation">
          {navigation.map(({ id, label, icon: Icon }) => (
            <button key={id} type="button" className={primaryRoute === id ? "is-current" : ""} aria-current={primaryRoute === id ? "page" : undefined} onClick={() => onNavigate(id)}>
              <Icon aria-hidden="true" /><span>{label}</span>
            </button>
          ))}
        </nav>
        {railContent ? <div className="rail-context">{railContent}</div> : null}
        <div className="rail-footer">
          <button className="rail-settings" type="button" onClick={() => setSettingsOpen(true)}><span aria-hidden="true">Aa</span><strong>Settings</strong></button>
          <SessionControl status={auth.status} session={auth.session} onSignIn={() => setLoginOpen(true)} onSignOut={signOut} />
          {accountMessage ? <small role="status">{accountMessage}</small> : null}
        </div>
      </aside>
      <main className="main-plane">{children}</main>
      </div>
      <SettingsDialog open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <LoginDialog open={loginOpen} onClose={() => setLoginOpen(false)} onAuthenticated={authenticated} />
    </>
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
