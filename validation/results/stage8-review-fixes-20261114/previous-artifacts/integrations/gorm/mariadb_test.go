// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadbgorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	mariadb "github.com/aantich/go-mariadb"
	upstream "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTranslationIdentity(t *testing.T) {
	for _, test := range []struct {
		number uint16
		target error
	}{
		{1062, gorm.ErrDuplicatedKey}, {1451, gorm.ErrForeignKeyViolated}, {1452, gorm.ErrForeignKeyViolated},
	} {
		t.Run(fmt.Sprint(test.number), func(t *testing.T) {
			original := &mariadb.MySQLError{Number: test.number, Message: "synthetic server error"}
			wrapped := fmt.Errorf("application: %w", original)
			got := (&dialect{}).Translate(wrapped)
			var server *mariadb.MySQLError
			if !errors.Is(got, test.target) || !errors.Is(got, wrapped) || !errors.As(got, &server) || server != original {
				t.Fatal("error identities lost", got)
			}
		})
	}
	for _, err := range []error{nil, (*mariadb.MySQLError)(nil), context.Canceled, &mariadb.MySQLError{Number: 1213}} {
		if got := (&dialect{}).Translate(err); got != err {
			t.Fatal("unrelated error changed", got)
		}
	}
}

func TestDedicatedPoolRequired(t *testing.T) {
	// The dialect's transitive dependency registers "mysql"; it must never be
	// selected accidentally when an application supplies the wrong pool.
	var dialed atomic.Int32
	config := upstream.NewConfig()
	const network = "gorm-foreign-pool-regression"
	upstream.RegisterDialContext(network, func(context.Context, string) (net.Conn, error) {
		dialed.Add(1)
		return nil, errors.New("foreign transport must not be dialed")
	})
	t.Cleanup(func() { upstream.DeregisterDialContext(network) })
	config.Net, config.Addr = network, "unused"
	connector, err := upstream.NewConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	_, err = gorm.Open(New(Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err == nil {
		t.Fatal("foreign driver pool accepted")
	}
	if dialed.Load() != 0 {
		t.Fatal("foreign driver pool was queried before rejection")
	}
}
