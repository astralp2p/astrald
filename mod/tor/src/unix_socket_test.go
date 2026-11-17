package tor

import (
	"bufio"
	"context"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// listenUnix listens on a unix socket in a fresh temporary directory.
func listenUnix(t *testing.T, name string) (net.Listener, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %v: %v", path, err)
	}
	t.Cleanup(func() { l.Close() })
	return l, path
}

// fakeEmbeddedTor serves one control connection the way tor-android's control socket does: it
// asks for no authentication, and answers ADD_ONION with a service ID. It returns the ADD_ONION
// line it received.
func fakeEmbeddedTor(t *testing.T, l net.Listener) <-chan string {
	t.Helper()
	added := make(chan string, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			var reply string
			switch {
			case strings.HasPrefix(line, "PROTOCOLINFO"):
				reply = "250-PROTOCOLINFO 1\r\n250-AUTH METHODS=NULL\r\n250-VERSION Tor=\"0.4.9.13\"\r\n250 OK\r\n"
			case strings.HasPrefix(line, "ADD_ONION"):
				added <- line
				reply = "250-ServiceID=" + testServiceID + "\r\n250 OK\r\n"
			default:
				reply = "250 OK\r\n"
			}
			if _, err = conn.Write([]byte(reply)); err != nil {
				return
			}
		}
	}()
	return added
}

// TestListenThroughUnixSockets: with control_addr and onion_target on unix sockets, the node
// authenticates to a control socket that asks for none, registers the onion with the unix target,
// and accepts the connections Tor forwards there.
func TestListenThroughUnixSockets(t *testing.T) {
	controlListener, controlPath := listenUnix(t, "control")
	added := fakeEmbeddedTor(t, controlListener)
	targetPath := filepath.Join(t.TempDir(), "onion.sock")

	mod := loadModule(t, "control_addr: unix:"+controlPath+"\nonion_target: unix:"+targetPath+"\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l, err := NewServer(mod).listen(ctx, nil)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	select {
	case line := <-added:
		if want := "ADD_ONION NEW:ED25519-V3 Port=1791,unix:" + targetPath; line != want {
			t.Fatalf("ADD_ONION line = %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ADD_ONION")
	}
	if l.Addr() != testServiceID {
		t.Fatalf("service ID = %q", l.Addr())
	}

	accepted := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			conn.Close()
		}
		accepted <- err
	}()
	conn, err := net.Dial("unix", targetPath)
	if err != nil {
		t.Fatalf("Tor could not reach the onion target: %v", err)
	}
	conn.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}

// fakeSOCKS5 serves one SOCKS5 CONNECT without authentication, then echoes. It returns the
// host:port asked for.
func fakeSOCKS5(t *testing.T, l net.Listener) <-chan string {
	t.Helper()
	asked := make(chan string, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 262)
		// greeting: version, method count, methods; answer "no authentication"
		if _, err = io.ReadFull(conn, buf[:2]); err != nil {
			return
		}
		if _, err = io.ReadFull(conn, buf[:buf[1]]); err != nil {
			return
		}
		conn.Write([]byte{5, 0})
		// request: version, CONNECT, reserved, domain name type, length, name, port
		if _, err = io.ReadFull(conn, buf[:5]); err != nil {
			return
		}
		n := int(buf[4])
		if _, err = io.ReadFull(conn, buf[:n+2]); err != nil {
			return
		}
		port := int(buf[n])<<8 | int(buf[n+1])
		asked <- net.JoinHostPort(string(buf[:n]), strconv.Itoa(port))
		conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		io.Copy(conn, conn)
	}()
	return asked
}

// TestDialThroughAUnixSOCKSProxy: with tor_proxy on a unix socket, Dial reaches the onion
// through that socket and the stream carries data both ways.
func TestDialThroughAUnixSOCKSProxy(t *testing.T) {
	socksListener, socksPath := listenUnix(t, "socks")
	asked := fakeSOCKS5(t, socksListener)

	mod := loadModule(t, "tor_proxy: unix:"+socksPath+"\n")
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	// note: a real v3 ID, since Dial packs and unpacks the endpoint, which an all-zero digest
	// does not survive
	endpoint, err := Parse(testOnionV3 + ".onion:1791")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	conn, err := mod.Dial(ctx, endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if got, want := <-asked, testOnionV3+".onion:1791"; got != want {
		t.Fatalf("the proxy was asked for %q, want %q", got, want)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "ping" {
		t.Fatalf("read %q, %v", got, err)
	}
}
