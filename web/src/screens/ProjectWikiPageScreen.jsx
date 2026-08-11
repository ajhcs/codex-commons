import { useEffect, useRef, useState } from "react";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import History from "../icons/History.tsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { Timestamp } from "../components/Controls.jsx";
import { DurableDocument, InlineState } from "../components/ProjectParts.jsx";
import { WikiRevisionDialog } from "../components/WikiRevisionDialog.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { useResource } from "../hooks/useResource.js";
import { ProvenanceDisclosure } from "../components/Provenance.jsx";

export function ProjectWikiPageScreen({ projectInfo, slug, revision = 0, onBack, onOpenRevision, onOpenCurrent }) {
  const projectID = projectInfo.project.id;
  const [refreshKey, setRefreshKey] = useState(0);
  const [editorOpen, setEditorOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [history, setHistory] = useState({ loaded: false, items: [], nextCursor: "" });
  const [historyStatus, setHistoryStatus] = useState({ loading: false, error: "" });
  const resumeRef = useRef(null);
  const historyControllerRef = useRef(null);
  const auth = useAuthSession();
  const resource = useResource(
    (signal) => revision
      ? commonsAdapter.readWikiRevision(projectID, slug, revision, signal)
      : commonsAdapter.readWikiPage(projectID, slug, signal),
    [projectID, slug, revision, refreshKey],
  );

  useEffect(() => {
    historyControllerRef.current?.abort();
    setHistoryOpen(false);
    setHistory({ loaded: false, items: [], nextCursor: "" });
    setHistoryStatus({ loading: false, error: "" });
  }, [projectID, slug]);

  useEffect(() => () => historyControllerRef.current?.abort(), []);

  function requestAuth(resume) {
    auth.expire();
    resumeRef.current = resume || null;
    setLoginOpen(true);
  }

  function startEdit() {
    if (auth.session?.authenticated) setEditorOpen(true);
    else requestAuth(() => setEditorOpen(true));
  }

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    const resume = resumeRef.current;
    resumeRef.current = null;
    resume?.();
  }

  async function readHistory(cursor = "") {
    historyControllerRef.current?.abort();
    const controller = new AbortController();
    historyControllerRef.current = controller;
    setHistoryStatus({ loading: true, error: "" });
    try {
      const page = await commonsAdapter.readWikiRevisions(projectID, slug, { cursor, limit: 20 }, controller.signal);
      setHistory((current) => {
        const known = new Set(current.items.map((item) => item.revision));
        return {
          loaded: true,
          items: cursor ? [...current.items, ...page.items.filter((item) => !known.has(item.revision))] : page.items,
          nextCursor: page.nextCursor,
        };
      });
      setHistoryStatus({ loading: false, error: "" });
    } catch (error) {
      if (error.name !== "AbortError") setHistoryStatus({ loading: false, error: error.message });
    }
  }

  function toggleHistory() {
    const next = !historyOpen;
    setHistoryOpen(next);
    if (next && !history.loaded && !historyStatus.loading) readHistory();
  }

  if (!resource.data) return <InlineState status={resource.status} error={resource.error} />;
  const page = resource.data.page;
  const isHistorical = Boolean(revision);

  return (
    <article className="wiki-reader">
      <div className="wiki-reader-command">
        <button className="detail-back" type="button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to wiki</button>
        {!isHistorical ? <button className="secondary-button" type="button" onClick={startEdit}>New revision</button> : <button className="secondary-button" type="button" onClick={onOpenCurrent}>Open current revision</button>}
      </div>
      {isHistorical ? <div className="historical-banner" role="status">You’re reading historical revision {page.revision}. It is immutable.</div> : null}
      <main className="wiki-document-plane">
        <header>
          <span>Project wiki</span>
          <h2>{page.title}</h2>
          <p>{page.summary}</p>
          <small>Revision {page.revision} · <Timestamp value={page.created} compact /></small>
          <ProvenanceDisclosure provenance={page.provenance} recorded={page.created} label="Revision provenance" compact />
        </header>
        <DurableDocument body={page.body} />
      </main>
      <section className={`wiki-history${historyOpen ? " is-open" : ""}`} aria-labelledby="wiki-history-title">
        <button className="wiki-history-toggle" type="button" aria-expanded={historyOpen} onClick={toggleHistory}>
          <span><History aria-hidden="true" /><strong id="wiki-history-title">Revision history</strong></span>
          <span>{history.loaded ? `${history.items.length} loaded` : "Open"}</span>
        </button>
        {historyOpen ? (
          <div className="wiki-history-body">
            {historyStatus.loading && !history.loaded ? <p className="muted">Loading revision metadata…</p> : null}
            {history.loaded && history.items.length ? (
              <ol>
                {history.items.map((item) => (
                  <li key={item.revision} className={item.revision === page.revision ? "is-current" : ""}>
                    <button type="button" disabled={item.revision === page.revision} onClick={() => onOpenRevision(item.revision)}>
                      <strong>Revision {item.revision}</strong><span>{item.summary}</span><small><Timestamp value={item.created} compact /></small>
                    </button>
                    <ProvenanceDisclosure provenance={item.provenance} recorded={item.created} label={`Revision ${item.revision} provenance`} compact />
                  </li>
                ))}
              </ol>
            ) : null}
            {history.loaded && !history.items.length ? <p>No revision metadata is available.</p> : null}
            {history.nextCursor ? <button className="history-load-more" type="button" disabled={historyStatus.loading} onClick={() => readHistory(history.nextCursor)}>{historyStatus.loading ? "Loading…" : "Load older revisions"}</button> : null}
            {historyStatus.error ? <p className="form-message form-message--error" role="status">{historyStatus.error}</p> : null}
          </div>
        ) : null}
      </section>
      {!isHistorical ? (
        <WikiRevisionDialog
          open={editorOpen}
          projectID={projectID}
          page={page}
          session={auth.session}
          onClose={() => setEditorOpen(false)}
          onSaved={() => { setEditorOpen(false); setRefreshKey((value) => value + 1); }}
          onConflict={() => setRefreshKey((value) => value + 1)}
          onAuthRequired={() => requestAuth(() => setEditorOpen(true))}
        />
      ) : null}
      <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeRef.current = null; }} onAuthenticated={authenticated} />
    </article>
  );
}
