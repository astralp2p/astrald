package nodes

import (
	"iter"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/exonet"
	ipmod "github.com/astralp2p/astrald/mod/ip"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// swarmMembers is the part of the user module that names the swarm's members.
type swarmMembers interface {
	// Ready is closed once the user module applied its first member set.
	Ready() <-chan struct{}
	Identity() *astral.Identity
	LocalSwarm() []*astral.Identity
	// ReadLocalSwarm is LocalSwarm with the error of its read.
	ReadLocalSwarm() ([]*astral.Identity, error)
	IsSwarmMember(id *astral.Identity) bool
}

// siblingRelay is the part of the user module that reaches the swarm's
// members: endpoint notices and relay contracts.
type siblingRelay interface {
	PushToSiblings(ctx *astral.Context, obj astral.Object)
	FetchRelayContractsReport(ctx *astral.Context, issuer *astral.Identity) (seq iter.Seq2[*astral.Identity, *apiauth.SignedContract], complete func() bool)
}

// userModule is the part of the user module nodes reads.
//
// why declared here and not mod/user's Module: nodes depends on no more of
// the user module than it reads. core.Inject matches the "user" module by
// field name and assignability, so the narrow interface injects as well.
type userModule interface {
	swarmMembers
	siblingRelay
}

type Deps struct {
	Auth      auth.Module
	Crypto    crypto.Module
	Dir       dir.Module
	User      userModule
	Exonet    exonet.Module
	Objects   objects.Module
	Scheduler scheduler.Module
	Events    events.Module
}

// LoadDependencies injects deps and registers the "linked" directory filter and
// the relay-for authorizer.
func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// why optional: a node without the ip module (no tcp or kcp) still links,
	// and core.Inject fails on a missing module.
	mod.subnets, _ = core.Load[subnetLister](mod.node, ipmod.ModuleName)
	mod.fingerprints, _ = core.Load[fingerprinter](mod.node, ipmod.ModuleName)

	mod.Dir.SetFilter("linked", func(identity *astral.Identity) bool {
		return mod.IsLinked(identity)
	})

	mod.Auth.Add(auth.Func[*nodes.RelayForAction](mod.AuthorizeRelayFor))

	return err
}
