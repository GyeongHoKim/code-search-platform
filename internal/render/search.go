package render

import (
	"sort"
	"strings"

	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// Search renders a search result. query is echoed back when nothing matched.
func Search(result *zoekt.SearchResult, query string) Result {
	rendered := Result{
		Query: query,
		Summary: Summary{
			FileCount:    result.FileCount,
			MatchCount:   result.MatchCount,
			ShownFiles:   len(result.Files),
			FilesSkipped: result.FilesSkipped,
		},
	}

	for i := range result.Files {
		rendered.Files = append(rendered.Files, renderFile(&result.Files[i]))
	}

	return rendered
}

// renderFile collects one file's matches into spans.
func renderFile(file *zoekt.FileMatch) File {
	rendered := File{Repo: file.Repository, Path: file.FileName}

	text := make(map[int]string)
	match := make(map[int]bool)

	for i := range file.LineMatches {
		if file.LineMatches[i].FileName {
			rendered.FilenameMatch = true

			continue
		}
		collect(&file.LineMatches[i], text)
	}

	// Marked after collecting, so that a line pulled in as context by one
	// match is still marked when another match lands on it.
	for i := range file.LineMatches {
		if !file.LineMatches[i].FileName {
			match[file.LineMatches[i].LineNumber] = true
		}
	}

	rendered.Spans = spans(text, match)

	return rendered
}

// collect splits one match's window into numbered lines.
func collect(line *zoekt.LineMatch, text map[int]string) {
	before := split(line.Before)
	for i, content := range before {
		text[line.LineNumber-len(before)+i] = content
	}

	text[line.LineNumber] = trim(string(line.Line))

	for i, content := range split(line.After) {
		text[line.LineNumber+1+i] = content
	}
}

// split breaks a context blob into lines, dropping the trailing newline's
// empty tail.
func split(blob []byte) []string {
	if len(blob) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimSuffix(string(blob), "\n"), "\n")
	for i, line := range lines {
		lines[i] = trim(line)
	}

	return lines
}

// trim removes the trailing whitespace a model cannot see but still pays for.
func trim(line string) string {
	return strings.TrimRight(line, " \t\r\n")
}

// spans groups the collected lines into runs of consecutive numbers.
func spans(text map[int]string, match map[int]bool) []Span {
	if len(text) == 0 {
		return nil
	}

	numbers := make([]int, 0, len(text))
	for number := range text {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)

	var grouped []Span
	current := Span{}

	for i, number := range numbers {
		if i > 0 && number != numbers[i-1]+1 {
			grouped = append(grouped, current)
			current = Span{}
		}
		current.Lines = append(current.Lines, Line{
			Number: number,
			Text:   text[number],
			Match:  match[number],
		})
	}

	return append(grouped, current)
}
