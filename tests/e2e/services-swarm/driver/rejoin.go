package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	objectsclient "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	userclient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// rejoinService is the service the rejoin flow's provider advertises.
const rejoinService = "sfml"

// rejoinFirst is the validity of sfml_member's first membership contract.
const rejoinFirst = 60 * time.Second

// rejoinSecond is the validity of the contract the member rejoins with.
const rejoinSecond = time.Hour

// rejoinWait bounds each wait for the follow to show a change.
// note: the oracle holds the changes to 15 s; the driver waits longer, so the
// facts show how late a change came.
const rejoinWait = 60 * time.Second

// rejoinFacts is the follow_member_leaves fact. Times are wall-clock unix
// seconds of the host clock; zero where the step never came.
type rejoinFacts struct {
	Provider     string  `json:"provider"`
	JoinedAt     float64 `json:"joined_at"`
	ExpiresAt    float64 `json:"expires_at"`
	RemovedAt    float64 `json:"removed_at"`
	RemovedCause string  `json:"removed_cause"`
	AcceptSecond string  `json:"accept_second"`
	PushedAt     float64 `json:"pushed_at"`
	BackAt       float64 `json:"back_at"`
	FollowEnded  string  `json:"follow_ended"`
	Err          string  `json:"err"`
}

// rejoinRun is the state of one rejoin flow.
type rejoinRun struct {
	w           *world
	s           session
	memberAdmin *astrald.Client
	key         *crypto.PrivateKey
	watch       *servicescli.Watcher
	offering    string
	f           *rejoinFacts
}

// rejoin opens a swarm follow on node1, admits sfml_member with a contract
// that expires in rejoinFirst, and once the follow dropped the member, makes
// it a member again with a second contract; prints the facts as JSON.
func rejoin(path string) error {
	f := &rejoinFacts{}
	err := rejoinSteps(path, f)
	if err != nil {
		f.Err = err.Error()
	}
	out, _ := json.Marshal(map[string]any{"follow_member_leaves": f})
	fmt.Println(string(out))
	return err
}

func rejoinSteps(path string, f *rejoinFacts) error {
	w, err := load(path)
	if err != nil {
		return err
	}
	s, err := loadSession(path)
	if err != nil {
		return err
	}
	key, err := nodeKeyFromEnv("MEMBER_NODE_KEY")
	if err != nil {
		return err
	}
	member := s.Nodes["sfml_member"]
	r := &rejoinRun{w: w, s: s, memberAdmin: connect(member.Endpoint, member.Token), key: key, f: f}
	stop, err := r.follow()
	if err != nil {
		return err
	}
	defer stop()
	for _, step := range []func() error{r.admit, r.leave, r.rejoin} {
		if err := step(); err != nil {
			return err
		}
	}
	select {
	case <-r.watch.Done():
		f.FollowEnded = fmt.Sprint(r.watch.Err())
	default:
	}
	return nil
}

// follow serves the provider on sfml_member and opens the follow of a fresh
// app on node1, without reconnect: the flow judges one follow throughout.
func (r *rejoinRun) follow() (stop func(), err error) {
	app, fc, err := r.w.app(r.w.admin1, r.w.n1)
	if err != nil {
		return nil, err
	}
	if err := r.w.grant(app, []string{rejoinService}); err != nil {
		return nil, err
	}
	providerID, cl, err := r.w.app(r.memberAdmin, r.s.Nodes["sfml_member"], serveApps)
	if err != nil {
		return nil, err
	}
	b, err := cl.AdvertiseOpen(r.w.ctx, []string{rejoinService}, true, (&evaluated{}).offer)
	if err != nil {
		return nil, err
	}
	r.watch, err = fc.WatchIn(r.w.ctx, services.ReachSwarm, []string{rejoinService})
	if err != nil {
		b.Close()
		return nil, err
	}
	r.f.Provider = providerID.String()
	r.offering = providerID.String() + "/" + rejoinService
	return func() { r.watch.Close(); b.Close() }, holds(r.watch)
}

// admit makes sfml_member a member with a contract valid for rejoinFirst and
// waits until the follow shows its offering.
func (r *rejoinRun) admit() error {
	signed, err := signNodeContract(r.w, r.s, r.s.Nodes["sfml_member"].Identity, r.key, rejoinFirst)
	if err != nil {
		return fmt.Errorf("first contract: %w", err)
	}
	if err := userclient.New(nil, r.memberAdmin).AcceptContract(r.w.ctx, signed); err != nil {
		return fmt.Errorf("accept the first contract on sfml_member: %w", err)
	}
	// why a push from the member: node1 indexes the contract, as for a member
	// that joined through adopt (the membership mode's expiry step).
	if err := objectsclient.New(r.w.node1, r.memberAdmin).Push(r.w.ctx, signed); err != nil {
		return fmt.Errorf("push the first contract to node1: %w", err)
	}
	r.f.ExpiresAt = unix(signed.ExpiresAt.Time())
	if err := holdsWithin(r.watch, time.Until(signed.ExpiresAt.Time()), r.offering); err != nil {
		return fmt.Errorf("the follow never showed the admitted member: %w", err)
	}
	r.f.JoinedAt = unix(time.Now())
	return nil
}

// leave waits until the follow drops the member's offering after its
// contract expired, and records the removal's cause.
func (r *rejoinRun) leave() error {
	expires := time.Unix(0, int64(r.f.ExpiresAt*1e9))
	if err := holdsWithin(r.watch, time.Until(expires)+rejoinWait); err != nil {
		return fmt.Errorf("the follow kept the member past its expiry: %w", err)
	}
	r.f.RemovedAt = unix(time.Now())
	for _, rm := range r.watch.Removals() {
		if rm.Offering != nil && rm.Offering.ProviderID.String() == r.f.Provider {
			r.f.RemovedCause = rm.Cause.String()
		}
	}
	return nil
}

// rejoin signs a second contract, valid for rejoinSecond, pushes it to node1,
// and waits until the open follow shows the member's offering again.
// note: user.accept_contract is tried and its answer recorded: a running node
// keeps an expired active contract until it restarts (astral-docs
// topics/user-and-swarm.md, Losing the owner), and the op refuses a node
// that holds one (mod/user/src/op_accept_contract.go).
func (r *rejoinRun) rejoin() error {
	signed, err := signNodeContract(r.w, r.s, r.s.Nodes["sfml_member"].Identity, r.key, rejoinSecond)
	if err != nil {
		return fmt.Errorf("second contract: %w", err)
	}
	r.f.AcceptSecond = "ok"
	if err := userclient.New(nil, r.memberAdmin).AcceptContract(r.w.ctx, signed); err != nil {
		r.f.AcceptSecond = err.Error()
	}
	if err := objectsclient.New(r.w.node1, r.memberAdmin).Push(r.w.ctx, signed); err != nil {
		return fmt.Errorf("push the second contract to node1: %w", err)
	}
	r.f.PushedAt = unix(time.Now())
	if err := holdsWithin(r.watch, rejoinWait, r.offering); err != nil {
		return fmt.Errorf("the follow never showed the rejoined member: %w", err)
	}
	r.f.BackAt = unix(time.Now())
	return nil
}

func unix(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }
