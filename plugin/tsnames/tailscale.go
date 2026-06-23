package tailscale

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"

	ts "github.com/coredns/coredns/plugin/tailscale"
)

type Tailscale struct {
	next plugin.Handler
	zone string
	fall fall.F

	mu      sync.RWMutex
	entries map[string]map[string][]string
}

// Name implements the Handler interface.
func (t *Tailscale) Name() string { return "tailscale" }

// start connects the Tailscale plugin to a tailscale daemon and populates DNS entries for nodes in the tailnet.
// DNS entries are automatically kept up to date with any node changes.
//
// If t.authkey is non-empty, this function uses that key to connect to the Tailnet using a tsnet server
// instead of connecting to the local tailscaled instance.
func (t *Tailscale) start() error {
	if ts.GetGlobalTailscale() == nil {
		return fmt.Errorf("tailscale not initialized, can't use 'tsnames' plugin")
	}

	go t.watchIPNBus()
	return nil
}

// busBackoffMin/busBackoffMax bound the reconnect backoff for the IPN bus
// watcher. tailscaled can accept a watch connection and then immediately drop
// the stream (e.g. while it recomputes the netmap around a node change). An
// unthrottled reconnect loop in that window spins a CPU core and allocates
// connection/decode garbage faster than the GC can reclaim it; under a tight
// cgroup memory limit the runtime then gets OOM-killed even though the live
// heap stays small.
const (
	busBackoffMin = 1 * time.Second
	busBackoffMax = 1 * time.Minute
)

// nextBusBackoff doubles the current backoff, saturating at busBackoffMax.
func nextBusBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > busBackoffMax {
		return busBackoffMax
	}
	return next
}

// watchIPNBus watches the Tailscale IPN Bus and updates DNS entries for any netmap update.
// This function does not return. If it is unable to read from the IPN Bus, it will continue to retry.
func (t *Tailscale) watchIPNBus() {
	backoff := busBackoffMin
	for {
		watcher, err := ts.GetGlobalTailscale().Client.WatchIPNBus(context.Background(), ipn.NotifyInitialNetMap)
		if err != nil {
			log.Warningf("unable to connect to Tailscale event bus: %v; retrying in %s", err, backoff)
			busReconnectsTotal.WithLabelValues(t.zone).Inc()
			time.Sleep(backoff)
			backoff = nextBusBackoff(backoff)
			continue
		}

		connectedAt := time.Now()
		for {
			n, err := watcher.Next()
			if err != nil {
				// Stream errored mid-flight. Close and reconnect — but the
				// reconnect MUST back off (see busBackoffMin/Max): tailscaled
				// can drop the stream the instant after accepting it, and an
				// unthrottled retry here spins the CPU and exhausts memory.
				// Don't `defer` the Close — the outer loop never exits, so
				// deferred closes would pile up forever.
				watcher.Close()
				break
			}
			t.processNetMap(n.NetMap)
		}

		// A watcher that stayed up comfortably longer than the cap is healthy,
		// so reset the backoff; rapid flapping keeps escalating it toward the cap.
		if time.Since(connectedAt) > busBackoffMax {
			backoff = busBackoffMin
		}
		log.Warningf("Tailscale event bus disconnected after %s; reconnecting in %s", time.Since(connectedAt).Round(time.Millisecond), backoff)
		busReconnectsTotal.WithLabelValues(t.zone).Inc()
		time.Sleep(backoff)
		backoff = nextBusBackoff(backoff)
	}
}

func (t *Tailscale) processNetMap(nm *netmap.NetworkMap) {
	if nm == nil {
		return
	}

	log.Debugf("Self tags: %+v", nm.SelfNode.Tags().AsSlice())
	nodes := []tailcfg.NodeView{nm.SelfNode}
	nodes = append(nodes, nm.Peers...)

	entries := map[string]map[string][]string{}
	for _, node := range nodes {
		if node.IsWireGuardOnly() {
			// IsWireGuardOnly identifies a node as a Mullvad exit node.
			continue
		}
		if !node.Sharer().IsZero() {
			// Skip shared nodes, since they don't necessarily have unique hostnames within this tailnet.
			// TODO: possibly make it configurable to include shared nodes and figure out what hostname to use.
			continue
		}

		hostname := node.ComputedName()
		entry, ok := entries[hostname]
		if !ok {
			entry = map[string][]string{}
		}

		// Currently entry["A"/"AAAA"] will have max one element
		for _, pfx := range node.Addresses().AsSlice() {

			addr := pfx.Addr()
			if addr.Is4() {
				entry["A"] = append(entry["A"], addr.String())
			} else if addr.Is6() {
				entry["AAAA"] = append(entry["AAAA"], addr.String())
			}
		}

		// Process Tags looking for cname- prefixed ones
		if node.Tags().Len() > 0 {
			for _, raw := range node.Tags().AsSlice() {
				if tag, ok := strings.CutPrefix(raw, "tag:cname-"); ok {
					if _, ok := entries[tag]; !ok {
						entries[tag] = map[string][]string{}
					}
					entries[tag]["CNAME"] = append(entries[tag]["CNAME"], fmt.Sprintf("%s.%s.", hostname, t.zone))
				}
			}
		}

		entries[hostname] = entry
	}

	t.mu.Lock()
	t.entries = entries
	t.mu.Unlock()

	entriesGauge.WithLabelValues(t.zone).Set(float64(len(entries)))
	netmapUpdatesTotal.WithLabelValues(t.zone).Inc()
	log.Debugf("updated %d Tailscale entries", len(entries))
}
