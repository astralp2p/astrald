package gateway

import "github.com/astralp2p/astrald/mod/scheduler"

// MaintainBindingTask is a scheduler task that keeps a gateway binding alive,
// re-establishing it after disconnections or expiry.
type MaintainBindingTask interface {
	scheduler.Task
}
