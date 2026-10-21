package gateway

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// A socket connection that closes before presenting a nonce is rejected with an
// error; the gateway does not dereference the missing nonce.
func TestHandleInboundRejectsConnClosedBeforeNonce(t *testing.T) {
	tests := []struct {
		name string
		sent []byte // written by the client before it closes
	}{
		{name: "close only"},
		{name: "newline then close", sent: []byte("\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			gw := &Module{
				log: log.New(astral.GenerateIdentity()),
				ctx: astral.NewContext(ctx),
			}

			clientEnd, gatewayEnd := net.Pipe()
			defer gatewayEnd.Close()
			// why: a gateway that waits for more input fails the test instead of hanging it.
			gatewayEnd.SetReadDeadline(time.Now().Add(2 * time.Second))

			go func() {
				if len(tt.sent) > 0 {
					clientEnd.Write(tt.sent)
				}
				clientEnd.Close()
			}()

			stop, err := gw.handleInbound(ctx, pipeConn{Conn: gatewayEnd})
			if err == nil {
				t.Fatal("handleInbound accepted a connection that sent no nonce")
			}
			if stop {
				t.Fatal("handleInbound stopped the listener")
			}
		})
	}
}
