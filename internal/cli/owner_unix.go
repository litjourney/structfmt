//go:build unix

package cli

import (
	"fmt"
	"os"
	"syscall"
)

func preserveOwner(tmp *os.File, original os.FileInfo) error {
	old, ok := original.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("file ownership information unavailable")
	}
	info, err := tmp.Stat()
	if err != nil {
		return err
	}
	current, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("temporary file ownership information unavailable")
	}
	if current.Uid == old.Uid && current.Gid == old.Gid {
		return nil
	}
	return tmp.Chown(int(old.Uid), int(old.Gid))
}
