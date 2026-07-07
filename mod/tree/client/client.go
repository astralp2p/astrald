package tree

import (
	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

type Client struct {
	astral   *astrald.Client
	targetID *astral.Identity
}

var defaultClient *Client

func New(targetID *astral.Identity, astral *astrald.Client) *Client {
	if astral == nil {
		astral = astrald.Default()
	}
	return &Client{astral: astral, targetID: targetID}
}

func Default() *Client {
	if defaultClient == nil {
		defaultClient = New(nil, nil)
	}
	return defaultClient
}

func SetDefault(client *Client) {
	defaultClient = client
}

func Root() tree.Node {
	return Default().Root()
}

func (client *Client) Root() tree.Node {
	return &Node{client: client, path: []string{}}
}

func (client *Client) queryCh(ctx *astral.Context, method string, args any, cfg ...channel.ConfigFunc) (*channel.Channel, error) {
	return client.astral.WithTarget(client.targetID).QueryChannel(ctx, method, args, cfg...)
}
