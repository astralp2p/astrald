package resources

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// MoveToDataRoot moves name from the root to the data root. The root copy is removed only after
// the data-root copy reads back equal to it. A root copy that stays in place returns an error
// wrapping ErrRootCopyKept; any other error leaves the root copy untouched.
func (res *FileResources) MoveToDataRoot(name string) error {
	oldPath, err := resourcePath(res.root, name)
	if err != nil {
		return err
	}

	oldInfo, err := os.Stat(oldPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}

	newInfo, err := os.Stat(filepath.Join(res.DataRoot(), name))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = res.copyToDataRoot(name)
	case err != nil:
		return err
	// why: one directory behind both roots, or a link, makes both paths the only copy
	case os.SameFile(oldInfo, newInfo):
		return nil
	default:
		err = res.matchDataRootCopy(name)
	}
	if err != nil {
		return err
	}

	if err = os.Remove(oldPath); err != nil {
		return fmt.Errorf("%w: %w", ErrRootCopyKept, err)
	}

	return nil
}

// copyToDataRoot writes the root copy of name to the data root and reads it back.
func (res *FileResources) copyToDataRoot(name string) error {
	data, err := res.Read(name)
	if err != nil {
		return err
	}

	if err = res.WriteData(name, data); err != nil {
		return err
	}

	copied, err := res.ReadData(name)
	if err != nil {
		return err
	}

	if !bytes.Equal(copied, data) {
		return fmt.Errorf("%s reads back different from %s", filepath.Join(res.DataRoot(), name), filepath.Join(res.root, name))
	}

	return nil
}

// matchDataRootCopy returns nil when the data-root copy of name equals the root copy.
//
// why: a different root copy was never copied, so it is kept and reported, not removed.
func (res *FileResources) matchDataRootCopy(name string) error {
	old, err := res.Read(name)
	if err != nil {
		return err
	}

	cur, err := res.ReadData(name)
	if err != nil {
		return err
	}

	if !bytes.Equal(old, cur) {
		return fmt.Errorf("%w: %s differs from %s", ErrRootCopyKept, filepath.Join(res.root, name), filepath.Join(res.DataRoot(), name))
	}

	return nil
}
