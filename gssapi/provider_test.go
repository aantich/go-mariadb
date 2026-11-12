// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package gssapi

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mariadb "github.com/aantich/go-mariadb"
)

type fakeNative struct {
	run     func([]byte) ([]byte, bool, error)
	onClose func()
	closed  atomic.Int32
}

func (n *fakeNative) step(token []byte) ([]byte, bool, error) { return n.run(token) }
func (n *fakeNative) close() {
	if n.onClose != nil {
		n.onClose()
	}
	n.closed.Add(1)
}

func waitDone(t *testing.T, c *client, state *fakeNative) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("native worker leaked")
	}
	if state.closed.Load() != 1 {
		t.Fatal("native resources not released exactly once")
	}
}

func TestProviderCancellation(t *testing.T) {
	for _, phase := range []string{"open", "step", "close during step", "provider error"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			marker := errors.New("provider failed")
			state := &fakeNative{run: func(token []byte) ([]byte, bool, error) {
				if phase == "provider error" {
					return nil, false, marker
				}
				close(entered)
				<-release
				if len(token) > 0 && string(token) != "input" {
					t.Error("canceled caller overwrote native input")
				}
				return []byte("ticket"), true, nil
			}}
			open := func(mariadb.GSSAPIRequest) (nativeContext, error) {
				if phase == "open" {
					close(entered)
					<-release
				}
				return state, nil
			}
			if phase == "open" {
				returned := make(chan error, 1)
				go func() {
					_, err := newClient(ctx, mariadb.GSSAPIRequest{ServicePrincipal: "service"}, open)
					returned <- err
				}()
				<-entered
				cancel()
				select {
				case err := <-returned:
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("open cancellation blocked on native work")
				}
				close(release)
				deadline := time.After(2 * time.Second)
				for state.closed.Load() != 1 {
					select {
					case <-deadline:
						t.Fatal("canceled open leaked resources")
					default:
						time.Sleep(time.Millisecond)
					}
				}
				return
			}
			provider, err := newClient(ctx, mariadb.GSSAPIRequest{ServicePrincipal: "service"}, open)
			if err != nil {
				t.Fatal(err)
			}
			c := provider.(*client)
			if phase == "provider error" {
				if _, _, err := c.Step(ctx, nil); !errors.Is(err, marker) {
					t.Fatal(err)
				}
				_ = c.Close()
				waitDone(t, c, state)
				return
			}
			token := []byte("input")
			returned := make(chan error, 1)
			go func() { _, _, err := c.Step(ctx, token); returned <- err }()
			<-entered
			if phase == "close during step" {
				_ = c.Close()
			} else {
				cancel()
			}
			select {
			case err := <-returned:
				if phase != "close during step" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("step cancellation blocked on native work")
			}
			copy(token, "other")
			close(release)
			_ = c.Close()
			_ = c.Close()
			waitDone(t, c, state)
			if _, _, err := c.Step(t.Context(), nil); err == nil {
				t.Fatal("closed provider accepted step")
			}
		})
	}
}

func TestProviderOpenFailure(t *testing.T) {
	state := &fakeNative{}
	marker := errors.New("open failed")
	_, err := newClient(t.Context(), mariadb.GSSAPIRequest{ServicePrincipal: "service"}, func(mariadb.GSSAPIRequest) (nativeContext, error) { return state, marker })
	if !errors.Is(err, marker) {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for state.closed.Load() != 1 {
		select {
		case <-deadline:
			t.Fatal("failed open leaked")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// SQL callers release their pool slots before canceled native work necessarily
// returns. Hold each native phase and verify the provider's independent bound,
// cancellation while waiting, and capacity recovery after actual cleanup.
func TestProviderAdmission(t *testing.T) {
	for _, phase := range []string{"open", "step", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			provider, err := NewProvider(2)
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var opens atomic.Int32
			states := make([]*fakeNative, 2)
			for i := range states {
				entered := make(chan struct{})
				hold := func() { close(entered); <-release }
				state := &fakeNative{run: func([]byte) ([]byte, bool, error) { hold(); return nil, true, nil }}
				if phase == "cleanup" {
					state.onClose = hold
				}
				states[i] = state
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				open := func(mariadb.GSSAPIRequest) (nativeContext, error) {
					opens.Add(1)
					if phase == "open" {
						hold()
					}
					return state, nil
				}
				returned := make(chan error, 1)
				if phase == "open" {
					go func() {
						_, err := provider.newClient(ctx, mariadb.GSSAPIRequest{ServicePrincipal: "service"}, open)
						returned <- err
					}()
				} else {
					c, err := provider.newClient(ctx, mariadb.GSSAPIRequest{ServicePrincipal: "service"}, open)
					if err != nil {
						t.Fatal(err)
					}
					if phase == "cleanup" {
						_ = c.Close()
					} else {
						go func() { _, _, err := c.Step(ctx, nil); returned <- err }()
					}
				}
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("native phase did not start")
				}
				cancel()
				if phase != "cleanup" {
					select {
					case err := <-returned:
						if !errors.Is(err, context.Canceled) {
							t.Fatal(err)
						}
					case <-time.After(2 * time.Second):
						t.Fatal("native work blocked caller cancellation")
					}
				}
			}
			for range 8 {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
				unexpected, err := provider.newClient(ctx, mariadb.GSSAPIRequest{ServicePrincipal: "service"}, func(mariadb.GSSAPIRequest) (nativeContext, error) {
					opens.Add(1)
					return &fakeNative{}, nil
				})
				cancel()
				if unexpected != nil {
					_ = unexpected.Close()
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("saturated provider admitted new native work: %v", err)
				}
			}
			if opens.Load() != 2 {
				t.Fatalf("canceled callers exceeded native capacity: %d opens", opens.Load())
			}
			unblock()
			// Admission must recover even when the caller never received a client
			// (canceled open), or cleanup had to outlive the caller (step/close).
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			state := &fakeNative{}
			c, err := provider.newClient(ctx, mariadb.GSSAPIRequest{ServicePrincipal: "service"}, func(mariadb.GSSAPIRequest) (nativeContext, error) { return state, nil })
			if err != nil {
				t.Fatal("capacity did not recover:", err)
			}
			_ = c.Close()
			waitDone(t, c.(*client), state)
			deadline := time.After(2 * time.Second)
			for states[0].closed.Load() != 1 || states[1].closed.Load() != 1 {
				select {
				case <-deadline:
					t.Fatal("held native cleanup did not finish")
				default:
					time.Sleep(time.Millisecond)
				}
			}
		})
	}
}

func TestProviderDefaultAdmission(t *testing.T) {
	policy := mariadb.GSSAPIRequest{ServicePrincipal: "service"}
	clients := make([]*client, 0, DefaultMaxContexts)
	states := make([]*fakeNative, 0, DefaultMaxContexts)
	defer func() {
		for i, c := range clients {
			_ = c.Close()
			waitDone(t, c, states[i])
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for range DefaultMaxContexts {
		state := &fakeNative{}
		c, err := newClient(ctx, policy, func(mariadb.GSSAPIRequest) (nativeContext, error) { return state, nil })
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
		clients = append(clients, c.(*client))
	}
	waiting, stop := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer stop()
	c, err := newClient(waiting, policy, func(mariadb.GSSAPIRequest) (nativeContext, error) {
		t.Error("default factory bypassed its shared native-context bound")
		return &fakeNative{}, nil
	})
	if c != nil {
		_ = c.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("full default budget did not honor waiting deadline:", err)
	}
}

func TestProviderCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		if _, err := NewProvider(capacity); err == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
	for _, provider := range []*Provider{nil, {}} {
		if _, err := provider.NewClient(t.Context(), mariadb.GSSAPIRequest{ServicePrincipal: "service"}); !errors.Is(err, mariadb.ErrGSSAPIPolicy) {
			t.Fatal("uninitialized provider accepted:", err)
		}
	}
	provider, _ := NewProvider(2)
	policy := mariadb.GSSAPIRequest{ServicePrincipal: "service"}
	states := []*fakeNative{{}, {}}
	clients := make([]*client, 0, 2)
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for _, state := range states {
		c, err := provider.newClient(ctx, policy, func(mariadb.GSSAPIRequest) (nativeContext, error) { return state, nil })
		if err != nil {
			t.Fatal("independent contexts serialized behind one owner:", err)
		}
		clients = append(clients, c.(*client))
	}
	// A separate provider has its own explicit budget, even when this one is full.
	other, _ := NewProvider(1)
	state := &fakeNative{}
	c, err := other.newClient(ctx, policy, func(mariadb.GSSAPIRequest) (nativeContext, error) { return state, nil })
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	waitDone(t, c.(*client), state)
	for i, c := range clients {
		_ = c.Close()
		waitDone(t, c, states[i])
	}
	// A canceled request must not start native work even with capacity available.
	canceled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := provider.newClient(canceled, policy, func(mariadb.GSSAPIRequest) (nativeContext, error) {
		t.Error("canceled request reached native open")
		return nil, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
