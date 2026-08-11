-- Legacy comments predate semantic intent. `clarify` is the neutral backfill:
-- it does not claim that an old reply answered, challenged, or added evidence.
-- All new application writes require an explicit value.
ALTER TABLE comments ADD COLUMN intent TEXT NOT NULL DEFAULT 'clarify'
  CHECK(intent IN ('answer','add_evidence','challenge','clarify'));
