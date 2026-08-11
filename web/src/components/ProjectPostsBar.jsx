import ChevronLeft from "../icons/ChevronLeft.tsx";
import { projectSections } from "./ProjectParts.jsx";

export function ProjectPostsBar({ projectInfo, onBack, onNavigate }) {
  const project = projectInfo?.project;
  return (
    <div className="project-posts-bar">
      <button type="button" className="project-posts-back" onClick={onBack} aria-label="Back to projects"><ChevronLeft aria-hidden="true" /><span>{project?.name || "Project"}</span></button>
      <nav aria-label="Project sections">
        {projectSections.map((section) => <button key={section.id} type="button" aria-current={section.id === "posts" ? "page" : undefined} onClick={() => onNavigate(section.id)}>{section.label}</button>)}
      </nav>
    </div>
  );
}
