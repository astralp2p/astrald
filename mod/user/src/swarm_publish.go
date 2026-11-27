package user

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// swarmPublisher emits the SwarmState events (astral-go api/user) computed
// from the view's checked results: after each change it reads the view again
// and compares it with what it last published, as publishSwarm does for the
// member set. It emits:
//   - SwarmRequestFiled once when a request first appears;
//   - SwarmRequestApproved, SwarmRequestDeclined or SwarmRequestExpired each
//     time a request's status becomes that status; only an ExpelRequest is
//     approved; user.RequestFulfilled and user.RequestOffered have no event
//     of their own, SwarmContractAdded and SwarmOfferAdded tell them;
//   - SwarmOfferAdded once when an offer is first valid
//     (swarmset.Snapshot.OfferValid): never for an offer whose issuer is
//     expelled or whose request is declined, expired or fulfilled;
//   - SwarmContractAdded once when a contract first counts, signed by both
//     parties (swarmset.Snapshot.ContractCounts); never for an offer.
//
// Nothing is emitted before record: what is stored at startup is published
// by no event.
type swarmPublisher struct {
	snapshot func() *swarmset.Snapshot
	emit     func(astral.Object)

	// mu guards status and counted; publish and record hold it while they diff.
	mu sync.Mutex
	// status holds the last published status of every request; nil before record.
	status map[astral.ObjectID]user.RequestStatus
	// counted holds the contracts SwarmContractAdded or SwarmOfferAdded was
	// emitted for, or recorded.
	counted map[astral.ObjectID]bool
}

// record takes the current verdicts as published, without events.
func (p *swarmPublisher) record() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status, p.counted = map[astral.ObjectID]user.RequestStatus{}, map[astral.ObjectID]bool{}
	p.diff(p.snapshot())
}

// publish emits one event per verdict that changed since the last publish. It
// emits nothing before record.
func (p *swarmPublisher) publish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status == nil {
		return
	}
	for _, e := range p.diff(p.snapshot()) {
		p.emit(e)
	}
}

// diff returns the events from the published verdicts to those of s, and
// takes those of s as published. The caller holds mu.
func (p *swarmPublisher) diff(s *swarmset.Snapshot) (events []astral.Object) {
	for _, id := range s.RequestIDs() {
		status := s.RequestStatus(id)
		prev, seen := p.status[*id]
		p.status[*id] = status
		if !seen {
			r, _ := s.Request(id)
			events = append(events, &user.SwarmRequestFiled{RequestID: id, Kind: r.Kind})
		}
		if status != prev && (seen || status != user.RequestPending) {
			if e := statusEvent(s, id, status); e != nil {
				events = append(events, e)
			}
		}
	}
	for id, sc := range s.Contracts() {
		if p.counted[id] {
			continue
		}
		switch {
		case swarmset.IsOffer(sc) && s.OfferValid(sc):
			events = append(events, &user.SwarmOfferAdded{OfferID: &id, RequestID: sc.RequestID, Issuer: sc.Issuer, Subject: sc.Subject})
		case s.ContractCounts(sc):
			events = append(events, &user.SwarmContractAdded{ContractID: &id, Issuer: sc.Issuer, Subject: sc.Subject, RequestID: sc.RequestID})
		default:
			continue
		}
		p.counted[id] = true
	}
	return events
}

// statusEvent returns the event of req's status, nil for pending.
func statusEvent(s *swarmset.Snapshot, req *astral.ObjectID, status user.RequestStatus) astral.Object {
	switch status {
	case user.RequestApproved:
		return &user.SwarmRequestApproved{RequestID: req, ApprovalID: s.ApprovalOf(req)}
	case user.RequestDeclined:
		return &user.SwarmRequestDeclined{RequestID: req}
	case user.RequestExpired:
		return &user.SwarmRequestExpired{RequestID: req}
	}
	return nil
}

// untilNextTimeout returns how long, on the view's clock, until the earliest
// open request (pending or offered) expires, past expiryMargin; an hour when
// none is open.
func (p *swarmPublisher) untilNextTimeout() time.Duration {
	s := p.snapshot()
	var next time.Time
	for _, id := range s.RequestIDs() {
		if st := s.RequestStatus(id); st != user.RequestPending && st != user.RequestOffered {
			continue
		}
		r, _ := s.Request(id)
		if t := r.Timeout.Time(); next.IsZero() || t.Before(next) {
			next = t
		}
	}
	if next.IsZero() {
		return time.Hour
	}
	return max(next.Sub(s.Now)+expiryMargin, 0)
}

// follow runs onChange and publishes after every change feed reports, and
// publishes once more after the earliest pending request's Timeout, so an
// expiry is published without a new entry. It returns when the feed ends.
func (p *swarmPublisher) follow(ctx *astral.Context, feed SwarmFeed, onChange func()) {
	changes := feed.Follow(ctx)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	for {
		timer.Reset(p.untilNextTimeout())
		select {
		case _, ok := <-changes:
			if !ok {
				return
			}
			onChange()
		case <-timer.C:
		}
		p.publish()
	}
}
