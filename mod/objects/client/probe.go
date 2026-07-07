package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

func (client *Client) Probe(ctx *astral.Context, objectID *astral.ObjectID, repo string) (probe *objects.Probe, err error) {
	// send the query
	ch, err := client.queryCh(ctx, objects.MethodProbe, query.Args{
		"id":   objectID,
		"repo": repo,
	})
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	err = ch.Switch(channel.Expect(&probe), channel.PassErrors, channel.WithContext(ctx))
	return
}

func Probe(ctx *astral.Context, objectID *astral.ObjectID, repo string) (*objects.Probe, error) {
	return Default().Probe(ctx, objectID, repo)
}
