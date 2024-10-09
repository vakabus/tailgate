package bind

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register("tsbind", setup) }

func setup(c *caddy.Controller) error {

}
