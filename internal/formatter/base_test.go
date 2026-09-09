package formatter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("STRUCTFMT_BASE_HELPER"); mode != "" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		switch mode {
		case "echo":
			_, _ = os.Stdout.Write(src)
			fmt.Fprint(os.Stderr, "a harmless diagnostic")
		case "format":
			out, err := format.Source(src)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(5)
			}
			_, _ = os.Stdout.Write(out)
		case "fail":
			fmt.Fprint(os.Stdout, "partial output must not escape")
			fmt.Fprint(os.Stderr, "useful external formatter error")
			os.Exit(7)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBaseFormatters(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"none", "package p\nvar x=1\n"},
		{"gofmt", "package p\n\nvar x = 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			base, err := newBaseFormatter(tc.name,
				func(string) (string, error) { t.Fatal("unexpected PATH lookup"); return "", nil },
				func(context.Context, string, []byte) ([]byte, []byte, error) {
					t.Fatal("unexpected external command")
					return nil, nil, nil
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			got, err := base.Format(context.Background(), []byte("package p\nvar x=1\n"))
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %q, err %v", got, err)
			}
		})
	}
}

func TestGoFumptInjectedRunner(t *testing.T) {
	sentinel := errors.New("exit status 7")
	for _, tc := range []struct {
		name, stderr string
		failure      error
	}{
		{"success", "", nil},
		{"failure with stderr", "formatter cannot process this input\n", sentinel},
		{"failure without stderr", "", sentinel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			base, err := newBaseFormatter("gofumpt", func(name string) (string, error) {
				if name != "gofumpt" {
					t.Fatalf("looked up %q", name)
				}
				return "an executable with spaces", nil
			}, func(_ context.Context, path string, src []byte) ([]byte, []byte, error) {
				calls++
				if path != "an executable with spaces" || string(src) != "source" {
					t.Fatal("incorrect subprocess input")
				}
				return []byte("final stdout"), []byte(tc.stderr), tc.failure
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := base.Format(context.Background(), []byte("source"))
			if calls != 1 {
				t.Fatalf("called runner %d times", calls)
			}
			if tc.failure != nil {
				if out != nil || !errors.Is(err, tc.failure) || !strings.Contains(err.Error(), strings.TrimSpace(tc.stderr)) {
					t.Fatalf("out=%q err=%v", out, err)
				}
			} else if err != nil || string(out) != "final stdout" {
				t.Fatalf("out=%q err=%v", out, err)
			}
		})
	}
}

func TestBaseFormatterErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct{ name, message string }{
		{"gofumpt", "gofumpt not found in PATH; install it with:"},
		{"gofumpt --extra", "invalid --base-formatter"},
		{"echo arbitrary", "invalid --base-formatter"},
		{"", "invalid --base-formatter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := NewBaseFormatter(tc.name)
			if err == nil || base != nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("base=%v error=%v", base, err)
			}
			if tc.name == "gofumpt" && !strings.Contains(err.Error(), "go install mvdan.cc/gofumpt@latest") {
				t.Fatal("missing installation hint")
			}
		})
	}
}

func TestRealCommandRunner(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"echo", "fail", "format"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("STRUCTFMT_BASE_HELPER", mode)
			src := []byte("package p\nvar s = `stdin, including \"quotes\" and spaces`\n")
			stdout, stderr, err := runCommand(context.Background(), executable, src)
			switch mode {
			case "echo":
				if err != nil || !bytes.Equal(stdout, src) || string(stderr) != "a harmless diagnostic" {
					t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
				}
			case "fail":
				if err == nil || !strings.Contains(string(stderr), "useful external formatter error") {
					t.Fatalf("stderr=%q err=%v", stderr, err)
				}
			case "format":
				if err != nil {
					t.Fatal(err)
				}
				again, _, err := runCommand(context.Background(), executable, stdout)
				if err != nil || !bytes.Equal(stdout, again) {
					t.Fatalf("fake integration not idempotent: %v", err)
				}
			}
		})
	}
}

type badFormatter struct{}

func (badFormatter) Format(context.Context, []byte) ([]byte, error) {
	return []byte("not Go source"), nil
}

func TestPipelineRejectsMalformedFormatterOutput(t *testing.T) {
	_, err := mustFormatter(t, badFormatter{}, 3).Format(context.Background(), "input.go", []byte("package p\n"))
	if err == nil || !strings.Contains(err.Error(), "invalid formatter output") {
		t.Fatalf("error=%v", err)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, base := range []BaseFormatter{NoneFormatter{}, GoFmtFormatter{}, &GoFumptFormatter{path: "nonexistent", run: runCommand}} {
		if _, err := base.Format(ctx, []byte("package p\n")); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
	}
}

func TestInstalledGoFumptIntegration(t *testing.T) {
	if _, err := exec.LookPath("gofumpt"); err != nil {
		t.Skip("optional integration: gofumpt is not installed")
	}
	base, err := NewBaseFormatter("gofumpt")
	if err != nil {
		t.Fatal(err)
	}
	f := mustFormatter(t, base, 3)
	src := []byte("package p\ntype Foo struct{ A, B, C any }; var x = Foo{A: 1, B: Foo{A: 1, B: 2, C: 3}, C: 3}\n")
	got, err := f.Format(context.Background(), "integration.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("Foo{\n")) {
		t.Fatalf("struct not expanded:\n%s", got)
	}
	again, err := f.Format(context.Background(), "integration.go", got)
	if err != nil || !bytes.Equal(got, again) {
		t.Fatalf("gofumpt not idempotent: %v\n%s", err, again)
	}
}

func TestGoFmtCRLF(t *testing.T) {
	got, err := (GoFmtFormatter{}).Format(context.Background(), []byte("package p\r\n\r\nvar x = 1\r\n"))
	if err != nil || bytes.ContainsRune(got, '\r') {
		t.Fatalf("gofmt CRLF normalization: %q, %v", got, err)
	}
}
