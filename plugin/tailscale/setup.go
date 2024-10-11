package tailscale

import (
	"context"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"tailscale.com/ipn/ipnstate"
)

var log = clog.NewWithPlugin("tailscale")

func init() {
	plugin.Register("tailscale", setup)
}

func setup(c *caddy.Controller) error {
	log.Info("initializing tailscale plugin...")

	// Global instance of the tailscale plugin
	SetGlobalTailscale(NewTailscalePlugin())

	// Wait for the tailscale server to properly initialize
	log.Info("waiting for tailscale to be ready...")
	for {
		status, err := GetGlobalTailscale().Client.StatusWithoutPeers(context.Background())
		if err != nil {
			return err
		}
		if status.BackendState == "Running" {
			log.Info("tailscale plugin initialized, running on " + status.Self.DNSName)
			initialize(c, status)
			break
		} else {
			log.Info("waiting for tailscale")
			time.Sleep(1 * time.Second)
		}
	}

	return nil
}

func initialize(c *caddy.Controller, status *ipnstate.Status) {
	config := dnsserver.GetConfig(c)

	// collect all local addresses from tailscale
	all := []string{}
	for _, ip := range status.TailscaleIPs {
		all = append(all, ip.String())
	}

	// and make sure we listen on all of them and nothing else
	config.ListenHosts = all
	log.Infof("DNS configured to listen on %v", all)
}
