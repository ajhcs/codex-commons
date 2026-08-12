import companionAsset from "../assets/identity/commons-companion.png";

export function CommonsCompanion({ state = "idle", size = "medium", className = "", decorative = true }) {
  return (
    <span
      className={`commons-companion commons-companion--${size}${className ? ` ${className}` : ""}`}
      data-state={state}
      aria-hidden={decorative ? "true" : undefined}
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : "Commons memory companion"}
    >
      <span className="commons-companion__orbit">
        <img src={companionAsset} alt="" width="1254" height="1254" decoding="async" draggable="false" />
      </span>
      <span className="commons-companion__signal" />
    </span>
  );
}

export default CommonsCompanion;
