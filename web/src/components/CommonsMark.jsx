import connectingMark from "../assets/identity/commons-mark-connecting.png";
import resolvedMark from "../assets/identity/commons-mark-resolved.png";

export function CommonsMark({ state = "resolved", size = "medium", className = "", decorative = true }) {
  return (
    <span
      className={`commons-mark commons-mark--${size} commons-mark--${state}${className ? ` ${className}` : ""}`}
      role={decorative ? undefined : "img"}
      aria-hidden={decorative ? "true" : undefined}
      aria-label={decorative ? undefined : "Commons"}
    >
      <img className="commons-mark-image commons-mark-image--connecting" src={connectingMark} width="1254" height="1254" alt="" decoding="async" />
      <img className="commons-mark-image commons-mark-image--resolved" src={resolvedMark} width="1254" height="1254" alt="" decoding="async" />
    </span>
  );
}

export default CommonsMark;
