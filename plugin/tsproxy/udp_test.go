package tsproxy

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// dialUDP opens a UDP socket connected to the proxy's listen port.
func dialUDP(t *testing.T, port int) *net.UDPConn {
	t.Helper()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	return conn
}

// udpRoundtrip sends payload and waits for the echoed reply. It retransmits on
// failure because the proxy binds its listener asynchronously in serve(), so an
// early datagram (or a lost one) must be resent until the session is up.
func udpRoundtrip(t *testing.T, conn *net.UDPConn, payload []byte) []byte {
	t.Helper()
	buf := make([]byte, 64*1024)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := conn.Write(payload); err != nil {
			t.Fatalf("udp write: %v", err)
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := conn.Read(buf)
		if err == nil {
			return buf[:n]
		}
	}
	t.Fatalf("udp roundtrip timed out for %q", payload)
	return nil
}

func TestUdpProxyRoundtrip(t *testing.T) {
	echoPort := udpEcho(t)
	listenPort := freePort(t)
	dst := fmt.Sprintf("127.0.0.1:%d", echoPort)

	proxy := NewUdpProxy("udp", listenPort, "127.0.0.1", echoPort)
	t.Cleanup(proxy.Close)

	conn := dialUDP(t, listenPort)
	t.Cleanup(func() { conn.Close() })

	payload := []byte("udp ping")
	got := udpRoundtrip(t, conn, payload)
	if string(got) != string(payload) {
		t.Fatalf("echo mismatch: got %q want %q", got, payload)
	}

	if !eventually(t, time.Second, func() bool {
		return metric(t, activeConnections.WithLabelValues("udp", itoa(listenPort), dst)) >= 1
	}) {
		t.Errorf("expected an active UDP session")
	}
}

// TestUdpProxySessionReuse sends two datagrams from the same client socket and
// asserts a single upstream session is created (one connection, one active gauge).
func TestUdpProxySessionReuse(t *testing.T) {
	echoPort := udpEcho(t)
	listenPort := freePort(t)
	dst := fmt.Sprintf("127.0.0.1:%d", echoPort)

	before := metric(t, connectionsCount.WithLabelValues("udp", itoa(listenPort), dst))

	proxy := NewUdpProxy("udp", listenPort, "127.0.0.1", echoPort)
	t.Cleanup(proxy.Close)

	conn := dialUDP(t, listenPort)
	t.Cleanup(func() { conn.Close() })

	udpRoundtrip(t, conn, []byte("first"))
	udpRoundtrip(t, conn, []byte("second"))

	if c := metric(t, connectionsCount.WithLabelValues("udp", itoa(listenPort), dst)); c != before+1 {
		t.Errorf("connectionsCount = %v, want %v (one session reused)", c, before+1)
	}
	if a := metric(t, activeConnections.WithLabelValues("udp", itoa(listenPort), dst)); a != 1 {
		t.Errorf("activeConnections = %v, want 1", a)
	}
}

// TestUdpProxyGCEviction uses a tiny idle timeout so the GC sweep evicts an idle
// session quickly; it asserts the session map empties and the gauge drops to 0.
func TestUdpProxyGCEviction(t *testing.T) {
	echoPort := udpEcho(t)
	listenPort := freePort(t)
	dst := fmt.Sprintf("127.0.0.1:%d", echoPort)

	proxy := newUdpProxy("udp", listenPort, "127.0.0.1", echoPort)
	proxy.idleTimeout = 20 * time.Millisecond
	proxy.gcInterval = 10 * time.Millisecond
	go proxy.serve()
	t.Cleanup(proxy.Close)

	conn := dialUDP(t, listenPort)
	t.Cleanup(func() { conn.Close() })

	udpRoundtrip(t, conn, []byte("transient"))

	if !eventually(t, 2*time.Second, func() bool {
		return metric(t, activeConnections.WithLabelValues("udp", itoa(listenPort), dst)) == 0
	}) {
		t.Errorf("idle UDP session was not garbage collected")
	}
}
