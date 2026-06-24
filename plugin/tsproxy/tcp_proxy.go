package tsproxy

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin/pkg/reuseport"
)

type TcpProxyProxy struct {
	listener   net.Listener
	wg         sync.WaitGroup
	quit       chan any
	dst        string
	protocol   string
	listenPort string
}

func NewTcpProxyProxy(protocol string, srcPort int, dstAddr string, dstPort int) *TcpProxyProxy {
	var proxy TcpProxyProxy

	listener, err := reuseport.Listen("tcp", fmt.Sprintf(":%d", srcPort))
	if err != nil {
		panic(err)
	}

	proxy.listener = listener
	proxy.wg.Add(1)
	proxy.dst = fmt.Sprintf("%s:%d", dstAddr, dstPort)
	proxy.quit = make(chan any)
	proxy.protocol = protocol
	proxy.listenPort = strconv.Itoa(srcPort)

	tcpProxyLog.Infof("starting TCP+PROXY proxy from local port %d to %s", srcPort, proxy.dst)

	go proxy.serve()
	return &proxy
}

func (proxy *TcpProxyProxy) serve() {
	defer proxy.wg.Done()

	for {
		conn, err := proxy.listener.Accept()
		if err != nil {
			select {
			case <-proxy.quit:
				return
			default:
				tcpProxyLog.Errorf("accept error: %v", err)
			}
		} else {
			connectionsCount.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Inc()
			activeConnections.WithLabelValues(proxy.protocol, proxy.listenPort, proxy.dst).Inc()
			proxy.wg.Go(func() {
				proxy.handleConnection(conn)
			})
			tcpProxyLog.Debugf("incomming connection from '%s' will be proxied to '%s' with PROXY header", conn.RemoteAddr().String(), proxy.dst)
		}
	}
}

func (proxy *TcpProxyProxy) Close() {
	close(proxy.quit)
	proxy.listener.Close()
	proxy.wg.Wait()
}

func (proxy *TcpProxyProxy) handleConnection(downstream net.Conn) {
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
		tcpProxyLog.Errorf("error dialing remote addr: %v", err)
		return
	}
	defer upstream.Close()

	// Write PROXY protocol v1 header
	srcAddr := downstream.RemoteAddr().(*net.TCPAddr)
	dstAddr := downstream.LocalAddr().(*net.TCPAddr)
	family := "TCP4"
	if srcAddr.IP.To4() == nil {
		family = "TCP6"
	}
	header := fmt.Sprintf("PROXY %s %s %s %d %d\r\n",
		family, srcAddr.IP, dstAddr.IP, srcAddr.Port, dstAddr.Port)
	if _, err := upstream.Write([]byte(header)); err != nil {
		tcpProxyLog.Errorf("error writing proxy protocol header: %v", err)
		return
	}

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
	tcpProxyLog.Debugf("connection from %s closed", downstream.RemoteAddr().String())
}
