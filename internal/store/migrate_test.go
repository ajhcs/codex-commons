package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func TestWithImmediateCommitAndRollbackUseUncanceledContext(t *testing.T) {
	db, err := sql.Open("sqlite", "file:with-immediate-cleanup?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	if err := withImmediate(ctx, conn, func() error {
		if _, err := conn.ExecContext(ctx, `CREATE TABLE committed_after_cancel(id INTEGER NOT NULL)`); err != nil {
			return err
		}
		cancel()
		return nil
	}); err != nil {
		t.Fatalf("commit after cancel: %v", err)
	}
	var n int
	if err := conn.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE name='committed_after_cancel'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("committed table missing n=%d err=%v", n, err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	err = withImmediate(ctx, conn, func() error {
		if _, err := conn.ExecContext(ctx, `CREATE TABLE rolled_back_after_cancel(id INTEGER NOT NULL)`); err != nil {
			return err
		}
		cancel()
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("expected rollback error")
	}
	if err := conn.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE name='rolled_back_after_cancel'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rolled-back table survived n=%d err=%v", n, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := withImmediate(canceled, conn, func() error {
		t.Fatal("fn must not run when BEGIN sees a canceled context")
		return nil
	}); err == nil {
		t.Fatal("expected canceled BEGIN to fail")
	}
}
