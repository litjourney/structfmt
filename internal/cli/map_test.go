package cli

import (
	"strings"
	"testing"
)

func TestCompositeOptions(t *testing.T) {
	const src = "package p\ntype Foo struct { A, B, C int }\nvar x = Foo{A: 1, B: 2, C: 3}\nvar m = map[string]int{\"a\": 1, \"b\": 2, \"c\": 3}\n"
	for _, tc := range []struct {
		name               string
		flags              []string
		structure, mapping bool
	}{
		{"default both", nil, true, true},
		{"both explicit", []string{"--expand=struct,map"}, true, true},
		{"struct only", []string{"--expand=struct"}, true, false},
		{"map only", []string{"--expand=map"}, false, true},
		{"threshold four", []string{"--min-elements=4"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := invoke(append([]string{"--base-formatter=none"}, tc.flags...), src)
			if code != Success || stderr != "" {
				t.Fatalf("%d %s", code, stderr)
			}
			if strings.Contains(out, "Foo{\n") != tc.structure || strings.Contains(out, "map[string]int{\n") != tc.mapping {
				t.Fatalf("incorrect selection:\n%s", out)
			}
		})
	}
	for _, flag := range []string{"--expand=", "--expand=slice", "--expand=array", "--expand=struct,", "--expand=map,map", "--min-fields=3"} {
		if code, out, stderr := invoke([]string{flag}, src); code != Failure || out != "" || stderr == "" {
			t.Fatalf("accepted invalid flag %s: %d %s %s", flag, code, out, stderr)
		}
	}
}
