import { useMemo, useRef, useState } from "react";
import Folder from "../icons/Folder.tsx";
import Plus from "../icons/Plus.tsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { PageHeader } from "../components/AppShell.jsx";
import { CursorPager, SearchField } from "../components/Controls.jsx";
import { DataTable } from "../components/DataTable.jsx";
import { ProjectEditorDialog } from "../components/ProjectManagementDialogs.jsx";
import { DurableActivity, ProgressMeter } from "../components/ProjectParts.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { useCursorPager } from "../hooks/useCursorPager.js";
import { useResource } from "../hooks/useResource.js";

export function ProjectsCoreScreen({ onNavigate }) {
  const [search, setSearch] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const resumeRef = useRef(null);
  const pager = useCursorPager(10);
  const auth = useAuthSession();
  const resource = useResource(
    (signal) => commonsAdapter.readProjects({ q: search, cursor: pager.cursor, limit: pager.limit }, signal),
    [search, pager.cursor, pager.limit, refreshKey],
  );
  const data = resource.data;
  const columns = useMemo(() => [
    {
      key: "project", label: "Project", className: "cell-project-name", render: (item) => (
        <button className="project-link project-link--stacked" type="button" onClick={() => onNavigate("project", item.id)}>
          <Folder aria-hidden="true" /><span><strong>{item.name}</strong><small>{item.purpose}</small></span>
        </button>
      ),
    },
    { key: "milestone", label: "Current milestone", render: (item) => item.activeMilestone ? <div className="milestone-cell"><strong>{item.activeMilestone.title}</strong><span>{item.activeMilestone.status}</span></div> : <span className="muted">No active milestone</span> },
    { key: "progress", label: "Task progress", render: (item) => <ProgressMeter counts={item.taskCounts} compact /> },
    { key: "blocked", label: "Blocked", render: (item) => <span className={`blocked-count${item.taskCounts.blocked ? " has-blockers" : ""}`}><strong>{item.taskCounts.blocked}</strong><span>{item.taskCounts.blocked === 1 ? "task" : "tasks"}</span></span> },
    { key: "activity", label: "Last durable activity", render: (item) => <DurableActivity activity={item.lastDurableActivity} /> },
  ], [onNavigate]);

  function updateSearch(value) {
    setSearch(value);
    pager.reset();
  }

  function requestAuth(resume) {
    auth.expire();
    resumeRef.current = resume || null;
    setLoginOpen(true);
  }

  function startNewProject() {
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
    <>
      <PageHeader title="Projects" description="Durable workspaces for milestones, tasks, posts, and shared knowledge">
        <button className="primary-button" type="button" onClick={startNewProject}><Plus aria-hidden="true" />New project</button>
      </PageHeader>
      <section className="content-section content-section--flush" aria-labelledby="projects-title">
        <div className="list-toolbar">
          <SearchField label="Search projects" value={search} onChange={updateSearch} placeholder="Search projects, purpose, or milestone" />
          <span className="result-count">{data?.total ?? "—"} projects</span>
        </div>
        <h2 className="sr-only" id="projects-title">Projects</h2>
        <DataTable label="Projects" columns={columns} items={data?.items || []} rowKey={(item) => item.id} loading={resource.status === "loading" && !data} error={resource.error} emptyMessage="No projects match this search." />
        <CursorPager page={pager.page} canPrevious={pager.canPrevious} canNext={Boolean(data?.nextCursor)} limit={pager.limit} total={data?.total || 0} onPrevious={pager.previous} onNext={() => pager.next(data?.nextCursor)} onLimit={pager.setLimit} />
      </section>
      <ProjectEditorDialog
        open={editorOpen}
        session={auth.session}
        onClose={() => setEditorOpen(false)}
        onSaved={({ id }) => { setEditorOpen(false); setRefreshKey((value) => value + 1); onNavigate("project", id); }}
        onAuthRequired={() => requestAuth(() => setEditorOpen(true))}
      />
      <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeRef.current = null; }} onAuthenticated={authenticated} />
    </>
  );
}
