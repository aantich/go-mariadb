// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"encoding/binary"
	"fmt"
)

// Every decoder operation checks the remaining input before indexing or
// allocating. A sticky error keeps nested metadata parsers easy to audit.
type reader struct {
	data []byte
	pos  int
	err  error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.data)-r.pos {
		r.err = fmt.Errorf("%w: truncated payload", ErrMalformed)
		return nil
	}
	value := r.data[r.pos : r.pos+n : r.pos+n]
	r.pos += n
	return value
}
func (r *reader) byte() byte {
	b := r.take(1)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
func (r *reader) le(n int) uint64 {
	b := r.take(n)
	var value uint64
	for i := len(b) - 1; i >= 0; i-- {
		value = value<<8 | uint64(b[i])
	}
	return value
}
func (r *reader) be(n int) uint64 {
	b := r.take(n)
	var value uint64
	for _, v := range b {
		value = value<<8 | uint64(v)
	}
	return value
}
func (r *reader) lenenc() uint64 {
	v := r.byte()
	switch v {
	case 0xfb, 0xff:
		r.err = fmt.Errorf("%w: invalid length encoding", ErrMalformed)
		return 0
	case 0xfc:
		return r.le(2)
	case 0xfd:
		return r.le(3)
	case 0xfe:
		return r.le(8)
	default:
		return uint64(v)
	}
}
func (r *reader) count(max int) int {
	n := r.lenenc()
	if r.err == nil && n > uint64(max) {
		r.err = ErrLimit
	}
	if r.err != nil {
		return 0
	}
	return int(n)
}
func (r *reader) rest() []byte { return r.take(len(r.data) - r.pos) }
func (r *reader) end() error {
	if r.err != nil {
		return r.err
	}
	if r.pos != len(r.data) {
		return fmt.Errorf("%w: trailing payload", ErrMalformed)
	}
	return nil
}
func bit(bitmap []byte, i int) bool { return bitmap[i/8]&(1<<uint(i%8)) != 0 }
func uint32le(b []byte) uint32      { return binary.LittleEndian.Uint32(b) }
