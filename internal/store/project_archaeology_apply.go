package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"codex-commons/internal/domain"
)

func selectionDigest(batchID string, outcomeIDs []string, proposals map[string]string) string {
	type selected struct{ ID, Proposal string }
	items := make([]selected, 0, len(outcomeIDs))
	for _, id := range outcomeIDs {
		items = append(items, selected{id, proposals[id]})
	}
	body, _ := json.Marshal(struct {
		BatchID string
		Items   []selected
	}{batchID, items})
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selectedOutcomeQuery(ids []string) (string, []any) {
	marks, args := make([]string, len(ids)), make([]any, len(ids))
	for i, id := range ids {
		marks[i], args[i] = "?", id
	}
	return strings.Join(marks, ","), args
}
func canonicalSelectedOutcomeIDs(values []string) ([]string, error) {
	ids := append([]string(nil), values...)
	sort.Strings(ids)
	if len(ids) < 1 || len(ids) > domain.ArchaeologyNativeMaxProjects*2 {
		return nil, domain.ErrInvalid
	}
	for index, id := range ids {
		if values[index] != id || !boundedCoreText(id, 120, true) || index > 0 && ids[index-1] == id {
			return nil, domain.ErrInvalid
		}
	}
	return ids, nil
}

func validateSelectedImportSources(imports []domain.HistoricalImportCommand) error {
	seen := map[string]struct{}{}
	for _, item := range imports {
		key := item.ProjectID + "\x00" + item.SourceDigest
		if _, exists := seen[key]; exists {
			return domain.ErrConflict
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sameSelectedIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Store) ReplayArchaeologySelectedImports(ctx context.Context, query domain.ArchaeologySelectedApplyReplayQuery) (domain.ArchaeologySelectedApplyReceipt, bool, error) {
	ids, err := canonicalSelectedOutcomeIDs(query.OutcomeIDs)
	if err != nil || !boundedCoreText(query.BatchID, 120, true) || !boundedCoreText(query.Principal, 200, true) || !boundedCoreText(query.RequestID, 200, true) {
		return domain.ArchaeologySelectedApplyReceipt{}, false, domain.ErrInvalid
	}
	var auditID, batchID, selection, manifest, idsJSON, resultJSON string
	err = s.db.QueryRowContext(ctx, `SELECT id,batch_id,selection_digest,manifest_digest,outcome_ids_json,result_json
FROM archaeology_selected_imports WHERE principal=? AND request_key=?`, query.Principal, query.RequestID).Scan(&auditID, &batchID, &selection, &manifest, &idsJSON, &resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ArchaeologySelectedApplyReceipt{}, false, nil
	}
	if err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, false, err
	}
	var storedIDs []string
	var receipt domain.ArchaeologySelectedApplyReceipt
	if json.Unmarshal([]byte(idsJSON), &storedIDs) != nil || json.Unmarshal([]byte(resultJSON), &receipt) != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, false, domain.ErrConflict
	}
	if batchID != query.BatchID || selection != query.SelectionDigest || manifest != query.ManifestDigest || !sameSelectedIDs(storedIDs, ids) ||
		receipt.AuditID != auditID || receipt.BatchID != batchID || receipt.SelectionDigest != selection || receipt.ManifestDigest != manifest ||
		!sameSelectedIDs(receipt.OutcomeIDs, storedIDs) || len(receipt.Imports) != len(storedIDs) {
		return domain.ArchaeologySelectedApplyReceipt{}, false, domain.ErrConflict
	}
	return receipt, true, nil
}
func selectedManifestDigest(batchID, selection string, before []int64, receipts []domain.HistoricalImportReceipt) string {
	type task struct{ Key, TaskID, Disposition string }
	type item struct {
		ProjectID, BatchID, ManifestDigest string
		PreRevision, ResultRevision        int64
		Tasks                              []task
	}
	items := make([]item, 0, len(receipts))
	for index, receipt := range receipts {
		tasks := make([]task, 0, len(receipt.Tasks))
		for _, t := range receipt.Tasks {
			tasks = append(tasks, task{t.Key, t.TaskID, t.Disposition})
		}
		items = append(items, item{receipt.ProjectID, receipt.BatchID, receipt.ManifestDigest, before[index], receipt.ProjectRevision, tasks})
	}
	body, _ := json.Marshal(struct {
		BatchID, Selection string
		Items              []item
	}{batchID, selection, items})
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Store) simulateSelectedImports(ctx context.Context, tx *sql.Tx, batchID, selection, requestID string, imports []domain.HistoricalImportCommand) (domain.ArchaeologySelectedPreviewReceipt, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT selected_import_preview`); err != nil {
		return domain.ArchaeologySelectedPreviewReceipt{}, err
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			_, _ = tx.ExecContext(context.Background(), `ROLLBACK TO selected_import_preview`)
			_, _ = tx.ExecContext(context.Background(), `RELEASE selected_import_preview`)
		}
	}()
	out := domain.ArchaeologySelectedPreviewReceipt{BatchID: batchID, SelectionDigest: selection}
	before := make([]int64, 0, len(imports))
	for index, item := range imports {
		normalized, manifest, counts, err := normalizeHistoricalImport(item, s.now().UTC())
		if err != nil {
			return out, err
		}
		preview, err := previewHistoricalImport(ctx, tx, normalized, manifest, counts)
		if err != nil {
			return out, err
		}
		before = append(before, preview.ProjectRevision)
		item.ConfirmSourceDigest, item.ConfirmManifestDigest = item.SourceDigest, manifest
		item.Meta.RequestID = deterministicHistoricalID("ASR-", requestID, fmt.Sprintf("%d", index), item.ProjectID, item.BatchID)
		receipt, err := s.applyHistoricalImportTx(ctx, tx, item)
		if err != nil {
			return out, err
		}
		out.PreparedImports = append(out.PreparedImports, item)
		out.Imports = append(out.Imports, receipt)
	}
	out.ManifestDigest = selectedManifestDigest(batchID, selection, before, out.Imports)
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO selected_import_preview`); err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `RELEASE selected_import_preview`); err != nil {
		return out, err
	}
	rolledBack = true
	return out, nil
}

func (s *Store) PreviewArchaeologySelectedImports(ctx context.Context, command domain.ArchaeologySelectedPreviewCommand) (domain.ArchaeologySelectedPreviewReceipt, error) {
	ids, idErr := canonicalSelectedOutcomeIDs(command.OutcomeIDs)
	if idErr != nil || !boundedCoreText(command.BatchID, 120, true) || !boundedCoreText(command.Principal, 200, true) || !boundedCoreText(command.RequestID, 200, true) || len(ids) != len(command.Imports) {
		return domain.ArchaeologySelectedPreviewReceipt{}, domain.ErrInvalid
	}
	if err := validateSelectedImportSources(command.Imports); err != nil {
		return domain.ArchaeologySelectedPreviewReceipt{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySelectedPreviewReceipt{}, err
	}
	defer tx.Rollback()
	proposals, projects := map[string]string{}, map[string]string{}
	marks, args := selectedOutcomeQuery(ids)
	queryArgs := []any{command.BatchID, command.Principal}
	queryArgs = append(queryArgs, args...)
	rows, err := tx.QueryContext(ctx, `SELECT o.id,o.project_id,o.proposal_json FROM archaeology_native_outcomes o JOIN archaeology_native_jobs j ON j.id=o.job_id JOIN archaeology_native_batches b ON b.id=j.batch_id JOIN archaeology_sessions s ON s.id=b.session_id WHERE b.id=? AND s.principal=? AND o.id IN (`+marks+`)`, queryArgs...)
	if err != nil {
		return domain.ArchaeologySelectedPreviewReceipt{}, err
	}
	for rows.Next() {
		var id, project, proposal string
		if err = rows.Scan(&id, &project, &proposal); err != nil {
			rows.Close()
			return domain.ArchaeologySelectedPreviewReceipt{}, err
		}
		proposals[id], projects[id] = proposal, project
	}
	if err = rows.Close(); err != nil {
		return domain.ArchaeologySelectedPreviewReceipt{}, err
	}
	for index, id := range ids {
		var wire struct {
			BatchID string `json:"batch_id"`
		}
		if proposals[id] == "" {
			return domain.ArchaeologySelectedPreviewReceipt{}, domain.ErrNotFound
		}
		if json.Unmarshal([]byte(proposals[id]), &wire) != nil || command.Imports[index].ProjectID != projects[id] || command.Imports[index].BatchID != wire.BatchID {
			return domain.ArchaeologySelectedPreviewReceipt{}, domain.ErrConflict
		}
	}
	selection := selectionDigest(command.BatchID, ids, proposals)
	out, err := s.simulateSelectedImports(ctx, tx, command.BatchID, selection, command.RequestID, command.Imports)
	if err != nil {
		return out, err
	}
	out.OutcomeIDs = ids
	return out, nil
}

func (s *Store) ApplyArchaeologySelectedImports(ctx context.Context, command domain.ArchaeologySelectedApplyCommand) (domain.ArchaeologySelectedApplyReceipt, error) {
	if !boundedCoreText(command.BatchID, 120, true) || !boundedCoreText(command.Principal, 200, true) || !boundedCoreText(command.RequestID, 200, true) || len(command.OutcomeIDs) < 1 || len(command.OutcomeIDs) > domain.ArchaeologyNativeMaxProjects*2 || len(command.Imports) != len(command.OutcomeIDs) {
		return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrInvalid
	}
	ids, err := canonicalSelectedOutcomeIDs(command.OutcomeIDs)
	if err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	if err = validateSelectedImportSources(command.Imports); err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	prior, found, err := s.ReplayArchaeologySelectedImports(ctx, domain.ArchaeologySelectedApplyReplayQuery{BatchID: command.BatchID, Principal: command.Principal, RequestID: command.RequestID, SelectionDigest: command.SelectionDigest, ManifestDigest: command.ManifestDigest, OutcomeIDs: ids})
	if err != nil || found {
		return prior, err
	}
	proposals := map[string]string{}
	projects := map[string]string{}
	marks, args := selectedOutcomeQuery(ids)
	queryArgs := []any{command.BatchID, command.Principal}
	queryArgs = append(queryArgs, args...)
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.project_id,o.proposal_json FROM archaeology_native_outcomes o JOIN archaeology_native_jobs j ON j.id=o.job_id JOIN archaeology_native_batches b ON b.id=j.batch_id JOIN archaeology_sessions s ON s.id=b.session_id WHERE b.id=? AND s.principal=? AND o.id IN (`+marks+`)`, queryArgs...)
	if err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	for rows.Next() {
		var id, project, proposal string
		if err = rows.Scan(&id, &project, &proposal); err != nil {
			rows.Close()
			return domain.ArchaeologySelectedApplyReceipt{}, err
		}
		proposals[id], projects[id] = proposal, project
	}
	if err = rows.Close(); err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	for _, id := range ids {
		if proposals[id] == "" {
			return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrNotFound
		}
	}
	selection := selectionDigest(command.BatchID, ids, proposals)
	if selection != command.SelectionDigest {
		return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrConflict
	}
	ordered := make([]domain.HistoricalImportCommand, 0, len(ids))
	seenCommand := map[string]struct{}{}
	for index, id := range ids {
		var wire struct {
			BatchID string `json:"batch_id"`
		}
		if json.Unmarshal([]byte(proposals[id]), &wire) != nil {
			return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrInvalid
		}
		item := command.Imports[index]
		key := item.ProjectID + "\x00" + item.BatchID
		if _, duplicate := seenCommand[key]; duplicate {
			return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrConflict
		}
		seenCommand[key] = struct{}{}
		if item.ProjectID != projects[id] || item.BatchID != wire.BatchID {
			return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrConflict
		}
		ordered = append(ordered, item)
	}
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	defer tx.Rollback()
	simulation, err := s.simulateSelectedImports(ctx, tx, command.BatchID, selection, command.RequestID, ordered)
	if err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	manifest := simulation.ManifestDigest
	if manifest != command.ManifestDigest {
		return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrConflict
	}
	if err := consumeSelectedReview(ctx, tx, command, ids, now); err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, err
	}
	receipt := domain.ArchaeologySelectedApplyReceipt{AuditID: deterministicHistoricalID("ASI-", command.BatchID, selection), BatchID: command.BatchID, SelectionDigest: selection, ManifestDigest: manifest, OutcomeIDs: ids}
	for _, item := range simulation.PreparedImports {
		applied, applyErr := s.applyHistoricalImportTx(ctx, tx, item)
		if applyErr != nil {
			return domain.ArchaeologySelectedApplyReceipt{}, applyErr
		}
		receipt.Imports = append(receipt.Imports, applied)
	}
	if len(receipt.Imports) != len(simulation.Imports) {
		return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrConflict
	}
	for index := range receipt.Imports {
		stable := func(v domain.HistoricalImportReceipt) any {
			return struct {
				ProjectID, BatchID, SourceDigest, ManifestDigest, CollisionPolicy, State string
				Applied                                                                  bool
				Tasks                                                                    []domain.HistoricalImportTaskReceipt
				Counts                                                                   domain.HistoricalImportCounts
				ProjectRevision                                                          int64
			}{v.ProjectID, v.BatchID, v.SourceDigest, v.ManifestDigest, v.CollisionPolicy, v.State, v.Applied, v.Tasks, v.Counts, v.ProjectRevision}
		}
		left, _ := json.Marshal(stable(receipt.Imports[index]))
		right, _ := json.Marshal(stable(simulation.Imports[index]))
		if string(left) != string(right) {
			return domain.ArchaeologySelectedApplyReceipt{}, domain.ErrConflict
		}
	}
	result, _ := json.Marshal(receipt)
	idsJSON, _ := json.Marshal(ids)
	if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_selected_imports(id,batch_id,principal,request_key,selection_digest,manifest_digest,outcome_ids_json,result_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, receipt.AuditID, command.BatchID, command.Principal, command.RequestID, selection, manifest, string(idsJSON), string(result), stamp(now)); err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySelectedApplyReceipt{}, mapErr(err)
	}
	return receipt, nil
}
