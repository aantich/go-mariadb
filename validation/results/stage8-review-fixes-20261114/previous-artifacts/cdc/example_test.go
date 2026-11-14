// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	mariadb "github.com/aantich/go-mariadb"
	"github.com/aantich/go-mariadb/cdc"
)

// This audit reader prints completed source groups. A replication consumer must
// instead apply destination writes and persist Resume atomically before ACK.
func ExampleOpen() {
	cfg := mariadb.NewConfig()
	cfg.User, cfg.Passwd = os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")
	cfg.Net, cfg.Addr = "unix", "/path/to/mariadb.sock"
	cfg.Timeout, cfg.WriteTimeout = 3*time.Second, 3*time.Second
	// Let Next's context control idle reads; short ReadTimeouts retire streams.
	cfg.ReadTimeout = 0
	gtids, err := mariadb.ParseGTIDSet("0-1-42") // Restore the last durable state.
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stream, err := cdc.Open(ctx, cfg, cdc.Options{
		ServerID:    74001, // Allocate a unique replica ID for this consumer.
		Start:       cdc.Resume{UseGTID: true, GTIDs: gtids},
		NonBlocking: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for {
		event, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Print(err)
			return
		}
		if rows, ok := event.Data.(cdc.Rows); ok {
			fmt.Printf("%s.%s: %d changes\n", rows.Table.Schema, rows.Table.Name, len(rows.Changes))
		}
		if event.Checkpoint != nil {
			fmt.Println("observed complete group:", event.Checkpoint.Resume().GTIDs)
		}
	}
}
