package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (client *Client) Create(ctx *astral.Context, repo string, alloc int) (objects.Writer, error) {
	// prepare arguments
	args := query.Args{}

	if alloc > 0 {
		args["alloc"] = alloc
	}
	if len(repo) > 0 {
		args["repo"] = repo
	}

	// send the query
	ch, err := client.queryCh(ctx, objects.MethodCreate, args)
	if err != nil {
		return nil, err
	}

	// wait for ack
	err = ch.Switch(channel.ExpectAck, channel.PassErrors, channel.WithContext(ctx))
	if err != nil {
		ch.Close()
		return nil, err
	}

	// the channel stays open for the returned writer and is closed by its Commit/Discard
	return &writer{ch: ch}, nil
}

func Create(ctx *astral.Context, repo string, alloc int) (objects.Writer, error) {
	return Default().Create(ctx, repo, alloc)
}
