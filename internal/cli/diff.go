package cli

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

func unifiedDiff(name string, original, final []byte) (string, error) {
	name = filepath.ToSlash(name)
	from, to := name+".orig", name
	if strings.ContainsAny(name, "\t\r\n\"") {
		from, to = strconv.Quote(from), strconv.Quote(to)
	}
	return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        diffLines(string(original)),
		B:        diffLines(string(final)),
		FromFile: from,
		ToFile:   to,
		Context:  3,
	})
}

func diffLines(src string) []string {
	if src == "" {
		return nil
	}
	lines := strings.SplitAfter(src, "\n")
	last := len(lines) - 1
	if lines[last] == "" {
		return lines[:last]
	}
	// go-difflib's SplitLines adds a missing newline. Retain the distinction
	// and its standard patch annotation as part of the final logical line.
	lines[last] += "\n\\ No newline at end of file\n"
	return lines
}
