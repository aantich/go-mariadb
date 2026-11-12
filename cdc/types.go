// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

// Package cdc implements bounded, pull-driven MariaDB change-data capture.
// It owns a dedicated connection; it never borrows one from database/sql.
// Checkpoints describe decoded complete groups, not destination durability.
// Persist destination changes and resume state atomically before acknowledging.
// Reopen explicitly from that state after failure; delivery is at least once.
package cdc

import (
	"errors"
	"fmt"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

var (
	// ErrMalformed reports corrupt framing, checksums or event contents.
	ErrMalformed = errors.New("mariadb/cdc: malformed event")
	// ErrUnsupported reports an event or replication mode that cannot be decoded safely.
	ErrUnsupported = errors.New("mariadb/cdc: unsupported replication feature")
	// ErrLimit reports an exceeded caller-configured resource budget.
	ErrLimit = errors.New("mariadb/cdc: resource limit exceeded")
	// ErrCheckpoint reports missing, foreign or decreasing acknowledgement tokens.
	ErrCheckpoint = errors.New("mariadb/cdc: invalid checkpoint")
	// ErrClosed reports explicit closure of a stream.
	ErrClosed = errors.New("mariadb/cdc: stream closed")
)

// GTID is a MariaDB domain/server/sequence identifier.
type GTID = mariadb.GTID

// GTIDSet is immutable completed progress; construct it with mariadb.ParseGTIDSet.
type GTIDSet = mariadb.GTIDSet

// Resume is durable, serializable source progress. GTID mode is explicit so an
// empty GTID set means available history rather than silently starting at now.
// File coordinates are exact; COM_BINLOG_DUMP rejects starts beyond uint32.
// Cached events may have a zero header position; only explicit, advancing
// complete-group positions become checkpoints. Position rollover is rejected.
type Resume struct {
	File     string  `json:"file,omitempty"`
	Position uint64  `json:"position,omitempty"`
	GTIDs    GTIDSet `json:"gtids"`
	UseGTID  bool    `json:"use_gtid"`
}

// Limits bound each expanded event, retained table maps and decoded cell count.
// Zero fields use defaults. These bound driver state, not events retained by an
// application. No entire-transaction buffering is performed.
type Limits struct {
	MaxEventBytes int // 64 MiB by default; applies before reading or expanding an event.
	MaxTables     int // 4096 retained table maps by default.
	MaxTableBytes int // 16 MiB by default, conservatively charged including Go objects.
	MaxColumns    int // 4096 columns per table by default.
	MaxCells      int // 1,048,576 cells per event, including absent sparse-image cells.
}

func (l Limits) normalize() (Limits, error) {
	fields := []*int{&l.MaxEventBytes, &l.MaxTables, &l.MaxTableBytes, &l.MaxColumns, &l.MaxCells}
	defaults := []int{64 << 20, 4096, 16 << 20, 4096, 1 << 20}
	for i, p := range fields {
		if *p < 0 {
			return l, fmt.Errorf("%w: negative budget", ErrLimit)
		}
		if *p == 0 {
			*p = defaults[i]
		}
	}
	if l.MaxEventBytes < 19 || l.MaxEventBytes > 1<<30 || l.MaxColumns > 65535 || l.MaxTables > 1<<20 || l.MaxCells > 1<<26 {
		return l, fmt.Errorf("%w: invalid budget", ErrLimit)
	}
	return l, nil
}

// Options chooses explicit progress and a unique replica ID. SemiSync requires
// enabled server semisync; no automatic on-read acknowledgement is ever sent.
type Options struct {
	ServerID    uint32 // Nonzero and unique among connected replicas.
	Start       Resume
	Limits      Limits
	NonBlocking bool // Return io.EOF at the current source end instead of waiting.
	SemiSync    bool
	Heartbeat   time.Duration // Zero uses the source default; otherwise 1 ms..1 hour.
}

// Event is an owned binlog event. Data is one of Format, Rotate, GTID, GTIDList,
// Query, Table, Rows, uint64 (XID), string (binlog checkpoint file),
// []byte (annotation/context), or nil (heartbeat/stop).
// Checkpoint is non-nil only at a complete transaction/event-group boundary.
type Event struct {
	Type         byte
	WireType     byte // Original type before compressed/fragmented row normalization.
	Timestamp    uint32
	ServerID     uint32
	File         string
	Position     uint64
	Flags        uint16
	Data         any
	Checkpoint   *Checkpoint
	AckRequested bool
}

type checkpointOwner struct{ marker byte }

// Checkpoint is an opaque, stream-local completed-group token. Persist Resume,
// never this token: a restored Resume cannot acknowledge another live stream.
type Checkpoint struct {
	owner    *checkpointOwner
	sequence uint64
	state    Resume
}

// Resume returns immutable progress suitable for JSON encoding and a fresh Open.
func (c *Checkpoint) Resume() Resume {
	if c == nil {
		return Resume{}
	}
	return c.state
}

// Format describes a validated MariaDB format-4 binlog file.
type Format struct {
	ServerVersion string
	HeaderLengths []byte
	Checksum      bool
}

// Rotate identifies the next binlog file and its starting position.
type Rotate struct {
	File     string
	Position uint64
}

// GTIDList preserves the source's history summary; it is not a durable checkpoint.
type GTIDList struct {
	Flags   uint32
	Entries []GTID
}

// Query preserves DDL/statement context. Status is raw MariaDB status-variable
// metadata. SQL is untrusted source text, not an instruction to execute it.
type Query struct {
	ThreadID, ExecutionTime uint32
	ErrorCode               uint16
	Schema                  string
	Status, SQL             []byte
}

// Metadata preserves optional table-map TLVs, including unknown extensions.
type Metadata struct {
	Type byte
	Data []byte
}

// Column metadata is from this binlog generation, never a live catalog lookup.
// Type/Metadata normalize STRING-encoded ENUM/SET. SignednessKnown distinguishes
// absent signedness metadata from an explicit signed integer declaration.
type Column struct {
	Type            byte
	WireType        byte
	Metadata        uint16
	Nullable        bool
	Name            string
	Unsigned        bool
	SignednessKnown bool
}

// Table is an owned table-map snapshot valid for its containing row event.
// Mutating an emitted Table does not alter the decoder's retained generation.
type Table struct {
	ID           uint64
	Flags        uint16
	Schema, Name string
	Columns      []Column
	Metadata     []Metadata
}

// Cell preserves sparse images: !Present differs from SQL Null. Raw contains
// the value's exact binlog bytes, without string/blob length prefixes. It is
// owned by the event and remains valid across Next calls. Decode converts on
// demand, avoiding conversion/allocation for columns the consumer does not use.
type Cell struct {
	Present, Null bool
	Raw           []byte
}

// Change contains one row mutation. Inserts have After, deletes Before, and
// updates both; individual columns may be absent in sparse images.
type Change struct{ Before, After []Cell }

// Rows contains mutations and the exact table-map generation that decodes them.
type Rows struct {
	Table   Table
	Flags   uint16
	Changes []Change
}

// Integer preserves integer bits when optional signedness was not logged. The
// consumer must use schema knowledge rather than guessing from the high bit.
type Integer struct {
	Bits  uint64
	Width uint8
}

// Temporal preserves zero/partial dates and negative TIME without timezone or
// calendar normalization. Timestamp types use UnixSeconds; others use components.
type Temporal struct {
	Type                                                byte
	Negative                                            bool
	Year, Month, Day, Hour, Minute, Second, Microsecond int
	UnixSeconds                                         uint32
}

// MariaDB event identifiers. Compressed and fragmented row events are normalized
// to these types; Event.WireType retains their original wire identifier.
const (
	QueryEvent            byte = 2
	StopEvent             byte = 3
	RotateEvent           byte = 4
	FormatEvent           byte = 15
	XIDEvent              byte = 16
	TableMapEvent         byte = 19
	WriteRowsV1           byte = 23
	UpdateRowsV1          byte = 24
	DeleteRowsV1          byte = 25
	HeartbeatEvent        byte = 27
	WriteRowsV2           byte = 30
	UpdateRowsV2          byte = 31
	DeleteRowsV2          byte = 32
	AnnotateEvent         byte = 160
	BinlogCheckpointEvent byte = 161
	GTIDEvent             byte = 162
	GTIDListEvent         byte = 163
)
