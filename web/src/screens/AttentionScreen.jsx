import { useMemo, useState } from "react";
import { PageHeader, Notice } from "../components/AppShell.jsx";
import { ActionButton, CursorPager, DateFilter, SearchField, Select, SeverityIndicator, Timestamp } from "../components/Controls.jsx";
import { DataTable } from "../components/DataTable.jsx";
import { TaskPreviewDialog } from "../components/TaskPreviewDialog.jsx";
import { fixtureAdapter } from "../data/adapter.js";
import { useCursorPager } from "../hooks/useCursorPager.js";
import { useResource } from "../hooks/useResource.js";

export function AttentionScreen() {
  const [filters, setFilters] = useState({ source: "", owner: "", severity: "", project: "", dateRange: "7d", search: "" });
  const [notice, setNotice] = useState("");
  const [selectedTask, setSelectedTask] = useState(null);
  const pager = useCursorPager(10);
  const updatedFrom = filters.dateRange ? new Date(Date.parse("2026-08-09T12:00:00Z") - (filters.dateRange === "7d" ? 7 : 30) * 86400000).toISOString() : "";
  const resource = useResource(
    (signal) => fixtureAdapter.readAttention({
      q: filters.search,
      source: filters.source,
      owner: filters.owner,
      severity: filters.severity,
      project: filters.project,
      updated_from: updatedFrom,
      cursor: pager.cursor,
      limit: pager.limit,
    }, signal),
    [filters.source, filters.owner, filters.severity, filters.project, filters.dateRange, filters.search, pager.cursor, pager.limit],
  );
  const data = resource.data;

  function updateFilter(key, value) {
    setFilters((current) => ({ ...current, [key]: value }));
    pager.reset();
  }

  const columns = useMemo(() => [
    { key: "severity", label: "Severity", className: "cell-severity", render: (item) => <SeverityIndicator severity={item.severity} /> },
    { key: "item", label: "Item", className: "cell-primary", render: (item) => <div className="primary-cell"><strong>{item.title}</strong><span>{item.id}</span></div> },
    { key: "source", label: "Project · source", render: (item) => <div className="source-cell"><strong>{item.project}</strong><span>{sourceLabel(item.source.kind)}</span></div> },
    { key: "owner", label: "Owner", render: (item) => item.owner },
    { key: "action", label: "Next action", render: (item) => <ActionButton destination={item.destination} onOpen={(destination) => {
      if (destination.kind === "task") {
        setSelectedTask({ ref: destination.ref, title: item.title, project: item.project, owner: item.owner, updated: item.updated, nextAction: item.nextAction });
      } else {
        setNotice(`Canonical ${destination.kind} reference ready: ${destination.ref}`);
      }
    }}>{item.nextAction}</ActionButton> },
    { key: "updated", label: "Updated", render: (item) => <Timestamp value={item.updated} /> },
  ], []);

  const facets = data?.facets || {
    sources: [], owners: [], severities: [], projects: [],
    owners_truncated: false, projects_truncated: false,
  };
  return (
    <>
      <PageHeader title="General" description="Operational overview for shared engineering workspaces" />
      <div className="section-tabs" aria-label="General sections"><span aria-current="page">Needs attention</span></div>
      <section className="content-section" aria-labelledby="attention-title">
        <div className="section-heading-row"><div><h2 id="attention-title">Needs attention</h2><p>Explicit attention events with deterministic next actions.</p></div><span className="result-count">{data?.total ?? "—"} items</span></div>
        <div className="filter-bar">
          <Select label="Source" value={filters.source} onChange={(value) => updateFilter("source", value)} options={facets.sources} allLabel="All sources" />
          <Select label="Owner" value={filters.owner} onChange={(value) => updateFilter("owner", value)} options={facets.owners} optionsTruncated={facets.owners_truncated} allLabel="All owners" />
          <Select label="Severity" value={filters.severity} onChange={(value) => updateFilter("severity", value)} options={facets.severities} allLabel="All severities" />
          <Select label="Project" value={filters.project} onChange={(value) => updateFilter("project", value)} options={facets.projects} optionsTruncated={facets.projects_truncated} allLabel="All projects" />
          <DateFilter value={filters.dateRange} onChange={(value) => updateFilter("dateRange", value)} />
          <SearchField label="Search attention items" value={filters.search} onChange={(value) => updateFilter("search", value)} placeholder="Search items or IDs" />
        </div>
        <Notice message={notice} onDismiss={() => setNotice("")} />
        <DataTable
          label="Needs attention"
          columns={columns}
          items={data?.items || []}
          rowKey={(item) => item.id}
          loading={resource.status === "loading" && !data}
          error={resource.error}
        />
        <CursorPager page={pager.page} canPrevious={pager.canPrevious} canNext={Boolean(data?.nextCursor)} limit={pager.limit} total={data?.total || 0} onPrevious={pager.previous} onNext={() => pager.next(data?.nextCursor)} onLimit={pager.setLimit} />
      </section>
      <TaskPreviewDialog task={selectedTask} onClose={() => setSelectedTask(null)} />
    </>
  );
}

function sourceLabel(kind) {
  return ({ github_check: "GitHub check", github_pull_request: "Pull request", task: "Task" })[kind] || kind;
}
