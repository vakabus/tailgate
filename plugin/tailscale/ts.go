package tailscale

import "tailscale.com/client/tailscale"

type TailscalePlugin struct {
	Client *tailscale.LocalClient
}

func NewTailscalePlugin() *TailscalePlugin {
	return &TailscalePlugin{
		Client: &tailscale.LocalClient{},
	}
}

// Name implements plugin.Handler.
func (b *TailscalePlugin) Name() string { return "tailscale" }

var global *TailscalePlugin = nil

func GetGlobalTailscale() *TailscalePlugin {
	return global
}

func SetGlobalTailscale(t *TailscalePlugin) {
	if global != nil {
		panic("tailscale plugin already initialized")
	}

	global = t
}
