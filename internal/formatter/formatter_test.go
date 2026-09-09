package formatter

import (
	"bytes"
	"context"
	"go/format"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustFormatter(t *testing.T, base BaseFormatter, minElements int, kinds ...string) *Formatter {
	t.Helper()
	expand := ""
	if len(kinds) > 0 {
		expand = kinds[0]
	}
	f, err := New(Options{
		MinElements: minElements,
		Base:        base,
		Expand:      expand,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func canonical(t *testing.T, src string) string {
	t.Helper()
	out, err := format.Source([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestExpansion(t *testing.T) {
	const foo = "type Foo struct { A, B, C any }\n"
	tests := []struct {
		name, decl, input, want string
		min                     int
	}{
		{name: "basic", decl: foo, input: "Foo{A: 1, B: 2, C: 3}", want: "Foo{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "pointer", decl: foo, input: "&Foo{A: 1, B: 2, C: 3}", want: "&Foo{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "generic index", decl: "type Foo[T any] struct { A, B, C T }\n", input: "Foo[int]{A: 1, B: 2, C: 3}", want: "Foo[int]{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "generic index list", decl: "type Foo[T, U any] struct { A, B, C T }\n", input: "Foo[int, string]{A: 1, B: 2, C: 3}", want: "Foo[int, string]{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "nested", decl: foo + "type Bar struct { X, Y, Z int }\n", input: "Foo{A: 1, B: Bar{X: 1, Y: 2, Z: 3}, C: 3}", want: "Foo{\nA: 1,\nB: Bar{\nX: 1,\nY: 2,\nZ: 3,\n},\nC: 3,\n}"},
		{name: "map", input: `map[string]int{"a": 1, "b": 2, "c": 3}`},
		{name: "slice", input: "[]int{1, 2, 3}"},
		{name: "array", input: "[3]int{1, 2, 3}"},
		{name: "keyed array", input: "[3]int{0: 1, 1: 2, 2: 3}"},
		{name: "positional", decl: foo, input: "Foo{1, 2, 3}"},
		{name: "mixed", decl: foo, input: "Foo{A: 1, 2, C: 3}"},
		{name: "empty", decl: foo, input: "Foo{}"},
		{name: "one field", decl: foo, input: "Foo{A: 1}"},
		{name: "two fields", decl: foo, input: "Foo{A: 1, B: 2}"},
		{name: "threshold two", decl: foo, input: "Foo{A: 1, B: 2}", min: 2, want: "Foo{\nA: 1,\nB: 2,\n}"},
		{name: "threshold one empty", decl: foo, input: "Foo{}", min: 1},
		{name: "threshold one", decl: foo, input: "Foo{A: 1}", min: 1, want: "Foo{\nA: 1,\n}"},
		{name: "already formatted", decl: foo, input: "Foo{\n\tA: 1,\n\tB: 2,\n\tC: 3,\n}"},
		{name: "grouped multiline", decl: foo, input: "Foo{\n\tA: 1, B: 2, C: 3,\n}", want: "Foo{\n\tA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "trailing comment", decl: foo, input: "Foo{A: 1, B: 2, C: 3} // comment", want: "Foo{\nA: 1,\nB: 2,\nC: 3,\n} // comment"},
		{name: "field comments", decl: foo, input: "Foo{\n\tA: 1, // foo\n\tB: 2, // bar\n\tC: 3,\n}"},
		{name: "leading comment", decl: foo, input: "Foo{\n\t// foo\n\tA: 1,\n\tB: 2,\n\tC: 3,\n}"},
		{name: "block before comma", decl: foo, input: "Foo{A: 1 /* foo */, B: 2, C: 3}"},
		{name: "ambiguous block after comma", decl: foo, input: "Foo{A: 1, /* whose comment? */ B: 2, C: 3}"},
		{name: "ambiguous opening comment", decl: foo, input: "Foo{/* whose comment? */ A: 1, B: 2, C: 3}"},
		{name: "ambiguous closing comment", decl: foo, input: "Foo{A: 1, B: 2, C: 3 /* closing */}"},
		{name: "line comment with grouped fields", decl: foo, input: "Foo{A: 1, // foo\nB: 2, C: 3}", want: "Foo{\nA: 1, // foo\nB: 2,\nC: 3,\n}"},
		{name: "block key comment", decl: foo, input: "Foo{A /* key */: 1, B: /* value */ 2, C: 3}", want: "Foo{\nA /* key */: 1,\nB: /* value */ 2,\nC: 3,\n}"},
		{name: "nested map value", decl: foo, input: `Foo{A: 1, B: map[string]int{"x": 1, "y": 2}, C: 3}`, want: "Foo{\nA: 1,\nB: map[string]int{\"x\": 1, \"y\": 2},\nC: 3,\n}"},
		{name: "function literal", decl: foo, input: "Foo{A: func() {\n\tprintln(1)\n}, B: makeConfig(), C: logger}", want: "Foo{\nA: func() {\n\tprintln(1)\n},\nB: makeConfig(),\nC: logger,\n}"},
		{name: "string delimiters", decl: foo, input: `Foo{A: "}, /* x */", B: 2, C: 3}`, want: "Foo{\nA: \"}, /* x */\",\nB: 2,\nC: 3,\n}"},
		{name: "raw string", decl: foo, input: "Foo{A: 1, B: 2, C: `first,\nlast }`}", want: "Foo{\nA: 1,\nB: 2,\nC: `first,\nlast }`,\n}"},
		{name: "named map", decl: "type Foo map[string]int\n", input: `Foo{"a": 1, "b": 2, "c": 3}`},
		{name: "named map identifier keys", decl: "type Foo map[int]int\nconst ( A = 0; B = 1; C = 2 )\n", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "named slice", decl: "type Foo []int\nconst ( A = 0; B = 1; C = 2 )\n", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "named array", decl: "type Foo [3]int\nconst ( A = 0; B = 1; C = 2 )\n", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "alias map", decl: "type Map map[int]int\ntype Foo = Map\n", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "alias struct", decl: foo + "type Alias = Foo\n", input: "Alias{A: 1, B: 2, C: 3}", want: "Alias{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "defined struct", decl: foo + "type Other Foo\n", input: "Other{A: 1, B: 2, C: 3}", want: "Other{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "unknown named type", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "unknown selector", input: "pkg.Foo{A: 1, B: 2, C: 3}"},
		{name: "unknown generic selector", input: "pkg.Foo[string]{A: 1, B: 2, C: 3}"},
		{name: "cyclic aliases", decl: "type Foo Bar\ntype Bar Foo\n", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "anonymous struct", input: "struct { A, B, C int }{A: 1, B: 2, C: 3}", want: "struct { A, B, C int }{\nA: 1,\nB: 2,\nC: 3,\n}"},
		{name: "inferred struct is skipped", decl: foo, input: "[]Foo{{A: 1, B: 2, C: 3}}"},
		{name: "struct within slice", decl: foo, input: "[]Foo{Foo{A: 1, B: 2, C: 3}}", want: "[]Foo{Foo{\nA: 1,\nB: 2,\nC: 3,\n}}"},
	}
	for _, base := range []string{"none", "gofmt"} {
		t.Run(base, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					minElements := tc.min
					if minElements == 0 {
						minElements = 3
					}
					prefix := "package p\n\n" + tc.decl + "\nvar x = "
					input := prefix + tc.input + "\n"
					wantExpr := tc.want
					if wantExpr == "" {
						wantExpr = tc.input
					}
					want := prefix + wantExpr + "\n"
					b, err := NewBaseFormatter(base)
					if err != nil {
						t.Fatal(err)
					}
					if base == "gofmt" {
						want = canonical(t, want)
					}
					f := mustFormatter(t, b, minElements, "struct")
					got, err := f.Format(context.Background(), "test.go", []byte(input))
					if err != nil {
						t.Fatal(err)
					}
					if base == "gofmt" && string(got) != want {
						t.Fatalf("got:\n%s\nwant:\n%s", got, want)
					}
					if base == "none" && (string(got) != input) != (tc.want != "") {
						t.Fatalf("unexpected expansion (want change=%v):\n%s", tc.want != "", got)
					}
					if base == "none" && !reflect.DeepEqual(semanticTokens([]byte(input)), semanticTokens(got)) {
						t.Fatal("non-comma tokens or comment order changed")
					}
					again, err := f.Format(context.Background(), "test.go", got)
					if err != nil || !bytes.Equal(got, again) {
						t.Fatalf("not idempotent: %v\n%s", err, again)
					}
				})
			}
		})
	}
}

func semanticTokens(src []byte) []string {
	var scan scanner.Scanner
	file := token.NewFileSet().AddFile("", -1, len(src))
	scan.Init(file, src, nil, scanner.ScanComments)
	var out []string
	for {
		_, tok, lit := scan.Scan()
		if tok == token.EOF {
			return out
		}
		if tok != token.COMMA && !(tok == token.SEMICOLON && lit == "\n") {
			out = append(out, tok.String()+":"+lit)
		}
	}
}

func TestUnrelatedExpressionsStayOnTheirLines(t *testing.T) {
	tests := []string{
		"result := doSomething(argumentOne, argumentTwo, argumentThree, argumentFour, argumentFive, argumentSix, argumentSeven, argumentEight)",
		"result := client.WithFoo(foo).WithBar(bar).WithBaz(baz).WithVeryLongOption(argumentOne, argumentTwo, argumentThree, argumentFour).Execute(ctx)",
		"if firstCondition && secondCondition && thirdCondition && fourthCondition && fifthCondition && sixthCondition && seventhCondition {\n\tdoSomething()\n}",
		"result := firstCondition && secondCondition && thirdCondition && fourthCondition && fifthCondition && sixthCondition && seventhCondition",
		"return doSomething(argumentOne, argumentTwo, argumentThree, argumentFour, argumentFive, argumentSix, argumentSeven, argumentEight)",
		"result := argumentOne + argumentTwo + argumentThree + argumentFour + argumentFive + argumentSix + argumentSeven + argumentEight",
		`result := "an extremely long string stays exactly where it is, regardless of whether it crosses one hundred or one hundred forty columns in the original Go file"`,
	}
	for _, base := range []BaseFormatter{NoneFormatter{}, GoFmtFormatter{}} {
		for _, expr := range tests {
			t.Run(expr[:min(len(expr), 30)], func(t *testing.T) {
				input := canonical(t, "package p\nfunc f(argumentOne, argumentTwo, argumentThree, argumentFour, argumentFive, argumentSix, argumentSeven, argumentEight int) {\n"+expr+"\n}\n")
				got, err := mustFormatter(t, base, 3).Format(context.Background(), "long.go", []byte(input))
				if err != nil || string(got) != input {
					t.Fatalf("unrelated expression changed: %v\n%s", err, got)
				}
			})
		}
	}
}

func TestScopes(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		expand       bool
	}{
		{"local struct", "func f() { type Foo struct{ A, B, C int }; _ = Foo{A: 1, B: 2, C: 3} }", true},
		{"map shadows struct", "type Foo struct { A, B, C int }; func f() { type Foo map[int]int; _ = Foo{A: 1, B: 2, C: 3} }", false},
		{"variable shadows struct", "type Foo struct { A, B, C int }; func f() { Foo := 1; _ = Foo{A: 1, B: 2, C: 3} }", false},
		{"type parameter shadows struct", "type Foo struct { A, B, C int }; func f[Foo any]() { _ = Foo{A: 1, B: 2, C: 3} }", false},
		{"block type stays in scope", "func f() { type Foo struct{ A, B, C int }; _ = Foo{} }; var x = Foo{A: 1, B: 2, C: 3}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\n" + tc.source + "\n"
			got, err := mustFormatter(t, NoneFormatter{}, 3, "struct").Format(context.Background(), "scope.go", []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if changed := string(got) != src; changed != tc.expand {
				t.Fatalf("changed=%v, want %v:\n%s", changed, tc.expand, got)
			}
		})
	}
}

func TestSpecialSources(t *testing.T) {
	const body = "package p\n\ntype Foo struct { A, B, C any }\nvar x = Foo{A: 1, B: 2, C: 3}\n"
	for _, tc := range []struct {
		name, src                 string
		include, changed, failure bool
	}{
		{name: "generated", src: "// Code generated by foo. DO NOT EDIT.\n\n" + body},
		{name: "include generated", src: "// Code generated by foo. DO NOT EDIT.\n\n" + body, include: true, changed: true},
		{name: "nonstandard generated marker", src: "// code generated by foo. DO NOT EDIT.\n\n" + body, changed: true},
		{name: "marker after package", src: strings.Replace(body, "package p", "package p\n// Code generated by foo. DO NOT EDIT.", 1), changed: true},
		{name: "marker in string", src: body + "var s = `// Code generated by foo. DO NOT EDIT.`\n", changed: true},
		{name: "block marker", src: "/* Code generated by foo. DO NOT EDIT. */\n" + body, changed: true},
		{name: "build tag", src: "//go:build windows\n\n" + body, changed: true},
		{name: "line directive", src: strings.Replace(body, "var x", "//line somewhere_else.go:900\nvar x", 1), changed: true},
		{name: "CRLF", src: strings.ReplaceAll(body, "\n", "\r\n"), changed: true},
		{name: "CRLF raw string", src: strings.ReplaceAll(strings.Replace(body, "C: 3", "C: `first\nlast`", 1), "\n", "\r\n"), changed: true},
		{name: "CRLF block comment", src: strings.ReplaceAll(strings.Replace(body, "A: 1", "A: /* first\nlast */ 1", 1), "\n", "\r\n"), changed: true},
		{name: "unicode", src: strings.ReplaceAll(body, "Foo", "測試"), changed: true},
		{name: "UTF-8 BOM", src: "\ufeff" + body, changed: true},
		{name: "syntax error", src: "package p\nvar x = Foo{A:\n", failure: true},
		{name: "invalid UTF-8", src: body + "//\xff", failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := New(Options{
				MinElements:      3,
				Base:             NoneFormatter{},
				IncludeGenerated: tc.include,
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.Format(context.Background(), "special.go", []byte(tc.src))
			if (err != nil) != tc.failure {
				t.Fatalf("error=%v, want failure=%v", err, tc.failure)
			}
			if tc.failure {
				return
			}
			if changed := string(got) != tc.src; changed != tc.changed {
				t.Fatalf("changed=%v, want %v\n%s", changed, tc.changed, got)
			}
			if strings.Contains(tc.name, "CRLF") && strings.Contains(strings.ReplaceAll(string(got), "\r\n", ""), "\n") {
				t.Fatal("introduced LF into CRLF input")
			}
			if tc.name == "CRLF raw string" && !bytes.Contains(got, []byte("`first\r\nlast`")) {
				t.Fatal("raw string bytes changed")
			}
			if tc.name == "CRLF block comment" && !bytes.Contains(got, []byte("/* first\r\nlast */")) {
				t.Fatal("comment bytes changed")
			}
			again, err := f.Format(context.Background(), "special.go", got)
			if err != nil || !bytes.Equal(got, again) {
				t.Fatalf("not idempotent: %v", err)
			}
		})
	}
}

func writeSource(t *testing.T, root, name, src string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSourceResolution(t *testing.T) {
	for _, tc := range []struct {
		name, imports, decl, expr string
		expand                    bool
	}{
		{"selector", `import "example.test/m/model"`, "", "pkg.Foo{A: 1, B: 2, C: 3}", true},
		{"pointer selector", `import "example.test/m/model"`, "", "&pkg.Foo{A: 1, B: 2, C: 3}", true},
		{"renamed selector", `import other "example.test/m/model"`, "", "other.Foo{A: 1, B: 2, C: 3}", true},
		{"generic selector", `import "example.test/m/model"`, "", "pkg.Generic[string]{A: 1, B: 2, C: 3}", true},
		{"generic selector index list", `import "example.test/m/model"`, "", "pkg.Pair[string, int]{A: 1, B: 2, C: 3}", true},
		{"imported named map", `import "example.test/m/model"`, "", "pkg.Map{A: 1, B: 2, C: 3}", true},
		{"imported map alias", `import "example.test/m/model"`, "type Alias = pkg.Map", "Alias{A: 1, B: 2, C: 3}", true},
		{"imported struct alias", `import "example.test/m/model"`, "type Alias = pkg.Foo", "Alias{A: 1, B: 2, C: 3}", true},
		{"sibling declaration", "", "", "Local{A: 1, B: 2, C: 3}", true},
		{"sibling alias", "", "type Alias Local", "Alias{A: 1, B: 2, C: 3}", true},
		{"generated declaration", "", "", "Generated{A: 1, B: 2, C: 3}", true},
		{"stdlib selector", `import "net/url"`, "", `url.URL{Scheme: "https", Host: "example.org", Path: "/"}`, true},
		{"external unknown", `import "example.org/unavailable"`, "", "unavailable.Foo{A: 1, B: 2, C: 3}", false},
		{"dot import", `import . "example.test/m/model"`, "", "Foo{A: 1, B: 2, C: 3}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSource(t, root, "go.mod", "module example.test/m\n\ngo 1.23.0\n")
			writeSource(t, root, "model/model.go", "package pkg\ntype Foo struct { A, B, C int }\ntype Generic[T any] struct { A, B, C int }\ntype Pair[T, U any] struct { A, B, C int }\ntype Map map[int]int\n")
			writeSource(t, root, "types.go", "//go:build windows\n\npackage p\ntype Local struct { A, B, C int }\n")
			writeSource(t, root, "generated.go", "// Code generated by tests. DO NOT EDIT.\npackage p\ntype Generated struct { A, B, C int }\n")
			input := "package p\n" + tc.imports + "\n" + tc.decl + "\nvar x = " + tc.expr + "\n"
			path := writeSource(t, root, "input.go", input)
			_, got, err := mustFormatter(t, NoneFormatter{}, 3).FormatFile(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if changed := string(got) != input; changed != tc.expand {
				t.Fatalf("changed=%v, want %v\n%s", changed, tc.expand, got)
			}
		})
	}
}

func TestAmbiguousPackageContext(t *testing.T) {
	for _, tc := range []struct{ name, contextName, context string }{
		{"platform conflict", "map_linux.go", "//go:build linux\n\npackage p\ntype Foo map[int]int\n"},
		{"broken sibling", "broken.go", "package p\ntype (\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			src := "//go:build windows\n\npackage p\ntype Foo struct { A, B, C int }\nvar x = Foo{A: 1, B: 2, C: 3}\n"
			path := writeSource(t, root, "input_windows.go", src)
			writeSource(t, root, tc.contextName, tc.context)
			_, got, err := mustFormatter(t, NoneFormatter{}, 3).FormatFile(context.Background(), path)
			if err != nil || string(got) != src {
				t.Fatalf("ambiguous type expanded: %v\n%s", err, got)
			}
		})
	}
}

func TestMinElementsValidation(t *testing.T) {
	for _, n := range []int{-3, -1, 0} {
		if _, err := New(Options{MinElements: n}); err == nil {
			t.Fatalf("accepted %d", n)
		}
	}
}

func TestExplicitSourceFilenames(t *testing.T) {
	for _, name := range []string{"input.go", "input_test.go", "_input.go", ".input.go"} {
		t.Run(name, func(t *testing.T) {
			src := "package p\ntype Foo struct { A, B, C int }; var x = Foo{A: 1, B: 2, C: 3}\n"
			path := writeSource(t, t.TempDir(), name, src)
			_, got, err := mustFormatter(t, NoneFormatter{}, 3).FormatFile(context.Background(), path)
			if err != nil || !bytes.Contains(got, []byte("Foo{\n")) {
				t.Fatalf("explicit source was not expanded: %v\n%s", err, got)
			}
		})
	}
}

func FuzzExpansion(f *testing.F) {
	for _, src := range []string{
		"package p\nvar x = map[string]any{\"a\": 1, \"b\": map[int]int{1: 1, 2: 2, 3: 3}, \"c\": 3}\n",
		"package p\nfunc f() { call(a, b, map[string]int{`\f`: 1, `\v`: 2, `c`: 3}, d) }\n",
		"package p\ntype T struct { A, B, C int }; var x = map[any]int{T{A: 1, B: 2, C: 3}: 1, \"b\": 2, \"c\": 3}\n",
		"package p\ntype T struct { A, B, C any }; var x = T{A: 1, B: 2, C: 3}\n",
		"package p\ntype T struct { A, B, C any }; var x = T{A: 1 /* hi */, B: T{A: 1, B: 2, C: `a\nb`}, C: 3}\n",
		"package p\ntype T struct { A, B, C any }; var x = T{A: 1, // hi\nB: 2, C: 3}\n",
	} {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, input string) {
		formatter := mustFormatter(t, NoneFormatter{}, 3)
		got, err := formatter.Format(context.Background(), "fuzz.go", []byte(input))
		if err != nil {
			if original := parse("fuzz.go", []byte(input)); original.err == nil {
				t.Fatalf("valid source failed transformation: %v", err)
			}
			return
		}
		if !reflect.DeepEqual(semanticTokens([]byte(input)), semanticTokens(got)) {
			t.Fatal("token or comment order changed")
		}
		again, err := formatter.Format(context.Background(), "fuzz.go", got)
		if err != nil || !bytes.Equal(got, again) {
			t.Fatalf("not idempotent: %v", err)
		}
	})
}
