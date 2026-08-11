function optionalString(value) {
  return typeof value === "string" && value ? value : "";
}

export function normalizeProvenance(value, fallback = {}) {
  const raw = value !== null && typeof value === "object" && !Array.isArray(value) ? value : {};
  const source = raw.source !== null && typeof raw.source === "object" && !Array.isArray(raw.source) ? raw.source : null;
  const recordedBy = raw.recorded_by !== null && typeof raw.recorded_by === "object" && !Array.isArray(raw.recorded_by) ? raw.recorded_by : null;
  return {
    kind: raw.kind === "historical" ? "historical" : "attested",
    actor: optionalString(raw.actor || fallback.actor),
    session: optionalString(raw.session || fallback.session),
    purpose: optionalString(raw.purpose || fallback.purpose),
    role: optionalString(raw.role),
    confidence: ["verified", "supported", "uncertain"].includes(raw.confidence) ? raw.confidence : "",
    recordedAt: optionalString(raw.recorded_at || fallback.recorded_at),
    source: source ? {
      kind: optionalString(source.kind), stableID: optionalString(source.stable_id), digest: optionalString(source.digest), occurredAt: optionalString(source.occurred_at),
    } : null,
    recordedBy: recordedBy ? { actor: optionalString(recordedBy.actor), session: optionalString(recordedBy.session) } : null,
  };
}

export function normalizeContributors(value) {
  if (!Array.isArray(value)) return [];
  return value.slice(0, 20).map((item) => normalizeProvenance(item)).filter((item) => item.session);
}
