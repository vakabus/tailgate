package tailscale

import "tailscale.com/client/local"

type TailscalePlugin struct {
	Client *local.Client
}

func NewTailscalePlugin() *TailscalePlugin {
	return &TailscalePlugin{
		Client: &local.Client{},
	}
}

// Name implements plugin.Handler.
func (b *TailscalePlugin) Name() string { return "tailscale" }

var global *TailscalePlugin

func GetGlobalTailscale() *TailscalePlugin {
	return global
}

func SetGlobalTailscale(t *TailscalePlugin) {
	if global != nil {
		panic("tailscale plugin already initialized")
	}

	global = t
}
