import { commonsAdapter } from "../data/adapter.js";
import { useResource } from "../hooks/useResource.js";
import { InlineState, ProgressMeter, TaskState } from "../components/ProjectParts.jsx";
import { Timestamp } from "../components/Controls.jsx";

export function ProjectOverviewCoreScreen({ projectInfo, onOpenTask, onNavigate, onEditMilestone }) {
  const projectID = projectInfo.project.id;
  const resource = useResource(
    (signal) => commonsAdapter.readProjectTasks(projectID, { state: "", milestone: "", cursor: "", limit: 20 }, signal),
    [projectID],
  );

  if (!resource.data) return <InlineState status={resource.status} error={resource.error} />;
  const tasks = resource.data;
  const counts = tasks.stateCounts;
  const currentWork = tasks.items.filter((task) => ["ready", "in_progress", "blocked"].includes(task.state)).slice(0, 6);

  return (
    <div className="project-overview-core">
      <section className="milestone-callout" aria-labelledby="current-milestone-title">
        <div>
          <span>Current milestone</span>
          <h2 id="current-milestone-title">{projectInfo.activeMilestone?.title || "No active milestone"}</h2>
          <p>{projectInfo.project.now || "No current focus has been recorded."}</p>
        </div>
        <div className="milestone-callout-actions">
          {projectInfo.activeMilestone ? <button type="button" onClick={onEditMilestone}>Edit milestone</button> : null}
          <button type="button" onClick={() => onNavigate("tasks")}>View roadmap</button>
        </div>
      </section>

      <section className="continuity-summary" aria-label="Project progress">
        <ActivityChart activity={projectInfo.activity} />
        <div className="continuity-metric continuity-metric--progress">
          <span>Task progress</span>
          <strong>{counts.done}<small> / {Math.max(0, counts.total - counts.cancelled)}</small></strong>
          <ProgressMeter counts={counts} compact />
        </div>
        <div className="continuity-metric">
          <span>In progress</span>
          <strong>{counts.in_progress}</strong>
          <small>Canonical tasks</small>
        </div>
        <div className={`continuity-metric${counts.blocked ? " has-blockers" : ""}`}>
          <span>Blocked</span>
          <strong>{counts.blocked}</strong>
          <small>{counts.blocked === 1 ? "Task needs a dependency" : "Tasks need dependencies"}</small>
        </div>
      </section>

      <section className="current-work-section" aria-labelledby="current-work-title">
        <div className="current-work-heading">
          <div><h2 id="current-work-title">Current work</h2><p>The bounded set of tasks that can change the next project action.</p></div>
          <button type="button" onClick={() => onNavigate("tasks")}>View all tasks</button>
        </div>
        {currentWork.length ? (
          <div className="current-work-list">
            {currentWork.map((task) => (
              <button key={task.id} type="button" onClick={() => onOpenTask(task.id)}>
                <span className="work-state"><TaskState state={task.state} /></span>
                <span className="work-copy"><strong>{task.title}</strong><small>{task.id}</small></span>
                <span className="work-updated"><Timestamp value={task.updated} compact /></span>
              </button>
            ))}
          </div>
        ) : <InlineState empty emptyTitle="No current work" emptyDetail="Ready, in-progress, and blocked tasks will appear here." />}
        {tasks.nextCursor ? <p className="bounded-note">Overview shows a bounded task preview. Open Tasks for the complete paginated view.</p> : null}
      </section>
      <p className="snapshot-note">Snapshot {projectInfo.snapshot.relative} · {projectInfo.activity.timezone}</p>
    </div>
  );
}

function ActivityChart({ activity }) {
  const maximum = Math.max(...activity.days.map((day) => day.count), 1);
  return (
    <figure className="activity-chart continuity-activity">
      <figcaption><strong>Durable activity</strong><span>14 days</span></figcaption>
      <div className="activity-bars" aria-hidden="true">
        {activity.days.map((day) => <span key={day.day} style={{ "--bar-size": `${Math.max(8, (day.count / maximum) * 100)}%` }} />)}
      </div>
      <span className="sr-only">Daily action-changing activity: {activity.days.map((day) => `${day.day}, ${day.count}`).join("; ")}</span>
    </figure>
  );
}
