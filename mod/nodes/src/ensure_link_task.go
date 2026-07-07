package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
)

var _ scheduler.Task = &EnsureLinkTask{}

type EnsureLinkTask struct {
	mod        *Module
	Target     *astral.Identity
	Strategies []string
	Networks   []string
	Create     bool

	Info *nodes.LinkInfo // set on success
	Err  error
}

func (m *Module) NewEnsureLinkTask(
	target *astral.Identity,
	strategies []string,
	networks []string,
	forceNew bool,
) nodes.EnsureLinkTask {
	return &EnsureLinkTask{
		mod:        m,
		Target:     target,
		Strategies: strategies,
		Networks:   networks,
		Create:     forceNew,
	}
}

func (c *EnsureLinkTask) Run(ctx *astral.Context) (err error) {
	defer func() {
		if err != nil {
			c.Err = err
		}
	}()

	var opts []RetrieveLinkOption
	if c.Create {
		opts = append(opts, WithForceNew())
	}
	if len(c.Strategies) > 0 {
		opts = append(opts, WithStrategies(c.Strategies...))
	}
	if len(c.Networks) > 0 {
		opts = append(opts, WithNetworks(c.Networks...))
	}

	linkFuture := c.mod.linkPool.RetrieveLink(ctx, c.Target, opts...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-linkFuture:
		if result.Err != nil {
			return result.Err
		}

		s := result.Link
		c.Info = &nodes.LinkInfo{
			ID:             s.id,
			LocalIdentity:  s.LocalIdentity(),
			RemoteIdentity: s.RemoteIdentity(),
			LocalEndpoint:  s.LocalEndpoint(),
			RemoteEndpoint: s.RemoteEndpoint(),
			Outbound:       astral.Bool(s.outbound),
			Network:        astral.String8(s.Network()),
		}

		return nil
	}
}

func (c *EnsureLinkTask) Result() (info *nodes.LinkInfo, err error) {
	return c.Info, c.Err
}

func (c *EnsureLinkTask) String() string {
	return "nodes.ensure_link"
}
