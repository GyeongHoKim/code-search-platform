// Package httpauth guards the http transport with a bearer token.
//
// It is a leaf: it takes the tokens it accepts as an argument rather than
// reading configuration, so it can be tested without an environment and so the
// static check here can be swapped for a JWKS one when an OAuth 2.1 server
// arrives. Everything above it keeps calling RequireToken.
package httpauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// RequireToken returns middleware that answers 401 unless the request carries
// one of accepted as its bearer token.
func RequireToken(accepted []string) func(http.Handler) http.Handler {
	// Hashed once here rather than per request, and stored instead of the
	// tokens themselves so a heap dump of a running server does not hand over
	// the credentials it was started with.
	digests := make([][sha256.Size]byte, len(accepted))
	for i, token := range accepted {
		digests[i] = sha256.Sum256([]byte(token))
	}

	return auth.RequireBearerToken(verify(digests), &auth.RequireBearerTokenOptions{
		// A static token carries no expiry. The alternative is inventing one
		// the operator did not ask for and cannot see.
		AllowMissingExpiration: true,
	})
}

// verify reports whether token hashes to one of accepted.
func verify(accepted [][sha256.Size]byte) auth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		// Digests, not the tokens: ConstantTimeCompare returns at once when
		// the lengths differ, so comparing raw tokens times a guess of the
		// wrong length faster than a guess of the right one. Hashing makes
		// every comparison the same 32 bytes.
		got := sha256.Sum256([]byte(token))

		// Every candidate is compared, so neither the answer nor the position
		// of a match leaks through how long this took.
		var match int
		for _, want := range accepted {
			match |= subtle.ConstantTimeCompare(got[:], want[:])
		}

		if match != 1 {
			return nil, fmt.Errorf("bearer token: %w", auth.ErrInvalidToken)
		}

		// Empty: a static token says only that the caller holds it. Identity
		// and scopes arrive with OAuth, and belong in this struct then.
		return &auth.TokenInfo{}, nil
	}
}
