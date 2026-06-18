package tsproxy

import (
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain runs the suite under goleak so any proxy goroutine that outlives its
// Close() fails the run, and shortens the TCP drain timeout so tests where one
// peer keeps its half open don't wait the full production 90s.
func TestMain(m *testing.M) {
	tcpDrainTimeout = 200 * time.Millisecond
	goleak.VerifyTestMain(m)
}

// TestTcpProxyShutdownWithOpenConnection is the direct regression test for the
// goroutine-leak fix: a connection is left half-open, then Close() must return
// promptly and leave no goroutines behind (verified by goleak in TestMain).
func TestTcpProxyShutdownWithOpenConnection(t *testing.T) {
	echoPort := tcpEcho(t)
	listenPort := freePort(t)

	proxy := NewTcpProxy("tcp", listenPort, "127.0.0.1", echoPort)

	conn := dialTCP(t, listenPort)
	// Send a byte so the handler + both copy goroutines are definitely running,
	// then leave the connection open.
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readN(t, conn, 1)

	done := make(chan struct{})
	go func() {
		proxy.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy.Close() did not return promptly with an open connection")
	}

	conn.Close()
}

// TestUdpProxyShutdown verifies the UDP proxy tears down cleanly.
func TestUdpProxyShutdown(t *testing.T) {
	echoPort := udpEcho(t)
	listenPort := freePort(t)

	proxy := NewUdpProxy("udp", listenPort, "127.0.0.1", echoPort)
	conn := dialUDP(t, listenPort)
	udpRoundtrip(t, conn, []byte("warmup"))
	conn.Close()

	done := make(chan struct{})
	go func() {
		proxy.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("UDP proxy.Close() did not return promptly")
	}
}
