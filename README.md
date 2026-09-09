# structfmt

structfmt is a structural Go source formatter that expands selected composite
literals without enforcing a maximum line length.

It expands keyed **structs and maps** by default, followed by an optional base
formatter. Slices and arrays stay compact.

**structfmt does not enforce a maximum line length and does not wrap arbitrary Go expressions.**

It is not a line-length formatter such as `golines`. It does not introduce line
wrapping for calls, method chains, arguments, signatures, conditions, boolean
expressions, assignments, returns, imports, or strings. The selected base
formatter can still apply its normal Go formatting rules.

## Before

```go
transport := &fallbackTransport{primary: primary, backup: backup, clock: clock, proxy: primary.Proxy, logger: logger, states: diagnostics.NewStates(logger), purpose: purpose}
```

## After

```go
transport := &fallbackTransport{
	primary: primary,
	backup:  backup,
	clock:   clock,
	proxy:   primary.Proxy,
	logger:  logger,
	states:  diagnostics.NewStates(logger),
	purpose: purpose,
}
```

The `fallbackTransport` struct declaration must be available in the file or its
package directory. An isolated snippet with an unknown type is conservatively
skipped. Input must be a complete Go file, including its `package` clause.

These lines stay on one line, regardless of their width:

```go
result := doSomething(argumentOne, argumentTwo, argumentThree, argumentFour, argumentFive, argumentSix)
result := client.WithFoo(foo).WithBar(bar).WithBaz(baz).Execute(ctx)
```

Maps inside calls are expanded without splitting the other arguments:

```go
// Before
f.addZIP("r1", "1.0", map[string]string{"Client.exe": "内部客户端|1.0", "config.ini": "默认配置", "old.dll": "旧文件"})

// After (also with --base-formatter=none)
f.addZIP("r1", "1.0", map[string]string{
	"Client.exe": "内部客户端|1.0",
	"config.ini": "默认配置",
	"old.dll":    "旧文件",
})
```

Named maps such as `type Files map[string]string` are supported when their
declarations can be resolved, just like named structs.

## Installation

Requires **Go 1.23 or later** to build. The binary formats the Go syntax supported
by the toolchain that built it; build with a newer Go version when formatting
newer language syntax. There is no `toolchain` directive or runtime Go command
dependency for the default formatter.

```bash
go install github.com/litjourney/structfmt@latest
```

From a checkout (also works before the first published version):

```bash
go install .
```

Optional `gofumpt` installation:

```bash
go install mvdan.cc/gofumpt@latest
```

Ensure the Go installation's binary directory, usually `$(go env GOPATH)/bin`,
is in `PATH`.

## Usage

Recommended:

```bash
structfmt --base-formatter=gofumpt -w .
```

The default uses the standard library's `go/format`, without starting an
external `gofmt` executable. These commands are equivalent:

```bash
structfmt -w .
structfmt --base-formatter=gofmt -w .
```

Only perform composite literal expansion:

```bash
structfmt --base-formatter=none -w .
```

`none` inserts the required newlines and trailing commas, copies surrounding
indentation with one tab for each new element level, and uses the standard
`text/tabwriter` to align simple key columns. These edits stay inside selected
literals. It does not reindent expression bodies or normalize unrelated source;
use `gofmt` or `gofumpt` for general Go formatting.

Select the literal kinds explicitly:

```bash
structfmt --expand=struct,map -w .  # default
structfmt --expand=struct -w .      # only structs
structfmt --expand=map -w .         # only maps
```

The first version accepts only `struct` and `map`. Empty, duplicate, or unsupported
entries (including `slice` and `array`) are errors.

Other examples:

```bash
structfmt --min-elements=2 -w foo.go
structfmt -w ./...
structfmt -w ./foo ./bar
structfmt -w foo.go bar.go
structfmt -l .
structfmt -d .
structfmt --check .
cat foo.go | structfmt
cat foo.go | structfmt --base-formatter=gofumpt
```

Flags must precede paths. A directory and a trailing `/...` pattern both recurse
through local directories; there is no package loading or Go import-pattern
expansion. With no paths, stdin is used. Stdin does not read declarations from
the working directory; it can resolve declarations in the supplied source and
readable standard-library imports.

| Flag | Behavior |
| --- | --- |
| `-w` | Write final results; do not write unchanged files. |
| `-l` | Print changed filenames, one per line. |
| `-d` | Print standard unified diffs, with three context lines. |
| `--check` | Print changed filenames; exit 1 if any formatting differs. |
| `--min-elements=N` | Expand literals with at least N elements (`len(lit.Elts)`); default 3, must be positive. |
| `--expand=struct,map` | Comma-separated kinds; default `struct,map`; either kind may be selected alone. |
| `--base-formatter=gofmt\|gofumpt\|none` | Select the final formatting stage; default `gofmt`. |
| `--include-generated` | Include generated files, which are skipped by default. |

Mode combinations are explicit:

- `-w` conflicts with `-d` and `--check`, and is forbidden for stdin.
- `-l` combines with all modes. `-w -l` writes and lists successfully changed files.
- `-d --check` prints each changed filename followed by its diff and exits 1.
- `-l --check` lists each changed filename once.
- Without these four mode flags, formatted source goes to stdout. For multiple
  files, output is concatenated in sorted filename order, like `gofmt`.
- `-l`, `-d`, and `--check` do not write files unless `-w` was explicitly supplied
  in an allowed combination. Stdin reporting uses `<standard input>` as its name.

Every comparison uses **original source versus the final base-formatter result**,
including changes caused solely by the base formatter.

| Exit code | Meaning |
| --- | --- |
| 0 | Success, including a clean check. `-l` and `-d` alone return 0 for differences. |
| 1 | `--check` found formatting differences. |
| 2 | Invalid arguments, source/encoding errors, filesystem errors, cancellation, or formatter failures. |

Errors take precedence over check differences. Batch processing reports filenames
and specific errors, continues with other readable files, and returns 2 if any
file failed. A batch is not a transaction: earlier successful writes remain.

## Detection and correctness

The pipeline is:

```text
read and parse source → classify composites → edit selected element layout
                     → base formatter → validate final syntax → optional write
```

All implementation is independent; no GPL formatter code is used.

- Uses `go/parser`, `go/ast`, and `go/scanner`, not regular expressions to classify
  literals. Kind classification is independent of whether elements are keyed.
  Every element must be a `KeyValueExpr`; struct keys must be identifiers, while
  map keys may be arbitrary expressions.
- Follows local and same-package type declarations, aliases, and defined types
  to an underlying struct, map, slice, or array. Block-local shadowing and type parameters
  are respected. Cyclic or duplicate declarations are skipped.
- Recognizes pointers, named types, anonymous structs, selectors, and generic
  `ast.IndexExpr` / `ast.IndexListExpr` types when their declarations resolve.
- Selectors resolve through imports whose source is inside the same module or
  readable under the binary's `GOROOT`. Explicit import aliases and package
  names that differ from directory names are supported.
- Does not interpret `go.work`, dependency versions, `replace` directives,
  vendored dependencies, or the module cache. Imports outside those two source
  locations are skipped, even if their names look like structs. Nested modules,
  dot imports, missing source, and ambiguous package context are also skipped.
- No `go/types` or `go/packages` check, `go list`, module download, or workspace
  compilation runs at formatter runtime. Unrelated type errors do not prevent
  formatting a syntactically valid file.
- Explicit and named maps are classified as maps and honor `--expand=map`.
  Explicit and named slices and arrays are never expanded.
  Empty, positional, mixed, and below-threshold literals are left alone. Elided
  types such as `[]Foo{{A: 1, B: 2, C: 3}}` are conservatively skipped.
- Nested structs and maps are visited independently, even inside maps, slices,
  closures, or an outer literal that is itself skipped.
- Source declarations are cached for a single serial run. Target files and
  sibling declarations are parsed directly, regardless of GOOS, GOARCH, or build
  tags. Test files are formatted normally; their declarations do not affect
  non-test files. Generated declarations can identify types even when their
  files are excluded from output changes.

This deliberately favors avoiding false positives over formatting every possible
literal. For example, `Foo{A: 1, B: 2, C: 3}` with no available `Foo` declaration
is unchanged. Conflicting platform-specific `Foo` declarations are skipped rather
than choosing the current platform's definition. An unreadable or syntactically
broken sibling makes package-level identification unavailable; explicitly
selected broken files still produce an error.

### Comments and element values

Edits affect whitespace at literal/element boundaries, simple key-column padding,
and a missing final comma. They are collected against original byte offsets and
applied in one ordered copy, so nested edits cannot invalidate offsets. Raw
strings, closure bodies, and other element expressions are copied unchanged.

Line comments before elements and after commas keep their existing line boundaries.
Boundary block comments cause the entire literal to be skipped, including both
`A: 1 /* foo */,` and `A: 1, /* foo */ B: 2`. This deliberately conservative rule
also covers the layout produced when `gofmt` moves a block comment across a comma,
so a second run cannot suddenly expand a previously skipped literal. Comments
inside expressions are copied intact. Nested literals can still be formatted
independently.

Composite expansion preserves comment text. A selected base formatter retains its
normal behavior, including Go doc-comment and build-tag formatting.

### Generated files and encoding

Generated detection uses the standard library's
[`ast.IsGenerated`](https://pkg.go.dev/go/ast#IsGenerated), following the
[Go generated-code convention](https://go.dev/s/generatedcode): a matching
`// Code generated ... DO NOT EDIT.` line before the package clause. Similar
phrases in strings, block comments, or after the package clause are not markers.
Skipped generated files bypass the base formatter as well. Malformed source and
invalid UTF-8 are reported before formatting, including in generated files.

`none` preserves existing bytes outside its edits. Inserted newlines use CRLF
when the first existing newline is CRLF, otherwise LF. Existing mixed endings
are not normalized. `gofmt` uses standard `go/format` line endings (normally LF);
`gofumpt` uses the installed formatter's behavior. No custom conversion is applied
to raw strings or comment contents.

## Filesystem behavior

Recursive scanning skips `.git`, `vendor`, `node_modules`, hidden directories,
hidden files, and all symlinks. Explicitly supplied symlink files or directories
are rejected with exit 2; pass the real path. An explicitly supplied hidden or
vendor directory can be used as a scan root. Ordinary `*_test.go` files are
included. Paths are deduplicated by their resolved absolute path and processed
serially in deterministic filename order.

Writing stages the **complete, validated final output** in the same directory,
preserves permission bits (including executable bits), syncs and closes the
temporary file, checks for changed original contents, and renames it over the
destination. On Unix it preserves owner/group or fails without replacing the
file. Temporary files are cleaned up on failures. No temporary files are used
for the formatter subprocess itself.

The replacement uses Go's cross-platform `os.Rename`; rename failures are reported
without deleting the destination first. Atomicity depends on the operating system
and filesystem (particularly on Windows). Non-Unix ownership follows the
directory's defaults. ACLs, extended attributes, and hard-link relationships are
not preserved by replacement. Avoid concurrent editors writing during `-w`; the
contents check reduces lost updates but is not a filesystem lock.

## gofumpt execution

`gofumpt` is located with `exec.LookPath` and invoked directly, once per processed
file, using stdin/stdout. There is no shell, arbitrary command option, or silent
fallback to `gofmt`. A missing executable is an error:

```text
structfmt: gofumpt not found in PATH; install it with:
  go install mvdan.cc/gofumpt@latest
```

Nonzero exits include captured stderr. Parse errors, formatter failures, and
invalid formatter output leave that file's original bytes untouched. `none` and
`gofmt` never launch a formatter subprocess.

## Editor and automation examples

GoLand / JetBrains External Tool or File Watcher:

```text
Program:           structfmt
Arguments:         --base-formatter=gofumpt -w $FilePath$
Working directory: $ProjectFileDir$
```

Pre-commit command:

```bash
structfmt --base-formatter=gofumpt -w .
```

The base formatter is already included; no second `gofumpt -w .` command is needed.

CI:

```bash
go install mvdan.cc/gofumpt@latest
go install github.com/litjourney/structfmt@latest
structfmt --base-formatter=gofumpt --check .
```

Pin the tool versions in CI when reproducible formatting across upgrades matters.

## Development

```bash
go test ./...
go vet ./...
go test -race ./...
go test ./internal/formatter -fuzz=FuzzExpansion -fuzztime=10s
go run . --check .
```

Table-driven tests cover struct/map selection, named maps, map arguments with
byte-exact `none` output, scope/type resolution, comments,
CRLF, generated/build-tagged files, all CLI modes, exit codes, safe writes, and
subprocess behavior. Fake Go helper executables exercise stdin/stdout, stderr,
failures, and idempotency without needing an installed `gofumpt`. An additional
integration test uses the real executable when available.

Two small runtime libraries are used: BSD-licensed
[`go-difflib`](https://github.com/pmezard/go-difflib) for unified diffs, and
[`golang.org/x/mod/modfile`](https://pkg.go.dev/golang.org/x/mod/modfile) for reading
the enclosing module's path. Neither invokes external commands.

Licensed under [MIT](LICENSE). Dependency notices are in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
