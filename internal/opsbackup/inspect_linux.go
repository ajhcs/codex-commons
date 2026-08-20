//go:build linux

package opsbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"codex-commons/internal/opsfs"
	_ "modernc.org/sqlite"
)

func marshalReceipt(doc receipt) ([]byte, error) {
	if err := validateReceipt(doc); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	if _, err := parseReceipt(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseReceipt(body []byte) (receipt, error) {
	var doc receipt
	if len(body) == 0 || len(body) > opsfs.MaxReceiptBytes {
		return doc, fmt.Errorf("receipt size")
	}
	if !utf8.Valid(body) {
		return doc, fmt.Errorf("receipt is not valid UTF-8")
	}
	if bytes.Count(body, []byte{'\n'}) != 1 || body[len(body)-1] != '\n' {
		return doc, fmt.Errorf("receipt line count")
	}
	for _, b := range body {
		if b == 0x7f || (b < 0x20 && b != '\n') {
			return doc, fmt.Errorf("receipt contains a control character")
		}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return receipt{}, fmt.Errorf("receipt json: %w", err)
	}
	if dec.More() {
		return receipt{}, fmt.Errorf("receipt trailing data")
	}
	if err := validateReceipt(doc); err != nil {
		return receipt{}, err
	}
	return doc, nil
}

// validatePublishedSet requires leaf, leaf.sha256, and leaf.receipt.json to be
// one coherent published set: each is a validated regular file, the checksum
// names this absolute path, and the receipt File/SHA256 match the file digest.
func validatePublishedSet(dir *opsfs.Dir, leaf string) error {
	if dir == nil {
		return fmt.Errorf("closed directory")
	}
	path := dir.Path + "/" + leaf
	if err := opsfs.ValidAbsPath(path); err != nil {
		return err
	}
	digest, err := dir.SHA256(leaf)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	sumBody, err := dir.ReadValidatedRegular(leaf+".sha256", opsfs.MaxChecksumBytes)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	sumDigest, err := opsfs.ParseSHA256Sum(sumBody, path)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if sumDigest != digest {
		return fmt.Errorf("checksum digest mismatch")
	}
	recBody, err := dir.ReadValidatedRegular(leaf+".receipt.json", opsfs.MaxReceiptBytes)
	if err != nil {
		return fmt.Errorf("receipt: %w", err)
	}
	doc, err := parseReceipt(recBody)
	if err != nil {
		return fmt.Errorf("receipt: %w", err)
	}
	if doc.File != leaf || doc.SHA256 != digest {
		return fmt.Errorf("receipt does not match backup")
	}
	return nil
}

func validateReceipt(doc receipt) error {
	if doc.Integrity != "ok" || doc.ForeignKeys != 0 {
		return fmt.Errorf("receipt integrity")
	}
	if err := opsfs.ValidAbsPath("/x/" + doc.File); err != nil {
		return fmt.Errorf("receipt file: %w", err)
	}
	if len(doc.SHA256) != 64 || len(doc.SchemaDigest) != 64 || len(doc.SelectedDigest) != 64 {
		return fmt.Errorf("receipt digest")
	}
	for _, digest := range []string{doc.SHA256, doc.SchemaDigest, doc.SelectedDigest} {
		for i := 0; i < len(digest); i++ {
			c := digest[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return fmt.Errorf("receipt digest")
			}
		}
	}
	if strings.ContainsAny(doc.Counts, " \t\r\n\"'\\") {
		return fmt.Errorf("receipt counts")
	}
	if doc.Schema < 0 {
		return fmt.Errorf("receipt schema")
	}
	if !safeStamp(doc.VerifiedAt) {
		return fmt.Errorf("receipt timestamp")
	}
	return nil
}

func safeStamp(stamp string) bool {
	if stamp == "" || len(stamp) > 32 {
		return false
	}
	for _, r := range stamp {
		if unicode.IsControl(r) {
			return false
		}
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == ':' || r == 'T' || r == 'Z':
		default:
			return false
		}
	}
	return true
}

func inspectBackup(ctx context.Context, uri string) (backupMeta, error) {
	var out backupMeta
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return out, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return out, err
	}
	if integrity != "ok" {
		return out, fmt.Errorf("integrity_check %q", integrity)
	}
	var fk int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fk); err != nil {
		return out, err
	}
	if fk != 0 {
		return out, fmt.Errorf("foreign_key_check %d", fk)
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&out.schema); err != nil {
		return out, err
	}

	var schemaSQL string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(group_concat(type||':'||name||':'||tbl_name||':'||coalesce(sql,''), char(10)), '') FROM (SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name,tbl_name)`).Scan(&schemaSQL); err != nil {
		return out, err
	}
	// sqlite3 CLI hashes the selected text plus its trailing record newline.
	out.schemaDigest = sha256Hex([]byte(schemaSQL + "\n"))

	var selected string
	var selectedCount int
	var hasSelected int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='archaeology_selected_imports'`).Scan(&hasSelected); err != nil {
		return out, err
	}
	if hasSelected == 1 {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports`).Scan(&selectedCount); err != nil {
			return out, err
		}
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(group_concat(id||':'||batch_id||':'||principal||':'||request_key||':'||selection_digest||':'||manifest_digest||':'||outcome_ids_json||':'||result_json||':'||created_at,'|'),'') FROM (SELECT * FROM archaeology_selected_imports ORDER BY id)`).Scan(&selected); err != nil {
			return out, err
		}
	}
	// Command substitution strips sqlite3's trailing newline before hashing.
	out.selectedDigest = sha256Hex([]byte(selected))

	var projects, tasks, batches int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&projects); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&tasks); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_batches`).Scan(&batches); err != nil {
		return out, err
	}
	out.counts = fmt.Sprintf("%d,%d,%d,%d", projects, tasks, batches, selectedCount)
	if strings.ContainsAny(out.counts, " \t\n\r\"'\\") {
		return out, fmt.Errorf("invalid counts")
	}
	if bytes.Contains([]byte(selected), []byte{0}) {
		return out, fmt.Errorf("selected digest input")
	}
	return out, nil
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func recordStatus(ctx context.Context, pin *opsfs.PinnedDB, status string, verifiedAt any) error {
	if pin == nil {
		return nil
	}
	if status != "verified" && status != "failed" {
		return fmt.Errorf("invalid backup status")
	}
	db, err := opsfs.OpenPinned(ctx, pin, true)
	if err != nil {
		return err
	}
	defer db.Close()
	var hasTable int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='installation_status'`).Scan(&hasTable); err != nil {
		return err
	}
	if hasTable != 1 {
		return nil
	}
	_, err = db.ExecContext(ctx, `UPDATE installation_status SET backup_status=?,backup_verified_at=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1`, status, verifiedAt)
	if err != nil {
		return err
	}
	return pin.Revalidate()
}
