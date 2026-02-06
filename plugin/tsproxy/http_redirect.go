package tsproxy

import (
	"fmt"
	"net/http"

	"github.com/coredns/coredns/plugin/pkg/reuseport"
)

type HttpsRedirect struct {
	server *http.Server
}

func NewHttpsRedirect(srcPort int, targetPort int) *HttpsRedirect {
	redirect := &HttpsRedirect{}

	listener, err := reuseport.Listen("tcp", fmt.Sprintf(":%d", srcPort))
	if err != nil {
		panic(err)
	}

	redirect.server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if targetPort != 443 {
				host = fmt.Sprintf("%s:%d", r.Host, targetPort)
			}
			target := "https://" + host + r.RequestURI
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}

	httpsRedirectLog.Infof("starting HTTP->HTTPS redirect on port %d (target port %d)", srcPort, targetPort)

	go redirect.server.Serve(listener)
	return redirect
}

func (r *HttpsRedirect) Close() {
	r.server.Close()
}
