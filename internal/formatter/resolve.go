package formatter

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

type typeDecl struct {
	spec *ast.TypeSpec
	file *source
}

type packageKey struct {
	dir, name string
	tests     bool
}

type packageTypes struct {
	name     string
	decls    map[string][]typeDecl
	complete bool
}

func addTypes(p *packageTypes, f *source) {
	for _, decl := range f.ast.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			t := spec.(*ast.TypeSpec)
			p.decls[t.Name.Name] = append(p.decls[t.Name.Name], typeDecl{spec: t, file: f})
		}
	}
}

// packageAt reads syntax only, including build-tagged and generated declarations.
// Conflicting platform declarations and incomplete context are deliberately not
// resolved. Imported packages exclude tests; a test file can see its own tests.
func (w *workspace) packageAt(key packageKey) *packageTypes {
	if p, ok := w.packages[key]; ok {
		return p
	}
	p := &packageTypes{decls: make(map[string][]typeDecl), complete: true}
	w.packages[key] = p
	entries, err := os.ReadDir(key.dir)
	if err != nil {
		p.complete = false
		return p
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || (!key.tests && strings.HasSuffix(name, "_test.go")) {
			continue
		}
		if !entry.Type().IsRegular() {
			// A symlink could hide another declaration. Do not follow it or
			// assume that the remaining declarations are the whole package.
			if entry.Type()&os.ModeSymlink != 0 {
				p.complete = false
			}
			continue
		}
		f := w.read(filepath.Join(key.dir, name))
		if f.ast != nil {
			if (key.name != "" && f.ast.Name.Name != key.name) || (key.name == "" && f.ast.Name.Name == "main") {
				// A main package cannot be imported. Standalone generators in
				// an imported directory therefore supply no type declarations.
				continue
			}
		}
		if f.err != nil {
			p.complete = false
			continue
		}
		if p.name != "" && p.name != f.ast.Name.Name {
			p.complete = false
		}
		p.name = f.ast.Name.Name
		addTypes(p, f)
	}
	return p
}

func (w *workspace) declaration(f *source, id *ast.Ident) (typeDecl, bool) {
	if id.Obj != nil {
		spec, ok := id.Obj.Decl.(*ast.TypeSpec)
		if !ok {
			return typeDecl{}, false // variable, type parameter, or other shadow
		}
		if f.ast.Scope.Lookup(id.Name) != id.Obj {
			return typeDecl{spec: spec, file: f}, true // block-local type
		}
	}
	var p *packageTypes
	name := filepath.Base(f.name)
	if f.dir == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		// Explicit source files ignored by Go's package file convention can
		// still resolve their own declarations without polluting siblings.
		if f.types == nil {
			f.types = &packageTypes{decls: make(map[string][]typeDecl), complete: true}
			addTypes(f.types, f)
		}
		p = f.types
	} else {
		p = w.packageAt(packageKey{
			dir:   f.dir,
			name:  f.ast.Name.Name,
			tests: strings.HasSuffix(f.name, "_test.go"),
		})
	}
	decls := p.decls[id.Name]
	if !p.complete || len(decls) != 1 {
		return typeDecl{}, false
	}
	return decls[0], true
}

func (w *workspace) kind(f *source, expr ast.Expr, seen map[*ast.TypeSpec]bool) CompositeKind {
	switch typ := expr.(type) {
	case *ast.StructType:
		return CompositeKindStruct
	case *ast.MapType:
		return CompositeKindMap
	case *ast.ArrayType:
		if typ.Len == nil {
			return CompositeKindSlice
		}
		return CompositeKindArray
	case *ast.ParenExpr:
		return w.kind(f, typ.X, seen)
	case *ast.IndexExpr:
		return w.kind(f, typ.X, seen)
	case *ast.IndexListExpr:
		return w.kind(f, typ.X, seen)
	case *ast.Ident:
		decl, ok := w.declaration(f, typ)
		if ok {
			return w.declaredKind(decl, seen)
		}
	case *ast.SelectorExpr:
		id, ok := typ.X.(*ast.Ident)
		if !ok || (id.Obj != nil && id.Obj.Kind != ast.Pkg) || !ast.IsExported(typ.Sel.Name) {
			return CompositeKindUnknown
		}
		for _, imp := range f.ast.Imports {
			if imp.Name != nil && imp.Name.Name != id.Name {
				continue
			}
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			dir := w.importDir(f.dir, path)
			if dir == "" {
				continue
			}
			p := w.packageAt(packageKey{dir: dir})
			if !p.complete || (imp.Name == nil && p.name != id.Name) {
				continue
			}
			decls := p.decls[typ.Sel.Name]
			if len(decls) == 1 {
				return w.declaredKind(decls[0], seen)
			}
			return CompositeKindUnknown
		}
	}
	return CompositeKindUnknown
}

func (w *workspace) declaredKind(decl typeDecl, seen map[*ast.TypeSpec]bool) CompositeKind {
	if seen[decl.spec] || len(seen) >= 100 {
		return CompositeKindUnknown
	}
	seen[decl.spec] = true
	defer delete(seen, decl.spec)
	return w.kind(decl.file, decl.spec.Type, seen)
}

type module struct{ root, path string }

func (w *workspace) moduleAt(dir string) module {
	if dir == "" {
		return module{}
	}
	if m, ok := w.modules[dir]; ok {
		return m
	}
	var m module
	src, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err == nil {
		m = module{root: dir, path: modfile.ModulePath(src)}
	} else if os.IsNotExist(err) && filepath.Dir(dir) != dir {
		m = w.moduleAt(filepath.Dir(dir))
	}
	w.modules[dir] = m
	return m
}

// importDir intentionally does not run go list, download modules, inspect build
// constraints, or interpret workspace/replacement graphs. Only source with an
// unambiguous location in this module or GOROOT is considered.
func (w *workspace) importDir(from, path string) string {
	if path == "" || strings.Contains(path, "\\") || strings.HasPrefix(path, "/") {
		return ""
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return ""
		}
	}
	m := w.moduleAt(from)
	if m.path != "" && (path == m.path || strings.HasPrefix(path, m.path+"/")) {
		suffix := strings.TrimPrefix(strings.TrimPrefix(path, m.path), "/")
		dir := filepath.Join(m.root, filepath.FromSlash(suffix))
		if w.moduleAt(dir) != m || hasSymlink(m.root, dir) {
			return ""
		}
		return dir
	}
	// Standard library import paths have no dot in the first component.
	first, _, _ := strings.Cut(path, "/")
	if strings.Contains(first, ".") || path == "C" {
		return ""
	}
	root := filepath.Join(runtime.GOROOT(), "src")
	dir := filepath.Join(root, filepath.FromSlash(path))
	if hasSymlink(root, dir) {
		return ""
	}
	return dir
}

func hasSymlink(root, dir string) bool {
	for dir != root {
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
		dir = parent
	}
	return false
}
