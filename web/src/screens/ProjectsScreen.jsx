import { useMemo, useState } from "react";
import Folder from "../icons/Folder.tsx";
import { PageHeader } from "../components/AppShell.jsx";
import { CursorPager, SearchField, Timestamp } from "../components/Controls.jsx";
import { DataTable } from "../components/DataTable.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useCursorPager } from "../hooks/useCursorPager.js";
import { useResource } from "../hooks/useResource.js";

export function ProjectsScreen({ onNavigate }) {
  const [search, setSearch] = useState("");
  const pager = useCursorPager(10);
  const resource = useResource(
    (signal) => commonsAdapter.readProjects({ q: search, cursor: pager.cursor, limit: pager.limit }, signal),
    [search, pager.cursor, pager.limit],
  );
  const data = resource.data;
  const columns = useMemo(() => [
    { key: "name", label: "Name", className: "cell-project-name", render: (item) => <button className="project-link" type="button" onClick={() => onNavigate("project", item.id)}><Folder aria-hidden="true" /><span>{item.name}</span></button> },
    { key: "purpose", label: "Purpose", className: "cell-copy", render: (item) => item.purpose },
    { key: "current", label: "Current work", className: "cell-copy", render: (item) => <div className="primary-cell"><strong>{item.currentWork}</strong><span>{item.openTasks} open work items</span></div> },
    { key: "sessions", label: "Active sessions", render: (item) => <span className="count-with-label"><strong>{item.activeSessions}</strong><span>{item.activeSessions === 1 ? "session" : "sessions"}</span></span> },
    { key: "activity", label: "Last activity", render: (item) => <Timestamp value={item.lastActivity} /> },
  ], [onNavigate]);

  function updateSearch(value) {
    setSearch(value);
    pager.reset();
  }

  return (
    <>
      <PageHeader title="Projects" description="Canonical workspaces for shared engineering efforts" />
      <section className="content-section content-section--flush" aria-labelledby="projects-title">
        <div className="list-toolbar">
          <SearchField label="Search projects" value={search} onChange={updateSearch} placeholder="Search projects or purpose" />
          <span className="result-count">{data?.total ?? "—"} projects</span>
        </div>
        <h2 className="sr-only" id="projects-title">Projects</h2>
        <DataTable label="Projects" columns={columns} items={data?.items || []} rowKey={(item) => item.id} loading={resource.status === "loading" && !data} error={resource.error} emptyMessage="No projects match this search." />
        <CursorPager page={pager.page} canPrevious={pager.canPrevious} canNext={Boolean(data?.nextCursor)} limit={pager.limit} total={data?.total || 0} onPrevious={pager.previous} onNext={() => pager.next(data?.nextCursor)} onLimit={pager.setLimit} />
      </section>
    </>
  );
}
