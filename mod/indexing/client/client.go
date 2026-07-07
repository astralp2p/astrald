package indexing

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

// Client is an RPC client for the indexing module. A nil targetID routes
// calls to the local node's indexing service.
type Client struct {
	astral   *astrald.Client
	targetID *astral.Identity
}

func New(targetID *astral.Identity, a *astrald.Client) *Client {
	if a == nil {
		a = astrald.Default()
	}
	return &Client{astral: a, targetID: targetID}
}

var defaultClient *Client

func Default() *Client {
	if defaultClient == nil {
		defaultClient = New(nil, astrald.Default())
	}
	return defaultClient
}

func (c *Client) queryCh(ctx *astral.Context, method string, args any, cfg ...channel.ConfigFunc) (*channel.Channel, error) {
	return c.astral.WithTarget(c.targetID).QueryChannel(ctx, method, args, cfg...)
}
