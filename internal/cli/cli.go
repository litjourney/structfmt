// Package cli implements deterministic serial processing and stable exit codes.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/litjourney/structfmt/internal/formatter"
)

const (
	Success     = 0
	Differences = 1
	Failure     = 2
)

type options struct {
	write, list, diff, check, generated bool
	minElements                         int
	base                                string
	expand                              string
}

// Run leaves process exit and signal handling to main, and all I/O injectable.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("structfmt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts options
	flags.BoolVar(&opts.write, "w", false, "write final formatting back to files")
	flags.BoolVar(&opts.list, "l", false, "list files whose final formatting differs")
	flags.BoolVar(&opts.diff, "d", false, "print unified diffs of final formatting")
	flags.BoolVar(&opts.check, "check", false, "list changed files and exit 1 if formatting differs")
	flags.IntVar(&opts.minElements, "min-elements", 3, "minimum number of literal elements (must be > 0)")
	flags.StringVar(&opts.expand, "expand", "struct,map", "literal kinds to expand: struct, map, or struct,map")
	flags.StringVar(&opts.base, "base-formatter", "gofmt", "base formatter: gofmt, gofumpt, or none")
	flags.BoolVar(&opts.generated, "include-generated", false, "also format generated Go files")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: structfmt [flags] [file.go | directory | ./... ...]\nWith no paths, reads a complete Go source file from stdin.")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "-w conflicts with -d and --check; -l combines with all modes; -d combines with --check.\nExit codes: 0 success, 1 check differences, 2 error. Flags must precede paths.")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Success
		}
		return Failure
	}
	report := func(err error) { fmt.Fprintf(stderr, "structfmt: %v\n", err) }
	if opts.minElements <= 0 {
		report(fmt.Errorf("--min-elements must be greater than 0 (got %d)", opts.minElements))
		return Failure
	}
	if opts.write && (opts.diff || opts.check) {
		report(fmt.Errorf("-w cannot be combined with -d or --check"))
		return Failure
	}
	if opts.write && flags.NArg() == 0 {
		report(fmt.Errorf("-w cannot be used with stdin"))
		return Failure
	}
	if _, err := formatter.ParseExpand(opts.expand); err != nil {
		report(err)
		return Failure
	}
	base, err := formatter.NewBaseFormatter(opts.base)
	if err != nil {
		report(err)
		return Failure
	}
	f, err := formatter.New(formatter.Options{
		MinElements:      opts.minElements,
		IncludeGenerated: opts.generated,
		Base:             base,
		Expand:           opts.expand,
	})
	if err != nil {
		report(err)
		return Failure
	}
	if flags.NArg() == 0 {
		original, err := io.ReadAll(stdin)
		if err != nil {
			report(fmt.Errorf("<standard input>: %w", err))
			return Failure
		}
		final, err := f.Format(ctx, "<standard input>", original)
		if err != nil {
			report(err)
			return Failure
		}
		changed := !bytes.Equal(original, final)
		if err := output(stdout, "<standard input>", original, final, opts); err != nil {
			report(err)
			return Failure
		}
		if opts.check && changed {
			return Differences
		}
		return Success
	}
	files, errs := collect(flags.Args())
	code := Success
	for _, err := range errs {
		report(err)
		code = Failure
	}
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			report(err)
			return Failure
		}
		original, final, err := f.FormatFile(ctx, name)
		if err != nil {
			report(fmt.Errorf("%s: %w", name, err))
			code = Failure
			continue
		}
		changed := !bytes.Equal(original, final)
		if opts.write && changed {
			if err := replaceFile(name, original, final); err != nil {
				report(fmt.Errorf("%s: %w", name, err))
				code = Failure
				continue
			}
		}
		if err := output(stdout, name, original, final, opts); err != nil {
			report(err)
			return Failure
		}
		if opts.check && changed && code != Failure {
			code = Differences
		}
	}
	return code
}

func output(out io.Writer, name string, original, final []byte, opts options) error {
	if !opts.write && !opts.list && !opts.diff && !opts.check {
		_, err := out.Write(final)
		return err
	}
	if bytes.Equal(original, final) {
		return nil
	}
	if opts.list || opts.check {
		if _, err := fmt.Fprintln(out, name); err != nil {
			return fmt.Errorf("write file list: %w", err)
		}
	}
	if opts.diff {
		diff, err := unifiedDiff(name, original, final)
		if err != nil {
			return fmt.Errorf("%s: diff: %w", name, err)
		}
		if _, err := io.WriteString(out, diff); err != nil {
			return fmt.Errorf("write diff: %w", err)
		}
	}
	return nil
}
