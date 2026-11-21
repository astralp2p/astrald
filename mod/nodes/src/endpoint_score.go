package nodes

import "time"

// The numbers of the dial-order score; every one is tuned here, none in the strategy.
const (
	// ewmaAlpha is the weight of the newest dial in SuccessEWMA and LatencyEWMA.
	ewmaAlpha = 0.3
	// successPrior is the success rate of an endpoint with no dial history.
	// why: neutral, so a new endpoint is neither avoided nor preferred over one that works half the time.
	successPrior = 0.5

	// weightRank is the score of one point of transport rank (transportProfiles).
	weightRank = 0.01
	// weightSuccess is the score of a success rate of 1.
	weightSuccess = 1.0
	// weightLatency is the score taken off for a latency of latencyScale or more.
	weightLatency = 0.2
	// latencyScale is the latency at which the latency term is at its full weight.
	latencyScale = 5 * time.Second
	// weightStable is the bonus of an endpoint that its subject lists on stableDays days.
	weightStable = 0.1
	// stableDays is the SeenDays from which an endpoint counts as habitual.
	stableDays = 3
)

// dialScore returns the score of a row: the higher it is, the sooner the row dials.
// health is the dial history of the row from the current network, nil for none;
// seenDays is how many days the subject listed the endpoint.
//
//	score = weightRank*rank + weightSuccess*success - weightLatency*norm(latency) + weightStable*stable(seenDays)
func dialScore(network string, health *dbEndpointHealth, seenDays uint32) float64 {
	score := weightRank * float64(rankOf(network))
	if seenDays >= stableDays {
		score += weightStable
	}

	if !health.hasDialHistory() {
		return score + weightSuccess*successPrior
	}
	score += weightSuccess * health.SuccessEWMA
	latency := min(float64(health.LatencyEWMA)/float64(latencyScale), 1)
	return score - weightLatency*latency
}
