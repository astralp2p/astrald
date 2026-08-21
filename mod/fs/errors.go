package fs

import "errors"

var ErrNotAbsolute = errors.New("path not absolute")
var ErrInvalidPath = errors.New("invalid path")
var ErrNotDirectory = errors.New("path is not a directory")
