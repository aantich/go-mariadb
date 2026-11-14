// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package apps_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Default timestamp columns deliberately have no explicit precision tag: this
// checks the dialect's automatic clock configuration, not caller-side rounding.
type automaticRecord struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (automaticRecord) TableName() string { return "go_app_automatic" }

func TestGORMDialectContract(t *testing.T) {
	for _, mode := range []string{"plain", "compressed", "tls", "tls_compressed"} {
		t.Run(mode, func(t *testing.T) {
			db := pool(t, config(t, mode))
			g := orm(t, db, true).WithContext(t.Context())
			exec(t, db, "DROP TABLE IF EXISTS go_app_automatic")
			t.Cleanup(func() { db.ExecContext(context.Background(), "DROP TABLE IF EXISTS go_app_automatic") })
			if err := g.AutoMigrate(&automaticRecord{}); err != nil {
				t.Fatal(err)
			}
			t.Run("automatic_timestamps", func(t *testing.T) {
				row := automaticRecord{ID: 1, Name: "created"}
				if err := g.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				var got automaticRecord
				if err := g.First(&got, "id=?", row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || !got.CreatedAt.Equal(row.CreatedAt) || !got.UpdatedAt.Equal(row.UpdatedAt) {
					t.Fatal("automatic create timestamps changed after reload", row, got)
				}
				// Start with a known old update timestamp so the update callback must
				// generate a new value without sleeping or depending on clock resolution.
				old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
				if err := g.Model(&row).UpdateColumn("updated_at", old).Error; err != nil {
					t.Fatal(err)
				}
				row.UpdatedAt = old
				if err := g.Model(&row).Update("name", "updated").Error; err != nil {
					t.Fatal(err)
				}
				if err := g.First(&got, "id=?", row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if !row.UpdatedAt.After(old) || !got.UpdatedAt.Equal(row.UpdatedAt) || !got.CreatedAt.Equal(row.CreatedAt) {
					t.Fatal("automatic update timestamp changed after reload", row, got)
				}
			})
			t.Run("savepoints_and_nested_rollback", func(t *testing.T) {
				rollback := errors.New("rollback inner transaction")
				called := false
				err := g.Transaction(func(tx *gorm.DB) error {
					if err := tx.Create(&automaticRecord{ID: 10, Name: "outer"}).Error; err != nil {
						return err
					}
					if err := tx.SavePoint("application_checkpoint").Error; err != nil {
						return err
					}
					if err := tx.Create(&automaticRecord{ID: 11, Name: "discard explicit"}).Error; err != nil {
						return err
					}
					if err := tx.RollbackTo("application_checkpoint").Error; err != nil {
						return err
					}
					if err := tx.Transaction(func(inner *gorm.DB) error {
						called = true
						if err := inner.Create(&automaticRecord{ID: 12, Name: "discard nested"}).Error; err != nil {
							return err
						}
						return rollback
					}); !errors.Is(err, rollback) {
						return err
					}
					return tx.Transaction(func(inner *gorm.DB) error { return inner.Create(&automaticRecord{ID: 13, Name: "commit nested"}).Error })
				})
				if err != nil || !called {
					t.Fatal("nested transaction", called, err)
				}
				scalar(t, db, "SELECT COUNT(*) FROM go_app_automatic WHERE id IN (10,13)", 2)
				scalar(t, db, "SELECT COUNT(*) FROM go_app_automatic WHERE id IN (11,12)", 0)
			})
		})
	}
}
