package cli

import (
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func collect(args []string) ([]string, []error) {
	paths := make(map[string]string)
	var errs []error
	add := func(name string) {
		abs, err := filepath.Abs(name)
		if err == nil {
			abs, err = filepath.EvalSymlinks(abs)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		name = filepath.Clean(name)
		if old, exists := paths[abs]; !exists || name < old {
			paths[abs] = name
		}
	}
	for _, arg := range args {
		root, recursive := strings.CutSuffix(filepath.ToSlash(arg), "/...")
		path := filepath.FromSlash(root)
		info, err := os.Lstat(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", arg, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Errorf("%s: explicit symlinks are not supported", arg))
			continue
		}
		if !info.IsDir() {
			if recursive || !info.Mode().IsRegular() || filepath.Ext(path) != ".go" {
				errs = append(errs, fmt.Errorf("%s: expected a regular .go file or directory", arg))
			} else {
				add(path)
			}
			continue
		}
		err = filepath.WalkDir(path, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, walkErr))
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() {
				if name != path && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() && !strings.HasPrefix(entry.Name(), ".") && filepath.Ext(name) == ".go" {
				add(name)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", arg, err))
		}
	}
	files := slices.Sorted(maps.Values(paths))
	slices.SortFunc(errs, func(a, b error) int { return cmp.Compare(a.Error(), b.Error()) })
	return files, errs
}

// replaceFile stages a complete result in the same directory. A failed format,
// staging operation, metadata change, or rename leaves the original untouched.
func replaceFile(name string, original, final []byte) (err error) {
	if bytes.Equal(original, final) {
		return nil
	}
	info, err := os.Lstat(name)
	if err != nil {
		return fmt.Errorf("stat before write: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace a non-regular file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), ".structfmt-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err = tmp.Write(final); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err = preserveOwner(tmp, info); err != nil {
		return fmt.Errorf("preserve ownership: %w", err)
	}
	if err = tmp.Chmod(info.Mode()); err != nil {
		return fmt.Errorf("preserve permissions: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	currentInfo, err := os.Lstat(name)
	if err != nil {
		return fmt.Errorf("stat before replacement: %w", err)
	}
	current, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read before replacement: %w", err)
	}
	if !os.SameFile(info, currentInfo) || !bytes.Equal(current, original) {
		return fmt.Errorf("file changed while formatting; refusing to overwrite it")
	}
	if err = os.Rename(tmp.Name(), name); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
