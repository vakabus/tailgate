package tsproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// udpIdleTimeout is how long a UDP session may stay idle before it is garbage
// collected, and udpGCInterval is how often the GC sweep runs.
const (
	udpIdleTimeout = 90 * time.Second
	udpGCInterval  = 90 * time.Second
)

type UdpProxy struct {
	srcPort    int
	dst        string
	quit       chan struct{}
	protocol   string
	listenPort string

	// idleTimeout/gcInterval are configurable so tests can exercise session GC
	// without waiting for the production 90s timeout. NewUdpProxy defaults them.
	idleTimeout time.Duration
	gcInterval  time.Duration

	downstream downstreamProxy
	upstream   map[string]*upstreamProxy
}

// newUdpProxy builds an unstarted UdpProxy with default settings. It is split
// from NewUdpProxy so tests can tweak fields (e.g. idleTimeout/gcInterval)
// before serve() reads them.
func newUdpProxy(protocol string, srcPort int, dstAddr string, dstPort int) *UdpProxy {
	var proxy UdpProxy

	proxy.srcPort = srcPort
	proxy.dst = fmt.Sprintf("%s:%d", dstAddr, dstPort)
	proxy.quit = make(chan struct{})
	proxy.protocol = protocol
	proxy.listenPort = strconv.Itoa(srcPort)
	proxy.idleTimeout = udpIdleTimeout
	proxy.gcInterval = udpGCInterval

	return &proxy
}

func NewUdpProxy(protocol string, srcPort int, dstAddr string, dstPort int) *UdpProxy {
	proxy := newUdpProxy(protocol, srcPort, dstAddr, dstPort)

	udpLog.Infof("starting UDP proxy from local port %d to %s", proxy.srcPort, proxy.dst)

	go proxy.serve()
	return proxy
}

func (proxy *UdpProxy) serve() {
	// open downstream listener
	listener, err := net.ListenUDP("udp", &net.UDPAddr{Port: proxy.srcPort})
	if err != nil {
		panic(err)
	}

	// prepare upstream
	proxy.upstream = make(map[string]*upstreamProxy)
	defer func() {
		for key, ch := range proxy.upstream {
			ch.close()
			delete(proxy.upstream, key)
		}
	}()

	// start downstream
	proxy.downstream.in = listener
	proxy.downstream.toDownstream = make(chan msg)
	proxy.downstream.toUpstream = make(chan msg)
	proxy.downstream.start()
	defer proxy.downstream.close()

	// prepare upstream gc
	cleanupUpstream := func(now time.Time) {
		deadline := now.Add(-proxy.idleTimeout)
		for key, ch := range proxy.upstream {
			if time.Unix(0, ch.lastUsed.Load()).Before(deadline) {
				ch.close()
				delete(proxy.upstream, key)
			}
		}
	}
	gcTicker := time.NewTicker(proxy.gcInterval)
	defer gcTicker.Stop()

	// start main loop
	for {
		select {
		case <-proxy.quit:
			return
		case now := <-gcTicker.C:
			cleanupUpstream(now)
		case msg, ok := <-proxy.downstream.toUpstream:
			if !ok {
				return
			}

			proxy.send(msg)
		}
	}
}

func (proxy *UdpProxy) send(m msg) {
	up, ok := proxy.upstream[m.addr.String()]

	if !ok {
		var conn *net.UDPConn
		var err error

		addr, err := net.ResolveUDPAddr("udp", proxy.dst)
		if err != nil {
			udpLog.Errorf("failed to resolve target UDP address '%v' of the proxy: %v", proxy.dst, err)
			return
		}

		conn, err = net.DialUDP("udp", nil, addr)
		if err != nil {
			udpLog.Errorf("udp dial error: %v", err)
			return
		}

		newUpstream := &upstreamProxy{
			toDownstream:      proxy.downstream.toDownstream,
			toUpstream:        make(chan msg),
			conn:              conn,
			downstreamAddress: m.addr,
			protocol:          proxy.protocol,
			listenPort:        proxy.listenPort,
			target:            proxy.dst,
		}
		newUpstream.start()
		proxy.upstream[m.addr.String()] = newUpstream

		connectionsCount.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Inc()
		activeConnections.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Inc()

		up = newUpstream
	}

	up.toUpstream <- m
}

type msg struct {
	data []byte
	addr *net.UDPAddr
}

type downstreamProxy struct {
	in           *net.UDPConn
	toUpstream   chan msg
	toDownstream chan msg
	quit         chan struct{}
}

func (proxy *downstreamProxy) writer() {
	for {
		select {
		case <-proxy.quit:
			proxy.in.SetDeadline(time.Now())
			return
		case pkt := <-proxy.toDownstream:
			n, _, err := proxy.in.WriteMsgUDP(pkt.data, nil, pkt.addr)
			if n != len(pkt.data) {
				udpLog.Errorf("wrote only %d out of %d bytes to downstream", n, len(pkt.data))
			}
			if err != nil {
				udpLog.Errorf("downstream send error: %v", err)
			}
		}
	}
}

func (proxy *downstreamProxy) reader() {
	defer proxy.in.Close()

	for {
		buffer := make([]byte, 16*1024)
		n, _, _, addr, err := proxy.in.ReadMsgUDP(buffer, nil)
		if n > 0 {
			proxy.toUpstream <- msg{data: buffer[:n], addr: addr}
		}
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return
			}
			udpLog.Error("downstream UDP reading failed", err)
		}
	}
}

func (proxy *downstreamProxy) close() {
	close(proxy.quit)
}

func (proxy *downstreamProxy) start() {
	proxy.quit = make(chan struct{})

	go proxy.reader()
	go proxy.writer()
}

type upstreamProxy struct {
	toDownstream      chan msg
	toUpstream        chan msg
	quit              chan struct{}
	conn              *net.UDPConn
	downstreamAddress *net.UDPAddr
	lastUsed          atomic.Int64 // unix-nanos; accessed concurrently by reader/writer/GC

	protocol   string
	listenPort string
	target     string
	created    time.Time
	bytesUp    atomic.Int64 // client -> target
	bytesDown  atomic.Int64 // target -> client
}

func (proxy *upstreamProxy) reader() {
	defer proxy.conn.Close()

	for {
		proxy.lastUsed.Store(time.Now().UnixNano())
		buffer := make([]byte, 16*1024)
		n, _, _, _, err := proxy.conn.ReadMsgUDP(buffer, nil)
		if n > 0 {
			proxy.bytesDown.Add(int64(n))
			proxiedBytesCount.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.target, directionDown).Add(float64(n))
			proxy.toDownstream <- msg{data: buffer[:n], addr: proxy.downstreamAddress}
		}
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return
			}
			udpLog.Errorf("upstream UDP reading failed: %v", err)
		}
	}
}

func (proxy *upstreamProxy) writer() {
	for {
		proxy.lastUsed.Store(time.Now().UnixNano())

		select {
		case <-proxy.quit:
			proxy.conn.SetDeadline(time.Now())
			return
		case pkt := <-proxy.toUpstream:
			n, _, err := proxy.conn.WriteMsgUDP(pkt.data, nil, nil)
			if n > 0 {
				proxy.bytesUp.Add(int64(n))
				proxiedBytesCount.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.target, directionUp).Add(float64(n))
			}
			if n != len(pkt.data) {
				udpLog.Errorf("wrote only %d out of %d bytes to upstream", n, len(pkt.data))
			}
			if err != nil {
				udpLog.Errorf("upstream send error: %v", err)
			}
		}
	}
}

func (proxy *upstreamProxy) start() {
	now := time.Now()
	proxy.lastUsed.Store(now.UnixNano())
	proxy.created = now
	proxy.quit = make(chan struct{})

	go proxy.reader()
	go proxy.writer()
}

func (proxy *upstreamProxy) close() {
	close(proxy.quit)

	activeConnections.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.target).Dec()
	connectionDuration.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.target).Observe(time.Since(proxy.created).Seconds())
	total := proxy.bytesUp.Load() + proxy.bytesDown.Load()
	connectionBytes.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.target).Observe(float64(total))
}

func (proxy *UdpProxy) Close() {
	close(proxy.quit)
}
