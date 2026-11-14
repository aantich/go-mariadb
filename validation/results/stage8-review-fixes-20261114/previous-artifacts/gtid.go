// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"encoding"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// GTID identifies a MariaDB event group: domain-server-sequence, not a UUID
// interval. Sequence must be positive; a server change can occur within a domain.
type GTID struct {
	Domain   uint32
	Server   uint32
	Sequence uint64
}

// GTIDSet is immutable completed progress, with one entry per domain. Its zero
// value is empty. Constructors copy their inputs; Entries returns a copy. This
// makes sharing a checkpoint safe without copying its state for every row.
type GTIDSet struct {
	entries      []GTID
	encodedBytes int
}

var _ encoding.TextMarshaler = GTIDSet{}
var _ encoding.TextUnmarshaler = (*GTIDSet)(nil)

// NewGTIDSet validates at most 4096 unique domains with positive sequences.
func NewGTIDSet(entries []GTID) (GTIDSet, error) {
	if len(entries) > 4096 {
		return GTIDSet{}, errors.New("mariadb: GTID domain limit exceeded")
	}
	copy := append([]GTID(nil), entries...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].Domain < copy[j].Domain })
	for i, g := range copy {
		if g.Sequence == 0 || (i > 0 && copy[i-1].Domain == g.Domain) {
			return GTIDSet{}, errors.New("mariadb: zero sequence or duplicate GTID domain")
		}
	}
	size := 0
	for i, g := range copy {
		size += gtidSize(g)
		if i > 0 {
			size++
		}
	}
	if size > 65535 {
		return GTIDSet{}, errors.New("mariadb: GTID text limit exceeded")
	}
	return GTIDSet{entries: copy, encodedBytes: size}, nil
}

// ParseGTIDSet accepts MariaDB connect state and returns its canonical form.
// Empty input starts at the beginning of available history, not "now".
func ParseGTIDSet(text string) (GTIDSet, error) {
	if len(text) > 65535 || strings.Count(text, ",") >= 4096 {
		return GTIDSet{}, errors.New("mariadb: GTID state limit exceeded")
	}
	if text == "" {
		return GTIDSet{}, nil
	}
	var entries []GTID
	for _, part := range strings.Split(text, ",") {
		fields := strings.Split(part, "-")
		if len(fields) != 3 {
			return GTIDSet{}, errors.New("mariadb: expected domain-server-sequence GTID")
		}
		var n [3]uint64
		for i, field := range fields {
			if field == "" || len(field) > 20 {
				return GTIDSet{}, errors.New("mariadb: invalid GTID number")
			}
			for _, c := range field {
				if c < '0' || c > '9' {
					return GTIDSet{}, errors.New("mariadb: invalid GTID number")
				}
			}
			width := 32
			if i == 2 {
				width = 64
			}
			value, err := strconv.ParseUint(field, 10, width)
			if err != nil {
				return GTIDSet{}, errors.New("mariadb: GTID number overflow")
			}
			n[i] = value
		}
		entries = append(entries, GTID{uint32(n[0]), uint32(n[1]), n[2]})
	}
	return NewGTIDSet(entries)
}

// Entries returns completed domains in ascending domain order.
func (s GTIDSet) Entries() []GTID { return append([]GTID(nil), s.entries...) }
func (s GTIDSet) String() string {
	var b strings.Builder
	b.Grow(s.encodedBytes)
	var scratch [64]byte
	for i, g := range s.entries {
		if i > 0 {
			b.WriteByte(',')
		}
		text := strconv.AppendUint(scratch[:0], uint64(g.Domain), 10)
		text = append(text, '-')
		text = strconv.AppendUint(text, uint64(g.Server), 10)
		text = append(text, '-')
		text = strconv.AppendUint(text, g.Sequence, 10)
		b.Write(text)
	}
	return b.String()
}
func (s GTIDSet) MarshalText() ([]byte, error) { return []byte(s.String()), nil }
func (s *GTIDSet) UnmarshalText(text []byte) error {
	next, err := ParseGTIDSet(string(text))
	if err == nil {
		*s = next
	}
	return err
}

// Advance returns new immutable progress. It rejects repeated/backward
// sequences even if the originating server changed within the domain.
func (s GTIDSet) Advance(g GTID) (GTIDSet, error) {
	i := sort.Search(len(s.entries), func(i int) bool { return s.entries[i].Domain >= g.Domain })
	exists := i < len(s.entries) && s.entries[i].Domain == g.Domain
	if g.Sequence == 0 || (exists && s.entries[i].Sequence >= g.Sequence) {
		return GTIDSet{}, errors.New("mariadb: GTID sequence did not advance")
	}
	if !exists && len(s.entries) >= 4096 {
		return GTIDSet{}, errors.New("mariadb: GTID domain limit exceeded")
	}
	size := s.encodedBytes + gtidSize(g)
	if exists {
		size -= gtidSize(s.entries[i])
	} else if len(s.entries) > 0 {
		size++
	}
	if size > 65535 {
		return GTIDSet{}, errors.New("mariadb: GTID text limit exceeded")
	}
	count := len(s.entries)
	if !exists {
		count++
	}
	entries := make([]GTID, count)
	copy(entries, s.entries[:i])
	entries[i] = g
	if exists {
		copy(entries[i+1:], s.entries[i+1:])
	} else {
		copy(entries[i+1:], s.entries[i:])
	}
	return GTIDSet{entries: entries, encodedBytes: size}, nil
}

func gtidSize(g GTID) int {
	digits := func(n uint64) int {
		count := 1
		for n >= 10 {
			n /= 10
			count++
		}
		return count
	}
	return digits(uint64(g.Domain)) + digits(uint64(g.Server)) + digits(g.Sequence) + 2
}
