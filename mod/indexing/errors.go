package indexing

import (
	"errors"
)

var ErrObjectAlreadyAdded = errors.New("object already added")
var ErrObjectNotPresent = errors.New("object not present")
var ErrRepoAlreadySyncing = errors.New("repo already syncing")
var ErrRepoNotSyncing = errors.New("repo not syncing")
var ErrInvalidIndexHeight = errors.New("index height must advance by exactly 1")
