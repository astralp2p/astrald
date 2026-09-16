package objects

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/sig"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type ExternalDescriber struct {
	mod     *Module
	id      *astral.Identity
	client  *objectscli.Client
	log     *log.Logger
	timeout time.Duration
	lease   *externalLease
}

func NewExternalDescriber(mod *Module, id *astral.Identity, lease *externalLease) *ExternalDescriber {
	return &ExternalDescriber{
		mod:     mod,
		id:      id,
		client:  objectscli.New(id, astrald.Default()),
		log:     mod.log.AppendTag(log.Tag(id.Fingerprint())),
		timeout: defaultExternalDiscovererTimeout,
		lease:   lease,
	}
}

func (d *ExternalDescriber) SourceIdentity() *astral.Identity { return d.id }

// expired reports whether this registration's lease has run out.
func (d *ExternalDescriber) expired(now time.Time) bool { return d.lease.expired(now) }

// renew extends this registration's lease and reports its new expiry.
func (d *ExternalDescriber) renew(now time.Time, dur time.Duration) time.Time {
	return d.lease.renew(now, dur)
}

// DescribeObject queries the remote peer and relays its descriptors, stamping
// each with the peer's identity. The stream runs under a per-call timeout and
// closes when it ends, errors, or the timeout fires. A peer not authorized as a
// describer is removed and not queried.
func (d *ExternalDescriber) DescribeObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	// why: a registration never outlives the authorization that permitted it, so a
	// provider whose grant was revoked or has expired is removed on its next call.
	// note: an external authority that cannot be reached refuses, so it removes a
	// permitted provider too.
	if !d.mod.Auth.Authorize(ctx, &auth.ServeObjectsAction{
		Action: auth.NewAction(d.id),
		Role:   auth.RoleDescriber,
	}) {
		d.mod.removeExternalDescriber(d)
		return nil, objectsmod.ErrExternalNotAuthorized
	}

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
