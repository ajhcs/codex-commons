//go:build linux

package opsbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"codex-commons/internal/opsfs"
	sqlite "modernc.org/sqlite"
)

var nowUTC = func() time.Time { return time.Now().UTC() }

type backuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

type receipt struct {
	File           string `json:"file"`
	SHA256         string `json:"sha256"`
	VerifiedAt     string `json:"verified_at"`
	Schema         int    `json:"schema"`
	SchemaDigest   string `json:"schema_digest"`
	Counts         string `json:"counts"`
	SelectedDigest string `json:"selected_digest"`
	Integrity      string `json:"integrity"`
	ForeignKeys    int    `json:"foreign_keys"`
}

type backupMeta struct {
	schema         int
	schemaDigest   string
	counts         string
	selectedDigest string
}

// Backup copies COMMONS_DB into COMMONS_BACKUP_DIR/daily with checksum and
// sanitized receipt sidecars. The backup-root directory descriptor is locked
// with a nonblocking flock; a pathname .backup.lock is never opened.
func Backup(ctx context.Context, dbPath, backupDir string) (published string, err error) {
	restore := opsfs.SetPrivateUmask()
	defer restore()

	if err := opsfs.ValidAbsPath(dbPath); err != nil {
		return "", err
	}
	if err := opsfs.ValidAbsPath(backupDir); err != nil {
		return "", err
	}

	root, err := opsfs.OpenBackupDir(backupDir)
	if err != nil {
		return "", fmt.Errorf("backup root must be an existing owned 0700 directory: %w", err)
	}
	defer root.Close()
	if err := root.FlockExclusiveNonblock(); err != nil {
		return "", err
	}
	if err := root.ValidateExact(opsfs.DirMode); err != nil {
		return "", err
	}

	daily, err := root.OpenOrCreateDir("daily")
	if err != nil {
		return "", err
	}
	defer daily.Close()
	monthly, err := root.OpenOrCreateDir("monthly")
	if err != nil {
		return "", err
	}
	defer monthly.Close()

	src, err := opsfs.PinDatabaseUnlocked(dbPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	if err := opsfs.WaitHold(ctx, opsfs.HoldPreOpen); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := src.Revalidate(); err != nil {
		return "", err
	}
	if err := root.ValidateExact(opsfs.DirMode); err != nil {
		return "", err
	}

	stamp := nowUTC().Format("20060102T150405Z")
	leaf := "commons-" + stamp + ".sqlite3"
	dailyPath := daily.Path + "/" + leaf
	if err := opsfs.ValidAbsPath(dailyPath); err != nil {
		return "", err
	}
	absent, err := daily.Absent(leaf)
	if err != nil {
		return "", err
	}
	if !absent {
		return "", fmt.Errorf("destination exists")
	}

	tmp, tmpName, err := daily.MkdirPrivate("backup")
	if err != nil {
		return "", err
	}
	publishedLeaves := map[string]struct{}{}
	cleanup := func() {
		if tmp == nil {
			return
		}
		for _, name := range []string{leaf, leaf + ".sha256", leaf + ".receipt.json"} {
			if _, done := publishedLeaves[name]; done {
				continue
			}
			_ = tmp.Unlink(name)
		}
		_ = tmp.Close()
		tmp = nil
		_ = daily.RemoveDir(tmpName)
	}
	defer cleanup()

	fail := func(err error) (string, error) {
		_ = recordStatus(ctx, src, "failed", nil)
		return "", err
	}
	failAfter := func(err error) (string, error) {
		_ = recordStatus(ctx, src, "failed", nil)
		return dailyPath, err
	}

	srcDB, err := opsfs.OpenPinned(ctx, src, false)
	if err != nil {
		return fail(err)
	}
	defer srcDB.Close()
	if err := src.Revalidate(); err != nil {
		return fail(err)
	}
	if err := backupTo(ctx, srcDB, tmp.ChildURI(leaf)); err != nil {
		return fail(err)
	}
	if err := tmp.FinalizeRegularFile(leaf); err != nil {
		return fail(err)
	}

	meta, err := inspectBackup(ctx, tmp.ChildURI(leaf)+"?mode=ro&_pragma=foreign_keys(1)")
	if err != nil {
		return fail(err)
	}
	digest, err := tmp.SHA256(leaf)
	if err != nil {
		return fail(err)
	}
	checksumBody, err := opsfs.FormatSHA256Sum(digest, dailyPath)
	if err != nil {
		return fail(err)
	}
	receiptBody, err := marshalReceipt(receipt{
		File:           leaf,
		SHA256:         digest,
		VerifiedAt:     stamp,
		Schema:         meta.schema,
		SchemaDigest:   meta.schemaDigest,
		Counts:         meta.counts,
		SelectedDigest: meta.selectedDigest,
		Integrity:      "ok",
		ForeignKeys:    0,
	})
	if err != nil {
		return fail(err)
	}
	if err := tmp.WriteExclusive(leaf+".sha256", checksumBody); err != nil {
		return fail(err)
	}
	if err := tmp.WriteExclusive(leaf+".receipt.json", receiptBody); err != nil {
		return fail(err)
	}

	if err := opsfs.WaitHold(ctx, opsfs.HoldPrePublish); err != nil {
		return fail(err)
	}
	if err := revalidateLocked(ctx, src, root, daily, tmp); err != nil {
		return fail(err)
	}

	if _, err := daily.PublishNoReplace(tmp, leaf, leaf); err != nil {
		return fail(err)
	}
	publishedLeaves[leaf] = struct{}{}

	if err := opsfs.WaitHold(ctx, opsfs.HoldBetweenPublications); err != nil {
		return failAfter(err)
	}
	if err := revalidateLocked(ctx, src, root, daily, tmp); err != nil {
		return failAfter(err)
	}
	if _, err := daily.PublishNoReplace(tmp, leaf+".sha256", leaf+".sha256"); err != nil {
		return failAfter(err)
	}
	publishedLeaves[leaf+".sha256"] = struct{}{}
	if _, err := daily.PublishNoReplace(tmp, leaf+".receipt.json", leaf+".receipt.json"); err != nil {
		return failAfter(err)
	}
	publishedLeaves[leaf+".receipt.json"] = struct{}{}

	if err := opsfs.WaitHold(ctx, opsfs.HoldAfterPublications); err != nil {
		return failAfter(err)
	}
	if err := revalidateLocked(ctx, src, root, daily, monthly); err != nil {
		return failAfter(err)
	}
	if err := validatePublishedSet(daily, leaf); err != nil {
		return failAfter(err)
	}

	monthLeaf := "commons-" + nowUTC().Format("2006-01") + ".sqlite3"
	if err := publishMonthly(ctx, daily, monthly, leaf, monthLeaf, stamp, digest, meta); err != nil {
		return failAfter(err)
	}
	if err := opsfs.WaitHold(ctx, opsfs.HoldAfterMonthlyPublications); err != nil {
		return failAfter(err)
	}
	if err := revalidateLocked(ctx, src, root, daily, monthly); err != nil {
		return failAfter(err)
	}
	if err := validatePublishedSet(monthly, monthLeaf); err != nil {
		return failAfter(err)
	}
	if err := retainValidated(ctx, daily, dailyKeep); err != nil {
		return failAfter(err)
	}
	if err := retainValidated(ctx, monthly, monthlyKeep); err != nil {
		return failAfter(err)
	}

	verifiedAt := nowUTC().Format(time.RFC3339)
	if err := recordStatus(ctx, src, "verified", verifiedAt); err != nil {
		return dailyPath, err
	}
	return dailyPath, nil
}

func revalidateLocked(ctx context.Context, src *opsfs.PinnedDB, dirs ...*opsfs.Dir) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if src != nil {
		if err := src.Revalidate(); err != nil {
			return err
		}
	}
	for _, dir := range dirs {
		if dir == nil {
			continue
		}
		if err := dir.ValidateExact(opsfs.DirMode); err != nil {
			return err
		}
	}
	return nil
}

func publishMonthly(ctx context.Context, daily, monthly *opsfs.Dir, dailyLeaf, monthLeaf, stamp, digest string, meta backupMeta) error {
	if err := daily.ValidateExact(opsfs.DirMode); err != nil {
		return err
	}
	if err := monthly.ValidateExact(opsfs.DirMode); err != nil {
		return err
	}
	absent, err := monthly.Absent(monthLeaf)
	if err != nil {
		return err
	}
	if !absent {
		if err := validatePublishedSet(monthly, monthLeaf); err != nil {
			return fmt.Errorf("monthly destination exists: %w", err)
		}
		return nil
	}
	if err := validatePublishedSet(daily, dailyLeaf); err != nil {
		return err
	}

	tmp, tmpName, err := monthly.MkdirPrivate("monthly")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Unlink(monthLeaf)
		_ = tmp.Unlink(monthLeaf + ".sha256")
		_ = tmp.Unlink(monthLeaf + ".receipt.json")
		_ = tmp.Close()
		_ = monthly.RemoveDir(tmpName)
	}()

	if err := daily.CopyExclusive(dailyLeaf, tmp, monthLeaf); err != nil {
		return err
	}
	copyDigest, err := tmp.SHA256(monthLeaf)
	if err != nil {
		return err
	}
	if copyDigest != digest {
		return fmt.Errorf("monthly copy digest mismatch")
	}
	monthlyPath := monthly.Path + "/" + monthLeaf
	checksumBody, err := opsfs.FormatSHA256Sum(copyDigest, monthlyPath)
	if err != nil {
		return err
	}
	receiptBody, err := marshalReceipt(receipt{
		File:           monthLeaf,
		SHA256:         copyDigest,
		VerifiedAt:     stamp,
		Schema:         meta.schema,
		SchemaDigest:   meta.schemaDigest,
		Counts:         meta.counts,
		SelectedDigest: meta.selectedDigest,
		Integrity:      "ok",
		ForeignKeys:    0,
	})
	if err != nil {
		return err
	}
	if err := tmp.WriteExclusive(monthLeaf+".sha256", checksumBody); err != nil {
		return err
	}
	if err := tmp.WriteExclusive(monthLeaf+".receipt.json", receiptBody); err != nil {
		return err
	}

	// Track only leaves proven published by this invocation. On later failure,
	// roll those back in reverse order using trusted (dev,ino) identity so a
	// preexisting/planted occupant is never unlinked.
	var published []opsfs.PublishedIdentity
	failPublished := func(err error) error {
		return errors.Join(err, rollbackThisInvocationMonthly(ctx, monthly, published))
	}
	record := func(id opsfs.PublishedIdentity) {
		if id.Proven() {
			published = append(published, id)
		}
	}

	id, err := monthly.PublishNoReplace(tmp, monthLeaf, monthLeaf)
	record(id)
	if err != nil {
		return failPublished(err)
	}
	if err := opsfs.WaitHold(ctx, opsfs.HoldBetweenMonthlyPublications); err != nil {
		return failPublished(err)
	}
	if err := monthly.ValidateExact(opsfs.DirMode); err != nil {
		return failPublished(err)
	}
	id, err = monthly.PublishNoReplace(tmp, monthLeaf+".sha256", monthLeaf+".sha256")
	record(id)
	if err != nil {
		return failPublished(err)
	}
	if err := opsfs.WaitHold(ctx, opsfs.HoldBetweenMonthlyPublications); err != nil {
		return failPublished(err)
	}
	if err := monthly.ValidateExact(opsfs.DirMode); err != nil {
		return failPublished(err)
	}
	id, err = monthly.PublishNoReplace(tmp, monthLeaf+".receipt.json", monthLeaf+".receipt.json")
	record(id)
	if err != nil {
		return failPublished(err)
	}
	if err := monthly.ValidateExact(opsfs.DirMode); err != nil {
		return failPublished(err)
	}
	if err := validatePublishedSet(monthly, monthLeaf); err != nil {
		return failPublished(err)
	}
	return nil
}

// rollbackThisInvocationMonthly removes only leaves proven published by this
// monthly publication attempt, in reverse order. Each unlink revalidates the
// trusted (dev,ino) before the name-based unlinkat(2). A same-uid actor can
// still replace the name after the last check; that residual race is not
// claimed closed. Preexisting or planted occupants with a different identity
// are skipped, never removed.
func rollbackThisInvocationMonthly(ctx context.Context, dir *opsfs.Dir, published []opsfs.PublishedIdentity) error {
	var errs []error
	for i := len(published) - 1; i >= 0; i-- {
		id := published[i]
		if err := unlinkValidated(ctx, dir, id.Name, id.Dev, id.Ino); err != nil {
			errs = append(errs, fmt.Errorf("rollback %s: %w", id.Name, err))
		}
	}
	return errors.Join(errs...)
}

func backupTo(ctx context.Context, src *sql.DB, destURI string) error {
	conn, err := src.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) error {
		b, ok := driverConn.(backuper)
		if !ok {
			return fmt.Errorf("sqlite backup API unavailable")
		}
		bak, err := b.NewBackup(destURI)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				_ = bak.Finish()
				return err
			}
			more, err := bak.Step(1024)
			if err != nil {
				_ = bak.Finish()
				return err
			}
			if !more {
				break
			}
		}
		return bak.Finish()
	})
}
