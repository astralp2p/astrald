package tree

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// refusingRouter answers every outbound query with an error. It stands for a
// target that refuses the mounting node's identity.
//
// why: MountRemote builds its client from astrald.Default(), so the default
// router is the only seam a unit test has on the remote side.
type refusingRouter struct {
	id *astral.Identity
}

func (r *refusingRouter) RouteQuery(*astral.Context, *astral.InFlightQuery) (astral.Conn, error) {
	return nil, errors.New("refused")
}

func (r *refusingRouter) GuestID() *astral.Identity { return r.id }

func (r *refusingRouter) HostID() *astral.Identity { return r.id }

// TestMountRemoteRefusedTargetRecordsNoMount pins the mount's failure mode: a
// target that refuses the mounting node fails the operation, and the mount point
// stays empty.
//
// why: a rootless mount used to record the mount point without querying the
// target, so tree.mount_remote answered ack and the refusal first surfaced on a
// later tree.get or tree.list.
// note: the test replaces the process-wide default client. No other test in this
// package routes an outbound query.
func TestMountRemoteRefusedTargetRecordsNoMount(t *testing.T) {
	astrald.SetDefault(astrald.New(&refusingRouter{id: astral.GenerateIdentity()}))

	mod := &Module{}

	err := mod.MountRemote(astral.NewContext(nil), "/remote/peer", astral.GenerateIdentity(), "")
	if err == nil {
		t.Fatal("a refused target must fail the mount")
	}

	if node := mod.getMount("/remote/peer"); node != nil {
		t.Fatal("a failed mount must record no mount point")
	}
}

// TestMountRemoteRefusedTargetRecordsNoMountWithRoot is the same property with a
// root given, which queried the target before this change and must keep doing so.
func TestMountRemoteRefusedTargetRecordsNoMountWithRoot(t *testing.T) {
	astrald.SetDefault(astrald.New(&refusingRouter{id: astral.GenerateIdentity()}))

	mod := &Module{}

	err := mod.MountRemote(astral.NewContext(nil), "/remote/peer", astral.GenerateIdentity(), "/mod")
	if err == nil {
		t.Fatal("a refused target must fail the mount")
	}

	if node := mod.getMount("/remote/peer"); node != nil {
		t.Fatal("a failed mount must record no mount point")
	}
}
