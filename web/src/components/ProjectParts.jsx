import { PageHeader } from "./AppShell.jsx";
import { Timestamp } from "./Controls.jsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";

export const projectSections = [
  { id: "overview", label: "Overview" },
  { id: "tasks", label: "Tasks" },
  { id: "posts", label: "Posts" },
  { id: "wiki", label: "Wiki" },
];

export const taskStateLabels = {
  ready: "Ready",
  in_progress: "In progress",
  blocked: "Blocked",
  done: "Done",
  cancelled: "Cancelled",
};

export function ProjectHeader({ project, activeSection, onBack, onNavigate, actions = null }) {
  return (
    <>
      <PageHeader title={project.name} description={project.purpose}>
        <div className="project-header-actions">
          {actions}
          <button className="back-button" type="button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to projects</button>
        </div>
      </PageHeader>
      <nav className="project-tabs" aria-label="Project sections">
        {projectSections.map((section) => (
          <button
            key={section.id}
            type="button"
            aria-current={activeSection === section.id ? "page" : undefined}
            onClick={() => onNavigate(section.id)}
          >
            {section.label}
          </button>
        ))}
      </nav>
    </>
  );
}

export function TaskState({ state }) {
  return <span className={`task-state task-state--${state}`}>{taskStateLabels[state] || state}</span>;
}

export function ProgressMeter({ counts, compact = false }) {
  const activeTotal = Math.max(0, counts.total - counts.cancelled);
  const percentage = activeTotal ? Math.round((counts.done / activeTotal) * 100) : 0;
  return (
    <div className={`progress-meter${compact ? " progress-meter--compact" : ""}`}>
      <div className="progress-track" aria-hidden="true"><span style={{ width: `${percentage}%` }} /></div>
      <span>{counts.done} of {activeTotal} complete</span>
    </div>
  );
}

export function DurableActivity({ activity }) {
  if (!activity) return <span className="muted">No durable activity yet</span>;
  return (
    <div className="durable-activity">
      <strong>{activity.title}</strong>
      <span>{activity.kind.replaceAll("_", " ")} · <Timestamp value={activity.occurred} compact /></span>
    </div>
  );
}

export function InlineState({ status, error, empty = false, emptyTitle = "Nothing here yet", emptyDetail = "This space will fill from canonical Commons data." }) {
  if (status === "loading") return <div className="inline-state" role="status">Loading current data…</div>;
  if (status === "error") return <div className="inline-state inline-state--error" role="alert"><strong>Couldn’t load this view</strong><span>{error}</span></div>;
  if (empty) return <div className="inline-state"><strong>{emptyTitle}</strong><span>{emptyDetail}</span></div>;
  return null;
}

function flushParagraph(output, lines) {
  if (!lines.length) return;
  output.push(<p key={`paragraph-${output.length}`}>{lines.join(" ")}</p>);
  lines.length = 0;
}

export function DurableDocument({ body }) {
  const output = [];
  const paragraph = [];
  let bullets = [];
  let code = [];
  let inCode = false;

  function flushBullets() {
    if (!bullets.length) return;
    output.push(<ul key={`list-${output.length}`}>{bullets.map((item, index) => <li key={`${index}:${item}`}>{item}</li>)}</ul>);
    bullets = [];
  }
  function flushCode() {
    if (!code.length) return;
    output.push(<pre key={`code-${output.length}`}><code>{code.join("\n")}</code></pre>);
    code = [];
  }

  for (const sourceLine of body.split("\n")) {
    const line = sourceLine.trimEnd();
    if (line.trim().startsWith("```")) {
      flushParagraph(output, paragraph);
      flushBullets();
      if (inCode) flushCode();
      inCode = !inCode;
      continue;
    }
    if (inCode) {
      code.push(sourceLine);
      continue;
    }
    if (/^#{1,3}\s/.test(line)) {
      flushParagraph(output, paragraph);
      flushBullets();
      const level = line.match(/^#+/)[0].length;
      const Heading = `h${Math.min(3, level + 1)}`;
      output.push(<Heading key={`heading-${output.length}`}>{line.replace(/^#{1,3}\s+/, "")}</Heading>);
      continue;
    }
    if (line.startsWith("- ")) {
      flushParagraph(output, paragraph);
      bullets.push(line.slice(2));
      continue;
    }
    if (!line.trim()) {
      flushParagraph(output, paragraph);
      flushBullets();
      continue;
    }
    flushBullets();
    paragraph.push(line.trim());
  }
  flushParagraph(output, paragraph);
  flushBullets();
  flushCode();
  return <div className="durable-document">{output}</div>;
}
