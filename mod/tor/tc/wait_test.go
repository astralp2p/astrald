package tc

import (
	"net"
	"testing"
	"time"
)

// TestWaitReturnsWhenTransportCloses: Wait discards what Tor sends and returns once the control
// connection closes, and WaitClose reports the close.
func TestWaitReturnsWhenTransportCloses(t *testing.T) {
	client, server := net.Pipe()
	ctl := New(client)

	done := make(chan error, 1)
	go func() { done <- ctl.Wait() }()

	server.Write([]byte("650 STATUS_GENERAL NOTICE X\r\n"))
	server.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Wait returned no error after the close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after the close")
	}
	select {
	case <-ctl.WaitClose():
	default:
		t.Fatal("WaitClose is still open after Wait returned")
	}
}
