package tsproxy

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// freePort asks the OS for an unused TCP port, then releases it so a proxy can
// bind it. There is a tiny race window between release and re-bind, but the
// proxies set SO_REUSEPORT (for TCP) and the suite uses fresh ports per test,
// so collisions are not a problem in practice.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// tcpEcho starts a TCP server on 127.0.0.1 that echoes everything it reads back
// on the same connection. It returns the port and is cleaned up automatically.
func tcpEcho(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcpEcho listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	return l.Addr().(*net.TCPAddr).Port
}

// udpEcho starts a UDP server on 127.0.0.1 that echoes each datagram back to
// its sender. It returns the port and is cleaned up automatically.
func udpEcho(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("udpEcho listen: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], addr)
		}
	}()

	return conn.LocalAddr().(*net.UDPAddr).Port
}

// metric reads the current value of a single labeled counter/gauge.
func metric(t *testing.T, c prometheus.Collector) float64 {
	t.Helper()
	return testutil.ToFloat64(c)
}

// eventually polls fn until it returns true or the timeout elapses.
func eventually(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fn()
}
