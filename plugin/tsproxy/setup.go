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
	channels, err := parseChannels(c)
	if err != nil {
		return err
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

// parseChannels reads the tsproxy block(s) from the Corefile and returns the
// list of configured proxy channels. It is split out of setup so it can be
// tested without the startup/shutdown wiring.
func parseChannels(c *caddy.Controller) ([]channel, error) {
	var channels []channel
	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "https_redirect":
				args := c.RemainingArgs()
				if len(args) != 3 || args[1] != "->" {
					return nil, fmt.Errorf("unexpected format for https_redirect, expected: https_redirect <listen_port> -> <target_port>")
				}

				mp, err := strconv.ParseUint(args[0], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid numeral for listen port %s", args[0])
				}

				tp, err := strconv.ParseUint(args[2], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid numeral for target port %s", args[2])
				}

				channels = append(channels, channel{
					protocol:   "https_redirect",
					myPort:     int(mp),
					targetPort: int(tp),
				})
			case "udp", "tcp", "tcp_proxy":
				protocol := c.Val()
				args := c.RemainingArgs()
				if len(args) != 4 || args[1] != "->" {
					return nil, fmt.Errorf("unexpected format for %s, expected: %s <listen_port> -> <target_host> <target_port>", protocol, protocol)
				}

				mp, err := strconv.ParseUint(args[0], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid numeral for listen port %s", args[0])
				}

				tp, err := strconv.ParseUint(args[3], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid numeral for target port %s", args[3])
				}

				channels = append(channels, channel{
					protocol:   protocol,
					myPort:     int(mp),
					target:     args[2],
					targetPort: int(tp),
				})
			default:
				return nil, fmt.Errorf("unexpected token %s", c.Val())
			}
		}
	}

	return channels, nil
}
