package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

const (
	ErrCodeAuthFailed       = "auth_failed"
	ErrCodeDenied           = "denied"
	ErrCodeRouteNotFound    = "route_not_found"
	ErrCodeInternalError    = "internal_error"
	ErrCodeProtocolError    = "protocol_error"
	ErrCodeTimeout          = "timeout"
	ErrCodeCanceled         = "canceled"
	ErrCodeTargetNotAllowed = "target_not_allowed"
)

// ErrorMsg represents an error message.
type ErrorMsg struct {
	Code astral.String8
}

func (ErrorMsg) ObjectType() string {
	return "mod.apphost.error_msg"
}

func (msg ErrorMsg) WriteTo(w io.Writer) (n int64, err error) {
	return msg.Code.WriteTo(w)
}

func (msg *ErrorMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return msg.Code.ReadFrom(r)
}

func (msg ErrorMsg) Error() string {
	return msg.Code.String()
}

func init() {
	_ = astral.Add(&ErrorMsg{})
}
