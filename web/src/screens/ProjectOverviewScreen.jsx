import { useMemo, useState } from "react";
import Bell from "../icons/Bell.tsx";
import Branch from "../icons/Branch.tsx";
import Clipboard from "../icons/Clipboard.tsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import Members from "../icons/Members.tsx";
import { Notice, PageHeader } from "../components/AppShell.jsx";
import { ActionButton, SeverityIndicator, Timestamp } from "../components/Controls.jsx";
import { DataTable } from "../components/DataTable.jsx";
import { TaskPreviewDialog } from "../components/TaskPreviewDialog.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useResource } from "../hooks/useResource.js";

const projectTabs = ["Overview", "Tasks", "Posts", "Wiki", "GitHub", "History"];

export function ProjectOverviewScreen({ onBack, projectID }) {
  const [notice, setNotice] = useState("");
  const [selectedTask, setSelectedTask] = useState(null);
  const resource = useResource((signal) => commonsAdapter.readProjectOverview(
    projectID,
    { attention_limit: 3, work_limit: 4 },
    signal,
  ), [projectID]);
  const data = resource.data;
  const attentionColumns = useMemo(() => [
    { key: "severity", label: "Severity", className: "cell-severity", render: (item) => <SeverityIndicator severity={item.severity} /> },
    { key: "item", label: "Item", className: "cell-primary", render: (item) => <div className="primary-cell"><strong>{item.title}</strong><span>{item.id}</span></div> },
    { key: "source", label: "Canonical source", render: (item) => <div className="source-cell"><strong>{item.source.ref}</strong><span>{item.source.kind.replaceAll("_", " ")}</span></div> },
    { key: "owner", label: "Owner", render: (item) => item.owner },
    { key: "action", label: "Next action", render: (item) => <ActionButton destination={item.destination} onOpen={(destination) => {
      if (destination.kind === "task") {
        setSelectedTask({ ref: destination.ref, title: item.title, project: data?.project.name, owner: item.owner, updated: item.updated, nextAction: item.nextAction });
      } else {
        setNotice(`Canonical reference ready: ${destination.ref}`);
      }
    }}>{item.nextAction}</ActionButton> },
    { key: "updated", label: "Updated", render: (item) => <Timestamp value={item.updated} compact /> },
  ], [data?.project.name]);
  const workColumns = useMemo(() => [
    { key: "work", label: "Work item", className: "cell-primary", render: (item) => <div className="primary-cell"><strong>{item.title}</strong><span>{item.id}</span></div> },
    { key: "state", label: "State", render: (item) => <span className="state-label">{item.state.replaceAll("_", " ")}</span> },
    { key: "owner", label: "Assignee", render: (item) => item.owner || "Unassigned" },
    { key: "updated", label: "Updated", render: (item) => <Timestamp value={item.updated} compact /> },
    { key: "action", label: "Task", render: (item) => <ActionButton destination={item.target} onOpen={(destination) => setSelectedTask({ ...item, ref: destination.ref, project: data?.project.name, nextAction: "Continue this task from its latest canonical state" })}>Open task</ActionButton> },
  ], [data?.project.name]);

  if (resource.status === "loading" && !data) return <OverviewSkeleton />;
  if (resource.status === "error") return (
    <div className="page-error" role="alert">
      <button className="back-button" type="button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to projects</button>
      <h1>Project overview unavailable</h1>
      <p>{resource.error}</p>
    </div>
  );

  return (
    <>
      <PageHeader title={data.project.name} description={data.project.purpose}>
        <button className="back-button" type="button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to projects</button>
      </PageHeader>
      <nav className="project-tabs" aria-label="Project sections">
        {projectTabs.map((tab, index) => <span key={tab} aria-current={index === 0 ? "page" : undefined}>{tab}</span>)}
      </nav>
      <section className="overview-summary" aria-label="Project summary">
        <ActivityChart activity={data.activity} />
        <Metric icon={Bell} value={data.metrics.attention_total} label="Attention items" detail={`${data.metrics.attention_high} high priority`} />
        <Metric icon={Clipboard} value={data.metrics.open_work} label="Open work items" detail="Across canonical tasks" />
        <Metric icon={Branch} value={data.metrics.merged_pull_requests.available ? data.metrics.merged_pull_requests.count : "—"} label="Merged PRs" detail={data.metrics.merged_pull_requests.available ? "This week" : "GitHub not synced"} />
        <Metric icon={Members} value={data.metrics.active_sessions} label="Active sessions" detail="Connected right now" />
      </section>
      <Notice message={notice} onDismiss={() => setNotice("")} />
      <OverviewSection title="Needs attention" total={data.attention.total}>
        <DataTable label="Project needs attention" columns={attentionColumns} items={data.attention.items} rowKey={(item) => item.id} />
      </OverviewSection>
      <OverviewSection title="Current work" total={data.work.total}>
        <DataTable label="Project current work" columns={workColumns} items={data.work.items} rowKey={(item) => item.id} />
      </OverviewSection>
      <p className="snapshot-note">Snapshot {data.snapshot.relative} · {data.activity.timezone}</p>
      <TaskPreviewDialog task={selectedTask} onClose={() => setSelectedTask(null)} />
    </>
  );
}

function ActivityChart({ activity }) {
  const maximum = Math.max(...activity.days.map((day) => day.count), 1);
  return (
    <figure className="activity-chart">
      <figcaption><strong>Activity</strong><span>14 days</span></figcaption>
      <div className="activity-bars" aria-hidden="true">
        {activity.days.map((day) => <span key={day.day} style={{ "--bar-size": `${Math.max(10, (day.count / maximum) * 100)}%` }} />)}
      </div>
      <span className="sr-only">Daily action-changing activity: {activity.days.map((day) => `${day.day}, ${day.count}`).join("; ")}</span>
    </figure>
  );
}

function Metric({ icon: Icon, value, label, detail }) {
  return (
    <div className="metric"><Icon aria-hidden="true" /><strong>{value}</strong><span>{label}</span><small>{detail}</small></div>
  );
}

function OverviewSection({ title, total, children }) {
  return (
    <section className="overview-table-section">
      <div className="overview-section-heading"><h2>{title}</h2><span>{total} total</span></div>
      {children}
    </section>
  );
}

function OverviewSkeleton() {
  return (
    <div className="overview-skeleton" role="status" aria-label="Loading project overview">
      <div /><div /><div /><div /><div /><div />
    </div>
  );
}
