package formatter

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"slices"
	"strings"
	"text/tabwriter"
)

type sourceToken struct {
	kind       token.Token
	start, end int
}

type edit struct {
	start, end int
	text       string
}

type literalLayout struct {
	lit    *ast.CompositeLit
	indent string
}

func expand(src *source, minElements int, kinds map[CompositeKind]bool, work *workspace) ([]byte, error) {
	var tokens []sourceToken
	var edits []edit
	var parents []literalLayout
	ast.Inspect(src.ast, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for len(parents) > 0 && lit.Pos() > parents[len(parents)-1].lit.Rbrace {
			parents = parents[:len(parents)-1]
		}
		if len(lit.Elts) < minElements {
			return true
		}
		kind := work.kind(src, lit.Type, make(map[*ast.TypeSpec]bool))
		if !kinds[kind] {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if kind == CompositeKindStruct {
				if _, ok := kv.Key.(*ast.Ident); !ok {
					return true
				}
			}
		}
		if tokens == nil {
			tokens = scanTokens(src.bytes)
		}
		indent := literalIndent(src, lit, parents)
		if planned, safe := literalEdits(src, lit, tokens, indent); safe {
			edits = append(edits, planned...)
			parents = append(parents, literalLayout{lit: lit, indent: indent})
		}
		return true // still visit nested literals when the parent was skipped
	})
	if len(edits) == 0 {
		return src.bytes, nil
	}
	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
	// Disjoint edits are copied once from original offsets. Nested literal
	// edits touch different separators, so no offsets can drift.
	var out bytes.Buffer
	out.Grow(len(src.bytes) + len(edits))
	end := 0
	for _, e := range edits {
		if e.start < end || e.end < e.start {
			return nil, fmt.Errorf("overlapping composite edits at byte %d", e.start)
		}
		out.Write(src.bytes[end:e.start])
		out.WriteString(e.text)
		end = e.end
	}
	out.Write(src.bytes[end:])
	return out.Bytes(), nil
}

func literalEdits(src *source, lit *ast.CompositeLit, tokens []sourceToken, indent string) ([]edit, bool) {
	newline := "\n"
	if i := bytes.IndexByte(src.bytes, '\n'); i > 0 && src.bytes[i-1] == '\r' {
		newline = "\r\n"
	}
	var edits []edit
	addBreak := func(start, end int, comma bool, prefix string) bool {
		gap := src.bytes[start:end]
		if len(bytes.Trim(gap, " \t\r\n")) != 0 {
			// A base formatter can move an inline block comment onto a new
			// line. Keep skipping separator block comments in that layout
			// too, so running the whole pipeline again stays idempotent.
			if bytes.Contains(gap, []byte("/*")) {
				return false
			}
			// Preserve trailing and leading comments that already have their
			// own line boundaries. Inline inter-field comments are ambiguous;
			// skip the entire literal instead of guessing their attachment.
			last := bytes.LastIndexByte(gap, '\n')
			return !comma && last >= 0 && len(bytes.Trim(gap[last+1:], " \t\r")) == 0
		}
		if !comma && bytes.ContainsRune(gap, '\n') {
			return true
		}
		text := newline + prefix
		if comma {
			text = "," + text
		}
		edits = append(edits, edit{
			start: start,
			end:   end,
			text:  text,
		})
		return true
	}
	if !addBreak(src.token.Offset(lit.Lbrace)+1, src.token.Offset(lit.Elts[0].Pos()), false, indent+"\t") {
		return nil, false
	}
	for i := range lit.Elts {
		boundary := src.token.Offset(lit.Rbrace)
		last := i == len(lit.Elts)-1
		if !last {
			boundary = src.token.Offset(lit.Elts[i+1].Pos())
		}
		n, _ := slices.BinarySearchFunc(tokens, boundary, func(t sourceToken, offset int) int {
			return cmp.Compare(t.start, offset)
		})
		if n == 0 {
			return nil, false
		}
		previous := tokens[n-1]
		if previous.kind == token.COMMA && n >= 2 {
			if bytes.Contains(src.bytes[tokens[n-2].end:previous.start], []byte("/*")) {
				// gofmt may move a block comment from after this comma to
				// before it. Treat both positions conservatively so the next
				// run cannot suddenly start expanding a previously skipped node.
				return nil, false
			}
		}
		comma := previous.kind != token.COMMA
		prefix := indent + "\t"
		if last {
			prefix = indent
		}
		if (!last && comma) || !addBreak(previous.end, boundary, comma, prefix) {
			return nil, false
		}
	}
	if len(edits) > 0 {
		edits = append(edits, alignmentEdits(src, lit)...)
	}
	return edits, true
}

func lineStart(src []byte, offset int) int {
	return bytes.LastIndexByte(src[:offset], '\n') + 1
}

func leadingIndent(src []byte, offset int) string {
	start := lineStart(src, offset)
	end := start
	for end < offset && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}

// Copy the surrounding indentation and add one tab for an inserted element
// line. Account only for ancestor elements moved from the same original line;
// no expression body or unrelated line is reindented.
func literalIndent(src *source, lit *ast.CompositeLit, parents []literalLayout) string {
	offset := src.token.Offset(lit.Pos())
	line := lineStart(src.bytes, offset)
	for i := len(parents) - 1; i >= 0; i-- {
		parent := parents[i]
		for j, elt := range parent.lit.Elts {
			end := parent.lit.Rbrace
			if j+1 < len(parent.lit.Elts) {
				end = parent.lit.Elts[j+1].Pos()
			}
			pos := src.token.Offset(elt.Pos())
			if elt.Pos() <= lit.Pos() && lit.Pos() < end && lineStart(src.bytes, pos) == line {
				if len(bytes.Trim(src.bytes[line:pos], " \t")) == 0 {
					return leadingIndent(src.bytes, pos)
				}
				return parent.indent + "\t"
			}
		}
	}
	return leadingIndent(src.bytes, offset)
}

// Use the standard tabwriter for simple key columns, editing only horizontal
// whitespace after colons. Multiline keys and comments opt out of alignment.
func alignmentEdits(src *source, lit *ast.CompositeLit) []edit {
	keys := make([]string, len(lit.Elts))
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 8, 1, ' ', 0)
	for i, elt := range lit.Elts {
		kv := elt.(*ast.KeyValueExpr)
		key := src.bytes[src.token.Offset(kv.Key.Pos()):src.token.Offset(kv.Colon)]
		gap := src.bytes[src.token.Offset(kv.Colon)+1 : src.token.Offset(kv.Value.Pos())]
		if bytes.ContainsAny(key, "\t\r\n\v\f") || bytes.Contains(key, []byte("/*")) || len(bytes.Trim(gap, " \t")) != 0 {
			return nil
		}
		keys[i] = string(key)
		fmt.Fprintf(w, "%s:\tX\n", key)
	}
	_ = w.Flush() // bytes.Buffer writes cannot fail
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	var edits []edit
	for i, elt := range lit.Elts {
		kv := elt.(*ast.KeyValueExpr)
		padding := lines[i][len(keys[i])+1 : len(lines[i])-1]
		start, end := src.token.Offset(kv.Colon)+1, src.token.Offset(kv.Value.Pos())
		if string(src.bytes[start:end]) != padding {
			edits = append(edits, edit{
				start: start,
				end:   end,
				text:  padding,
			})
		}
	}
	return edits
}

func scanTokens(src []byte) []sourceToken {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var scan scanner.Scanner
	scan.Init(file, src, nil, scanner.ScanComments)
	var tokens []sourceToken
	for {
		pos, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COMMENT || (kind == token.SEMICOLON && literal == "\n") {
			continue
		}
		start := file.Offset(pos)
		length := len(literal)
		if length == 0 {
			length = len(kind.String())
		}
		// scanner removes CR bytes from raw-string token values; AST End()
		// therefore need not be the real source end. Locate its closing
		// backtick in the already-validated raw-string token instead.
		if kind == token.STRING && src[start] == '`' {
			length = bytes.IndexByte(src[start+1:], '`') + 2
		}
		tokens = append(tokens, sourceToken{
			kind:  kind,
			start: start,
			end:   start + length,
		})
	}
	return tokens
}
