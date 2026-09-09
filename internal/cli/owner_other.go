//go:build !unix

package cli

import "os"

// Non-Unix platforms use the destination directory's normal ownership rules.
func preserveOwner(_ *os.File, _ os.FileInfo) error { return nil }
