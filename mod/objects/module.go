package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

const (
	ModuleName   = "objects"
	DBPrefix     = "objects__"
	MethodCreate = "objects.create"

	MethodNew               = "objects.new"
	MethodLoad              = "objects.load"
	MethodStore             = "objects.store"
	MethodDelete            = "objects.delete"
	MethodPurge             = "objects.purge"
	MethodContains          = "objects.contains"
	MethodScan              = "objects.scan"
	MethodSearch            = "objects.search"
	MethodDescribe          = "objects.describe"
	MethodFind              = "objects.find"
	MethodRegisterSearcher  = "objects.register_searcher"
	MethodRegisterDescriber = "objects.register_describer"
	MethodRegisterFinder    = "objects.register_finder"
	MethodRegisterBlueprint = "objects.register_blueprint"
	MethodProbe             = "objects.probe"
	MethodRead              = "objects.read"
	MethodGetType           = "objects.get_type"
	MethodPush              = "objects.push"
	MethodNewMem            = "objects.new_mem"
	MethodRepositories      = "objects.repositories"
	MethodRemoveRepository  = "objects.remove_repository"
	MethodBlueprints        = "objects.blueprints"
	MethodEcho              = "objects.echo"

	RepoMain      = "main"      // everything
	RepoDevice    = "device"    // device: memory, local, removable
	RepoMemory    = "memory"    // memcache repos
	RepoLocal     = "local"     // local storage
	RepoRemovable = "removable" // removable storage
	RepoVirtual   = "virtual"   // virtual repos (archives, encryption, chunks)
	RepoNetwork   = "network"   // network repos
	RepoSystem    = "system"
)

// MaxObjectSize is the maximum size of an object that can be loaded into memory
const MaxObjectSize int64 = 64 << 20 // 64 MB

type Module interface {
	// AddRepository registers a Repository
	AddRepository(name string, repo Repository) error

	// RemoveRepository removes a Repository by its name
	RemoveRepository(name string) error

	// GetRepository returns a Repository by its name
	GetRepository(name string) Repository

	// ReadDefault returns the default repository for reading
	ReadDefault() Repository

	// WriteDefault returns the default repository for writing
	WriteDefault() Repository

	// System returns the system repository
	System() Repository

	// AddGroup adds a repository to a group
	AddGroup(groupName string, repoName string) error

	// RemoveGroup removes a repository from a group
	RemoveGroup(groupName string, repoName string) error

	// Load loads an object from a repository
	Load(*astral.Context, Repository, *astral.ObjectID) (astral.Object, error)

	// Store stores an object in a repository
	Store(*astral.Context, Repository, astral.Object) (*astral.ObjectID, error)

	AddDescriber(Describer) error
	Describe(*astral.Context, *astral.ObjectID) (<-chan *Descriptor, error)

	Search(ctx *astral.Context, query SearchQuery) (<-chan *SearchResult, error)
	AddSearcher(Searcher) error
	AddSearchPreprocessor(SearchPreprocessor) error

	AddFinder(Finder) error
	Find(*astral.Context, *astral.ObjectID) (<-chan *astral.Identity, error)

	AddHolder(Holder) error
	Holders(objectID *astral.ObjectID) []Holder

	AddReceiver(Receiver) error
	Receive(astral.Object, *astral.Identity) error

	Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error

	// Probe probes the object (checks type and latency)
	Probe(ctx *astral.Context, repo Repository, objectID *astral.ObjectID) (probe *Probe, err error)

	// Deprecated: Use Probe instead.
	GetType(ctx *astral.Context, objectID *astral.ObjectID) (objectType string, err error)

	// Register stores a runtime *astral.Blueprint (struct kind or alias kind) and returns
	// its content-addressed ObjectID. Other Object types return ErrBlueprintInvalid.
	Register(astral.Object) (*astral.ObjectID, error)
}

type Receiver interface {
	ReceiveObject(Drop) error
}

// Drop is a received object pending a decision; Accept resolves it, persisting when save is true.
type Drop interface {
	SenderID() *astral.Identity
	Object() astral.Object
	Accept(save bool) error
}

type Describer interface {
	DescribeObject(*astral.Context, *astral.ObjectID) (<-chan *Descriptor, error)
}

type Holder interface {
	HoldObject(*astral.ObjectID) bool
}

// IsOffsetLimitValid reports whether the offset/limit window fits within the object.
// A limit of 0 is treated as a valid zero-length window, not "read to end".
func IsOffsetLimitValid(objectID *astral.ObjectID, offset int64, limit int64) bool {
	// offset has to be non-negative and cannot be larger than the object
	if offset < 0 || offset > int64(objectID.Size) {
		return false
	}

	// limit has to be non-negative and exceed the size of the object
	if limit < 0 || offset+limit > int64(objectID.Size) {
		return false
	}

	return true
}
