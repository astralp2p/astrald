package nodes

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
)

var _ scheduler.Task = &CreateLinkTask{}
var _ nodesmod.CreateLinkTask = &CreateLinkTask{}

type CreateLinkTask struct {
	mod      *Module
	Target   *astral.Identity
	Endpoint exonet.Endpoint
	Info     *nodes.LinkInfo
	Err      error
}

func (m *Module) NewCreateLinkTask(target *astral.Identity, endpoint exonet.Endpoint) nodesmod.CreateLinkTask {
	return &CreateLinkTask{
		mod:      m,
		Target:   target,
		Endpoint: endpoint,
	}
}

func (c *CreateLinkTask) Run(ctx *astral.Context) error {
	conn, err := c.mod.Exonet.Dial(ctx, c.Endpoint)
	if err != nil {
		c.Err = err
		return err
	}

	rawLink, err := c.mod.EstablishOutboundLink(ctx, c.Target, conn)
	if err != nil {
		conn.Close()
		c.Err = err
		return err
	}
	link := rawLink.(*Link)

	c.Info = &nodes.LinkInfo{
		ID:             link.id,
		LocalIdentity:  link.LocalIdentity(),
		RemoteIdentity: link.RemoteIdentity(),
		LocalEndpoint:  link.LocalEndpoint(),
		RemoteEndpoint: link.RemoteEndpoint(),
		Outbound:       astral.Bool(link.outbound),
		Network:        astral.String8(link.Network()),
	}

	return nil
}

func (c *CreateLinkTask) Result() (*nodes.LinkInfo, error) {
	return c.Info, c.Err
}

func (c *CreateLinkTask) String() string {
	return "nodes.create_link"
}
