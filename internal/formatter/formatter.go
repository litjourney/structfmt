// Package formatter performs conservative composite literal expansion,
// followed by a separately configured base formatter.
package formatter

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"path/filepath"
)

type Options struct {
	MinElements      int
	IncludeGenerated bool
	Base             BaseFormatter
	Expand           string // empty defaults to struct,map
}

// Formatter caches source declarations for one serial CLI run. Create a new
// instance after changing source declarations; it is not safe for concurrent use.
type Formatter struct {
	opts  Options
	work  *workspace
	kinds map[CompositeKind]bool
}

func New(opts Options) (*Formatter, error) {
	if opts.MinElements <= 0 {
		return nil, fmt.Errorf("--min-elements must be greater than 0 (got %d)", opts.MinElements)
	}
	if opts.Base == nil {
		opts.Base = GoFmtFormatter{}
	}
	expand := opts.Expand
	if expand == "" {
		expand = "struct,map"
	}
	kinds, err := ParseExpand(expand)
	if err != nil {
		return nil, err
	}
	return &Formatter{
		opts:  opts,
		work:  newWorkspace(),
		kinds: kinds,
	}, nil
}

// Format formats a complete in-memory Go file. Only declarations in src and
// readable standard-library imports are available for type identification.
func (f *Formatter) Format(ctx context.Context, name string, src []byte) ([]byte, error) {
	return f.format(ctx, parse(name, src))
}

// FormatFile reads and caches a file and its package declarations. The caller
// receives both versions and decides whether to print, diff, or safely write.
func (f *Formatter) FormatFile(ctx context.Context, name string) (original, final []byte, err error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve path: %w", err)
	}
	src := f.work.read(abs)
	final, err = f.format(ctx, src)
	return src.bytes, final, err
}

func (f *Formatter) format(ctx context.Context, src *source) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src.err != nil {
		return nil, fmt.Errorf("read/parse: %w", src.err)
	}
	if !f.opts.IncludeGenerated && ast.IsGenerated(src.ast) {
		return src.bytes, nil
	}
	expanded, err := expand(src, f.opts.MinElements, f.kinds, f.work)
	if err != nil {
		return nil, err
	}
	final, err := f.opts.Base.Format(ctx, expanded)
	if err != nil {
		return nil, err
	}
	// Validate the final result before any write, including successful external
	// processes that returned malformed output. The original was already parsed.
	if !bytes.Equal(final, src.bytes) {
		if checked := parse(src.name, final); checked.err != nil {
			return nil, fmt.Errorf("invalid formatter output: %w", checked.err)
		}
	}
	return final, ctx.Err()
}
