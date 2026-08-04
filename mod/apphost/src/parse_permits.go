package apphost

import (
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// parsePermits reads the actions an app asks to hold from a comma-separated
// argument. Blanks are dropped rather than turned into a permit for nothing.
func parsePermits(joined string) (permits []*auth.Permit) {
	for _, action := range strings.Split(joined, ",") {
		if action = strings.TrimSpace(action); action != "" {
			permits = append(permits, &auth.Permit{Action: astral.String8(action)})
		}
	}
	return
}
