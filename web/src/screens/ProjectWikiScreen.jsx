import { useRef, useState } from "react";
import Plus from "../icons/Plus.tsx";
import BookOpen from "../icons/BookOpen.tsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { SearchField, Timestamp } from "../components/Controls.jsx";
import { InlineState } from "../components/ProjectParts.jsx";
import { WikiRevisionDialog } from "../components/WikiRevisionDialog.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { useCursorPager } from "../hooks/useCursorPager.js";
import { useResource } from "../hooks/useResource.js";

export function ProjectWikiScreen({ projectInfo, onOpenPage }) {
  const projectID = projectInfo.project.id;
  const [search, setSearch] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const resumeRef = useRef(null);
  const pager = useCursorPager(20);
  const auth = useAuthSession();
  const resource = useResource((signal) => commonsAdapter.readWikiPages(projectID, {
    q: search, cursor: pager.cursor, limit: pager.limit,
  }, signal), [projectID, search, pager.cursor, pager.limit]);

  function updateSearch(value) {
    setSearch(value);
    pager.reset();
  }

  function requestAuth(resume) {
    auth.expire();
    resumeRef.current = resume || null;
    setLoginOpen(true);
  }

  function startNewPage() {
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

  return (
    <section className="project-core-section wiki-workspace" aria-labelledby="wiki-title">
      <div className="project-core-toolbar">
        <div><h2 id="wiki-title">Wiki</h2><p>Revisioned project knowledge that remains readable across tasks, sessions, and time.</p></div>
        <button className="primary-button" type="button" onClick={startNewPage}><Plus aria-hidden="true" />New page</button>
      </div>
      <div className="wiki-search-row">
        <SearchField label="Search wiki" value={search} onChange={updateSearch} placeholder="Search page titles and summaries" />
        <span>{resource.data ? `${resource.data.total} ${resource.data.total === 1 ? "page" : "pages"}` : "—"}</span>
      </div>
      {!resource.data ? <InlineState status={resource.status} error={resource.error} /> : null}
      {resource.data?.items.length ? (
        <div className="wiki-page-list">
          {resource.data.items.map((page) => (
            <button key={page.id} type="button" onClick={() => onOpenPage(page.slug)}>
              <span className="wiki-page-icon" aria-hidden="true"><BookOpen /></span>
              <span className="wiki-page-copy"><strong>{page.title}</strong><span>{page.summary}</span><small>Revision {page.revision} · <Timestamp value={page.updated} compact /></small></span>
            </button>
          ))}
        </div>
      ) : null}
      {resource.data && !resource.data.items.length ? <InlineState empty emptyTitle="No wiki pages found" emptyDetail={search ? "Try a different server-bounded search." : "Create the first durable page for this project."} /> : null}
      {resource.data ? (
        <nav className="simple-pager" aria-label="Wiki pagination">
          <button type="button" disabled={!pager.canPrevious} onClick={pager.previous}>Previous</button>
          <span>Page {pager.page} · {resource.data.total} total</span>
          <button type="button" disabled={!resource.data.nextCursor} onClick={() => pager.next(resource.data.nextCursor)}>Next</button>
        </nav>
      ) : null}
      <WikiRevisionDialog
        open={editorOpen}
        projectID={projectID}
        session={auth.session}
        onClose={() => setEditorOpen(false)}
        onSaved={({ slug }) => { setEditorOpen(false); onOpenPage(slug); }}
        onConflict={() => {}}
        onOpenConflict={(conflictSlug) => { setEditorOpen(false); onOpenPage(conflictSlug); }}
        onAuthRequired={() => requestAuth(() => setEditorOpen(true))}
      />
      <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeRef.current = null; }} onAuthenticated={authenticated} />
    </section>
  );
}
