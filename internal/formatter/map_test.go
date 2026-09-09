package formatter

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMapExpansion(t *testing.T) {
	for _, tc := range []struct {
		name, decl, input, want, expand string
		min                             int
	}{
		{name: "explicit map", input: `map[string]string{"a": "1", "b": "2", "c": "3"}`, want: "map[string]string{\n\t\"a\": \"1\",\n\t\"b\": \"2\",\n\t\"c\": \"3\",\n}"},
		{name: "numeric keys", input: "map[int]int{1: 1, 20: 2, 300: 3}", want: "map[int]int{\n\t1:   1,\n\t20:  2,\n\t300: 3,\n}"},
		{name: "raw key control bytes", input: "map[string]int{`\f`: 1, `\v`: 2, `c`: 3}", want: "map[string]int{\n\t`\f`: 1,\n\t`\v`: 2,\n\t`c`: 3,\n}"},
		{name: "named map", decl: "type Files map[string]string\n", input: `Files{"a": "1", "b": "2", "c": "3"}`, want: "Files{\n\t\"a\": \"1\",\n\t\"b\": \"2\",\n\t\"c\": \"3\",\n}"},
		{name: "aliased map", decl: "type Files map[string]string\ntype Alias = Files\n", input: `Alias{"a": "1", "b": "2", "c": "3"}`, want: "Alias{\n\t\"a\": \"1\",\n\t\"b\": \"2\",\n\t\"c\": \"3\",\n}"},
		{name: "generic map", decl: "type Files[K comparable, V any] map[K]V\n", input: `Files[string, int]{"a": 1, "b": 2, "c": 3}`, want: "Files[string, int]{\n\t\"a\": 1,\n\t\"b\": 2,\n\t\"c\": 3,\n}"},
		{name: "empty", input: "map[string]int{}"},
		{name: "two elements", input: `map[string]int{"a": 1, "b": 2}`},
		{name: "two at threshold", input: `map[string]int{"a": 1, "b": 2}`, min: 2, want: "map[string]int{\n\t\"a\": 1,\n\t\"b\": 2,\n}"},
		{name: "already multiline", input: "map[string]int{\n\t\"a\": 1,\n\t\"b\": 2,\n\t\"c\": 3,\n}"},
		{name: "line comments", input: "map[string]int{\n\t\"a\": 1, // foo\n\t\"b\": 2, // bar\n\t\"c\": 3,\n}"},
		{name: "leading comment", input: "map[string]int{\n\t// first element\n\t\"a\": 1,\n\t\"b\": 2,\n\t\"c\": 3,\n}"},
		{name: "inline block conservative skip", input: `map[string]string{"a": "1" /* foo */, "b": "2", "c": "3"}`},
		{name: "ambiguous block conservative skip", input: `map[string]string{"a": "1", /* foo */ "b": "2", "c": "3"}`},
		{name: "map off", expand: "struct", input: `map[string]int{"a": 1, "b": 2, "c": 3}`},
		{name: "struct off", expand: "map", decl: "type Foo struct{ A, B, C int }\n", input: "Foo{A: 1, B: 2, C: 3}"},
		{name: "named map off", expand: "struct", decl: "type Files map[string]int\n", input: `Files{"a": 1, "b": 2, "c": 3}`},
		{name: "unknown named map", input: `Files{"a": 1, "b": 2, "c": 3}`},
		{name: "slice excluded", input: `[]string{"a", "b", "c"}`},
		{name: "array excluded", input: `[3]string{"a", "b", "c"}`},
		{name: "named slice excluded", decl: "type Files []string\n", input: `Files{0: "a", 1: "b", 2: "c"}`},
		{name: "named array excluded", decl: "type Files [3]string\n", input: `Files{0: "a", 1: "b", 2: "c"}`},
		{name: "nested maps", input: `map[string]any{"a": 1, "b": map[string]int{"x": 1, "y": 2, "z": 3}, "c": 3}`, want: "map[string]any{\n\t\"a\": 1,\n\t\"b\": map[string]int{\n\t\t\"x\": 1,\n\t\t\"y\": 2,\n\t\t\"z\": 3,\n\t},\n\t\"c\": 3,\n}"},
		{name: "map in struct", decl: "type Config struct { Name string; Files map[string]string; Enabled bool }\n", input: `Config{Name: "test", Files: map[string]string{"a": "1", "b": "2", "c": "3"}, Enabled: true}`, want: "Config{\n\tName:    \"test\",\n\tFiles:   map[string]string{\n\t\t\"a\": \"1\",\n\t\t\"b\": \"2\",\n\t\t\"c\": \"3\",\n\t},\n\tEnabled: true,\n}"},
		{name: "struct in map", decl: "type Foo struct { A, B, C int }\n", input: `map[string]any{"a": 1, "b": Foo{A: 1, B: 2, C: 3}, "c": 3}`, want: "map[string]any{\n\t\"a\": 1,\n\t\"b\": Foo{\n\t\tA: 1,\n\t\tB: 2,\n\t\tC: 3,\n\t},\n\t\"c\": 3,\n}"},
	} {
		for _, base := range []string{"none", "gofmt"} {
			t.Run(tc.name+"/"+base, func(t *testing.T) {
				b, err := NewBaseFormatter(base)
				if err != nil {
					t.Fatal(err)
				}
				n := tc.min
				if n == 0 {
					n = 3
				}
				f := mustFormatter(t, b, n, tc.expand)
				prefix := "package p\n\n" + tc.decl + "\nvar x = "
				input := prefix + tc.input + "\n"
				want := tc.want
				if want == "" {
					want = tc.input
				}
				want = prefix + want + "\n"
				if base == "gofmt" {
					want = canonical(t, want)
				}
				got, err := f.Format(context.Background(), "map.go", []byte(input))
				if err != nil || string(got) != want {
					t.Fatalf("error=%v\ngot:\n%s\nwant:\n%s", err, got, want)
				}
				again, err := f.Format(context.Background(), "map.go", got)
				if err != nil || !bytes.Equal(got, again) {
					t.Fatalf("not idempotent: %v\nfirst:\n%s\nsecond:\n%s", err, got, again)
				}
			})
		}
	}
}

func TestMapArgumentDoesNotWrapCall(t *testing.T) {
	const input = "package p\n\nfunc test() {\n\tf.addZIP(\"r1\", \"1.0\", map[string]string{\"Client.exe\": \"内部客户端|1.0\", \"config.ini\": \"默认配置\", \"old.dll\": \"旧文件\"})\n}\n"
	const want = "package p\n\nfunc test() {\n\tf.addZIP(\"r1\", \"1.0\", map[string]string{\n\t\t\"Client.exe\": \"内部客户端|1.0\",\n\t\t\"config.ini\": \"默认配置\",\n\t\t\"old.dll\":    \"旧文件\",\n\t})\n}\n"
	for _, name := range []string{"none", "gofmt"} {
		t.Run(name, func(t *testing.T) {
			base, err := NewBaseFormatter(name)
			if err != nil {
				t.Fatal(err)
			}
			f := mustFormatter(t, base, 3)
			got, err := f.Format(context.Background(), "call.go", []byte(input))
			if err != nil || string(got) != want {
				t.Fatalf("error=%v\ngot:\n%s\nwant:\n%s", err, got, want)
			}
			again, err := f.Format(context.Background(), "call.go", got)
			if err != nil || !bytes.Equal(got, again) {
				t.Fatalf("not idempotent: %v", err)
			}
		})
	}
}

func TestNonePreservesSurroundingBytes(t *testing.T) {
	const prefix = "package p\n\nfunc  f ( ) {\n  unrelated  :=   longFunction(  first, second, third, fourth, fifth, sixth, seventh )\n  result  :=  call( first,   "
	const suffix = ",  last, anotherVeryLongArgument, anotherArgument ) // preserve this comment\n}\n"
	input := prefix + `map[string]int{"a":1,"long":2,"c":3}` + suffix
	want := prefix + "map[string]int{\n  \t\"a\":    1,\n  \t\"long\": 2,\n  \t\"c\":    3,\n  }" + suffix
	got, err := mustFormatter(t, NoneFormatter{}, 3).Format(context.Background(), "surrounding.go", []byte(input))
	if err != nil || string(got) != want {
		t.Fatalf("unrelated bytes changed: %v\n%s", err, got)
	}
}

func TestExpandValidation(t *testing.T) {
	for _, value := range []string{"", "slice", "array", "struct,slice", "struct,", ",map", "map,map", "unknown"} {
		t.Run(value, func(t *testing.T) {
			if _, err := ParseExpand(value); err == nil {
				t.Fatalf("accepted %q", value)
			}
		})
	}
	for _, value := range []string{"struct", "map", "struct,map", "map,struct", "struct, map"} {
		if kinds, err := ParseExpand(value); err != nil || len(kinds) != strings.Count(value, ",")+1 {
			t.Fatalf("%q: %v", value, err)
		}
	}
}
