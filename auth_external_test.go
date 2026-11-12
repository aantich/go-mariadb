// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type testGSSClient struct {
	step   func(context.Context, []byte) ([]byte, bool, error)
	closed int
}

func TestGSSAPIRequiredMethod(t *testing.T) {
	for _, reply := range []string{"password-only OK", "password switch"} {
		t.Run(reply, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			_ = server.SetDeadline(deadline)
			checked := make(chan error, 1)
			go func() {
				caps := clientProtocol41 | clientSecureConn | clientPluginAuth
				if _, err := server.Write(authTestHandshake("mysql_native_password", caps)); err != nil {
					checked <- err
					return
				}
				packet, err := readAuthTestPacket(server, 1)
				if err != nil {
					checked <- err
					return
				}
				if len(packet) < 33 {
					checked <- fmt.Errorf("short handshake")
					return
				}
				_, rest, found := bytes.Cut(packet[32:], []byte{0})
				if !found || len(rest) == 0 || rest[0] != 0 {
					checked <- fmt.Errorf("password proof sent before GSSAPI switch")
					return
				}
				data := []byte{0, 0, 0, 2, 0, 0, 0}
				if reply == "password switch" {
					data = []byte("\xfemysql_native_password\x000123456789abcdefghij\x00")
				}
				_, err = server.Write(makePacket(2, data))
				checked <- err
			}()
			cfg := NewConfig()
			cfg.User = "user"
			cfg.Passwd = "must-not-send-password-proof"
			cfg.DialFunc = func(context.Context, string, string) (net.Conn, error) { return client, nil }
			_ = cfg.Apply(WithGSSAPI(GSSAPIConfig{ServicePrincipal: "mariadb/localhost@REALM", NewClient: func(context.Context, GSSAPIRequest) (GSSAPIClient, error) {
				t.Error("provider called before GSSAPI switch")
				return nil, nil
			}}))
			connector, err := NewConnector(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := connector.Connect(ctx); !errors.Is(err, ErrGSSAPIPolicy) {
				t.Fatalf("required method bypassed: %v", err)
			}
			if err := <-checked; err != nil {
				t.Fatal(err)
			}
		})
	}
}

type closingAuthTest struct {
	authTestPlugin
	closed *int
}

func (p *closingAuthTest) Close() error {
	*p.closed = *p.closed + 1
	return nil
}

func TestExternalAuthPanicCleanup(t *testing.T) {
	closed := 0
	marker := errors.New("callback panic")
	registerTestAuthPlugin(t, "test_external_panic", func() AuthPlugin {
		return &closingAuthTest{authTestPlugin: authTestPlugin{init: func(context.Context, []byte, *AuthContext) ([]byte, error) { panic(marker) }}, closed: &closed}
	})
	mock, _ := newRWMockConn(1)
	mock.data = authTestHandshake("test_external_panic", clientProtocol41|clientSecureConn|clientPluginAuth)
	cfg := NewConfig()
	cfg.DialFunc = func(context.Context, string, string) (net.Conn, error) { return mock, nil }
	connector, err := NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() != marker {
				t.Error("panic identity lost")
			}
		}()
		_, _ = connector.Connect(t.Context())
	}()
	if closed != 1 || !mock.closed {
		t.Fatalf("panic leaked resources: closes=%d transport closed=%v", closed, mock.closed)
	}
}

func (c *testGSSClient) Step(ctx context.Context, token []byte) ([]byte, bool, error) {
	return c.step(ctx, token)
}
func (c *testGSSClient) Close() error { c.closed++; return nil }

func TestExternalAuthPolicy(t *testing.T) {
	for _, name := range []string{"dialog", "auth_gssapi_client"} {
		t.Run("permission-"+name, func(t *testing.T) {
			registerTestAuthPlugin(t, name, func() AuthPlugin {
				t.Error("registry override bypassed external authentication permission")
				return nil
			})
			_, mc := newRWMockConn(1)
			if _, _, err := mc.initAuth(t.Context(), name, nil, newAuthContext(mc.cfg, "", false)); err == nil {
				t.Fatal("disabled external method accepted")
			}
		})
	}
	for _, max := range []int{-1, 65} {
		t.Run(fmt.Sprint(max), func(t *testing.T) {
			cfg := NewConfig()
			if err := cfg.Apply(WithPAMDialog(PAMDialogConfig{MaxPrompts: max})); err == nil {
				t.Fatal("invalid prompt policy accepted")
			}
			if err := cfg.Apply(WithGSSAPI(GSSAPIConfig{ServicePrincipal: "svc", MaxSteps: max})); err == nil {
				t.Fatal("invalid GSSAPI policy accepted")
			}
		})
	}
	t.Run("snapshots and DSN", func(t *testing.T) {
		cfg := NewConfig()
		if err := cfg.Apply(WithPAMDialog(PAMDialogConfig{Prompt: func(context.Context, PAMPrompt) (string, error) { return "", nil }}),
			WithGSSAPI(GSSAPIConfig{ServicePrincipal: "mariadb/localhost@REALM", NewClient: func(context.Context, GSSAPIRequest) (GSSAPIClient, error) { return nil, nil }})); err != nil {
			t.Fatal(err)
		}
		clone := cfg.Clone()
		cfg.pamDialog.MaxPrompts = 1
		cfg.gssapi.ServicePrincipal = "changed"
		if clone.pamDialog.MaxPrompts != 8 || clone.gssapi.MaxSteps != 16 || clone.gssapi.ServicePrincipal != "mariadb/localhost@REALM" {
			t.Fatal("configuration snapshots share mutable policies")
		}
		if strings.Contains(clone.FormatDSN(), "REALM") || strings.Contains(clone.FormatDSN(), "pam") {
			t.Fatal("external authentication policy serialized into DSN")
		}
		if err := clone.Apply(WithPAMDialog(PAMDialogConfig{}), WithGSSAPI(GSSAPIConfig{})); err != nil || clone.pamDialog != nil || clone.gssapi != nil {
			t.Fatal("zero policies did not disable plugins")
		}
	})
}

func dialogAuth(t *testing.T, callback func(context.Context, PAMPrompt) (string, error)) *AuthContext {
	t.Helper()
	cfg := NewConfig()
	cfg.TLS = &tls.Config{}
	if err := cfg.Apply(WithPAMDialog(PAMDialogConfig{Prompt: callback})); err != nil {
		t.Fatal(err)
	}
	return newAuthContext(cfg, "", true)
}

func TestPAMDialogProtocol(t *testing.T) {
	for _, kind := range []byte{2, 3, 4, 5} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			calls := 0
			auth := dialogAuth(t, func(_ context.Context, prompt PAMPrompt) (string, error) {
				calls++
				if prompt.Text != "Question?" || prompt.Number != 1 || prompt.Echo != (kind < 4) || prompt.Last != (kind&1 != 0) {
					t.Fatal("incorrect prompt metadata")
				}
				return "answer", nil
			})
			p := &dialogPlugin{}
			response, err := p.InitAuth(t.Context(), append([]byte{kind}, []byte("Question?\x00")...), auth)
			if err != nil || !bytes.Equal(response, []byte("answer\x00")) || calls != 1 || p.ValidateAuth() != nil {
				t.Fatalf("response=%q error=%v calls=%d", response, err, calls)
			}
			if kind&1 != 0 {
				if _, err := p.ContinuationAuth(t.Context(), []byte{2}, nil, auth); !errors.Is(err, ErrMalformPkt) || calls != 1 {
					t.Fatal("question after last was accepted")
				}
			}
		})
	}
	for i, packet := range [][]byte{{0}, {1}, {6}, {255}, {2, 'x', 0, 'y'}, bytes.Repeat([]byte{4}, 65538)} {
		t.Run(fmt.Sprintf("malformed-%d", i), func(t *testing.T) {
			auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { t.Fatal("called for invalid prompt"); return "", nil })
			if _, err := (&dialogPlugin{}).InitAuth(t.Context(), packet, auth); !errors.Is(err, ErrMalformPkt) {
				t.Fatal("invalid prompt accepted")
			}
		})
	}
	t.Run("empty switch acknowledgement", func(t *testing.T) {
		auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { t.Fatal("callback before prompt"); return "", nil })
		p := &dialogPlugin{}
		if response, err := p.InitAuth(t.Context(), nil, auth); err != nil || len(response) != 0 || p.ValidateAuth() == nil {
			t.Fatal("empty switch not safely acknowledged")
		}
		if _, err := p.ContinuationAuth(t.Context(), nil, nil, auth); !errors.Is(err, ErrMalformPkt) {
			t.Fatal("empty continuation accepted")
		}
	})
	for _, mode := range []string{"disabled", "plain", "unverified TLS", "Unix", "verified TLS"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { calls++; return "", nil })
			if mode == "disabled" {
				auth.pamDialog = nil
			}
			if mode == "plain" || mode == "unverified TLS" || mode == "Unix" {
				auth.verifiedTLS = false
			}
			if mode == "Unix" {
				auth.unixSocket = true
			}
			_, err := (&dialogPlugin{}).InitAuth(t.Context(), []byte{4}, auth)
			allowed := mode == "Unix" || mode == "verified TLS"
			if allowed != (err == nil) || allowed != (calls == 1) {
				t.Fatal("transport policy bypassed")
			}
		})
	}
	t.Run("bounded prompts", func(t *testing.T) {
		calls := 0
		auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { calls++; return "", nil })
		auth.pamDialog.MaxPrompts = 2
		p := &dialogPlugin{}
		for i := 0; i < 3; i++ {
			_, err := p.answer(t.Context(), []byte{4}, auth)
			if (err == nil) != (i < 2) {
				t.Fatal("incorrect prompt budget")
			}
		}
		if calls != 2 {
			t.Fatal("callback exceeded prompt budget")
		}
	})
	for _, response := range []string{"bad\x00tail", strings.Repeat("x", 65537)} {
		t.Run(fmt.Sprint(len(response)), func(t *testing.T) {
			auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { return response, nil })
			if data, err := (&dialogPlugin{}).InitAuth(t.Context(), []byte{4}, auth); err == nil || data != nil {
				t.Fatal("bad response accepted")
			}
		})
	}
	t.Run("cancellation and callback identity", func(t *testing.T) {
		marker := errors.New("callback error")
		auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { return "", marker })
		if _, err := (&dialogPlugin{}).InitAuth(t.Context(), []byte{4}, auth); !errors.Is(err, marker) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		auth.pamDialog.Prompt = func(context.Context, PAMPrompt) (string, error) { cancel(); return "secret", nil }
		if data, err := (&dialogPlugin{}).InitAuth(ctx, []byte{4}, auth); !errors.Is(err, context.Canceled) || data != nil {
			t.Fatal("canceled response returned")
		}
	})
}

func TestGSSAPIProtocol(t *testing.T) {
	const spn = "mariadb/localhost@REALM"
	for _, suffix := range []string{"\x00", "\x00\x00", "\x00Kerberos\x00", "\x00Negotiate\x00", "\x00NTLM\x00", "\x00Kerberos", "\x00\x00tail", ""} {
		t.Run(fmt.Sprintf("announcement-%x", suffix), func(t *testing.T) {
			cfg := NewConfig()
			calls := 0
			client := &testGSSClient{step: func(context.Context, []byte) ([]byte, bool, error) { return []byte("ticket"), false, nil }}
			_ = cfg.Apply(WithGSSAPI(GSSAPIConfig{ServicePrincipal: spn, NewClient: func(context.Context, GSSAPIRequest) (GSSAPIClient, error) { calls++; return client, nil }}))
			p := &gssapiPlugin{}
			_, err := p.InitAuth(t.Context(), []byte(spn+suffix), newAuthContext(cfg, "", false))
			allowed := suffix == "\x00" || suffix == "\x00\x00" || suffix == "\x00Kerberos\x00"
			if allowed != (err == nil) || allowed != (calls == 1) {
				t.Fatal("incorrect announcement policy")
			}
			_ = p.Close()
		})
	}
	t.Run("pin before provider", func(t *testing.T) {
		cfg := NewConfig()
		_ = cfg.Apply(WithGSSAPI(GSSAPIConfig{ServicePrincipal: spn, NewClient: func(context.Context, GSSAPIRequest) (GSSAPIClient, error) {
			t.Fatal("provider called before pin check")
			return nil, nil
		}}))
		if _, err := (&gssapiPlugin{}).InitAuth(t.Context(), []byte("other\x00\x00"), newAuthContext(cfg, "", false)); !errors.Is(err, ErrGSSAPIPolicy) {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"success", "premature OK", "step budget", "no first token", "oversized token", "error", "factory error", "nil provider", "SPNEGO"} {
		t.Run(mode, func(t *testing.T) {
			cfg := NewConfig()
			steps := 0
			marker := errors.New("provider error")
			client := &testGSSClient{step: func(_ context.Context, input []byte) ([]byte, bool, error) {
				steps++
				if mode == "no first token" {
					return nil, false, nil
				}
				if mode == "oversized token" {
					return make([]byte, (1<<20)+1), false, nil
				}
				if mode == "error" {
					return nil, false, marker
				}
				if steps == 1 {
					return []byte("ticket"), false, nil
				}
				if string(input) != "reply" {
					t.Fatal("wrong server token")
				}
				return nil, true, nil
			}}
			_ = cfg.Apply(WithGSSAPI(GSSAPIConfig{ServicePrincipal: spn, MaxSteps: 1, AllowSPNEGO: mode == "SPNEGO", NewClient: func(_ context.Context, r GSSAPIRequest) (GSSAPIClient, error) {
				if r.ServicePrincipal != spn || r.Negotiate != (mode == "SPNEGO") {
					t.Fatal("wrong pinned request")
				}
				if mode == "nil provider" {
					return nil, nil
				}
				if mode == "factory error" {
					return client, marker
				}
				return client, nil
			}}))
			if mode != "step budget" {
				cfg.gssapi.MaxSteps = 4
			}
			_, mc := newRWMockConn(2)
			mc.cfg = cfg
			packet := []byte(spn + "\x00\x00")
			if mode == "SPNEGO" {
				packet = []byte(spn + "\x00Negotiate\x00")
			}
			p, _, err := mc.initAuth(t.Context(), "auth_gssapi_client", packet, newAuthContext(cfg, "", false))
			if mode == "nil provider" || mode == "factory error" || mode == "error" || mode == "no first token" || mode == "oversized token" {
				if err == nil {
					t.Fatal("invalid initialization succeeded")
				}
				if mode != "nil provider" && client.closed != 1 {
					t.Fatal("failed initialization leaked provider")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			g := p.(*gssapiPlugin)
			if g.ValidateAuth() == nil {
				t.Fatal("premature OK accepted")
			}
			if mode != "premature OK" {
				response, err := g.ContinuationAuth(t.Context(), []byte("reply"), nil, nil)
				if mode == "step budget" {
					if !errors.Is(err, ErrGSSAPIPolicy) {
						t.Fatal("step budget bypassed")
					}
				} else if err != nil || response != nil || g.ValidateAuth() != nil {
					t.Fatal("mutual completion failed")
				}
			}
			_ = g.Close()
			_ = g.Close()
			if client.closed != 1 {
				t.Fatal("provider cleanup not exactly once")
			}
		})
	}
}

func TestExternalAuthWire(t *testing.T) {
	for _, outcome := range []string{"success", "premature OK", "server error", "switch", "read error"} {
		t.Run(outcome, func(t *testing.T) {
			conn, mc := newRWMockConn(2)
			provider := &testGSSClient{step: func(context.Context, []byte) ([]byte, bool, error) { return nil, true, nil }}
			plugin := &gssapiPlugin{client: provider, limit: 4}
			switch outcome {
			case "success":
				conn.data = append(makePacket(2, append([]byte{1}, "server-token"...)), makePacket(3, []byte{0, 0, 0, 2, 0, 0, 0})...)
			case "premature OK":
				conn.data = makePacket(2, []byte{0, 0, 0, 2, 0, 0, 0})
			case "server error":
				conn.data = makePacket(2, append([]byte{255, 21, 4, '#', '2', '8', '0', '0', '0'}, "secret-ticket"...))
			case "switch":
				conn.data = makePacket(2, []byte("\xfemysql_native_password\x000123456789abcdefghij\x00"))
				conn.queuedReplies = [][]byte{makePacket(4, []byte{0, 0, 0, 2, 0, 0, 0})}
			case "read error":
				conn.closed = true
			}
			if outcome != "read error" {
				conn.maxReads = 2
			}
			err := mc.handleAuthResult(t.Context(), nil, plugin, newAuthContext(mc.cfg, "", false))
			if (err == nil) != (outcome == "success" || outcome == "switch") {
				t.Fatalf("wrong result: %v", err)
			}
			if outcome == "server error" {
				var server *MySQLError
				if !errors.As(err, &server) || server.Number != 1045 || strings.Contains(err.Error(), "secret-ticket") {
					t.Fatal("error identity/redaction failed")
				}
			}
			if provider.closed != 1 {
				t.Fatal("provider leaked")
			}
		})
	}
	t.Run("binary announcement terminators", func(t *testing.T) {
		_, mc := newRWMockConn(2)
		for _, plugin := range []string{"dialog", "auth_gssapi_client"} {
			packet := append(append([]byte{254}, []byte(plugin)...), 0)
			packet = append(packet, 'x', 0, 0)
			_, data := mc.parseAuthSwitchData(packet, nil)
			if !bytes.Equal(data, []byte{'x', 0, 0}) {
				t.Fatal("binary announcement was trimmed")
			}
		}
	})
}

// A peer can change methods after receiving a PAM answer. Exercise both an
// initial dialog and entry through a switch so redaction cannot depend on the
// last plugin, or only on the plugin selected by the greeting.
func TestExternalAuthErrorSecrecy(t *testing.T) {
	const secret = "synthetic-response-sentinel"
	for _, initial := range []bool{true, false} {
		for _, outcome := range []string{"ERR", "unknown plugin", "native then ERR", "native then unknown", "SHA256 PEM type", "caching PEM type", "callback error", "callback server error"} {
			t.Run(fmt.Sprintf("initial=%v/%s", initial, outcome), func(t *testing.T) {
				mock, mc := newRWMockConn(2)
				auth := dialogAuth(t, func(context.Context, PAMPrompt) (string, error) { return secret, nil })
				switchPacket := func(name string, challenge []byte) []byte {
					return append(append(append([]byte{iEOF}, name...), 0), challenge...)
				}
				seed := []byte("0123456789abcdefghij")
				name, challenge := "dialog", []byte{4, '?'}
				if !initial {
					name, challenge = "mysql_native_password", seed
				}
				plugin, response, err := mc.initAuth(t.Context(), name, challenge, auth)
				if err != nil {
					t.Fatal(err)
				}
				if err := mc.writeAuthSwitchPacket(response); err != nil {
					t.Fatal(err)
				}
				var replies [][]byte
				if !initial {
					replies = append(replies, switchPacket("dialog", []byte{4, '?'}))
				}
				failure := append([]byte{iERR, 21, 4, '#', '2', '8', '0', '0', '0'}, secret...)
				want := error(&MySQLError{Number: 1045})
				var marker error = fmt.Errorf("application callback: %w", ErrMalformPkt)
				if outcome == "callback server error" {
					marker = &MySQLError{Number: 1234, Message: "trusted callback error"}
				}
				callbackText := marker.Error()
				switch outcome {
				case "ERR":
					replies = append(replies, failure)
				case "unknown plugin":
					replies = append(replies, switchPacket(secret, nil))
					want = ErrUnknownPlugin
				case "native then ERR", "native then unknown":
					replies = append(replies, switchPacket("mysql_native_password", append(seed, 0)))
					if outcome == "native then ERR" {
						replies = append(replies, failure)
					} else {
						replies = append(replies, switchPacket(secret, nil))
						want = ErrUnknownPlugin
					}
				case "SHA256 PEM type", "caching PEM type":
					// This custom test plugin drives the inherited public-key parser
					// on the already protected transport, without enabling MySQL use.
					registerTestAuthPlugin(t, "test_peer_key", func() AuthPlugin {
						next := (&sha256PasswordPlugin{awaitingPublicKey: true}).ContinuationAuth
						if outcome == "caching PEM type" {
							next = (&cachingSha2PasswordPlugin{state: cachingSha2PublicKey}).ContinuationAuth
						}
						return &authTestPlugin{
							init: func(context.Context, []byte, *AuthContext) ([]byte, error) { return nil, nil },
							next: next,
						}
					})
					replies = append(replies, switchPacket("test_peer_key", nil),
						append([]byte{iAuthMoreData}, pem.EncodeToMemory(&pem.Block{Type: secret, Bytes: []byte{1}})...))
					want = ErrMalformPkt
				case "callback error", "callback server error":
					registerTestAuthPlugin(t, "test_callback_error", func() AuthPlugin {
						return &authTestPlugin{init: func(context.Context, []byte, *AuthContext) ([]byte, error) { return nil, marker }}
					})
					replies = append(replies, switchPacket("test_callback_error", nil))
					want = marker
				}
				sequence := byte(3)
				mock.data = makePacket(sequence, replies[0])
				for _, reply := range replies[1:] {
					sequence += 2 // Every preceding switch/continuation expects a reply.
					mock.queuedReplies = append(mock.queuedReplies, makePacket(sequence, reply))
				}
				mock.maxReads = len(replies) + 1
				err = mc.handleAuthResult(t.Context(), challenge, plugin, auth)
				if !errors.Is(err, want) || strings.Contains(err.Error(), secret) {
					t.Fatalf("error secrecy or classification lost: %v", err)
				}
				if !bytes.Contains(mock.written, []byte(secret)) {
					t.Fatal("probe never sent its synthetic PAM response")
				}
				if strings.HasPrefix(outcome, "callback") && (err != marker || err.Error() != callbackText) {
					t.Fatal("trusted callback error identity changed")
				}
				if server, ok := err.(*MySQLError); ok && strings.HasSuffix(outcome, "ERR") && server.SQLState != [5]byte{'2', '8', '0', '0', '0'} {
					t.Fatal("server SQLSTATE changed")
				}
			})
		}
	}
}
