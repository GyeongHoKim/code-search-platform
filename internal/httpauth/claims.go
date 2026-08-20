package httpauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// errNoKID means a token's header carried no key id, so there is nothing to
// look its signing key up by.
var errNoKID = errors.New("token header carries no kid")

// allowedAlgs is the explicit allow-list checked against the token's alg
// header. jwt.WithValidMethods is what closes the classic algorithm-confusion
// hole: without it, a library will happily use whatever alg the caller wrote
// in the header, including asking an RSA verifier to treat a public key as an
// HMAC secret.
var allowedAlgs = []string{jwt.SigningMethodRS256.Alg(), jwt.SigningMethodES256.Alg()}

// verifyJWT returns an auth.TokenVerifier that checks a bearer token's
// signature against keys, and its iss/aud/exp/nbf against issuer and
// audience.
func verifyJWT(keys *keySet, issuer, audience string, skew time.Duration) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		var claims jwt.MapClaims

		parsed, err := jwt.ParseWithClaims(token, &claims, keyfunc(ctx, keys),
			jwt.WithValidMethods(allowedAlgs),
			jwt.WithAudience(audience),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(skew),
		)
		if err == nil {
			err = checkIssuer(claims, issuer)
		}

		switch {
		case err == nil && parsed.Valid:
			return tokenInfo(claims), nil
		case errors.Is(err, auth.ErrInvalidToken):
			// keyfunc already branded this one: an unknown kid or a
			// missing-kid header is the caller's fault, not this server's.
			return nil, fmt.Errorf("bearer token: %w", err)
		case errors.As(err, new(*jwtKeyfuncError)):
			// The JWKS endpoint itself failed to answer: this server's fault,
			// not the caller's token, so it must not be branded invalid --
			// RequireBearerToken maps a plain error to 500, which is the
			// honest status here.
			return nil, fmt.Errorf("bearer token: %w", err)
		default:
			// Any other parse/claim failure (bad signature, wrong iss/aud,
			// expired, malformed, disallowed alg) is the caller's token
			// being wrong.
			return nil, fmt.Errorf("bearer token: %w: %w", err, auth.ErrInvalidToken)
		}
	}
}

// checkIssuer is jwt.WithIssuer minus its sensitivity to a trailing slash.
// Issuer identifiers are compared as strings, and config normalises the
// configured one by trimming "/", but some authorization servers (Authentik:
// "https://host/application/o/<slug>/") mint iss with the slash. Either form
// names the same server, so neither may cause a rejection. A missing iss is
// still one.
func checkIssuer(claims jwt.MapClaims, issuer string) error {
	iss, err := claims.GetIssuer()
	if err != nil {
		return fmt.Errorf("%w: %w", jwt.ErrTokenInvalidIssuer, err)
	}
	if strings.TrimSuffix(iss, "/") != strings.TrimSuffix(issuer, "/") {
		return fmt.Errorf("%w: %q", jwt.ErrTokenInvalidIssuer, iss)
	}

	return nil
}

// jwtKeyfuncError wraps an error keyfunc returns for a reason that is this
// server's fault (the JWKS endpoint failed to answer), so verifyJWT can tell
// it apart from a caller-fault verification failure after jwt.ParseWithClaims
// has wrapped it.
type jwtKeyfuncError struct{ err error }

func (e *jwtKeyfuncError) Error() string { return e.err.Error() }
func (e *jwtKeyfuncError) Unwrap() error { return e.err }

// keyfunc looks up the public key a token's kid names. A missing kid or one
// keys does not recognise (even after a refetch) is the caller's fault; keys
// itself failing to answer is this server's.
func keyfunc(ctx context.Context, keys *keySet) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("%w: %w", errNoKID, auth.ErrInvalidToken)
		}

		pub, err := keys.key(ctx, kid)
		if err != nil {
			if errors.Is(err, errUnknownKID) {
				return nil, fmt.Errorf("%w: %w", err, auth.ErrInvalidToken)
			}

			return nil, &jwtKeyfuncError{err: err}
		}

		return pub, nil
	}
}

// tokenInfo maps validated claims onto what the rest of this server sees.
// Extra is left nil: nothing downstream reads it yet.
func tokenInfo(claims jwt.MapClaims) *auth.TokenInfo {
	info := &auth.TokenInfo{}

	// Both getters can only fail if the claim is present but holds the wrong
	// JSON type; parsing already required exp, and skipping a malformed sub
	// just leaves UserID empty rather than failing a token that otherwise
	// verified.
	if sub, err := claims.GetSubject(); err == nil {
		info.UserID = sub
	}
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		info.Expiration = exp.Time
	}
	if scope, ok := claims["scope"].(string); ok && scope != "" {
		info.Scopes = strings.Fields(scope)
	}

	return info
}
