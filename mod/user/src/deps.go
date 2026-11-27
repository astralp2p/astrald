package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/bip137sig"
	cryptoapi "github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/api/shell"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/events"
	kcpmod "github.com/astralp2p/astrald/mod/kcp"
	"github.com/astralp2p/astrald/mod/nearby"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
	shellmod "github.com/astralp2p/astrald/mod/shell"
	sysmod "github.com/astralp2p/astrald/mod/sys"
	tcpmod "github.com/astralp2p/astrald/mod/tcp"
	"github.com/astralp2p/astrald/mod/tree"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

type Deps struct {
	Apphost   apphostmod.Module
	Auth      authmod.Module
	Crypto    crypto.Module
	Dir       dir.Module
	Events    events.Module
	Objects   objectsmod.Module
	Nodes     nodesmod.Module
	Scheduler scheduler.Module
	Shell     shellmod.Module
	Sys       sysmod.Module
	Nearby    nearby.Module
	Tree      tree.Module
	// VFS holds this node's copy of SwarmState, in the node's own VFS.
	VFS vfsmod.Module
}

// keyDeriver is the part of the bip137sig module user.bootstrap reads.
type keyDeriver interface {
	DeriveKey(seed bip137sig.Seed, path string) (cryptoapi.PrivateKey, error)
}

// OptionalDeps holds the bip137sig module. Without it user.bootstrap refuses.
type OptionalDeps struct {
	Bip137sig keyDeriver
}

// listenPorter is the part of the tcp and kcp modules the nearby status composer reads.
type listenPorter interface {
	ListenPort() int
}

// Transports holds the tcp and kcp modules. A node without one sends port 0 for it in
// its nearby status.
type Transports struct {
	TCP listenPorter
	KCP listenPorter
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// why optional: core.Inject fails on a missing module, and only
	// user.bootstrap needs bip137sig.
	_ = core.Inject(mod.node, &mod.OptionalDeps)

	// why: one lookup per transport, since core.Inject stops at the first missing module.
	mod.Transports.TCP, _ = core.Load[listenPorter](mod.node, tcpmod.ModuleName)
	mod.Transports.KCP, _ = core.Load[listenPorter](mod.node, kcpmod.ModuleName)

	// bind the config
	err = tree.BindPath(ctx, &mod.config, mod.Tree.Root(), "/mod/user/config", true)
	if err != nil {
		return err
	}

	// why NoLocal: a failed save must fail the setter, not fall back to memory.
	mod.config.Owned.NoLocal = true

	mod.Nodes.AddLinkAdmitter(mod)
	mod.Auth.OnContractIndexed(mod.onContractIndexed)
	// why: every path into the auth index, the indexer included, passes the one relay rule.
	mod.Auth.AddAdmitter(func(sc *auth.SignedContract) error {
		return mod.AdmitRelayContract(nil, sc)
	})
	// why: expulsion leaves the node's contracts in force, so the authorizers
	// refuse a banned identity at every level of a walk instead.
	mod.Auth.AddVeto(mod.isBanned)

	mod.wireSwarm()
	chain := newChainRules(mod, mod)

	mod.Auth.Add(authmod.Func[*auth.SeeObjectsAction](chain.AuthorizeSeeObjects))
	mod.Auth.Add(authmod.Func[*auth.StoreObjectsAction](mod.AuthorizeStoreObjects))
	mod.Auth.Add(authmod.Func[*auth.AdminObjectsAction](mod.AuthorizeAdminObjects))
	mod.Auth.Add(authmod.Func[*auth.ServeObjectsAction](mod.AuthorizeServeObjects))
	mod.Auth.Add(authmod.Func[*services.ServiceDiscoveryAction](chain.AuthorizeServiceDiscovery))
	mod.Auth.Add(authmod.Func[*user.SeeSwarmAction](mod.AuthorizeSeeSwarm))
	mod.Auth.Add(authmod.Func[*user.AdminSwarmAction](mod.AuthorizeAdminSwarm))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*user.AdminSwarmAction](chain.AuthorizeAdminSwarmAsPrivileged)))
	mod.Auth.Add(authmod.Func[*user.ExpelAction](mod.AuthorizeExpel))
	mod.Auth.Add(authmod.Func[*auth.AdminNetworkAction](mod.AuthorizeAdminNetwork))
	mod.Auth.Add(authmod.Func[*auth.AdminManageAppsAction](mod.AuthorizeAdminManageApps))
	mod.Auth.Add(authmod.Func[*auth.ConfigureNodeStateAction](mod.AuthorizeConfigureNodeState))
	mod.Auth.Add(authmod.Func[*auth.ServeAppsAction](mod.AuthorizeServeApps))
	mod.Auth.Add(authmod.Func[*auth.SeeNodeStateAction](mod.AuthorizeSeeNodeState))
	mod.Auth.Add(authmod.Func[*auth.UseBlueprintsAction](mod.AuthorizeUseBlueprints))
	mod.Auth.Add(authmod.Func[*shell.ShellAction](mod.AuthorizeShell))
	mod.Auth.Add(authmod.Func[*auth.RegisterSignerAction](mod.AuthorizeRegisterSigner))
	mod.Auth.Add(authmod.Func[*messaging.SendAction](mod.AuthorizeSend))
	mod.Auth.Add(authmod.Func[*messaging.ReceiveAction](mod.AuthorizeReceive))
	mod.Auth.Add(authmod.Func[*messaging.ReadMailboxAction](mod.AuthorizeReadMailbox))

	// why: localuser as a name, to match localuser as a filter
	err = mod.Dir.AddResolver(mod)
	if err != nil {
		return
	}

	// add localswarm filter
	mod.Dir.SetFilter("localswarm", func(identity *astral.Identity) bool {
		if identity.IsZero() {
			return false
		}
		for _, swarm := range mod.LocalSwarm() {
			if identity.IsEqual(swarm) {
				return true
			}
		}
		return false
	})

	// add localuser filter
	mod.Dir.SetFilter("localuser", func(identity *astral.Identity) bool {
		if identity.IsZero() {
			return false
		}
		return identity.IsEqual(mod.Identity())
	})

	return
}
