package tsproxy

type channel struct {
	protocol   string
	myPort     int
	target     string
	targetPort int
}

type closeable interface {
	Close()
}

type tsproxy struct {
	proxies []closeable
}

func (proxy *tsproxy) start(channels []channel) {
	log.Infof("starting tsproxy on %d channels", len(channels))

	// run the proxies
	for _, channel := range channels {
		var p closeable
		switch channel.protocol {
		case "udp":
			p = NewUdpProxy(channel.myPort, channel.target, channel.targetPort)
		case "tcp":
			p = NewTcpProxy(channel.myPort, channel.target, channel.targetPort)
		default:
			panic("wat " + channel.protocol)
		}

		proxy.proxies = append(proxy.proxies, p)
	}

	log.Infof("%d proxies started", len(proxy.proxies))
}

func (proxy *tsproxy) close() {
	// stop the proxies
	for _, p := range proxy.proxies {
		p.Close()
	}
}
