package nodes

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/nodes"
)

type opNewLinkArgs struct {
	Identity   string `query:"required"`
	Endpoint   string
	Strategies string
	Out        string
}

// OpNewLink establishes a link to the named node, either to a specific endpoint or via the
// given (or all) strategies. The link is built by a scheduled task; the query is accepted
// before it completes so creation may outlast the query timeout. A bad identity or endpoint is
// answered with an error_message; a task failure is sent as its error.
func (mod *Module) OpNewLink(ctx *astral.Context, q *routing.IncomingQuery, args opNewLinkArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	target, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		ch := q.Accept(channel.WithOutputFormat(args.Out))
		defer ch.Close()
		return ch.Send(dir.ErrUnknownIdentity)
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
			ch := q.Accept(channel.WithOutputFormat(args.Out))
			defer ch.Close()
			return ch.Send(exonet.ErrInvalidEndpoint)
		}

		endpoint, err := mod.Exonet.Parse(split[0], split[1])
		switch {
		case errors.Is(err, exonet.ErrUnsupportedNetwork):
			ch := q.Accept(channel.WithOutputFormat(args.Out))
			defer ch.Close()
			return ch.Send(exonet.ErrUnsupportedNetwork)
		case err != nil:
			ch := q.Accept(channel.WithOutputFormat(args.Out))
			defer ch.Close()
			return ch.Send(exonet.ErrInvalidEndpoint)
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
		return ctx.Err()
	case <-scheduledTask.Done():
	}

	info, err := task.Result()
	switch {
	case err == nil:
		return ch.Send(info)
	default:
		return ch.Send(astral.Err(err))
	}
}
