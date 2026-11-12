// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	mariadb "github.com/aantich/go-mariadb"
)

type transport interface {
	Read(context.Context) (mariadb.ReplicationPacket, error)
	Acknowledge(context.Context, string, uint64) error
	Close() error
}

// Stream reads one event at a time. Next and Acknowledge serialize and their
// waits honor context cancellation. Close may run concurrently to stop I/O.
// LatestCheckpoint is observed progress; AcknowledgedCheckpoint is the last
// token the consumer explicitly reported durable. Neither persists anything.
type Stream struct {
	wire                       transport
	decoder                    *decoder
	semi                       bool
	gate                       chan struct{}
	mu                         sync.RWMutex
	latest, acknowledged       Checkpoint
	hasLatest, hasAcknowledged bool
	err                        error
	once                       sync.Once
	closed                     atomic.Bool
}

// Open creates a dedicated MariaDB stream at an explicit file or GTID state.
// Cancellation of an in-flight read is terminal; reconnect from durable state
// explicitly. The caller assigns a unique nonzero Options.ServerID.
func Open(ctx context.Context, cfg *mariadb.Config, options Options) (*Stream, error) {
	limits, err := options.Limits.normalize()
	if err != nil {
		return nil, err
	}
	if !options.Start.UseGTID && (!validFile(options.Start.File) || options.Start.Position < 4) {
		return nil, fmt.Errorf("%w: explicit start coordinate required", ErrCheckpoint)
	}
	wire, err := mariadb.OpenReplication(ctx, cfg, mariadb.ReplicationOptions{ServerID: options.ServerID, File: options.Start.File, Position: options.Start.Position, GTIDs: options.Start.GTIDs, UseGTID: options.Start.UseGTID, NonBlocking: options.NonBlocking, SemiSync: options.SemiSync, MaxEventBytes: limits.MaxEventBytes, Heartbeat: options.Heartbeat})
	if err != nil {
		return nil, err
	}
	return newStream(wire, limits, options.Start, options.SemiSync), nil
}
func newStream(wire transport, limits Limits, start Resume, semi bool) *Stream {
	return &Stream{wire: wire, decoder: newDecoder(limits, start), semi: semi, gate: make(chan struct{}, 1)}
}
func (s *Stream) acquire(ctx context.Context) error {
	if s == nil || s.wire == nil || s.gate == nil {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-s.gate
		return err
	}
	return nil
}
func (s *Stream) fail(err error) error {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	err = s.err
	s.mu.Unlock()
	s.Close()
	return err
}

// Next returns owned events and never acknowledges on read. A failed or ended
// stream returns the same terminal error thereafter; no partial-group checkpoint
// is issued. Applications implement backpressure by choosing when to call Next.
func (s *Stream) Next(ctx context.Context) (*Event, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-s.gate }()
	s.mu.RLock()
	terminal := s.err
	s.mu.RUnlock()
	if terminal != nil {
		return nil, terminal
	}
	if s.closed.Load() {
		return nil, s.fail(ErrClosed)
	}
	for {
		packet, err := s.wire.Read(ctx)
		if err != nil {
			if s.closed.Load() && ctx.Err() == nil {
				err = ErrClosed
			}
			if err == io.EOF {
				if invalid := s.decoder.end(); invalid != nil {
					err = invalid
				}
			}
			return nil, s.fail(err)
		}
		event, err := s.decoder.decode(packet.Data, packet.AckRequested)
		if err != nil {
			return nil, s.fail(err)
		}
		if event == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, s.fail(err)
		}
		if s.closed.Load() {
			return nil, s.fail(ErrClosed)
		}
		if event.Checkpoint != nil {
			s.mu.Lock()
			s.latest = *event.Checkpoint
			s.hasLatest = true
			s.mu.Unlock()
		}
		return event, nil
	}
}

// Acknowledge accepts only an already delivered, nondecreasing checkpoint from
// this stream. Call after atomically persisting Resume with destination writes.
// Without semisync this records local progress, including after a clean EOF.
// With semisync it also sends a source receipt, before advancing local progress;
// call it before reading beyond the checkpoint to a closed/failed transport.
func (s *Stream) Acknowledge(ctx context.Context, checkpoint *Checkpoint) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-s.gate }()
	s.mu.RLock()
	latest, acked, terminal, hasLatest, hasAcked := s.latest, s.acknowledged, s.err, s.hasLatest, s.hasAcknowledged
	s.mu.RUnlock()
	if checkpoint == nil || checkpoint.owner != s.decoder.owner || !hasLatest || checkpoint.sequence > latest.sequence || (hasAcked && checkpoint.sequence < acked.sequence) {
		return ErrCheckpoint
	}
	if hasAcked && acked.sequence == checkpoint.sequence {
		return nil
	}
	if terminal != nil && terminal != io.EOF {
		return terminal
	}
	if terminal == nil && s.closed.Load() {
		return ErrClosed
	}
	if s.semi {
		state := checkpoint.state
		if err := s.wire.Acknowledge(ctx, state.File, state.Position); err != nil {
			return s.fail(err)
		}
	}
	s.mu.Lock()
	s.acknowledged = *checkpoint
	s.hasAcknowledged = true
	s.mu.Unlock()
	return nil
}

// LatestCheckpoint returns an owned token copy, including after termination.
func (s *Stream) LatestCheckpoint() *Checkpoint {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.hasLatest {
		return nil
	}
	copy := s.latest
	return &copy
}

// AcknowledgedCheckpoint returns an owned copy of consumer-acknowledged progress.
func (s *Stream) AcknowledgedCheckpoint() *Checkpoint {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.hasAcknowledged {
		return nil
	}
	copy := s.acknowledged
	return &copy
}

// Close is idempotent and interrupts blocked I/O. A stream is never returned to
// a pool, and Close never sends COM_QUIT into a replication packet exchange.
func (s *Stream) Close() error {
	if s == nil {
		return nil
	}
	s.closed.Store(true)
	s.once.Do(func() {
		if s.wire != nil {
			s.wire.Close()
		}
	})
	return nil
}
