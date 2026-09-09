package cli

import "testing"

func TestUnifiedDiff(t *testing.T) {
	for _, tc := range []struct{ name, before, after, want string }{
		{"change", "a\nb\nc\n", "a\nB\nc\n", "--- x.go.orig\n+++ x.go\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"},
		{"add newline", "package p", "package p\n", "--- x.go.orig\n+++ x.go\n@@ -1 +1 @@\n-package p\n\\ No newline at end of file\n+package p\n"},
		{"remove newline", "package p\n", "package p", "--- x.go.orig\n+++ x.go\n@@ -1 +1 @@\n-package p\n+package p\n\\ No newline at end of file\n"},
		{"unchanged", "a\n", "a\n", ""},
		{"new file", "", "a\n", "--- x.go.orig\n+++ x.go\n@@ -0,0 +1 @@\n+a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unifiedDiff("x.go", []byte(tc.before), []byte(tc.after))
			if err != nil || got != tc.want {
				t.Fatalf("diff=%q want=%q error=%v", got, tc.want, err)
			}
		})
	}
}
