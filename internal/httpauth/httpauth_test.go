package httpauth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GyeongHoKim/code-search-platform/internal/httpauth"
)

const (
	first  = "first-test-token"
	second = "second-test-token"
)

// reached reports whether the guarded handler ran, and answers 204 when it did.
func reached(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestRequireTokenRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no authorization header": "",
		"the wrong scheme":        "Token " + first,
		"a bare token":            first,
		"an unknown token":        "Bearer 0000000000000000000000000000000",
		"an empty bearer token":   "Bearer ",
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var ran bool
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()

			httpauth.RequireToken([]string{first})(reached(&ran)).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if ran {
				t.Error("the guarded handler ran")
			}
		})
	}
}

func TestRequireTokenAcceptsAnyConfiguredToken(t *testing.T) {
	t.Parallel()

	// Both are live at once so that a token can be rotated without a window in
	// which every caller is broken.
	for _, token := range []string{first, second} {
		t.Run(token[:8], func(t *testing.T) {
			t.Parallel()

			var ran bool
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			httpauth.RequireToken([]string{first, second})(reached(&ran)).ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if !ran {
				t.Error("the guarded handler did not run")
			}
		})
	}
}

func TestRequireTokenCopiesItsTokens(t *testing.T) {
	t.Parallel()

	tokens := []string{first}
	guard := httpauth.RequireToken(tokens)
	tokens[0] = second

	var ran bool
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+first)
	rec := httptest.NewRecorder()

	guard(reached(&ran)).ServeHTTP(rec, req)

	if !ran {
		t.Errorf("status = %d: the caller's slice changed what the guard accepts", rec.Code)
	}
}
