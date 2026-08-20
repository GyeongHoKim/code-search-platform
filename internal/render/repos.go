package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

// columnGap separates the aligned columns of a repository listing.
const columnGap = 2

// Repos renders a repository listing.
//
// symbols= is included because it decides whether find_symbol can answer at
// all for that repository.
func Repos(list *zoekt.RepoList) string {
	if len(list.Repos) == 0 {
		return "no repositories are indexed\n"
	}

	rows := make([][]string, 0, len(list.Repos))
	for _, entry := range list.Repos {
		rows = append(rows, row(entry))
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", plural(len(list.Repos), "repository", "repositories"))
	writeAligned(&out, rows)

	return out.String()
}

// row is one repository's cells.
func row(entry *zoekt.RepoListEntry) []string {
	symbols := "no"
	if entry.Repository.HasSymbols {
		symbols = "yes"
	}

	return []string{
		entry.Repository.Name,
		"symbols=" + symbols,
		"docs=" + strconv.Itoa(entry.Stats.Documents),
		branches(entry.Repository.Branches),
		"indexed=" + entry.IndexMetadata.IndexTime.UTC().Format(time.DateOnly),
	}
}

// branches joins each indexed branch with the revision it pointed at.
func branches(indexed []zoekt.RepositoryBranch) string {
	named := make([]string, 0, len(indexed))
	for _, branch := range indexed {
		named = append(named, branch.Name+"@"+branch.Version)
	}

	return strings.Join(named, ",")
}

// writeAligned pads every column but the last to its widest cell.
func writeAligned(out *strings.Builder, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, cells := range rows {
		for i, cell := range cells {
			widths[i] = max(widths[i], len(cell))
		}
	}

	for _, cells := range rows {
		var line strings.Builder
		for i, cell := range cells {
			if i == len(cells)-1 {
				line.WriteString(cell)

				break
			}
			fmt.Fprintf(&line, "%-*s", widths[i]+columnGap, cell)
		}
		out.WriteString(trim(line.String()) + "\n")
	}
}
