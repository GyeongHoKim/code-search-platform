// Package version carries the build stamps goreleaser injects with ldflags.
//
// Nothing in git holds a real version number: the tag is the only source, and
// a local build deliberately reports "dev" so an unreleased binary can never
// be mistaken for a released one.
package version

import "fmt"

// Values injected at build time. See the LDFLAGS variable in the justfile.
var (
	// Version is the release tag, or "dev" for a local build.
	Version = "dev"
	// Commit is the git revision the binary was built from.
	Commit = "none"
	// Date is the build timestamp, in RFC 3339 where goreleaser set it.
	Date = "unknown"
)

// String renders the stamps as one line, suitable for --version output.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
