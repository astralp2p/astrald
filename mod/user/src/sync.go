package user

import (
	"fmt"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
)

// NOTE: Legacy methods below are result of lack of universal solution to
// this set of problems

func (mod *Module) syncAssets(ctx *astral.Context, nodeID *astral.Identity) (err error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return user.ErrNoActiveContract
	}

	nodePath := fmt.Sprintf("/mod/user/assets/%v/next_height", nodeID.String())
	var args any

	heightNode, err := tree.Query(ctx, mod.Tree.Root(), nodePath, true)
	if err != nil {
		return err
	}

	height, _ := tree.Get[*astral.Uint64](ctx, heightNode)
	if height != nil {
		args = opSyncAssetsArgs{Start: *height}
	}

	var q = query.New(ac.Issuer, nodeID, user.OpSyncAssets, args)
	ch, err := query.Route(ctx, mod.node, q)
	if err != nil {
		return err
	}
	defer ch.Close()

	for {
		msg, err := ch.Receive()
		if err != nil {
			mod.log.Error("syncAssets: error reading from channel: %v", err)
			return err
		}

		switch m := msg.(type) {
		case *OpUpdate:
			if m.Removed {
				err = mod.db.RemoveAssetByNonce(m.Nonce, m.ObjectID)
			} else {
				err = mod.db.AddAssetWithNonce(m.ObjectID, m.Nonce)
			}
			if err != nil {
				mod.log.Error("syncAssets: error syncing asset: %v", err)
				return err
			}

		case *astral.Uint64:
			return heightNode.Set(ctx, m)

		default:
			mod.log.Error("syncAssets: protocol error: unknown msg: %v", m.ObjectType())
			return err
		}
	}
}

func (mod *Module) syncAlias(ctx *astral.Context, nodeID *astral.Identity) (err error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return user.ErrNoActiveContract
	}

	var q = query.New(ac.Issuer, nodeID, user.OpInfo, nil)
	ch, err := query.Route(ctx, mod.node, q)
	if err != nil {
		return err
	}
	defer ch.Close()

	obj, err := ch.Receive()
	if err != nil {
		return err
	}

	info, ok := obj.(*user.Info)
	if !ok {
		return fmt.Errorf("protocol error: invalid object type %s", obj.ObjectType())
	}

	if len(info.NodeAlias) == 0 {
		return nil
	}

	if mod.Dir.DisplayName(ac.Issuer) == "" {
		mod.Dir.SetAlias(ac.Issuer, string(info.UserAlias))
	}

	mod.log.Info("syncAlias: updating %v alias %v", nodeID, info.NodeAlias)

	return mod.Dir.SetAlias(nodeID, string(info.NodeAlias))
}

func (mod *Module) syncSiblings(ctx *astral.Context, with *astral.Identity) {
	ac := mod.ActiveContract()
	if ac == nil {
		return
	}

	contracts, err := mod.ActiveNodeContracts(ac.Issuer)
	if err != nil {
		mod.log.Error("syncSiblings: error getting active contracts: %v", err)
		return
	}

	for _, contract := range contracts {
		if contract.Subject.IsEqual(mod.node.Identity()) {
			continue
		}

		if contract.Subject.IsEqual(with) {
			continue
		}

		mod.Objects.Push(ctx, with, contract)
	}

}

// syncExpulsions pushes every ban issued by the active user to the remote node,
// so a peer that was offline during an expel still learns the ban on next link.
func (mod *Module) syncExpulsions(ctx *astral.Context, with *astral.Identity) {
	ac := mod.ActiveContract()
	if ac == nil {
		return
	}

	expulsions, err := mod.db.Expulsions(ac.Issuer)
	if err != nil {
		mod.log.Error("syncExpulsions: error getting expulsions: %v", err)
		return
	}

	for _, signed := range expulsions {
		if signed.Subject.IsEqual(with) {
			continue
		}

		mod.Objects.Push(ctx, with, signed)
	}
}

func (mod *Module) syncApps(ctx *astral.Context, with *astral.Identity) {
	apps, err := mod.Apphost.LocalApps()
	if err != nil {
		mod.log.Error("syncApps: error getting local apps: %v", err)
		return
	}

	for _, app := range apps {
		contracts, err := mod.Auth.SignedContracts().WithIssuer(app.AppID).Find(ctx)
		if err != nil {
			mod.log.Error("syncApps: error getting contracts: %v", err)
			return
		}

		for _, contract := range contracts {
			mod.Objects.Push(ctx, with, contract)
		}
	}
}

func (mod *Module) pushActiveContract(ctx *astral.Context, remoteIdentity *astral.Identity) {
	contract := mod.ActiveContract()
	if contract == nil {
		return
	}

	mod.Objects.Push(ctx, remoteIdentity, contract)
}

// PushToLocalSwarm broadcasts obj to every member of the local swarm except the node itself.
func (mod *Module) PushToLocalSwarm(ctx *astral.Context, obj astral.Object) {
	for _, sib := range mod.LocalSwarm() {
		if sib.IsEqual(mod.node.Identity()) {
			continue
		}
		sib := sib
		mod.Objects.Push(ctx, sib, obj)
	}
}
