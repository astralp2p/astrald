package vfs

import "github.com/astralp2p/astral-go/astral"

// errEmptyFile answers a write of zero bytes. A VFS holds no empty file.
// todo: move it into astral-go api/vfs as ErrEmptyFile.
var errEmptyFile = astral.NewError("empty file")
