package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

func (client *Client) Repositories(ctx *astral.Context) (repos []*objects.RepositoryInfo, err error) {
	ch, err := client.queryCh(ctx, objects.MethodRepositories, nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	// collect repo names
	err = ch.Switch(channel.Collect(&repos), channel.BreakOnEOS, channel.PassErrors, channel.WithContext(ctx))
	return
}
