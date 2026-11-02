package apphost

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/lib/query"
)

// validatePermits runs auth.ValidatePermit on every permit and returns the
// first failure, which wraps auth.ErrInvalidPermit and names the action.
//
// why: an op calls it before any policy is asked or any row written, so a
// permit that can never authorize is refused at once and never shown to a
// delegate.
func validatePermits(permits []*auth.Permit) error {
	for _, p := range permits {
		if err := auth.ValidatePermit(p); err != nil {
			return err
		}
	}
	return nil
}

// refuseRemovedArgs returns an error wrapping query.ErrUnknownArgument when
// queryString names one of the removed arguments.
//
// why: the argument binder skips a key no argument declares, so without this a
// caller still sending a removed argument is accepted and then waits for an
// input object it never sends.
func refuseRemovedArgs(queryString string, names ...string) error {
	_, params := query.Parse(queryString)
	for _, name := range names {
		if _, found := params[name]; found {
			return fmt.Errorf("%w: %s", query.ErrUnknownArgument, name)
		}
	}
	return nil
}
