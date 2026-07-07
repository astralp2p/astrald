package dir

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

// Client is a dir module RPC client; when EnableCache is true, resolved identities
// and aliases are memoized for the lifetime of the client.
type Client struct {
	EnableCache  bool
	targetID     *astral.Identity
	astral       *astrald.Client
	resolveCache sig.Map[string, *astral.Identity]
	aliasCache   sig.Map[string, string]
}

var defaultClient *Client

func New(targetID *astral.Identity, client *astrald.Client) *Client {
	if client == nil {
		client = astrald.Default()
	}

	return &Client{
		astral:   client,
		targetID: targetID,
	}
}

func Default() *Client {
	if defaultClient == nil {
		defaultClient = New(nil, astrald.Default())
	}

	return defaultClient
}

func (client *Client) ClearCache() {
	client.resolveCache = sig.Map[string, *astral.Identity]{}
	client.aliasCache = sig.Map[string, string]{}
}

func (client *Client) queryCh(ctx *astral.Context, method string, args any) (*channel.Channel, error) {
	return client.astral.WithTarget(client.targetID).QueryChannel(ctx, method, args)
}
