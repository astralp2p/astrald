package user

import "io"

// watchRequesterClose calls cancel when the requester's side of conn ends. It
// suits ops whose requester sends nothing more by the time it is started, so a
// read returns only when the conn ends.
//
// why: an op runs on a context detached from the requester's, so nothing else
// tells a waiting policy that nobody waits for its answer.
//
// note: after the op answers, the read returns when the requester closes the
// conn, which a requester does once it holds the answer. The same pattern
// serves messaging.wait (mod/messaging/src/op_wait.go).
func watchRequesterClose(conn io.Reader, cancel func()) {
	defer cancel()
	_, _ = io.Copy(io.Discard, conn)
}
