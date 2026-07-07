package objects

import (
	"errors"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	log "github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/astrald"
	"github.com/cryptopunkscc/astrald/mod/objects"
	objectscli "github.com/cryptopunkscc/astrald/mod/objects/client"
)

type ExternalSearcher struct {
	mod     *Module
	id      *astral.Identity
	client  *objectscli.Client
	log     *log.Logger
	timeout time.Duration
}

func NewExternalSearcher(mod *Module, id *astral.Identity) *ExternalSearcher {
	return &ExternalSearcher{
		mod:     mod,
		id:      id,
		client:  objectscli.New(id, astrald.Default()),
		log:     mod.log.AppendTag(log.Tag(id.Fingerprint())),
		timeout: defaultExternalDiscovererTimeout,
	}
}

func (s *ExternalSearcher) SourceIdentity() *astral.Identity { return s.id }

// SearchObject runs the query against the remote peer and relays its results,
// stamping each with the peer's identity. The stream runs under a per-call
// timeout and closes when it ends, errors, or the timeout fires.
func (s *ExternalSearcher) SearchObject(ctx *astral.Context, q objects.SearchQuery) (<-chan *objects.SearchResult, error) {
	providerCtx, cancel := ctx.WithTimeout(s.timeout)
	in, errPtr := s.client.Search(providerCtx, q)
	if in == nil {
		cancel()
		if errPtr != nil && *errPtr != nil {
			return nil, *errPtr
		}
		return nil, errors.New("external search returned no stream")
	}

	out := make(chan *objects.SearchResult)
	go func() {
		defer cancel()
		defer close(out)

		for {
			result, ok, err := sig.RecvOk(providerCtx, in)
			if err != nil {
				s.log.Errorv(1, "external searcher: %v", err)
				return
			}
			if !ok {
				break
			}
			if result == nil {
				continue
			}

			result.SourceID = s.id
			if err := sig.Send(providerCtx, out, result); err != nil {
				return
			}
		}

		if errPtr != nil && *errPtr != nil {
			s.log.Errorv(1, "external searcher: %v", *errPtr)
		}
	}()

	return out, nil
}
