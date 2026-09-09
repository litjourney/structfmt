package formatter

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"os/exec"
	"strings"
)

// BaseFormatter is the final stage of the per-file formatting pipeline.
type BaseFormatter interface {
	Format(context.Context, []byte) ([]byte, error)
}

type NoneFormatter struct{}

func (NoneFormatter) Format(ctx context.Context, src []byte) ([]byte, error) {
	return src, ctx.Err()
}

type GoFmtFormatter struct{}

func (GoFmtFormatter) Format(ctx context.Context, src []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("gofmt: %w", err)
	}
	return out, nil
}

// commandRunner keeps subprocess tests independent of an installed gofumpt.
type commandRunner func(context.Context, string, []byte) (stdout, stderr []byte, err error)

type GoFumptFormatter struct {
	path string
	run  commandRunner
}

func (f *GoFumptFormatter) Format(ctx context.Context, src []byte) ([]byte, error) {
	stdout, stderr, err := f.run(ctx, f.path, src)
	if err != nil {
		if detail := strings.TrimSpace(string(stderr)); detail != "" {
			return nil, fmt.Errorf("gofumpt: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("gofumpt: %w", err)
	}
	return stdout, nil
}

func runCommand(ctx context.Context, path string, src []byte) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = bytes.NewReader(src)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func NewBaseFormatter(name string) (BaseFormatter, error) {
	return newBaseFormatter(name, exec.LookPath, runCommand)
}

func newBaseFormatter(name string, lookPath func(string) (string, error), run commandRunner) (BaseFormatter, error) {
	switch name {
	case "none":
		return NoneFormatter{}, nil
	case "gofmt":
		return GoFmtFormatter{}, nil
	case "gofumpt":
		path, err := lookPath("gofumpt")
		if err != nil {
			return nil, fmt.Errorf("gofumpt not found in PATH; install it with:\n  go install mvdan.cc/gofumpt@latest\nlookup: %w", err)
		}
		return &GoFumptFormatter{path: path, run: run}, nil
	default:
		return nil, fmt.Errorf("invalid --base-formatter %q: want gofmt, gofumpt, or none", name)
	}
}
