package user

import (
	"sort"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

type opSwarmRequestsArgs struct {
	All bool
	Out string
}

// OpSwarmRequests authorizes the caller under SeeSwarm (Members may see
// requests), then streams the requests stored in SwarmState, oldest filed
// first; requests filed at the same time sort by ID. Without All it lists only
// the open requests, pending or offered. For each request it sends three
// objects: the request's entry ID (object_id), its status
// (mod.user.request_status), and the stored signed request. It ends with EOS.
func (mod *Module) OpSwarmRequests(ctx *astral.Context, q *routing.IncomingQuery, args opSwarmRequestsArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &user.SeeSwarmAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	ids, err := mod.SwarmEntries(user.EntryRequests)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	type listed struct {
		id      *astral.ObjectID
		status  user.RequestStatus
		req     astral.Object
		filedAt time.Time
	}
	var list []listed
	for _, id := range ids {
		status := mod.RequestStatus(id)
		if !args.All && status != user.RequestPending && status != user.RequestOffered {
			continue
		}
		obj, err := mod.SwarmEntry(id)
		if err != nil {
			// why: an entry listed a moment ago cannot leave a grow-only
			// set, so a failed read is an I/O error; the rest still list.
			continue
		}
		r, ok := swarmset.ParseRequest(obj)
		if !ok {
			continue
		}
		list = append(list, listed{id, status, obj, r.FiledAt.Time()})
	}

	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].filedAt.Equal(list[j].filedAt) {
			return list[i].filedAt.Before(list[j].filedAt)
		}
		return list[i].id.String() < list[j].id.String()
	})

	for _, l := range list {
		for _, obj := range []astral.Object{l.id, &l.status, l.req} {
			if err = ch.Send(obj); err != nil {
				return err
			}
		}
	}

	return ch.Send(&astral.EOS{})
}
