package zoekt

import "time"

// SearchOptions are the knobs this client sends to Zoekt.
//
// Zoekt's own SearchOptions has roughly fifteen fields; these are the ones that
// change what a caller gets back. The rest are scoring and tracing controls that
// belong to whoever is tuning the engine, not to a tool call.
//
// ChunkMatches is deliberately absent. Upstream documents it as EXPERIMENTAL,
// and LineMatches carries the same information with Before/After context.
type SearchOptions struct {
	// NumContextLines is how many lines Zoekt puts in Before and After.
	//
	// Upstream warns that those context lines may themselves contain matches,
	// so the windows of two nearby matches overlap. Merging them is the
	// caller's job -- see the render layer.
	NumContextLines int `json:",omitempty"`

	// MaxDocDisplayCount truncates the number of files after sorting.
	MaxDocDisplayCount int `json:",omitempty"`

	// MaxMatchDisplayCount truncates the number of matches after sorting.
	MaxMatchDisplayCount int `json:",omitempty"`

	// MaxWallTime bounds the search server side. Zoekt applies a 20 second
	// default when this is zero, which is longer than any tool call should
	// wait, so it is always set.
	MaxWallTime time.Duration `json:",omitempty"`

	// Whole asks for the entire file content rather than matching lines.
	// This is what makes reading a file possible: a query that matches only a
	// filename still comes back with Content populated.
	Whole bool `json:",omitempty"`
}

// SearchResult is the subset of Zoekt's result this client decodes.
//
// Unknown fields are dropped by encoding/json, which is the point: Zoekt's
// SearchResult also carries per-shard counters, scoring debug output and URL
// templates that nothing here needs.
type SearchResult struct {
	Files []FileMatch

	// Stats is embedded rather than named because upstream embeds it too, so
	// its counters arrive flattened into the same object as Files -- there is
	// no "Stats" key in the response. A named field here decodes to zero on
	// every search, silently. It trails Files only to satisfy fieldalignment;
	// what flattens the counters is the embedding, not the position.
	Stats
}

// Stats is the handful of counters worth reporting back to a caller.
type Stats struct {
	// FileCount is how many files matched.
	FileCount int
	// MatchCount is how many individual matches were found.
	MatchCount int
	// FilesSkipped is how many files the engine did not consider.
	FilesSkipped int
}

// FileMatch is one file that matched, with the lines that matched inside it.
type FileMatch struct {
	FileName   string
	Repository string
	Language   string
	Version    string
	Branches   []string

	LineMatches []LineMatch

	// Content is the whole file, and is populated only when
	// SearchOptions.Whole was set.
	//
	// Zoekt sends every one of these byte slices as base64, because that is
	// how encoding/json marshals []byte. Declaring them as []byte here rather
	// than string is what makes the decoding automatic -- a string field would
	// hand the caller the base64 itself.
	Content []byte
}

// LineMatch is a single matching line plus the context around it.
//
// The byte slices are grouped ahead of LineNumber for fieldalignment; they
// belong together anyway, since Before, Line and After are one window.
type LineMatch struct {
	Line   []byte
	Before []byte
	After  []byte

	LineFragments []LineFragment

	LineNumber int
}

// LineFragment locates one match within a line, and names the symbol it is
// part of when the index has symbol information.
type LineFragment struct {
	// SymbolInfo is nil unless the index carries symbols and the query asked
	// for them. It leads the struct for fieldalignment.
	SymbolInfo *Symbol

	LineOffset  int
	MatchLength int
}

// Symbol is what ctags recorded for a definition.
type Symbol struct {
	Sym        string
	Kind       string
	Parent     string
	ParentKind string
}

// RepoList is the subset of Zoekt's repository listing this client decodes.
type RepoList struct {
	Repos []*RepoListEntry
}

// RepoListEntry is one indexed repository.
//
// Ordered smallest-pointer-offset first for fieldalignment; Repository is the
// interesting one.
type RepoListEntry struct {
	IndexMetadata IndexMetadata
	Repository    Repository
	Stats         RepoStats
}

// Repository is the metadata Zoekt recorded when the repository was indexed.
//
// LatestCommitDate leads only because time.Time carries a pointer and govet's
// fieldalignment wants those near the front. Read it as Name, URL, Branches,
// HasSymbols, LatestCommitDate.
type Repository struct {
	LatestCommitDate time.Time

	Name     string
	URL      string
	Branches []RepositoryBranch

	// HasSymbols reports whether the index was built with ctags available.
	// Without it a sym: query returns silence rather than an error, so this is
	// the only way to tell "no such symbol" from "this index cannot answer
	// that question".
	HasSymbols bool
}

// RepositoryBranch is one indexed branch and the revision it pointed at.
type RepositoryBranch struct {
	Name    string
	Version string
}

// IndexMetadata describes the index itself rather than the repository.
type IndexMetadata struct {
	IndexTime time.Time
}

// RepoStats counts what the index holds for one repository.
type RepoStats struct {
	Documents    int
	ContentBytes int64
}
