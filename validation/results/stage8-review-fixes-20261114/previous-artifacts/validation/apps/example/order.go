// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

// Package example contains the executable application-guide transaction example.
package example

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordOrder atomically stores an operation and its outbox record. The schema
// must give operation_id a unique key. A failed Commit is an uncertain outcome:
// callers reconcile that ID through a fresh connection before deciding to retry.
func RecordOrder(ctx context.Context, db *sql.DB, operationID string, payload []byte) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO go_app_example_orders(operation_id,payload) VALUES(?,?)", operationID, payload); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO go_app_example_outbox(operation_id) VALUES(?)", operationID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed; reconcile operation %q before retry: %w", operationID, err)
	}
	return nil
}
