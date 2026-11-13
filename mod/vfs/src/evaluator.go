package vfs

import (
	"errors"
	"regexp"
	"strings"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// evaluatorPrefix and evaluatorSuffix bound the one endpoint form an evaluator takes:
// astral://<identity-or-alias>:vfs.authorize (operator decision, 2026-11-13).
const (
	evaluatorPrefix = "astral://"
	evaluatorSuffix = ":" + vfsapi.MethodAuthorize
)

// evaluatorTarget is the identity or alias of an endpoint. It admits no caller part, no op and no arguments.
var evaluatorTarget = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// maxEndpointLen is the longest endpoint mod.vfs.info carries.
const maxEndpointLen = 255

// SetEvaluator writes the evaluator endpoint of a VFS after checking it; see vfs.Module. An empty endpoint clears
// it.
//
// note: the lock is taken before the existence check.
// note: existence is checked before the endpoint, as a module checks authorization, then existence, then arguments.
func (mod *Module) SetEvaluator(id *astral.Identity, endpoint string) error {
	return mod.runLockedTx(id, func(tx *DB) error {
		vol, err := tx.findVolume(id)
		if err != nil {
			return err
		}
		if endpoint != "" && !isEvaluatorEndpoint(endpoint) {
			return vfsapi.ErrInvalidEndpoint
		}

		return tx.setVolumeEvaluator(vol.ID, endpoint)
	})
}

// isEvaluatorEndpoint reports whether endpoint is astral://<identity-or-alias>:vfs.authorize and fits
// mod.vfs.info.
//
// why: the node asks the evaluator as itself, so an endpoint naming another op or arguments would run that op
// with the node's authority.
func isEvaluatorEndpoint(endpoint string) bool {
	if len(endpoint) > maxEndpointLen {
		return false
	}
	target, ok := strings.CutPrefix(endpoint, evaluatorPrefix)
	if !ok {
		return false
	}
	target, ok = strings.CutSuffix(target, evaluatorSuffix)

	return ok && evaluatorTarget.MatchString(target)
}

// askEvaluator asks the evaluator of the VFS whether the actor of the action may do it. A VFS that does not exist,
// names no evaluator, or holds an endpoint not of the form isEvaluatorEndpoint accepts refuses without a question. A failure, a timeout or any answer but yes refuses.
//
// note: nothing remembers an answer; every call puts the question again.
// note: the timeout is the auth module's default for an external question.
func (mod *Module) askEvaluator(ctx *astral.Context, action apiauth.ActionObject, vfsID *astral.Identity) bool {
	if action.Actor().IsZero() || vfsID.IsZero() {
		return false
	}

	vol, err := mod.db.findVolume(vfsID)
	switch {
	case errors.Is(err, vfsapi.ErrNoSuchVFS):
		return false
	case err != nil:
		mod.log.Log("find the evaluator of %v: %v", vfsID, err)
		return false
	case vol.Evaluator == "":
		return false
	}

	// why: a row written before the check, or by hand, must not reach another op or another transport.
	if !isEvaluatorEndpoint(vol.Evaluator) {
		mod.log.Logv(1, "evaluator %v of %v is not an evaluator endpoint", vol.Evaluator, vfsID)
		return false
	}

	ask, err := mod.Auth.NewAuthorizeAsk(vol.Evaluator, 0)
	if err != nil {
		mod.log.Logv(1, "evaluator %v of %v: %v", vol.Evaluator, vfsID, err)
		return false
	}

	allow, err := ask.Ask(ctx, action)
	if err != nil {
		mod.log.Logv(1, "evaluator %v of %v: %v", vol.Evaluator, vfsID, err)
		return false
	}

	return allow
}
