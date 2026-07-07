package objects

import (
	"errors"
	"time"

	objectscli "github.com/cryptopunkscc/astral-go/api/objects/client"
	"github.com/cryptopunkscc/astral-go/astral"
	log "github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/sig"
)

type ExternalFinder struct {
	mod     *Module
	id      *astral.Identity
	client  *objectscli.Client
	log     *log.Logger
	timeout time.Duration
}

func NewExternalFinder(mod *Module, id *astral.Identity) *ExternalFinder {
	return &ExternalFinder{
		mod:     mod,
		id:      id,
		client:  objectscli.New(id, astrald.Default()),
		log:     mod.log.AppendTag(log.Tag(id.Fingerprint())),
		timeout: defaultExternalDiscovererTimeout,
	}
}

func (f *ExternalFinder) SourceIdentity() *astral.Identity { return f.id }

// FindObject queries the remote peer and relays the provider identities it
// returns. The stream runs under a per-call timeout and closes when it ends,
// errors, or the timeout fires.
func (f *ExternalFinder) FindObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *astral.Identity, error) {
	providerCtx, cancel := ctx.WithTimeout(f.timeout)

	providers, errPtr := f.client.Find(providerCtx, id)
	if providers == nil {
		cancel()
		if errPtr != nil && *errPtr != nil {
			return nil, *errPtr
		}
		return nil, errors.New("external find returned no stream")
	}

	out := make(chan *astral.Identity)
	go func() {
		defer cancel()
		defer close(out)

		for {
			provider, ok, err := sig.RecvOk(providerCtx, providers)
			if err != nil {
				f.log.Errorv(1, "external finder: %v", err)
				return
			}
			if !ok {
				break
			}

			if err := sig.Send(providerCtx, out, provider); err != nil {
				return
			}
		}

		if errPtr != nil && *errPtr != nil {
			f.log.Errorv(1, "external finder: %v", *errPtr)
		}
	}()

	return out, nil
}
