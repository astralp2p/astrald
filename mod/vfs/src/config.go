package vfs

import (
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

const (
	defaultByteLimit  uint64 = 1 << 30
	defaultInodeLimit uint64 = 1_000_000
)

type Config struct {
	// ByteLimit is the byte limit of a new VFS. 0 means no limit.
	ByteLimit uint64 `yaml:"byte_limit"`

	// InodeLimit is the inode limit of a new VFS. 0 means no limit.
	InodeLimit uint64 `yaml:"inode_limit"`
}

var defaultConfig = Config{
	ByteLimit:  defaultByteLimit,
	InodeLimit: defaultInodeLimit,
}

// removedKeys holds config keys that the module no longer reads.
type removedKeys struct {
	EntryLimit *uint64 `yaml:"entry_limit"`
}

// warnRemovedKeys logs each removed key that the config still sets.
//
// why: the config loader ignores unknown keys, so a node that still sets entry_limit would fall back to the default
// inode limit without a word.
func warnRemovedKeys(assets assets.Assets, log *log.Logger) {
	var keys removedKeys
	if err := assets.LoadYAML(vfsmod.ModuleName, &keys); err != nil {
		return
	}

	if keys.EntryLimit != nil {
		log.Log("config key entry_limit is ignored; use inode_limit")
	}
}
