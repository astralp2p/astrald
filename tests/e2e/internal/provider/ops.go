package provider

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// ops is the objects scope a Provider serves: objects.find, objects.describe,
// objects.search and objects.read.
type ops struct{ p *Provider }

type idArgs struct {
	ID  *astral.ObjectID `query:"required"`
	Out string
}

// Find names nothing (Answer), the claim list (Claim), or holds the query
// (Hang).
func (o *ops) Find(_ *astral.Context, q *routing.IncomingQuery, args idArgs) error {
	mode := o.p.hit(Find, args.ID.String())
	if mode == Hang {
		o.p.hang(Find, args.ID.String(), q.AcceptRaw())
		return nil
	}
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()
	if mode == Claim {
		o.p.mu.Lock()
		claims := append([]*astral.Identity(nil), o.p.claims...)
		o.p.mu.Unlock()
		for _, id := range claims {
			if err := ch.Send(id); err != nil {
				return err
			}
		}
	}
	return ch.Send(&astral.EOS{})
}

// Describe sends the descriptors set for the id (Answer) or holds the query
// (Hang).
func (o *ops) Describe(_ *astral.Context, q *routing.IncomingQuery, args idArgs) error {
	if o.p.hit(Describe, args.ID.String()) == Hang {
		o.p.hang(Describe, args.ID.String(), q.AcceptRaw())
		return nil
	}
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()
	o.p.mu.Lock()
	var ds []*objects.Descriptor
	for _, d := range o.p.descriptors {
		if d.ObjectID.IsEqual(args.ID) {
			ds = append(ds, d)
		}
	}
	o.p.mu.Unlock()
	for _, d := range ds {
		if err := ch.Send(d); err != nil {
			return err
		}
	}
	return ch.Send(&astral.EOS{})
}

type searchArgs struct {
	Query string `query:"key:q;required"`
	Out   string
}

// Search sends the results set (Answer) or holds the query (Hang). It counts
// a query by its text without tags.
func (o *ops) Search(_ *astral.Context, q *routing.IncomingQuery, args searchArgs) error {
	var sq objects.SearchQuery
	_ = sq.UnmarshalText([]byte(args.Query))
	if o.p.hit(Search, string(sq.Query)) == Hang {
		o.p.hang(Search, string(sq.Query), q.AcceptRaw())
		return nil
	}
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()
	o.p.mu.Lock()
	rs := append([]*objects.SearchResult(nil), o.p.results...)
	o.p.mu.Unlock()
	for _, r := range rs {
		if err := ch.Send(r); err != nil {
			return err
		}
	}
	return ch.Send(&astral.EOS{})
}

type readArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Offset astral.Uint64
	Limit  astral.Uint64
}

// Read streams the bytes set for the id and rejects any other id (Answer),
// or accepts and writes nothing (Hang).
func (o *ops) Read(_ *astral.Context, q *routing.IncomingQuery, args readArgs) error {
	if o.p.hit(Read, args.ID.String()) == Hang {
		o.p.hang(Read, args.ID.String(), q.AcceptRaw())
		return nil
	}
	o.p.mu.Lock()
	data, ok := o.p.data[args.ID.String()]
	o.p.mu.Unlock()
	end := uint64(len(data))
	if args.Limit > 0 && uint64(args.Offset)+uint64(args.Limit) < end {
		end = uint64(args.Offset) + uint64(args.Limit)
	}
	if !ok || uint64(args.Offset) > end {
		return q.Reject()
	}
	conn := q.AcceptRaw()
	defer conn.Close()
	_, err := conn.Write(data[args.Offset:end])
	return err
}
