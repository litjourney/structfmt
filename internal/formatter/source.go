package formatter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// source retains the original byte offsets, including CRLF and //line directives.
type source struct {
	name  string
	dir   string // empty for stdin: never infer a package from the working directory
	bytes []byte
	ast   *ast.File
	token *token.File
	err   error
	types *packageTypes // lazily indexed declarations for an in-memory source
}

func parse(name string, src []byte) *source {
	f := &source{name: name, bytes: src}
	if !utf8.Valid(src) {
		f.err = fmt.Errorf("%s: source is not valid UTF-8", name)
		return f
	}
	fset := token.NewFileSet()
	// Object resolution is intentional: local types and type parameters must
	// shadow package declarations, even in a file that does not type-check.
	f.ast, f.err = parser.ParseFile(fset, name, src, parser.ParseComments|parser.AllErrors)
	if f.ast != nil {
		f.token = fset.File(f.ast.Pos())
	}
	return f
}

type workspace struct {
	files    map[string]*source
	packages map[packageKey]*packageTypes
	modules  map[string]module
}

func newWorkspace() *workspace {
	return &workspace{
		files:    make(map[string]*source),
		packages: make(map[packageKey]*packageTypes),
		modules:  make(map[string]module),
	}
}

func (w *workspace) read(name string) *source {
	if f, ok := w.files[name]; ok {
		return f
	}
	src, err := os.ReadFile(name)
	var f *source
	if err != nil {
		f = &source{name: name, err: err}
	} else {
		f = parse(name, src)
		f.dir = filepath.Dir(name)
	}
	w.files[name] = f
	return f
}
