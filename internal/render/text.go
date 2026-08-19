package render

import (
	"fmt"
	"strings"
)

// numberWidth is the column the line numbers are right-aligned in.
const numberWidth = 6

// spanSeparator stands between two non-adjacent spans.
const spanSeparator = "--"

// String writes the result as the text a model reads.
func (r Result) String() string {
	var out strings.Builder

	if len(r.Files) == 0 {
		fmt.Fprintf(&out, "no matches for: %s\n", r.Query)

		return out.String()
	}

	out.WriteString(r.Summary.String())

	for _, file := range r.Files {
		file.write(&out)
	}

	return out.String()
}

// plural renders a count with the right form of its noun.
func plural(count int, one, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, one)
	}

	return fmt.Sprintf("%d %s", count, many)
}

// String writes the counts, and any warning that they are lower bounds.
func (s Summary) String() string {
	var out strings.Builder

	if s.Truncated() {
		fmt.Fprintf(&out, "%s, %s; showing %d -- narrow the query to see the rest\n",
			plural(s.FileCount, "file", "files"), plural(s.MatchCount, "match", "matches"), s.ShownFiles)
	} else {
		fmt.Fprintf(&out, "%s, %s\n", plural(s.FileCount, "file", "files"), plural(s.MatchCount, "match", "matches"))
	}

	if s.FilesSkipped > 0 {
		fmt.Fprintf(&out, "warning: zoekt stopped early; %d files were not examined, so these counts are lower bounds\n",
			s.FilesSkipped)
	}

	return out.String()
}

// write appends one file's header and spans.
func (f File) write(out *strings.Builder) {
	if f.FilenameMatch {
		fmt.Fprintf(out, "%s:%s (filename match)\n", f.Repo, f.Path)

		return
	}

	fmt.Fprintf(out, "%s:%s\n", f.Repo, f.Path)

	for i, span := range f.Spans {
		if i > 0 {
			out.WriteString(spanSeparator + "\n")
		}
		for _, line := range span.Lines {
			line.write(out)
		}
	}
}

// write appends one numbered line, marking it when it matched.
func (l Line) write(out *strings.Builder) {
	marker := " "
	if l.Match {
		marker = ">"
	}

	// Trimmed as a whole: a blank source line would otherwise leave the
	// number and marker followed by nothing.
	out.WriteString(trim(fmt.Sprintf("%*d%s %s", numberWidth, l.Number, marker, l.Text)) + "\n")
}
