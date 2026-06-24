package tsproxy

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/coredns/coredns/plugin/pkg/reuseport"
)

type HttpsRedirect struct {
	server *http.Server
}

func NewHttpsRedirect(protocol string, srcPort int, targetPort int) *HttpsRedirect {
	redirect := &HttpsRedirect{}

	listener, err := reuseport.Listen("tcp", fmt.Sprintf(":%d", srcPort))
	if err != nil {
		panic(err)
	}

	listenPort := strconv.Itoa(srcPort)
	redirect.server = &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			connectionsCount.WithLabelValues(protocol, listenPort, "").Inc()
			host := r.Host
			if hostname, _, err := net.SplitHostPort(r.Host); err == nil {
				host = hostname
			}
			if targetPort != 443 {
				host = fmt.Sprintf("%s:%d", host, targetPort)
			}
			target := "https://" + host + r.RequestURI
			http.Redirect(w, r, target, http.StatusMovedPermanently) //nolint:gosec // intentional: transparent HTTP→HTTPS protocol upgrade
		}),
	}

	httpsRedirectLog.Infof("starting HTTP->HTTPS redirect on port %d (target port %d)", srcPort, targetPort)

	go redirect.server.Serve(listener)
	return redirect
}

func (r *HttpsRedirect) Close() {
	r.server.Close()
}
