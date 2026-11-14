// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package apps_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
	"github.com/jmoiron/sqlx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type order struct {
	ID       uint64    `gorm:"primaryKey;autoIncrement:false" db:"id"`
	Amount   string    `gorm:"type:decimal(38,9)" db:"amount"`
	Name     string    `gorm:"size:100" db:"name"`
	Payload  []byte    `gorm:"type:blob" db:"payload"`
	Document []byte    `gorm:"type:json" db:"document"`
	Optional *string   `db:"optional"`
	Created  time.Time `gorm:"precision:6" db:"created"`
}

func (order) TableName() string { return "go_app_orders" }

type orderV2 struct {
	order
	Note *string
}

func (orderV2) TableName() string { return "go_app_orders" }

type line struct {
	ID      uint64 `gorm:"primaryKey;autoIncrement:false"`
	OrderID uint64
	Order   order `gorm:"foreignKey:OrderID;constraint:OnDelete:RESTRICT"`
}

func (line) TableName() string { return "go_app_lines" }

func serverError(t *testing.T, err error, number uint16) {
	t.Helper()
	var server *mariadb.MySQLError
	if !errors.As(err, &server) || server.Number != number {
		t.Fatal("original MariaDB error lost", err)
	}
}

func TestGORMApplication(t *testing.T) {
	for _, mode := range []string{"plain", "compressed", "tls", "tls_compressed"} {
		t.Run(mode, func(t *testing.T) {
			db := pool(t, config(t, mode))
			g := orm(t, db, true).WithContext(t.Context())
			exec(t, db, "DROP TABLE IF EXISTS go_app_lines")
			exec(t, db, "DROP TABLE IF EXISTS go_app_orders")
			t.Cleanup(func() {
				db.ExecContext(context.Background(), "DROP TABLE IF EXISTS go_app_lines")
				db.ExecContext(context.Background(), "DROP TABLE IF EXISTS go_app_orders")
			})
			for range 2 {
				if err := g.AutoMigrate(&order{}, &line{}); err != nil {
					t.Fatal(err)
				}
			}
			date := time.Date(2026, 11, 14, 1, 2, 3, 123456000, time.UTC)
			present := "present"
			values := []order{{ID: ^uint64(0), Amount: "-12345678901234567890123456789.123456789", Name: "hello 🌍", Payload: []byte{0, 255, 1}, Document: []byte(`{"n":7}`), Created: date},
				{ID: 2, Amount: "2.000000000", Name: "second", Document: []byte(`null`), Optional: &present, Created: date}}
			if result := g.CreateInBatches(&values, 2); result.Error != nil || result.RowsAffected != 2 {
				t.Fatal(result.Error, result.RowsAffected)
			}
			var got order
			if err := g.First(&got, "id=?", ^uint64(0)).Error; err != nil {
				t.Fatal(err)
			}
			if got.ID != values[0].ID || got.Amount != values[0].Amount || got.Name != values[0].Name || !bytes.Equal(got.Payload, values[0].Payload) || !bytes.Equal(got.Document, values[0].Document) || got.Optional != nil || !got.Created.Equal(date) {
				t.Fatal("GORM fidelity", got)
			}
			duplicate := g.Create(&order{ID: 2, Amount: "0", Created: date}).Error
			if !errors.Is(duplicate, gorm.ErrDuplicatedKey) {
				t.Fatal("duplicate translation", duplicate)
			}
			serverError(t, duplicate, 1062)
			if err := g.Create(&line{ID: 1, OrderID: 2}).Error; err != nil {
				t.Fatal(err)
			}
			foreign := g.Create(&line{ID: 2, OrderID: 99}).Error
			if !errors.Is(foreign, gorm.ErrForeignKeyViolated) {
				t.Fatal("foreign-key translation", foreign)
			}
			serverError(t, foreign, 1452)
			foreign = g.Delete(&order{}, "id=?", 2).Error
			if !errors.Is(foreign, gorm.ErrForeignKeyViolated) {
				t.Fatal("referenced delete translation", foreign)
			}
			serverError(t, foreign, 1451)
			rollback := errors.New("application rollback")
			if err := g.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&order{}).Where("id=?", 2).Update("name", "dirty").Error; err != nil {
					return err
				}
				return rollback
			}); !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			var name string
			if err := g.Model(&order{}).Select("name").Where("id=?", 2).Scan(&name).Error; err != nil || name != "second" {
				t.Fatal("rollback", name, err)
			}
			if err := g.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"name"})}).Create(&order{ID: 2, Name: "upsert", Amount: "3", Created: date}).Error; err != nil {
				t.Fatal(err)
			}
			if err := g.AutoMigrate(&orderV2{}); err != nil {
				t.Fatal(err)
			}
			if !g.Migrator().HasColumn(&orderV2{}, "Note") {
				t.Fatal("migration column missing")
			}
			got = order{}
			if err := g.First(&got, "id=?", 2).Error; err != nil {
				t.Fatal(err)
			}
			if got.Name != "upsert" || got.Optional == nil || *got.Optional != present {
				t.Fatal("upsert/migration lost data", got)
			}
			plain := orm(t, db, false)
			err := plain.WithContext(t.Context()).Create(&order{ID: 2, Amount: "0", Created: date}).Error
			serverError(t, err, 1062)
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				t.Fatal("translation unexpectedly enabled")
			}
			if err := g.Delete(&line{}, "id=?", 1).Error; err != nil {
				t.Fatal(err)
			}
			if result := g.Delete(&order{}, "id=?", 2); result.Error != nil || result.RowsAffected != 1 {
				t.Fatal("GORM delete", result.Error, result.RowsAffected)
			}
			scalar(t, db, "SELECT COUNT(*) FROM go_app_orders", 1)
		})
	}
}

func TestSQLxApplication(t *testing.T) {
	for _, mode := range []string{"plain", "compressed", "tls", "tls_compressed"} {
		t.Run(mode, func(t *testing.T) {
			db := pool(t, config(t, mode))
			x := extended(db)
			exec(t, db, "DROP TABLE IF EXISTS go_app_sqlx")
			exec(t, db, "CREATE TABLE go_app_sqlx(id BIGINT UNSIGNED PRIMARY KEY,amount DECIMAL(38,9),name VARCHAR(100),payload BLOB,document JSON,optional VARCHAR(20),created DATETIME(6)) ENGINE=InnoDB")
			t.Cleanup(func() { db.ExecContext(context.Background(), "DROP TABLE go_app_sqlx") })
			date := time.Date(2026, 11, 14, 1, 2, 3, 123456000, time.UTC)
			present := "present"
			values := []order{{ID: ^uint64(0), Amount: "12345678901234567890123456789.123456789", Name: "hello 🌍", Payload: []byte{0, 255}, Document: []byte(`{"n":7}`), Created: date}, {ID: 2, Amount: "2", Name: "second", Document: []byte(`null`), Optional: &present, Created: date}}
			// sqlx expands VALUES tuples only after an explicit column list.
			result, err := x.NamedExecContext(t.Context(), "INSERT INTO go_app_sqlx(id,amount,name,payload,document,optional,created) VALUES(:id,:amount,:name,:payload,:document,:optional,:created)", values)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := result.RowsAffected(); err != nil || n != 2 {
				t.Fatal(n, err)
			}
			stmt, err := x.PrepareNamedContext(t.Context(), "SELECT * FROM go_app_sqlx WHERE id=:id")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			var got order
			if err := stmt.GetContext(t.Context(), &got, map[string]any{"id": ^uint64(0)}); err != nil {
				t.Fatal(err)
			}
			if got.ID != values[0].ID || got.Amount != values[0].Amount || got.Name != values[0].Name || !bytes.Equal(got.Payload, values[0].Payload) || !bytes.Equal(got.Document, values[0].Document) || got.Optional != nil || !got.Created.Equal(date) {
				t.Fatal("sqlx fidelity", got)
			}
			var optional sql.NullString
			if err := x.GetContext(t.Context(), &optional, "SELECT optional FROM go_app_sqlx WHERE id=?", ^uint64(0)); err != nil || optional.Valid {
				t.Fatal("SQL NULL", optional, err)
			}
			if err := x.GetContext(t.Context(), &optional, "SELECT optional FROM go_app_sqlx WHERE id=?", 2); err != nil || !optional.Valid || optional.String != present {
				t.Fatal("non-NULL", optional, err)
			}
			query, args, err := sqlx.In("SELECT id FROM go_app_sqlx WHERE id IN (?) ORDER BY id", []uint64{2, ^uint64(0)})
			if err != nil {
				t.Fatal(err)
			}
			var ids []uint64
			if err := x.SelectContext(t.Context(), &ids, x.Rebind(query), args...); err != nil || len(ids) != 2 || ids[0] != 2 || ids[1] != ^uint64(0) {
				t.Fatal(ids, err)
			}
			tx, err := x.BeginTxx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.NamedExecContext(t.Context(), "UPDATE go_app_sqlx SET name=:name WHERE id=:id", map[string]any{"name": "dirty", "id": 2}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			var name string
			if err := x.GetContext(t.Context(), &name, "SELECT name FROM go_app_sqlx WHERE id=?", 2); err != nil || name != "second" {
				t.Fatal("sqlx rollback", name, err)
			}
			var returned uint64
			if err := x.QueryRowxContext(t.Context(), "INSERT INTO go_app_sqlx(id,amount,name) VALUES(3,3,'returning') RETURNING id").Scan(&returned); err != nil || returned != 3 {
				t.Fatal("MariaDB RETURNING", returned, err)
			}
			_, err = x.ExecContext(t.Context(), "INSERT INTO go_app_sqlx(id) VALUES(2)")
			serverError(t, err, 1062)
			scalar(t, db, "SELECT COUNT(*) FROM go_app_sqlx", 3)
			exec(t, db, "DELETE FROM go_app_sqlx WHERE id=3")
			scalar(t, db, "SELECT COUNT(*) FROM go_app_sqlx", 2)
		})
	}
}
