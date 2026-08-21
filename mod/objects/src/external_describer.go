package objects

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/sig"
)

type ExternalDescriber struct {
	mod     *Module
	id      *astral.Identity
	client  *objectscli.Client
	log     *log.Logger
	timeout time.Duration
}

func NewExternalDescriber(mod *Module, id *astral.Identity) *ExternalDescriber {
	return &ExternalDescriber{
		mod:     mod,
		id:      id,
		client:  objectscli.New(id, astrald.Default()),
		log:     mod.log.AppendTag(log.Tag(id.Fingerprint())),
		timeout: defaultExternalDiscovererTimeout,
	}
}

func (d *ExternalDescriber) SourceIdentity() *astral.Identity { return d.id }

// DescribeObject queries the remote peer and relays its descriptors, stamping
// each with the peer's identity. The stream runs under a per-call timeout and
// closes when it ends, errors, or the timeout fires.
func (d *ExternalDescriber) DescribeObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	ctx, cancel := ctx.WithTimeout(d.timeout)

	in, errPtr := d.client.Describe(ctx, id)
	if in == nil {
		cancel()
		if errPtr != nil && *errPtr != nil {
			return nil, *errPtr
		}
		return nil, errors.New("external describe returned no stream")
	}

	out := make(chan *objects.Descriptor)
	go func() {
		defer cancel()
		defer close(out)

		for {
			descriptor, ok, err := sig.RecvOk(ctx, in)
			if err != nil {
				d.log.Errorv(1, "external describer: %v", err)
				return
			}
			if !ok {
				break
			}
			if descriptor == nil {
				continue
			}

			descriptor.SourceID = d.id
			if err := sig.Send(ctx, out, descriptor); err != nil {
				return
			}
		}

		if errPtr != nil && *errPtr != nil {
			d.log.Errorv(1, "external describer: %v", *errPtr)
		}
	}()

	return out, nil
}
