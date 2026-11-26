package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/joins"

	"github.com/astralp2p/astral-go/api/services"
	userclient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
)

// membershipWait bounds how long the follow takes to reflect a member change.
// note: a carry to a new member retries on a capped backoff starting at 1s.
const membershipWait = 60 * time.Second

// membership opens a swarm follow on node1, adopts the fresh node ssm_late,
// then expels it, then admits ssm_expiry until its contract expires, and
// prints which steps held as JSON facts.
func membership(path string) error {
	facts := map[string]bool{}
	err := membershipSteps(path, facts)
	out, _ := json.Marshal(map[string]any{"services_swarm_membership": facts})
	fmt.Println(string(out))
	return err
}

func membershipSteps(path string, facts map[string]bool) error {
	w, err := load(path)
	if err != nil {
		return err
	}
	s, err := loadSession(path)
	if err != nil {
		return err
	}
	late := s.Nodes["ssm_late"]
	lateID, err := astral.ParseIdentity(late.Identity)
	if err != nil {
		return err
	}
	const name = "latejoin"

	app, f, err := w.app(w.admin1, w.n1)
	if err != nil {
		return err
	}
	if err := w.grant(app, []string{name}); err != nil {
		return err
	}
	watch, err := f.WatchIn(w.ctx, services.ReachSwarm, []string{name})
	if err != nil {
		return err
	}
	defer watch.Close()
	if err := holds(watch); err != nil {
		return err
	}

	users := userclient.New(w.node1, connect(w.n1.Endpoint, s.Facts.UserToken))
	// note: ssm_late joins through node1 as a Member
	if _, err := joins.Admit(w.ctx, connect(late.Endpoint, late.Token), connect(w.n1.Endpoint, s.Facts.UserToken), w.n1.Identity, late.Identity); err != nil {
		return fmt.Errorf("join: %w", err)
	}

	// why after the join: an app's contract signed while ssm_late was
	// unclaimed does not outlive the claim (a Non-goal of G7), so the provider
	// gets its contract through the swarm once ssm_late is a member.
	providerID, cl, err := w.app(connect(late.Endpoint, late.Token), late, serveApps)
	if err != nil {
		return err
	}
	provider := &evaluated{}
	b, err := cl.AdvertiseOpen(w.ctx, []string{name}, true, provider.offer)
	if err != nil {
		return err
	}
	defer b.Close()
	if err := step(facts, "late_join_reaches_open_follow", holdsWithin(watch, membershipWait, providerID.String()+"/"+name)); err != nil {
		return err
	}

	if _, err := users.Expel(w.ctx, lateID); err != nil {
		return fmt.Errorf("expel: %w", err)
	}
	if err := step(facts, "expulsion_removes_from_open_follow", holdsWithin(watch, membershipWait)); err != nil {
		return err
	}
	return expirySteps(w, s, watch, name, facts)
}

// session is the part of the session view the membership flow reads beyond
// node1 and node2.
type session struct {
	Nodes map[string]node `json:"nodes"`
	Facts struct {
		UserToken string `json:"user_token"`
		UserID    string `json:"user_id"`
	} `json:"facts"`
}

func loadSession(path string) (session, error) {
	var s session
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(raw, &s)
}
