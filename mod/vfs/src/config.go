package vfs

const (
	defaultByteLimit  uint64 = 1 << 30
	defaultEntryLimit uint64 = 1_000_000
)

type Config struct {
	// ByteLimit is the byte limit of a new VFS. 0 means no limit.
	ByteLimit uint64 `yaml:"byte_limit"`

	// EntryLimit is the entry limit of a new VFS. 0 means no limit.
	EntryLimit uint64 `yaml:"entry_limit"`
}

var defaultConfig = Config{
	ByteLimit:  defaultByteLimit,
	EntryLimit: defaultEntryLimit,
}
