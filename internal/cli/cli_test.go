package cli

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const inputStruct = "package p\n\ntype Foo struct {\n\tA, B, C int\n}\n\nvar x = Foo{A: 1, B: 2, C: 3}\n"
const formattedStruct = "package p\n\ntype Foo struct {\n\tA, B, C int\n}\n\nvar x = Foo{\n\tA: 1,\n\tB: 2,\n\tC: 3,\n}\n"

func TestMain(m *testing.M) {
	if mode := os.Getenv("STRUCTFMT_CLI_HELPER"); mode != "" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		switch mode {
		case "format":
			// Verify that the subprocess receives the expansion, then apply
			// an additional stable change to exercise final-result reporting.
			if bytes.Contains(src, []byte("Foo{A: 1, B: 2, C: 3}")) {
				fmt.Fprint(os.Stderr, "struct was not expanded before the base formatter")
				os.Exit(5)
			}
			out, err := format.Source(src)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(6)
			}
			if !bytes.HasPrefix(out, []byte("// fake base formatter\n")) {
				fmt.Fprint(os.Stdout, "// fake base formatter\n")
			}
			_, _ = os.Stdout.Write(out)
		case "fail":
			fmt.Fprint(os.Stdout, "partial intermediate output")
			fmt.Fprint(os.Stderr, "meaningful gofumpt failure")
			os.Exit(7)
		case "invalid":
			fmt.Fprint(os.Stdout, "this is not valid Go source")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func invoke(args []string, input string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func put(t *testing.T, root, name, src string) string {
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

func contents(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestFileModes(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		flags                     []string
		code                      int
		write, list, diff, source bool
	}{
		{name: "stdout", source: true},
		{name: "write", flags: []string{"-w"}, write: true},
		{name: "list", flags: []string{"-l"}, list: true},
		{name: "diff", flags: []string{"-d"}, diff: true},
		{name: "check", flags: []string{"--check"}, list: true, code: Differences},
		{name: "write list", flags: []string{"-w", "-l"}, write: true, list: true},
		{name: "list diff", flags: []string{"-l", "-d"}, list: true, diff: true},
		{name: "check diff", flags: []string{"--check", "-d"}, list: true, diff: true, code: Differences},
		{name: "list check", flags: []string{"-l", "--check"}, list: true, code: Differences},
		{name: "all reporting", flags: []string{"-l", "-d", "--check"}, list: true, diff: true, code: Differences},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := put(t, t.TempDir(), "input.go", inputStruct)
			args := append(append([]string{}, tc.flags...), path)
			code, out, stderr := invoke(args, "")
			if code != tc.code || stderr != "" {
				t.Fatalf("code=%d, stderr=%s", code, stderr)
			}
			wantFile := inputStruct
			if tc.write {
				wantFile = formattedStruct
			}
			if got := contents(t, path); got != wantFile {
				t.Fatalf("file:\n%s\nwant:\n%s", got, wantFile)
			}
			if tc.source && out != formattedStruct {
				t.Fatalf("stdout:\n%s", out)
			}
			if tc.list && !strings.HasPrefix(out, path+"\n") {
				t.Fatalf("list missing: %s", out)
			}
			if tc.diff && (!strings.Contains(out, "--- "+filepath.ToSlash(path)+".orig\n") || !strings.Contains(out, "@@ ") || !strings.Contains(out, "+\tA: 1,")) {
				t.Fatalf("bad diff:\n%s", out)
			}
			if !tc.list && !tc.diff && !tc.source && out != "" {
				t.Fatalf("unexpected stdout %q", out)
			}
		})
	}
}

func TestReportsUseFinalBaseFormatting(t *testing.T) {
	for _, base := range []string{"none", "gofmt"} {
		for _, flag := range []string{"-l", "-d", "--check"} {
			t.Run(base+flag, func(t *testing.T) {
				path := put(t, t.TempDir(), "base_only.go", "package p\n\nvar x=1\n")
				code, out, stderr := invoke([]string{"--base-formatter=" + base, flag, path}, "")
				wantCode := Success
				if base == "gofmt" && flag == "--check" {
					wantCode = Differences
				}
				if code != wantCode || stderr != "" {
					t.Fatalf("code=%d stderr=%s", code, stderr)
				}
				if base == "none" && out != "" {
					t.Fatalf("none changed unrelated code: %s", out)
				}
				if base == "gofmt" && out == "" {
					t.Fatal("base-only formatting difference was omitted")
				}
				if flag == "-d" && base == "gofmt" && !strings.Contains(out, "+var x = 1\n") {
					t.Fatalf("diff omits final output: %s", out)
				}
				if contents(t, path) != "package p\n\nvar x=1\n" {
					t.Fatal("reporting modified file")
				}
			})
		}
	}
}

func TestStdin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		code     int
		contains string
	}{
		{"default", nil, Success, formattedStruct},
		{"none", []string{"--base-formatter=none"}, Success, "Foo{\n\tA: 1,\n\tB: 2,\n\tC: 3,\n}"},
		{"list", []string{"-l"}, Success, "<standard input>\n"},
		{"diff", []string{"-d"}, Success, "--- <standard input>.orig\n"},
		{"check", []string{"--check"}, Differences, "<standard input>\n"},
		{"write rejected", []string{"-w"}, Failure, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := invoke(tc.args, inputStruct)
			if code != tc.code || !strings.Contains(out, tc.contains) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, stderr)
			}
			if tc.code == Failure && !strings.Contains(stderr, "stdin") {
				t.Fatalf("missing stdin diagnostic: %s", stderr)
			}
		})
	}
	if code, out, err := invoke([]string{"--check"}, formattedStruct); code != Success || out != "" || err != "" {
		t.Fatalf("clean stdin: %d %q %q", code, out, err)
	}
}

func TestArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--min-elements=0"}, {"--min-elements=-1"}, {"--min-elements=nope"}, {"--base-formatter=shell command"}, {"--unknown"},
		{"-w", "-d", "input.go"}, {"-w", "--check", "input.go"}, {"-w"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, stderr := invoke(args, inputStruct)
			if code != Failure || out != "" || stderr == "" {
				t.Fatalf("code=%d out=%q error=%q", code, out, stderr)
			}
		})
	}
	if code, _, stderr := invoke([]string{"--help"}, ""); code != Success || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("help: %d %s", code, stderr)
	}
}

func TestDirectoryScanning(t *testing.T) {
	for _, mode := range []string{"directory", "recursive pattern", "multiple directories", "multiple files", "overlap"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			const src = "package p\nvar x=1\n"
			a := put(t, root, "a.go", src)
			b := put(t, root, "child/b_test.go", src)
			c := put(t, root, "child/deep/c.go", src)
			skipped := []string{".git/ignored.go", "vendor/ignored.go", "node_modules/ignored.go", ".hidden/ignored.go", ".hidden.go", "notes.txt"}
			for _, name := range skipped {
				put(t, root, name, "not Go source")
			}
			generated := put(t, root, "generated.go", "// Code generated by foo. DO NOT EDIT.\n\n"+src)
			var args []string
			switch mode {
			case "directory":
				args = []string{root}
			case "recursive pattern":
				args = []string{filepath.Join(root, "...")}
			case "multiple directories":
				args = []string{filepath.Join(root, "child"), root}
			case "multiple files":
				args = []string{c, b, a}
			case "overlap":
				args = []string{a, root, filepath.Join(root, "child"), b, a}
			}
			code, out, stderr := invoke(append([]string{"-l"}, args...), "")
			want := strings.Join([]string{a, b, c}, "\n") + "\n"
			if code != Success || stderr != "" || out != want {
				t.Fatalf("code=%d out=%q want=%q stderr=%s", code, out, want, stderr)
			}
			if contents(t, a) != src {
				t.Fatal("-l modified a file")
			}
			code, _, stderr = invoke(append([]string{"-w"}, args...), "")
			if code != Success || stderr != "" {
				t.Fatalf("write: code=%d stderr=%s", code, stderr)
			}
			code, out, stderr = invoke(append([]string{"--check"}, args...), "")
			if code != Success || out != "" || stderr != "" {
				t.Fatalf("not idempotent: %d %s %s", code, out, stderr)
			}
			if !strings.HasSuffix(contents(t, generated), src) {
				t.Fatal("generated file changed")
			}
			for _, name := range skipped {
				if contents(t, filepath.Join(root, name)) != "not Go source" {
					t.Fatalf("changed excluded %s", name)
				}
			}
		})
	}
}

func TestGeneratedAndBuildTags(t *testing.T) {
	for _, tc := range []struct {
		name, prefix     string
		include, changed bool
	}{
		{"generated skip", "// Code generated by foo. DO NOT EDIT.\n\n", false, false},
		{"generated include", "// Code generated by foo. DO NOT EDIT.\n\n", true, true},
		{"build tag", "//go:build windows\n\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.prefix + inputStruct
			path := put(t, t.TempDir(), "input.go", original)
			args := []string{"-w"}
			if tc.include {
				args = append(args, "--include-generated")
			}
			code, _, stderr := invoke(append(args, path), "")
			if code != Success || stderr != "" {
				t.Fatalf("%d %s", code, stderr)
			}
			if changed := contents(t, path) != original; changed != tc.changed {
				t.Fatalf("changed=%v, want %v", changed, tc.changed)
			}
		})
	}
}

func TestFileErrorsDoNotWrite(t *testing.T) {
	for _, src := range []string{"package p\nvar x = Foo{A:\n", "package p\n//\xff"} {
		t.Run(fmt.Sprintf("%q", src), func(t *testing.T) {
			root := t.TempDir()
			path := put(t, root, "broken.go", src)
			good := put(t, root, "good.go", "package p\nvar x=1\n")
			code, _, stderr := invoke([]string{"-w", root}, "")
			if code != Failure || !strings.Contains(stderr, "broken.go") {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if contents(t, path) != src {
				t.Fatal("invalid source overwritten")
			}
			if contents(t, good) != "package p\n\nvar x = 1\n" {
				t.Fatal("other file was not processed")
			}
		})
	}
	root := t.TempDir()
	good := put(t, root, "good.go", "package p\nvar x=1\n")
	code, out, stderr := invoke([]string{"--check", good, filepath.Join(root, "missing.go")}, "")
	if code != Failure || !strings.Contains(out, good) || !strings.Contains(stderr, "missing.go") {
		t.Fatalf("errors must outrank differences: %d %s %s", code, out, stderr)
	}
	path := put(t, root, "not-go.txt", "hello")
	if code, _, _ := invoke([]string{path}, ""); code != Failure {
		t.Fatal("accepted a non-Go file")
	}
}

func fakeGoFumpt(t *testing.T) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "gofumpt"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestGoFumptCLI(t *testing.T) {
	fakeGoFumpt(t)
	for _, mode := range []string{"format", "fail", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("STRUCTFMT_CLI_HELPER", mode)
			path := put(t, t.TempDir(), "input.go", inputStruct)
			code, out, stderr := invoke([]string{"--base-formatter=gofumpt", "-w", path}, "")
			if mode == "format" {
				if code != Success || out != "" || stderr != "" {
					t.Fatalf("%d %s %s", code, out, stderr)
				}
				if got := contents(t, path); got != "// fake base formatter\n"+formattedStruct {
					t.Fatalf("final:\n%s", got)
				}
				code, out, stderr = invoke([]string{"--base-formatter=gofumpt", "--check", path}, "")
				if code != Success || out != "" || stderr != "" {
					t.Fatalf("not idempotent: %d %s %s", code, out, stderr)
				}
				code, out, stderr = invoke([]string{"--base-formatter=gofumpt"}, inputStruct)
				if code != Success || out != "// fake base formatter\n"+formattedStruct || stderr != "" {
					t.Fatalf("stdin: %d %s %s", code, out, stderr)
				}
			} else {
				if code != Failure || out != "" {
					t.Fatalf("failure returned %d %s", code, out)
				}
				if contents(t, path) != inputStruct {
					t.Fatal("formatter failure wrote intermediate source")
				}
				message := "meaningful gofumpt failure"
				if mode == "invalid" {
					message = "invalid formatter output"
				}
				if !strings.Contains(stderr, message) || !strings.Contains(stderr, path) {
					t.Fatalf("stderr=%s", stderr)
				}
			}
		})
	}
	t.Run("final reports", func(t *testing.T) {
		t.Setenv("STRUCTFMT_CLI_HELPER", "format")
		for _, flag := range []string{"-l", "-d", "--check"} {
			path := put(t, t.TempDir(), "base_only.go", "package p\n")
			code, out, stderr := invoke([]string{"--base-formatter=gofumpt", flag, path}, "")
			wantCode := Success
			if flag == "--check" {
				wantCode = Differences
			}
			if code != wantCode || out == "" || stderr != "" {
				t.Fatalf("%s: %d %s %s", flag, code, out, stderr)
			}
			if flag == "-d" && !strings.Contains(out, "+// fake base formatter\n") {
				t.Fatalf("diff missed base result: %s", out)
			}
			if contents(t, path) != "package p\n" {
				t.Fatal("report modified file")
			}
		}
	})
}

func TestMissingGoFumpt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := put(t, t.TempDir(), "input.go", inputStruct)
	code, out, stderr := invoke([]string{"--base-formatter=gofumpt", "-w", path}, "")
	if code != Failure || out != "" || !strings.Contains(stderr, "gofumpt not found in PATH") || !strings.Contains(stderr, "go install mvdan.cc/gofumpt@latest") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	if contents(t, path) != inputStruct {
		t.Fatal("missing formatter changed file")
	}
	code, _, stderr = invoke([]string{"-w", path}, "")
	if code != Success || stderr != "" {
		t.Fatalf("gofmt requires no binary: %d %s", code, stderr)
	}
}

func TestWriteMetadataAndNoop(t *testing.T) {
	path := put(t, t.TempDir(), "input.go", inputStruct)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o751); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := invoke([]string{"-w", path}, "")
	if code != Success || stderr != "" {
		t.Fatalf("%d %s", code, stderr)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() {
		t.Fatalf("mode changed from %v to %v", before.Mode(), after.Mode())
	}
	past := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	before, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr = invoke([]string{"-w", path}, "")
	if code != Success || stderr != "" {
		t.Fatalf("%d %s", code, stderr)
	}
	after, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
		t.Fatal("unchanged file was rewritten")
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".structfmt-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v, %v", leftovers, err)
	}
}

func TestWriteDetectsConcurrentChanges(t *testing.T) {
	root := t.TempDir()
	path := put(t, root, "input.go", "newer contents")
	err := replaceFile(path, []byte("old contents"), []byte("formatted contents"))
	if err == nil || !strings.Contains(err.Error(), "file changed") {
		t.Fatalf("error=%v", err)
	}
	if contents(t, path) != "newer contents" {
		t.Fatal("concurrent change overwritten")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file cleanup failed: %v %v", entries, err)
	}
}

func TestSymlinks(t *testing.T) {
	root := t.TempDir()
	external := put(t, t.TempDir(), "outside.go", inputStruct)
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := invoke([]string{"-w", root}, "")
	if code != Success || out != "" || stderr != "" {
		t.Fatalf("scan: %d %s %s", code, out, stderr)
	}
	for _, path := range []string{link, filepath.Join(root, "loop")} {
		code, _, stderr = invoke([]string{"-w", path}, "")
		if code != Failure || !strings.Contains(stderr, "symlinks") {
			t.Fatalf("explicit link: %d %s", code, stderr)
		}
	}
	if contents(t, external) != inputStruct {
		t.Fatal("followed symlink")
	}
}

type brokenIO struct{}

func (brokenIO) Read([]byte) (int, error)  { return 0, fmt.Errorf("input failed") }
func (brokenIO) Write([]byte) (int, error) { return 0, fmt.Errorf("output failed") }

func TestIOErrors(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run(context.Background(), nil, brokenIO{}, io.Discard, &stderr); code != Failure {
		t.Fatalf("read error: %d", code)
	}
	for _, args := range [][]string{nil, {"-l"}, {"-d"}} {
		if code := Run(context.Background(), args, strings.NewReader(inputStruct), brokenIO{}, &stderr); code != Failure {
			t.Fatalf("write error: %d", code)
		}
	}
}
