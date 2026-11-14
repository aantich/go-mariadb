// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"errors"
	"strings"
)

// ErrPAMPolicy reports a disabled dialogue, unprotected transport or exhausted
// prompt policy. No response to the rejected prompt has been sent.
var ErrPAMPolicy = errors.New("mariadb: PAM dialogue requires an explicit prompt policy and verified TLS or a Unix socket")

// PAMPrompt is one MariaDB dialog question. Text is an owned string containing
// untrusted server text. Echo says input may be displayed; it is not permission
// to log a response. Last is the server's final-question hint, not authentication
// success. Number starts at one and includes every answered question.
type PAMPrompt struct {
	Text   string
	Echo   bool
	Last   bool
	Number int
}

// PAMDialogConfig enables interactive PAM through MariaDB's dialog plugin.
// Prompt must explicitly recognize acceptable questions and honor ctx, including
// cancellation while waiting for user input. It may run concurrently for separate
// connections; never reuse a one-time code across connections. It must not log
// responses or blindly supply the password to every server-controlled question.
// MaxPrompts defaults to 8 and may be set from 1 to 64. Prompts and responses are
// each bounded to 64 KiB. Embedded NUL responses are rejected, not truncated.
// This policy requires verified TLS (InsecureSkipVerify=false) or a Unix socket.
// It does not enable mysql_clear_password and is never serialized into a DSN.
type PAMDialogConfig struct {
	Prompt     func(context.Context, PAMPrompt) (string, error)
	MaxPrompts int
}

// WithPAMDialog copies a dialogue policy into the connection configuration.
// Existing connectors retain their own snapshot. Disabling uses a nil Prompt
// with MaxPrompts zero. No terminal reader or automatic password reply is installed.
func WithPAMDialog(policy PAMDialogConfig) Option {
	return func(cfg *Config) error {
		if policy.Prompt == nil && policy.MaxPrompts == 0 {
			cfg.pamDialog = nil
			return nil
		}
		if policy.Prompt == nil || policy.MaxPrompts < 0 || policy.MaxPrompts > 64 {
			return ErrPAMPolicy
		}
		copy := policy
		if copy.MaxPrompts == 0 {
			copy.MaxPrompts = 8
		}
		cfg.pamDialog = &copy
		return nil
	}
}

type dialogPlugin struct {
	count int
	last  bool
}

func init() { RegisterAuthPlugin("dialog", func() AuthPlugin { return &dialogPlugin{} }) }

func (*dialogPlugin) sensitiveAuth() {}

func (p *dialogPlugin) InitAuth(ctx context.Context, packet []byte, auth *AuthContext) ([]byte, error) {
	if len(packet) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if auth.pamDialog == nil || !(auth.verifiedTLS || auth.UnixSocket()) {
			return nil, ErrPAMPolicy
		}
		// auth_pam can switch before its helper has produced a question.
		// Acknowledge once without invoking the callback or sending a password.
		return []byte{}, nil
	}
	return p.answer(ctx, packet, auth)
}

func (p *dialogPlugin) ContinuationAuth(ctx context.Context, packet, _ []byte, auth *AuthContext) ([]byte, error) {
	return p.answer(ctx, packet, auth)
}

func (p *dialogPlugin) ValidateAuth() error {
	if p.count == 0 {
		return ErrMalformPkt
	}
	return nil
}

func (p *dialogPlugin) answer(ctx context.Context, packet []byte, auth *AuthContext) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	policy := auth.pamDialog
	if policy == nil || !(auth.verifiedTLS || auth.UnixSocket()) || p.count >= policy.MaxPrompts {
		return nil, ErrPAMPolicy
	}
	// MariaDB auth_pam emits 2 (echo) or 4 (hidden); the low bit marks the
	// last question in the general dialog protocol. Payloads need not end in NUL.
	if p.last || len(packet) < 1 || len(packet) > 65537 || packet[0] < 2 || packet[0] > 5 {
		return nil, ErrMalformPkt
	}
	text := strings.TrimSuffix(string(packet[1:]), "\x00")
	if strings.ContainsRune(text, 0) {
		return nil, ErrMalformPkt
	}
	p.count++
	p.last = packet[0]&1 != 0
	response, err := policy.Prompt(ctx, PAMPrompt{Text: text, Echo: packet[0] < 4, Last: p.last, Number: p.count})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(response) > 65536 || strings.ContainsRune(response, 0) {
		return nil, ErrPAMPolicy
	}
	return append([]byte(response), 0), nil
}
