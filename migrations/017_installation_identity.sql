-- Stable per-database installation identity and append-only restore evidence.
-- Identity is generated exactly once with randomblob(16) and is never derived
-- from review_secret. Restore evidence is created empty; this migration does
-- not record a drill or change backup/restore status.
ALTER TABLE installation_status ADD COLUMN installation_id BLOB NOT NULL DEFAULT x'00000000000000000000000000000000' CHECK(length(installation_id)=16);
UPDATE installation_status SET installation_id=randomblob(16) WHERE id=1;
CREATE UNIQUE INDEX installation_status_installation_id ON installation_status(installation_id);

CREATE TRIGGER installation_status_identity_no_update BEFORE UPDATE OF installation_id ON installation_status
BEGIN SELECT RAISE(ABORT,'installation identity is immutable'); END;
CREATE TRIGGER installation_status_identity_no_delete BEFORE DELETE ON installation_status
BEGIN SELECT RAISE(ABORT,'installation identity is immutable'); END;

CREATE TABLE installation_restore_evidence (
  drill_id TEXT PRIMARY KEY CHECK (length(trim(drill_id)) BETWEEN 1 AND 200),
  installation_id BLOB NOT NULL REFERENCES installation_status(installation_id),
  recorded_at TEXT NOT NULL CHECK (length(trim(recorded_at)) BETWEEN 1 AND 100),
  restore_receipt_digest TEXT NOT NULL UNIQUE CHECK (
    length(restore_receipt_digest)=64
    AND restore_receipt_digest GLOB replace(hex(zeroblob(32)),'0','[0-9a-f]')
  ),
  restored_backup_digest TEXT NOT NULL CHECK (
    length(restored_backup_digest)=64
    AND restored_backup_digest GLOB replace(hex(zeroblob(32)),'0','[0-9a-f]')
  ),
  schema_version INTEGER NOT NULL CHECK (schema_version BETWEEN 1 AND 10000),
  release_id TEXT NOT NULL CHECK (length(trim(release_id)) BETWEEN 1 AND 200)
) STRICT;

CREATE TRIGGER installation_restore_evidence_no_update BEFORE UPDATE ON installation_restore_evidence
BEGIN SELECT RAISE(ABORT,'append-only restore evidence'); END;
CREATE TRIGGER installation_restore_evidence_no_delete BEFORE DELETE ON installation_restore_evidence
BEGIN SELECT RAISE(ABORT,'append-only restore evidence'); END;
