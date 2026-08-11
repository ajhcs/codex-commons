import { useState } from "react";
import Copy from "../icons/Copy.tsx";
import { Timestamp } from "./Controls.jsx";

function friendlyName(provenance) {
  return provenance?.purpose || provenance?.actor || provenance?.role || "Recorded contributor";
}

function RecordedTime({ value }) {
  if (!value) return null;
  if (typeof value !== "string") return <Timestamp value={value} />;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return (
    <time dateTime={date.toISOString()}>
      {new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(date)} UTC
    </time>
  );
}

function ProvenanceFacts({ provenance, recorded }) {
  const [copied, setCopied] = useState(false);
  if (!provenance?.session) return null;

  async function copySession() {
    try {
      if (globalThis.navigator?.clipboard?.writeText) {
        await globalThis.navigator.clipboard.writeText(provenance.session);
      } else {
        const field = document.createElement("textarea");
        field.value = provenance.session;
        field.setAttribute("readonly", "");
        field.style.position = "fixed";
        field.style.opacity = "0";
        document.body.append(field);
        field.select();
        const copied = document.execCommand("copy");
        field.remove();
        if (!copied) throw new Error("Copy is unavailable");
      }
      setCopied(true);
      globalThis.setTimeout(() => setCopied(false), 1800);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="provenance-facts">
      <dl>
        <div><dt>Recorded contributor</dt><dd>{friendlyName(provenance)}</dd></div>
        {provenance.role ? <div><dt>Role</dt><dd>{provenance.role.replaceAll("_", " ")}</dd></div> : null}
        {provenance.confidence ? <div><dt>Confidence</dt><dd>{provenance.confidence}</dd></div> : null}
        <div><dt>Session ID</dt><dd><code>{provenance.session}</code><button type="button" aria-label={`Copy session ID ${provenance.session}`} onClick={copySession}><Copy aria-hidden="true" /></button></dd></div>
        {recorded || provenance.recordedAt ? <div><dt>Recorded</dt><dd><RecordedTime value={provenance.recordedAt || recorded} /></dd></div> : null}
      </dl>
      <p>Historical provenance—not live presence, assignment, reachability, or a chat control.</p>
      <span className="sr-only" aria-live="polite">{copied ? "Session ID copied" : ""}</span>
    </div>
  );
}

export function ProvenanceDisclosure({ provenance, recorded, label = "Provenance", compact = false }) {
  if (!provenance?.session) return null;
  return (
    <details className={`provenance-disclosure${compact ? " provenance-disclosure--compact" : ""}`}>
      <summary><span>{friendlyName(provenance)}</span><small>{label}</small></summary>
      <ProvenanceFacts provenance={provenance} recorded={recorded} />
    </details>
  );
}

export function ContributorProvenance({ contributors = [], truncated = false }) {
  const bounded = contributors.slice(0, 20).filter((item) => item?.session);
  if (!bounded.length) return null;
  const roles = [...new Set(bounded.map((item) => item.role).filter(Boolean))].slice(0, 3).map((role) => role.replaceAll("_", " "));
  return (
    <details className="contributor-provenance">
      <summary><span>{bounded.length}{truncated ? "+" : ""} recorded contributor{bounded.length === 1 ? "" : "s"}</span>{roles.length ? <small>{roles.join(" · ")}</small> : null}</summary>
      <div className="contributor-provenance-list">
        {bounded.map((provenance) => <ProvenanceFacts key={`${provenance.session}:${provenance.role || "contributor"}`} provenance={provenance} />)}
      </div>
    </details>
  );
}
