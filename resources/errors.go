package resources

import "errors"

var ErrNotFound = errors.New("not found")

// ErrRootCopyKept reports a file that MoveToDataRoot left in the root. The data-root copy is in place.
var ErrRootCopyKept = errors.New("copy kept in the config directory")

// ErrNotLocal reports a resource name that reaches outside its root.
var ErrNotLocal = errors.New("name is not local")
