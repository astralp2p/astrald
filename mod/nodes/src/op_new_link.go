package nodes

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/nodes"
)

type opNewLinkArgs struct {
	Target     string `query:"required"`
	Endpoint   string
	Strategies string
	Out        string
}

// OpNewLink establishes a link to the target, either to a specific endpoint or via the
// given (or all) strategies. The link is built by a scheduled task; the query is accepted
// before it completes so creation may outlast the query timeout. Failures map to reject codes.
func (mod *Module) OpNewLink(ctx *astral.Context, q *routing.IncomingQuery, args opNewLinkArgs) (err error) {
	target, err := mod.Dir.ResolveIdentity(args.Target)
	if err != nil {
		return q.RejectWithCode(2)
	}

	var strategies []string
	if args.Strategies != "" {
		for _, raw := range strings.Split(args.Strategies, ",") {
			strategies = append(strategies, strings.TrimSpace(raw))
		}
	} else {
		for name := range mod.strategyFactories.Clone() {
			strategies = append(strategies, name)
		}
	}

	var task nodes.LinkProducerTask
	switch {
	case args.Endpoint != "":
		split := strings.SplitN(args.Endpoint, ":", 2)
		if len(split) != 2 {
			return q.RejectWithCode(3)
		}

		endpoint, err := mod.Exonet.Parse(split[0], split[1])
		if err != nil {
			return q.RejectWithCode(3)
		}
		task = mod.NewCreateLinkTask(target, endpoint)
	default:
		task = mod.NewEnsureLinkTask(target, strategies, nil, true)
	}

	scheduledTask, err := mod.Scheduler.Schedule(task)
	if err != nil {
		return q.RejectWithCode(5)
	}

	// We need to accept query in case that creating link takes longer than query timeout
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// Wait for task or context cancellation
	select {
	case <-ctx.Done():
		return q.RejectWithCode(4)
	case <-scheduledTask.Done():
	}

	info, err := task.Result()
	switch {
	case err == nil:
		return ch.Send(info)
	case errors.Is(err, nodes.ErrInvalidEndpointFormat):
		return q.RejectWithCode(2)
	case errors.Is(err, nodes.ErrEndpointParse):
		return q.RejectWithCode(3)
	case errors.Is(err, nodes.ErrIdentityResolve), errors.Is(err, nodes.ErrEndpointResolve):
		return q.RejectWithCode(4)
	default:
		return ch.Send(astral.Err(err))
	}
}
