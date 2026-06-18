package tsproxy

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin/pkg/reuseport"
)

// tcpDrainTimeout is how long the still-open half of a connection is given to
// finish after the other half closes, before it is force-closed via a deadline.
// It is a var (not a const) so tests can shorten it.
var tcpDrainTimeout = 90 * time.Second

type TcpProxy struct {
	listener   net.Listener
	wg         sync.WaitGroup
	quit       chan interface{}
	dst        string
	protocol   string
	listenPort string
}

func NewTcpProxy(protocol string, srcPort int, dstAddr string, dstPort int) *TcpProxy {
	var proxy TcpProxy

	listener, err := reuseport.Listen("tcp", fmt.Sprintf(":%d", srcPort))
	if err != nil {
		panic(err)
	}

	proxy.listener = listener
	proxy.wg.Add(1)
	proxy.dst = fmt.Sprintf("%s:%d", dstAddr, dstPort)
	proxy.quit = make(chan interface{})
	proxy.protocol = protocol
	proxy.listenPort = strconv.Itoa(srcPort)

	tcpLog.Infof("starting TCP proxy from local port %d to %s", srcPort, proxy.dst)

	go proxy.serve()
	return &proxy
}

func (proxy *TcpProxy) serve() {
	defer proxy.wg.Done()

	for {
		conn, err := proxy.listener.Accept()
		if err != nil {
			// an error happens
			select {
			case <-proxy.quit:
				return
			default:
				tcpLog.Errorf("accept error: %v", err)
			}
		} else {
			// normal connection accepted, spawn a handler goroutine
			connectionsCount.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Inc()
			activeConnections.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Inc()
			proxy.wg.Add(1)
			go func() {
				proxy.handleConnection(conn)
				proxy.wg.Done()
			}()
			tcpLog.Debugf("incomming connection from '%s' will be proxied to '%s'", conn.LocalAddr().String(), conn.RemoteAddr().String())
		}
	}
}

func (proxy *TcpProxy) Close() {
	close(proxy.quit)
	proxy.listener.Close()
	proxy.wg.Wait()
}

func (proxy *TcpProxy) handleConnection(downstream net.Conn) {
	defer downstream.Close()

	start := time.Now()
	defer func() {
		activeConnections.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Dec()
		connectionDuration.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Observe(time.Since(start).Seconds())
	}()

	var upstream net.Conn
	var err error
	upstream, err = net.Dial("tcp", proxy.dst)

	if err != nil {
		tcpLog.Errorf("error dialing remote addr: %v", err)
		return
	}
	defer upstream.Close()

	// Start threads for copying both ways
	closerChannel := make(chan struct{})
	defer close(closerChannel)

	var up, down int64
	var iowg sync.WaitGroup
	iowg.Add(2)
	go copy(&iowg, closerChannel, upstream, downstream, &up)   // client -> target
	go copy(&iowg, closerChannel, downstream, upstream, &down) // target -> client

	// Wait until one of:
	//  - one thread ends
	//  - a stop is requested from the outside
	select {
	case <-closerChannel:
	case <-proxy.quit:
	}

	// and then close the connection after the drain timeout also for the other side, if they don't close it themselves
	now := time.Now()
	downstream.SetDeadline(now.Add(tcpDrainTimeout))
	upstream.SetDeadline(now.Add(tcpDrainTimeout))

	// wait for both copy threads to finish
	iowg.Wait()
	recordBytes(proxy.protocol, proxy.listenPort, proxy.dst, up, down)
	tcpLog.Debugf("connection from %s closed", downstream.RemoteAddr().String())
}

// recordBytes accounts the per-direction and per-connection byte metrics for a
// finished connection/session.
func recordBytes(protocol, listenPort, target string, up, down int64) {
	proxiedBytesCount.WithLabelValues(protocol, listenPort, target, directionUp).Add(float64(up))
	proxiedBytesCount.WithLabelValues(protocol, listenPort, target, directionDown).Add(float64(down))
	connectionBytes.WithLabelValues(protocol, listenPort, target).Observe(float64(up + down))
}

func copy(wg *sync.WaitGroup, closer chan struct{}, dst io.Writer, src io.Reader, n *int64) {
	written, _ := io.Copy(dst, src)
	if n != nil {
		*n = written
	}

	// notify the parent that we should start closing
	// Note that in TCP, the connection can be closed on one side and the data stream
	// still continue from the other side.
	select {
	case closer <- struct{}{}:
		// this is blocking, so it only works if someone is actively receiving
	default:
		// if there is nobody receiving, we can continue
	}

	wg.Done()
}
