import { useEffect, useMemo, useRef, useState } from "react";
import { copyText, manualCopyShortcut } from "../browser/copyText.js";
import Copy from "../icons/Copy.tsx";
import { PageHeader, Notice } from "../components/AppShell.jsx";
import { CursorPager, SearchField, Select, Timestamp } from "../components/Controls.jsx";
import { DataTable } from "../components/DataTable.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useCursorPager } from "../hooks/useCursorPager.js";
import { useResource } from "../hooks/useResource.js";

export function PeopleScreen() {
  const [filters, setFilters] = useState({ search: "", project: "", execution: "", host: "", connectivity: "" });
  const [selected, setSelected] = useState(null);
  const [notice, setNotice] = useState("");
  const pager = useCursorPager(10);
  const resource = useResource(
    (signal) => commonsAdapter.readPeople({
      q: filters.search,
      project: filters.project,
      execution: filters.execution,
      host: filters.host,
      host_connected: filters.connectivity ? filters.connectivity === "connected" : undefined,
      cursor: pager.cursor,
      limit: pager.limit,
    }, signal),
    [filters.search, filters.project, filters.execution, filters.host, filters.connectivity, pager.cursor, pager.limit],
  );
  const data = resource.data;
  const facets = data?.facets || { projects: [], execution: [], hosts: [], connectivity: [] };

  function updateFilter(key, value) {
    setFilters((current) => ({ ...current, [key]: value }));
    pager.reset();
  }

  const columns = useMemo(() => [
    { key: "session", label: "Session ID", className: "cell-session", render: (item) => <button className="session-link" type="button" onClick={() => setSelected(item)}>{item.session}</button> },
    { key: "purpose", label: "Purpose", className: "cell-primary", render: (item) => <div className="primary-cell"><strong>{item.purpose}</strong><span>{item.actor}</span></div> },
    { key: "project", label: "Project", render: (item) => item.project },
    { key: "execution", label: "Execution", render: (item) => <FactBadge tone={item.execution === "executing" ? "success" : "neutral"}>{item.execution === "executing" ? "Executing" : "Not running"}</FactBadge> },
    { key: "host", label: "Host", render: (item) => <div className="host-facts"><strong>{item.host}</strong><span className={item.connected ? "connected" : "disconnected"}>{item.connected ? "Connected" : "Disconnected"}</span></div> },
    { key: "activity", label: "Last activity", render: (item) => <Timestamp value={item.updated} /> },
    { key: "loaded", label: "Loaded", render: (item) => <span className="loaded-fact">{item.loaded}</span> },
  ], []);

  return (
    <>
      <PageHeader title="People" description="Agent sessions across shared engineering workspaces" />
      <section className="content-section content-section--flush" aria-labelledby="people-title">
        <h2 className="sr-only" id="people-title">Sessions</h2>
        <div className="filter-bar filter-bar--people">
          <SearchField label="Search sessions" value={filters.search} onChange={(value) => updateFilter("search", value)} placeholder="Search sessions or purpose" />
          <Select label="Project" value={filters.project} onChange={(value) => updateFilter("project", value)} options={facets.projects} allLabel="All projects" />
          <Select label="Execution" value={filters.execution} onChange={(value) => updateFilter("execution", value)} options={facets.execution} allLabel="All executions" />
          <Select label="Host" value={filters.host} onChange={(value) => updateFilter("host", value)} options={facets.hosts} allLabel="All hosts" />
          <Select label="Connectivity" value={filters.connectivity} onChange={(value) => updateFilter("connectivity", value)} options={facets.connectivity} allLabel="Any connection" />
        </div>
        <Notice message={notice} onDismiss={() => setNotice("")} />
        <DataTable label="People and sessions" columns={columns} items={data?.items || []} rowKey={(item) => item.session} loading={resource.status === "loading" && !data} error={resource.error} emptyMessage="No sessions match these filters." />
        <CursorPager page={pager.page} canPrevious={pager.canPrevious} canNext={Boolean(data?.nextCursor)} limit={pager.limit} total={data?.total || 0} onPrevious={pager.previous} onNext={() => pager.next(data?.nextCursor)} onLimit={pager.setLimit} />
      </section>
      <SessionDialog session={selected} onClose={() => setSelected(null)} onCopied={() => setNotice(`Copied ${selected?.session || "session ID"}`)} />
    </>
  );
}

function FactBadge({ tone, children }) {
  return <span className={`fact-badge fact-badge--${tone}`}>{children}</span>;
}

function SessionDialog({ session, onClose, onCopied }) {
  const ref = useRef(null);
  const [copyStatus, setCopyStatus] = useState("");
  useEffect(() => {
    const dialog = ref.current;
    if (session && dialog && !dialog.open) dialog.showModal();
    if (!session && dialog?.open) dialog.close();
    if (session) setCopyStatus("");
  }, [session]);
  if (!session) return <dialog ref={ref} />;

  async function copyIdentity() {
    const copied = await copyText(session.session);
    if (copied) {
      onCopied();
      onClose();
      return;
    }
    setCopyStatus(`Copy isn’t available here. Select the session ID and press ${manualCopyShortcut()}.`);
  }

  return (
    <dialog ref={ref} className="session-dialog" onClose={onClose}>
      <div className="dialog-heading"><div><span className="dialog-kicker">Session details</span><h2>{session.session}</h2></div><button type="button" onClick={onClose}>Close</button></div>
      <dl className="session-fact-grid">
        <div><dt>Purpose</dt><dd>{session.purpose}</dd></div>
        <div><dt>Project</dt><dd>{session.project}</dd></div>
        <div><dt>Execution</dt><dd>{session.execution === "executing" ? "Executing" : "Not running"}</dd></div>
        <div><dt>Host connectivity</dt><dd>{session.host} · {session.connected ? "Connected" : "Disconnected"}</dd></div>
        <div><dt>Last activity</dt><dd><Timestamp value={session.updated} compact /></dd></div>
        <div><dt>Loaded</dt><dd>{session.loaded}</dd></div>
      </dl>
      <div className="dialog-note">This view reports observable facts only. It does not infer whether the agent is available.</div>
      {copyStatus ? <p className="form-message form-message--error" role="status">{copyStatus}</p> : null}
      <div className="dialog-actions"><button className="secondary-button" type="button" onClick={onClose}>Done</button><button className="primary-button" type="button" onClick={copyIdentity}><Copy aria-hidden="true" />Copy session ID</button></div>
    </dialog>
  );
}
