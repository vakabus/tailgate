package tsproxy

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestTcpProxyRoundtrip(t *testing.T) {
	echoPort := tcpEcho(t)
	listenPort := freePort(t)
	dst := fmt.Sprintf("127.0.0.1:%d", echoPort)

	before := metric(t, connectionsCount.WithLabelValues("tcp", itoa(listenPort), dst))

	proxy := NewTcpProxy("tcp", listenPort, "127.0.0.1", echoPort)
	t.Cleanup(proxy.Close)

	conn := dialTCP(t, listenPort)
	payload := []byte("hello tsproxy")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := readN(t, conn, len(payload))
	if string(got) != string(payload) {
		t.Fatalf("echo mismatch: got %q want %q", got, payload)
	}

	// Closing the client drives the proxy to finish and record byte metrics.
	conn.Close()

	if !eventually(t, time.Second, func() bool {
		return metric(t, activeConnections.WithLabelValues("tcp", itoa(listenPort), dst)) == 0
	}) {
		t.Errorf("activeConnections did not return to 0")
	}

	if c := metric(t, connectionsCount.WithLabelValues("tcp", itoa(listenPort), dst)); c != before+1 {
		t.Errorf("connectionsCount = %v, want %v", c, before+1)
	}

	// up = bytes client->target (the payload). down = echoed bytes back.
	up := metric(t, proxiedBytesCount.WithLabelValues("tcp", itoa(listenPort), dst, directionUp))
	down := metric(t, proxiedBytesCount.WithLabelValues("tcp", itoa(listenPort), dst, directionDown))
	if up < float64(len(payload)) {
		t.Errorf("proxied up bytes = %v, want >= %d", up, len(payload))
	}
	if down < float64(len(payload)) {
		t.Errorf("proxied down bytes = %v, want >= %d", down, len(payload))
	}
}

// TestTcpProxyDialFailure verifies that when the upstream is unreachable the
// proxy still accepts and then cleanly closes the client connection, and the
// active-connection gauge settles back to zero (no hang, no leak of accounting).
func TestTcpProxyDialFailure(t *testing.T) {
	deadPort := freePort(t) // nothing listening here
	listenPort := freePort(t)
	dst := fmt.Sprintf("127.0.0.1:%d", deadPort)

	proxy := NewTcpProxy("tcp", listenPort, "127.0.0.1", deadPort)
	t.Cleanup(proxy.Close)

	conn := dialTCP(t, listenPort)
	t.Cleanup(func() { conn.Close() })

	// The proxy fails to dial upstream and closes our connection; a read returns EOF.
	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 16)
	if _, err := conn.Read(buf); err == nil {
		t.Errorf("expected read error after upstream dial failure")
	}

	if !eventually(t, time.Second, func() bool {
		return metric(t, activeConnections.WithLabelValues("tcp", itoa(listenPort), dst)) == 0
	}) {
		t.Errorf("activeConnections did not return to 0 after dial failure")
	}
}

// --- small shared helpers used by the TCP-style tests ---

func itoa(i int) string { return fmt.Sprintf("%d", i) }

func dialTCP(t *testing.T, port int) net.Conn {
	t.Helper()
	var conn net.Conn
	var err error
	// The proxy's accept loop starts in a goroutine; retry briefly until it binds.
	for i := 0; i < 100; i++ {
		conn, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return conn
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("dial 127.0.0.1:%d: %v", port, err)
	return nil
}

func readN(t *testing.T, conn net.Conn, n int) []byte {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, n)
	got := 0
	for got < n {
		m, err := conn.Read(buf[got:])
		if err != nil {
			t.Fatalf("read: %v (got %d/%d)", err, got, n)
		}
		got += m
	}
	return buf
}
