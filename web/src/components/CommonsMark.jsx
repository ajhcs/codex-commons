import Link from "../icons/Link.tsx";

export function CommonsMark({ state = "idle", size = "medium", className = "", decorative = true }) {
  return (
    <span
      className={`commons-mark commons-mark--${size} commons-mark--${state}${className ? ` ${className}` : ""}`}
      aria-hidden={decorative ? "true" : undefined}
      aria-label={decorative ? undefined : "Commons"}
    >
      <Link />
    </span>
  );
}

export default CommonsMark;
