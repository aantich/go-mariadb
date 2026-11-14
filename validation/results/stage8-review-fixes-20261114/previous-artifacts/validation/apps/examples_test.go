// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package apps_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/aantich/go-mariadb/validation/apps/example"
)

func TestApplicationExamples(t *testing.T) {
	db := pool(t, config(t, "plain"))
	for _, table := range []string{"go_app_example_orders", "go_app_example_outbox"} {
		exec(t, db, "DROP TABLE IF EXISTS "+table)
		exec(t, db, "CREATE TABLE "+table+"(operation_id VARCHAR(100) PRIMARY KEY,payload BLOB) ENGINE=InnoDB")
		t.Cleanup(func() { db.ExecContext(context.Background(), "DROP TABLE "+table) })
	}
	payload := []byte{0, 255, 1}
	if err := example.RecordOrder(t.Context(), db, "order-1", payload); err != nil {
		t.Fatal(err)
	}
	if err := example.RecordOrder(t.Context(), db, "order-1", payload); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	var got []byte
	if err := db.QueryRowContext(t.Context(), "SELECT payload FROM go_app_example_orders WHERE operation_id='order-1'").Scan(&got); err != nil || !bytes.Equal(got, payload) {
		t.Fatal(got, err)
	}
	scalar(t, db, "SELECT COUNT(*) FROM go_app_example_outbox", 1)
	// An outbox failure must roll back the preceding order, not leave half of
	// the guide's transaction committed.
	exec(t, db, "INSERT INTO go_app_example_outbox(operation_id) VALUES('order-2')")
	if err := example.RecordOrder(t.Context(), db, "order-2", payload); err == nil {
		t.Fatal("expected outbox duplicate")
	}
	scalar(t, db, "SELECT COUNT(*) FROM go_app_example_orders WHERE operation_id='order-2'", 0)
}
