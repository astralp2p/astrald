package log

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

const DefaultLogLevel = 2

// DefaultFileMaxSize is the size in bytes past which the current log file
// rolls. why: nothing else bounds the logs directory, and a node that runs for
// months writes into it the whole time.
const DefaultFileMaxSize = 50 << 20

// DefaultFileMaxFiles is how many log files the logs directory keeps.
const DefaultFileMaxFiles = 5

// Config is the log module's configuration. Level lives in the tree at
// /mod/log/config/level, bound in LoadDependencies; the file fields come from
// <root>/config/log.yaml, because the log file is opened in Load, before the
// tree module exists.
type Config struct {
	Level tree.Value[*astral.Uint8] `yaml:"-"`

	// File writes the node's log to <root>/data/logs. A node whose output is
	// already collected — a container, a journal — sets it false and keeps
	// nothing on disk.
	File bool `yaml:"file"`

	// FileMaxSize bounds a single log file in bytes. Zero or less removes the
	// bound.
	FileMaxSize int64 `yaml:"file_max_size"`

	// FileMaxFiles is how many of the most recent files a roll leaves in the
	// logs directory. Zero or less removes the bound.
	FileMaxFiles int `yaml:"file_max_files"`
}

// setDefaults fills the fields log.yaml may override. why: Config holds a
// tree.Value, which carries a mutex, so the module cannot copy its defaults
// from a package-level Config the way the other modules do.
func (cfg *Config) setDefaults() {
	cfg.File = true
	cfg.FileMaxSize = DefaultFileMaxSize
	cfg.FileMaxFiles = DefaultFileMaxFiles
}
