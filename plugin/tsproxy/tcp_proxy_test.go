package tsproxy

import (
	"bufio"
	"fmt"
	"net"
	"regexp"
	"testing"
	"time"
)

// proxyHeaderRE matches a PROXY protocol v1 header for an IPv4 loopback connection.
var proxyHeaderRE = regexp.MustCompile(`^PROXY TCP4 127\.0\.0\.1 127\.0\.0\.1 \d+ \d+$`)

func TestTcpProxyProxyWritesProxyHeader(t *testing.T) {
	// Target that records the first line (the PROXY header) and echoes the rest.
	firstLine := make(chan string, 1)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	targetPort := l.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		firstLine <- line
		// Echo any remaining payload back so the client side can verify proxying.
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				conn.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	listenPort := freePort(t)
	proxy := NewTcpProxyProxy("tcp_proxy", listenPort, "127.0.0.1", targetPort)
	t.Cleanup(proxy.Close)

	conn := dialTCP(t, listenPort)
	t.Cleanup(func() { conn.Close() })

	payload := []byte("after-header")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case line := <-firstLine:
		trimmed := line
		if len(trimmed) >= 2 && trimmed[len(trimmed)-2:] == "\r\n" {
			trimmed = trimmed[:len(trimmed)-2]
		}
		if !proxyHeaderRE.MatchString(trimmed) {
			t.Fatalf("bad PROXY header: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive PROXY header at target")
	}

	got := readN(t, conn, len(payload))
	if string(got) != string(payload) {
		t.Fatalf("echo mismatch: got %q want %q", got, payload)
	}

	dst := fmt.Sprintf("127.0.0.1:%d", targetPort)
	if c := metric(t, connectionsCount.WithLabelValues("tcp_proxy", itoa(listenPort), dst)); c < 1 {
		t.Errorf("connectionsCount = %v, want >= 1", c)
	}
}
