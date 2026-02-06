package tsproxy

import (
	"fmt"
	"strconv"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/tailscale"
)

var log = clog.NewWithPlugin("tsproxy")
var tcpLog = clog.NewWithPlugin("tsproxy/tcp")
var tcpProxyLog = clog.NewWithPlugin("tsproxy/tcp_proxy")
var udpLog = clog.NewWithPlugin("tsproxy/udp")
var httpsRedirectLog = clog.NewWithPlugin("tsproxy/https_redirect")

func init() {
	plugin.Register("tsproxy", setup)
}

func setup(c *caddy.Controller) error {
	var channels []channel
	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "https_redirect":
				args := c.RemainingArgs()
				if len(args) != 3 || args[1] != "->" {
					return fmt.Errorf("unexpected format for https_redirect, expected: https_redirect <listen_port> -> <target_port>")
				}

				mp, err := strconv.ParseUint(args[0], 10, 16)
				if err != nil {
					return fmt.Errorf("invalid numeral for listen port %s", args[0])
				}

				tp, err := strconv.ParseUint(args[2], 10, 16)
				if err != nil {
					return fmt.Errorf("invalid numeral for target port %s", args[2])
				}

				channels = append(channels, channel{
					protocol:   "https_redirect",
					myPort:     int(mp),
					targetPort: int(tp),
				})
			case "udp", "tcp", "tcp_proxy":
				protocol := c.Val()
				args := c.RemainingArgs()
				if len(args) != 4 && args[1] != "->" {
					return fmt.Errorf("unexpected format for %s", c.Val())
				}

				mp, err := strconv.ParseUint(args[0], 10, 16)
				if err != nil {
					return fmt.Errorf("invalid numeral for listen port %s", args[0])
				}

				tp, err := strconv.ParseUint(args[3], 10, 16)
				if err != nil {
					return fmt.Errorf("invalid numeral for listen port %s", args[0])
				}

				channels = append(channels, channel{
					protocol:   protocol,
					myPort:     int(mp),
					target:     args[2],
					targetPort: int(tp),
				})
			default:
				return fmt.Errorf("unexpected token " + c.Val())
			}
		}
	}

	proxy := &tsproxy{}
	c.OnStartup(func() error {
		if tailscale.GetGlobalTailscale() == nil {
			return fmt.Errorf("tsproxy: tailscale plugin not initialized")
		}

		proxy.start(channels)
		return nil
	})

	c.OnShutdown(func() error {
		proxy.close()
		return nil
	})

	return nil
}
