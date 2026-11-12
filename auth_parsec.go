// Copyright 2026 Anton Antich. SPDX-License-Identifier: MPL-2.0

package mariadb

import (
	"context"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"fmt"
)

// parsecPlugin implements MariaDB's password-derived Ed25519 challenge exchange.
// Wire layout follows plugin/auth_parsec/server_parsec.cc: 32-byte server nonce,
// empty acknowledgement, algorithm/factor/18-byte salt, then client nonce and
// signature over server||client. A fresh instance belongs to one auth exchange.
type parsecPlugin struct {
	nonce [32]byte
	used  bool
}

func init() { RegisterAuthPlugin("parsec", func() AuthPlugin { return &parsecPlugin{} }) }

func (p *parsecPlugin) InitAuth(ctx context.Context, nonce []byte, auth *AuthContext) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(nonce) != len(p.nonce) {
		return nil, ErrMalformPkt
	}
	copy(p.nonce[:], nonce)
	p.used = false
	return []byte{}, nil
}

func (p *parsecPlugin) ContinuationAuth(ctx context.Context, salt, _ []byte, auth *AuthContext) ([]byte, error) {
	if p.used || len(salt) != 20 || salt[0] != 'P' {
		return nil, ErrMalformPkt
	}
	p.used = true
	// An untrusted peer controls the factor. Bound CPU before key derivation;
	// MariaDB's default factor is zero (1024 rounds). Never allocate by factor.
	if salt[1] > 10 {
		return nil, fmt.Errorf("mariadb: PARSEC work factor exceeds 1048576 rounds")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seed, err := pbkdf2.Key(sha512.New, auth.Password(), salt[2:], 1<<(10+salt[1]), ed25519.SeedSize)
	if err != nil {
		return nil, err
	}
	key := ed25519.NewKeyFromSeed(seed)
	defer clear(seed)
	defer clear(key)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var pair [64]byte
	copy(pair[:32], p.nonce[:])
	if _, err := rand.Read(pair[32:]); err != nil {
		return nil, err
	}
	response := make([]byte, 32, 96)
	copy(response, pair[32:])
	return append(response, ed25519.Sign(key, pair[:])...), nil
}
