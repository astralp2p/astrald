package objects

import (
	"fmt"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"slices"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

type SearchArgs struct {
	Query       string `query:"key:q"`
	Repo        string // return only objects that this repo contains
	Zone        *astral.Zone
	Sources     string // comma-separated identities to ask
	SourcesOnly bool   // ask only Sources, not the registered searchers
	Out         string
}

// OpSearch streams matches for the query, deduplicated by ObjectID and
// optionally filtered to objects the named repo contains. Bounded to one minute.
func (mod *Module) OpSearch(ctx *astral.Context, q *routing.IncomingQuery, args SearchArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(args.Repo),
	}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).WithTimeout(time.Minute)
	defer cancel()

	if args.Zone != nil {
		ctx = ctx.LimitZone(*args.Zone)
	}

	// why: a query from the network answers from this node alone. Its sender
	// already fans out, and a second hop sends it back (objects.search spec).
	if q.Origin() == astral.OriginNetwork {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

	// find repo if provided
	var repo objectsmod.Repository
	if len(args.Repo) > 0 {
		repo = mod.GetRepository(args.Repo)
		if repo == nil {
			return ch.Send(astral.NewError("repository not found"))
		}
	}

	// resolve sources if provided
	var sources []*astral.Identity
	if len(args.Sources) > 0 {
		for _, name := range strings.Split(args.Sources, ",") {
			source, err := mod.Dir.ResolveIdentity(name)
			if err != nil {
				return ch.Send(astral.Err(err))
			}
			if !slices.ContainsFunc(sources, source.IsEqual) {
				sources = append(sources, source)
			}
		}
	}

	var searchQuery objects.SearchQuery
	_ = searchQuery.UnmarshalText([]byte(args.Query))

	// run the search
	matches, err := mod.Search(ctx, &objects.Search{
		CallerID:    q.Caller(),
		Query:       searchQuery,
		Sources:     sources,
		SourcesOnly: args.SourcesOnly,
	})
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	var dup = make(map[string]struct{})

	for {
		match, ok, err := sig.RecvOk(ctx, matches)
		if err != nil || !ok {
			break
		}

		// deduplicate results
		if _, found := dup[match.ObjectID.String()]; found {
			continue
		}

		// filter by repository if provided
		if repo != nil {
			contains, err := repo.Contains(ctx, match.ObjectID)
			switch {
			case err != nil:
				continue
			case !contains:
				continue
			}
		}

		dup[match.ObjectID.String()] = struct{}{}

		err = ch.Send(match)
		if err != nil {
			return fmt.Errorf("error writing match: %w", err)
		}
	}

	return ch.Send(&astral.EOS{})
}
