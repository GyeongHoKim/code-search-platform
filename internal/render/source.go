package render

import (
	"fmt"
	"strings"
)

// maxFileLines bounds one read. Tokens are spent the moment they are
// returned, and a caller cannot know a file is enormous before asking.
const maxFileLines = 400

// Source renders content as numbered lines, bounded to maxFileLines.
//
// start and end are 1-based and inclusive; zero means "unspecified".
func Source(repo, path string, content []byte, start, end int) string {
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	total := len(lines)

	if start < 1 {
		start = 1
	}
	if start > total {
		return fmt.Sprintf("%s:%s has %d lines; start_line=%d is past the end\n", repo, path, total, start)
	}

	if end < start || end > total {
		end = total
	}

	truncated := end-start >= maxFileLines
	if truncated {
		end = start + maxFileLines - 1
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s:%s lines %d-%d of %d\n", repo, path, start, end, total)

	for number := start; number <= end; number++ {
		Line{Number: number, Text: trim(lines[number-1])}.write(&out)
	}

	if truncated {
		fmt.Fprintf(&out, "%s truncated at line %d of %d; call read_file again with start_line=%d\n",
			spanSeparator, end, total, end+1)
	}

	return out.String()
}
