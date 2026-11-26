package main

import (
	"bytes"
	"encoding/json"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// fetchBatch is how many IDs one user.swarm_fetch asks; the op takes 256.
const fetchBatch = 256

// listEntries reads every entry the node c reaches stores in SwarmState
// (user.swarm_digest, then user.swarm_fetch) and returns each as the node's
// JSON envelope {Type, Object}.
//
// why here and not in Python: the exchange ops speak only the binary
// encoding, which astral-py does not decode for these types.
func listEntries(ctx *astral.Context, c *astrald.Client) (map[string]any, error) {
	ch, err := c.QueryChannel(ctx, "user.swarm_digest", nil)
	if err != nil {
		return nil, err
	}
	var ids []*astral.ObjectID
	err = ch.Switch(channel.Collect(&ids), channel.BreakOnEOS, channel.PassErrors, channel.FailOnEOF())
	ch.Close()
	if err != nil {
		return nil, err
	}

	out := []map[string]any{}
	for i := 0; i < len(ids); i += fetchBatch {
		objs, err := fetchEntries(ctx, c, ids[i:min(i+fetchBatch, len(ids))])
		if err != nil {
			return nil, err
		}
		for _, obj := range objs {
			env, err := envelope(obj)
			if err != nil {
				return nil, err
			}
			id, err := astral.ResolveObjectID(obj)
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{"id": id.String(), "entry": env})
		}
	}
	return map[string]any{"entries": out}, nil
}

func fetchEntries(ctx *astral.Context, c *astrald.Client, ids []*astral.ObjectID) ([]astral.Object, error) {
	ch, err := c.QueryChannel(ctx, "user.swarm_fetch", nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	for _, id := range ids {
		if err = ch.Send(id); err != nil {
			return nil, err
		}
	}
	if err = ch.Send(&astral.EOS{}); err != nil {
		return nil, err
	}
	var objs []astral.Object
	err = ch.Switch(channel.BreakOnEOS, channel.PassErrors,
		func(obj astral.Object) error { objs = append(objs, obj); return nil },
		channel.FailOnEOF())
	return objs, err
}

// envelope is obj in the node's JSON form, as an op with out=json sends it.
func envelope(obj astral.Object) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := channel.New(&buf, channel.WithOutputFormat("json")).Send(obj); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes())), nil
}
