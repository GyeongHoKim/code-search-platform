// Package httpauth guards the http transport with a bearer token.
//
// It is a leaf: it takes the tokens it accepts as an argument rather than
// reading configuration, so it can be tested without an environment and so the
// static check here can be swapped for a JWKS one when an OAuth 2.1 server
// arrives. Everything above it keeps calling RequireToken.
package httpauth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// RequireToken returns middleware that answers 401 unless the request carries
// one of accepted as its bearer token.
func RequireToken(accepted []string) func(http.Handler) http.Handler {
	return auth.RequireBearerToken(verify(slices.Clone(accepted)), &auth.RequireBearerTokenOptions{
		// A static token carries no expiry. The alternative is inventing one
		// the operator did not ask for and cannot see.
		AllowMissingExpiration: true,
	})
}

// verify reports whether token is one of accepted.
func verify(accepted []string) auth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		// Every candidate is compared, and each comparison takes the same time
		// whatever the token is, so neither the answer nor the position of a
		// match leaks through how long this took.
		var match int
		for _, want := range accepted {
			match |= subtle.ConstantTimeCompare([]byte(token), []byte(want))
		}

		if match != 1 {
			return nil, fmt.Errorf("bearer token: %w", auth.ErrInvalidToken)
		}

		// Empty: a static token says only that the caller holds it. Identity
		// and scopes arrive with OAuth, and belong in this struct then.
		return &auth.TokenInfo{}, nil
	}
}
