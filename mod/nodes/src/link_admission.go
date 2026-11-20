package nodes

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// AddLinkAdmitter registers admitter; every later dial, inbound handshake and
// added link consults it.
func (mod *Module) AddLinkAdmitter(admitter nodesmod.LinkAdmitter) {
	if admitter != nil {
		mod.admitters.Add(admitter)
	}
}

// EnforceLinkPolicy closes every pooled link whose peer a registered admitter
// now refuses, with the refusal as the link's error.
func (mod *Module) EnforceLinkPolicy() {
	for _, link := range mod.linkPool.links.Clone() {
		if err := mod.admit(link.RemoteIdentity()); err != nil {
			_ = link.CloseWithError(err)
		}
	}
}

// admit returns nil when every registered admitter admits a link with remote,
// else the first refusal wrapped in ErrLinkDenied.
func (mod *Module) admit(remote *astral.Identity) error {
	for _, admitter := range mod.admitters.Clone() {
		if err := admitter.AdmitLink(remote); err != nil {
			return fmt.Errorf("%w: %w", nodesmod.ErrLinkDenied, err)
		}
	}
	return nil
}
