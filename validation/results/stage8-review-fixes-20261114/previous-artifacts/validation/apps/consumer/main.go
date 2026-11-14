// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

// This independent consumer is built from a private source export during the
// release check. It needs neither a server nor any application-framework module.
package main

import (
	"fmt"
	mariadb "github.com/aantich/go-mariadb"
	"github.com/aantich/go-mariadb/cdc"
)

func main() {
	cfg := mariadb.NewConfig()
	if cfg == nil {
		panic("nil default configuration")
	}
	if _, err := mariadb.ParseGTIDSet("0-1-1"); err != nil {
		panic(err)
	}
	var resume cdc.Resume
	_ = resume
	fmt.Println("MariaDB Alpha consumer ready")
}
