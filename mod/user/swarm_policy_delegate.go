package user

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// OpDecideSwarmJoin is the op a swarm join delegate serves. The node sends it
// one SwarmJoinRequest and reads one SwarmJoinDecision back.
const OpDecideSwarmJoin = "user.decide_swarm_join"

// OpDecideSwarmInvite is the op a swarm invite delegate serves. The node sends
// it one SwarmInviteRequest and reads one SwarmInviteDecision back.
const OpDecideSwarmInvite = "user.decide_swarm_invite"

// SwarmJoinRequest asks the swarm join delegate whether Requester may join
// this node's swarm.
type SwarmJoinRequest struct {
	Requester *astral.Identity
}

func (SwarmJoinRequest) ObjectType() string { return "mod.user.swarm_join_request" }

func (r SwarmJoinRequest) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&r).WriteTo(w)
}

func (r *SwarmJoinRequest) ReadFrom(rd io.Reader) (n int64, err error) {
	return astral.Objectify(r).ReadFrom(rd)
}

// SwarmJoinDecision is the swarm join delegate's answer.
type SwarmJoinDecision struct {
	Allow astral.Bool
}

func (SwarmJoinDecision) ObjectType() string { return "mod.user.swarm_join_decision" }

func (d SwarmJoinDecision) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&d).WriteTo(w)
}

func (d *SwarmJoinDecision) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(d).ReadFrom(r)
}

// SwarmInviteRequest asks the swarm invite delegate whether this node accepts
// Inviter's invitation and the membership Contract it offers.
type SwarmInviteRequest struct {
	Inviter  *astral.Identity
	Contract *auth.Contract
}

func (SwarmInviteRequest) ObjectType() string { return "mod.user.swarm_invite_request" }

func (r SwarmInviteRequest) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&r).WriteTo(w)
}

func (r *SwarmInviteRequest) ReadFrom(rd io.Reader) (n int64, err error) {
	return astral.Objectify(r).ReadFrom(rd)
}

// SwarmInviteDecision is the swarm invite delegate's answer.
type SwarmInviteDecision struct {
	Allow astral.Bool
}

func (SwarmInviteDecision) ObjectType() string { return "mod.user.swarm_invite_decision" }

func (d SwarmInviteDecision) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&d).WriteTo(w)
}

func (d *SwarmInviteDecision) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(d).ReadFrom(r)
}

func init() {
	astral.MustAdd(&SwarmJoinRequest{})
	astral.MustAdd(&SwarmJoinDecision{})
	astral.MustAdd(&SwarmInviteRequest{})
	astral.MustAdd(&SwarmInviteDecision{})
}
