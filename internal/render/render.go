// Package render turns Zoekt's results into the compact text a model reads.
//
// A SearchResult is mostly scoring metadata; everything here exists to spend
// as few tokens as possible on what remains.
package render

// Result is a rendered search, before it is written out as text.
type Result struct {
	// Query is echoed back when nothing matched.
	Query string

	Files   []File
	Summary Summary
}

// Summary counts what the search found and what was returned.
type Summary struct {
	// FileCount and MatchCount are totals from the engine, before display
	// truncation; ShownFiles is what survived it.
	FileCount    int
	MatchCount   int
	ShownFiles   int
	FilesSkipped int
}

// Truncated reports that the engine found more files than it returned.
func (s Summary) Truncated() bool {
	return s.ShownFiles < s.FileCount
}

// File is one matching file and the line spans worth showing from it.
type File struct {
	Repo  string
	Path  string
	Spans []Span

	// FilenameMatch reports that the path matched but the contents did not.
	FilenameMatch bool
}

// Span is a run of consecutive lines. Two spans are never adjacent.
type Span struct {
	Lines []Line
}

// Line is one source line, numbered from 1.
type Line struct {
	Text   string
	Number int
	Match  bool
}
